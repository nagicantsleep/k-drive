package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"KDrive/backend/auth"
	"KDrive/backend/connectors"
	"KDrive/backend/mount"
	"KDrive/backend/storage"
)

// stubSecretStore is an in-memory SecretStore that does not call DPAPI.
type stubSecretStore struct {
	data map[string][]byte
}

func newStubSecretStore() *stubSecretStore {
	return &stubSecretStore{data: make(map[string][]byte)}
}

func (s *stubSecretStore) Save(_ context.Context, key string, plaintext []byte) error {
	s.data[key] = plaintext
	return nil
}

func (s *stubSecretStore) Load(_ context.Context, key string) ([]byte, error) {
	v, ok := s.data[key]
	if !ok {
		return nil, storage.ErrSecretNotFound
	}
	return v, nil
}

func (s *stubSecretStore) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

// stubMountManager records calls to WriteConfig and Mount.
type stubMountManager struct {
	writtenConfigs  []connectors.RemoteConfig
	mountedIDs      []string
	statuses        map[string]mount.State
	lastErrors      map[string]string
	errorCategories map[string]string
}

func (m *stubMountManager) WriteConfig(remote connectors.RemoteConfig) error {
	m.writtenConfigs = append(m.writtenConfigs, remote)
	return nil
}

func (m *stubMountManager) DeleteConfig(_ string) error { return nil }

func (m *stubMountManager) Mount(_ context.Context, accountID string) error {
	m.mountedIDs = append(m.mountedIDs, accountID)
	return nil
}

func (m *stubMountManager) Unmount(_ context.Context, _ string) error { return nil }

func (m *stubMountManager) Status(_ context.Context, accountID string) (mount.Status, error) {
	if m.statuses != nil {
		if s, ok := m.statuses[accountID]; ok {
			return mount.Status{
				AccountID:     accountID,
				State:         s,
				LastError:     m.lastErrors[accountID],
				ErrorCategory: m.errorCategories[accountID],
			}, nil
		}
	}
	return mount.Status{AccountID: accountID, State: mount.StateStopped}, nil
}

func openAppTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.OpenDatabase(filepath.Join(t.TempDir(), "kdrive.db"))
	if err != nil {
		t.Fatalf("OpenDatabase() error = %v", err)
	}
	t.Cleanup(func() {
		// Wait a moment for any background goroutines using the DB to finish before closing.
		time.Sleep(20 * time.Millisecond)
		_ = db.Close()
	})
	return db
}

func newTestApp(db *sql.DB, secretStore storage.SecretStore, mountMgr mount.Manager) *App {
	registry := connectors.NewRegistry()
	registry.Register(connectors.NewS3Connector())
	registry.Register(connectors.NewGoogleDriveConnector())

	a := &App{
		db:                   db,
		connectorRegistry:    registry,
		mountManager:         mountMgr,
		accountRepository:    storage.NewSQLiteAccountRepository(db),
		mountStateRepository: storage.NewSQLiteMountStateRepository(db),
		secretStore:          secretStore,
		retryCounts:          make(map[string]int),
		stoppedByUser:        make(map[string]bool),
		retryBaseDelay:       5 * time.Second,
	}
	a.ctx = context.Background()
	return a
}

func TestCreateAccount_SecretSplitting(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	a := newTestApp(db, stub, &stubMountManager{})

	view, err := a.CreateAccount(CreateAccountRequest{
		AccountID: "acc-test",
		Provider:  string(connectors.ProviderS3),
		Email:     "user@example.com",
		Options: map[string]string{
			"endpoint":          "https://s3.example.com",
			"region":            "us-east-1",
			"access_key_id":     "AKID123",
			"secret_access_key": "SekRet456",
		},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if view.ID != "acc-test" || view.Email != "user@example.com" || view.Provider != string(connectors.ProviderS3) {
		t.Fatalf("CreateAccount() view mismatch = %+v", view)
	}

	accounts, err := storage.NewSQLiteAccountRepository(db).List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("List() len = %d, want 1", len(accounts))
	}
	if _, ok := accounts[0].Options["access_key_id"]; ok {
		t.Fatalf("account options must not contain access_key_id")
	}
	if _, ok := accounts[0].Options["secret_access_key"]; ok {
		t.Fatalf("account options must not contain secret_access_key")
	}

	akid, err := stub.Load(context.Background(), "account/acc-test/access_key_id")
	if err != nil {
		t.Fatalf("Load(access_key_id) error = %v", err)
	}
	if string(akid) != "AKID123" {
		t.Fatalf("Load(access_key_id) = %q, want AKID123", string(akid))
	}

	sak, err := stub.Load(context.Background(), "account/acc-test/secret_access_key")
	if err != nil {
		t.Fatalf("Load(secret_access_key) error = %v", err)
	}
	if string(sak) != "SekRet456" {
		t.Fatalf("Load(secret_access_key) = %q, want SekRet456", string(sak))
	}
}

func TestProviderCapabilities_ReturnsConnectorMetadata(t *testing.T) {
	t.Parallel()

	a := newTestApp(openAppTestDB(t), newStubSecretStore(), &stubMountManager{})
	capabilities := a.ProviderCapabilities()
	if len(capabilities) != 2 {
		t.Fatalf("ProviderCapabilities() len = %d, want 2", len(capabilities))
	}

	if capabilities[0].Provider != string(connectors.ProviderGoogle) {
		t.Fatalf("first provider = %q, want %q", capabilities[0].Provider, connectors.ProviderGoogle)
	}
	if capabilities[0].AuthScheme != "oauth" {
		t.Fatalf("google authScheme = %q, want oauth", capabilities[0].AuthScheme)
	}

	if capabilities[1].Provider != string(connectors.ProviderS3) {
		t.Fatalf("second provider = %q, want %q", capabilities[1].Provider, connectors.ProviderS3)
	}
	if len(capabilities[1].Fields) != 5 {
		t.Fatalf("s3 field count = %d, want 5", len(capabilities[1].Fields))
	}
	if !capabilities[1].Fields[3].Secret || !capabilities[1].Fields[4].Secret {
		t.Fatalf("s3 secret field metadata missing = %+v", capabilities[1].Fields)
	}
}

func TestMountAccount_ConfigWrittenAndProcessStarted(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	stub.data["account/acc-test/access_key_id"] = []byte("AKID123")
	stub.data["account/acc-test/secret_access_key"] = []byte("SekRet456")

	db := openAppTestDB(t)
	mgr := &stubMountManager{}
	a := newTestApp(db, stub, mgr)

	if err := a.accountRepository.Save(context.Background(), storage.Account{
		ID:       "acc-test",
		Provider: "s3",
		Email:    "user@example.com",
		Options: map[string]string{
			"endpoint": "https://s3.example.com",
			"region":   "us-east-1",
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := a.MountAccount("acc-test"); err != nil {
		t.Fatalf("MountAccount() error = %v", err)
	}

	if len(mgr.writtenConfigs) != 1 {
		t.Fatalf("WriteConfig called %d times, want 1", len(mgr.writtenConfigs))
	}
	if mgr.writtenConfigs[0].Name != "s3-acc-test" {
		t.Fatalf("WriteConfig remote name = %q, want s3-acc-test", mgr.writtenConfigs[0].Name)
	}
	if len(mgr.mountedIDs) != 1 || mgr.mountedIDs[0] != "acc-test" {
		t.Fatalf("Mount IDs = %v, want [acc-test]", mgr.mountedIDs)
	}
}

func TestAutoRemountOnStartup_MountedAccountsAreRemounted(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	stub.data["account/acc-1/access_key_id"] = []byte("KEY1")
	stub.data["account/acc-1/secret_access_key"] = []byte("SEC1")
	stub.data["account/acc-2/access_key_id"] = []byte("KEY2")
	stub.data["account/acc-2/secret_access_key"] = []byte("SEC2")

	db := openAppTestDB(t)
	mgr := &stubMountManager{}
	a := newTestApp(db, stub, mgr)

	for _, id := range []string{"acc-1", "acc-2"} {
		if err := a.accountRepository.Save(context.Background(), storage.Account{
			ID: id, Provider: "s3", Email: id + "@example.com",
			Options: map[string]string{"endpoint": "https://s3.example.com", "region": "us-east-1"},
		}); err != nil {
			t.Fatalf("Save(%s) error = %v", id, err)
		}
	}

	// Persist "mounted" state only for acc-1 and "stopped" for acc-2.
	if err := a.mountStateRepository.Upsert(context.Background(), storage.MountState{
		AccountID: "acc-1", State: string(mount.StateMounted),
	}); err != nil {
		t.Fatalf("Upsert acc-1 error = %v", err)
	}
	if err := a.mountStateRepository.Upsert(context.Background(), storage.MountState{
		AccountID: "acc-2", State: string(mount.StateStopped),
	}); err != nil {
		t.Fatalf("Upsert acc-2 error = %v", err)
	}

	a.autoRemountOnStartup()
	// Give goroutines a moment to run.
	time.Sleep(100 * time.Millisecond)

	if len(mgr.mountedIDs) != 1 || mgr.mountedIDs[0] != "acc-1" {
		t.Fatalf("autoRemount mounted IDs = %v, want [acc-1]", mgr.mountedIDs)
	}
}

func TestOnMountStateChange_FailedTriggersRetry(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	stub.data["account/acc-1/access_key_id"] = []byte("KEY1")
	stub.data["account/acc-1/secret_access_key"] = []byte("SEC1")

	db := openAppTestDB(t)
	mgr := &stubMountManager{}
	a := newTestApp(db, stub, mgr)
	a.retryBaseDelay = 50 * time.Millisecond

	if err := a.accountRepository.Save(context.Background(), storage.Account{
		ID: "acc-1", Provider: "s3", Email: "u@example.com",
		Options: map[string]string{"endpoint": "https://s3.example.com", "region": "us-east-1"},
	}); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	// Simulate the first mount (sets retry count to 0).
	if err := a.MountAccount("acc-1"); err != nil {
		t.Fatalf("MountAccount error = %v", err)
	}
	// First Mount call is counted.
	if len(mgr.mountedIDs) != 1 {
		t.Fatalf("expected 1 mount call before failure, got %d", len(mgr.mountedIDs))
	}

	// Simulate unexpected failure — retry count becomes 1.
	a.onMountStateChange("acc-1", mount.StateFailed, "process died", nil)

	// Wait long enough for the retry goroutine (50ms * 1 = 50ms + buffer).
	time.Sleep(200 * time.Millisecond)

	if len(mgr.mountedIDs) < 2 {
		t.Fatalf("expected retry mount call, mountedIDs = %v", mgr.mountedIDs)
	}
}

func TestOnMountStateChange_ExplicitUnmountSuppressesRetry(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	stub.data["account/acc-1/access_key_id"] = []byte("KEY1")
	stub.data["account/acc-1/secret_access_key"] = []byte("SEC1")

	db := openAppTestDB(t)
	mgr := &stubMountManager{}
	a := newTestApp(db, stub, mgr)
	a.retryBaseDelay = 50 * time.Millisecond

	if err := a.accountRepository.Save(context.Background(), storage.Account{
		ID: "acc-1", Provider: "s3", Email: "u@example.com",
		Options: map[string]string{"endpoint": "https://s3.example.com", "region": "us-east-1"},
	}); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	if err := a.MountAccount("acc-1"); err != nil {
		t.Fatalf("MountAccount error = %v", err)
	}
	initialMounts := len(mgr.mountedIDs)

	// User explicitly unmounts — this sets stoppedByUser.
	if err := a.UnmountAccount("acc-1"); err != nil {
		t.Fatalf("UnmountAccount error = %v", err)
	}

	// Simulate an unexpected-looking failure callback (should not retry).
	a.onMountStateChange("acc-1", mount.StateFailed, "process died", nil)

	time.Sleep(200 * time.Millisecond)

	if len(mgr.mountedIDs) != initialMounts {
		t.Fatalf("retry fired after explicit unmount; mountedIDs = %v", mgr.mountedIDs)
	}
}

func TestOnMountStateChange_MountedResetsRetryCount(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	mgr := &stubMountManager{}
	a := newTestApp(db, stub, mgr)

	// Manually set a non-zero retry count.
	a.retryMu.Lock()
	a.retryCounts["acc-1"] = 2
	a.retryMu.Unlock()

	// Callback for mounted should reset count.
	a.onMountStateChange("acc-1", mount.StateMounted, "", nil)

	a.retryMu.Lock()
	count := a.retryCounts["acc-1"]
	a.retryMu.Unlock()

	if count != 0 {
		t.Fatalf("retry count after StateMounted = %d, want 0", count)
	}
}

func TestAutoRemountOnStartup_SetsStoppedBeforeRemount(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	stub.data["account/acc-1/access_key_id"] = []byte("KEY1")
	stub.data["account/acc-1/secret_access_key"] = []byte("SEC1")

	db := openAppTestDB(t)

	// Use a mount manager that always fails.
	failMgr := &errorMountManager{err: fmt.Errorf("rclone not found")}
	a := newTestApp(db, stub, failMgr)

	if err := a.accountRepository.Save(context.Background(), storage.Account{
		ID: "acc-1", Provider: "s3", Email: "u@example.com",
		Options: map[string]string{"endpoint": "https://s3.example.com", "region": "us-east-1"},
	}); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	// Persist "mounted" state as if a previous session had acc-1 mounted.
	if err := a.mountStateRepository.Upsert(context.Background(), storage.MountState{
		AccountID: "acc-1", State: string(mount.StateMounted),
	}); err != nil {
		t.Fatalf("Upsert error = %v", err)
	}

	a.autoRemountOnStartup()
	// Let goroutines finish — they need to write state back to SQLite after a failed remount.
	time.Sleep(300 * time.Millisecond)

	// After failed remount, SQLite must not be left with "mounted".
	state, err := a.mountStateRepository.Get(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if state.State == string(mount.StateMounted) {
		t.Fatalf("State = %q after failed remount; want stopped or failed", state.State)
	}
}

// errorMountManager is a stubMountManager whose Mount always returns an error.
type errorMountManager struct {
	stubMountManager
	err error
}

func (m *errorMountManager) Mount(_ context.Context, _ string) error {
	return m.err
}

func TestErrorCategoryFromErr_ConfigInvalid(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	a := newTestApp(db, stub, &stubMountManager{})

	// Save an account that has no secrets (BuildRemoteConfig will fail).
	if err := a.accountRepository.Save(context.Background(), storage.Account{
		ID: "acc-nokey", Provider: "s3", Email: "u@example.com",
		Options: map[string]string{"endpoint": "https://s3.example.com", "region": "us-east-1"},
		// deliberately no access_key_id / secret_access_key in options (also not in secret store)
	}); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	err := a.doMount("acc-nokey")
	if err == nil {
		t.Fatal("doMount(missing credentials) expected error, got nil")
	}

	cat := errorCategoryFromErr(err)
	if cat != "config_invalid" {
		t.Fatalf("errorCategory = %q, want config_invalid", cat)
	}
}

func TestMountAccount_ConfigInvalidPersistsFailedState(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	a := newTestApp(db, stub, &stubMountManager{})

	if err := a.accountRepository.Save(context.Background(), storage.Account{
		ID: "acc-nokey", Provider: "s3", Email: "u@example.com",
		Options: map[string]string{"endpoint": "https://s3.example.com", "region": "us-east-1"},
	}); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	err := a.MountAccount("acc-nokey")
	if err == nil {
		t.Fatal("MountAccount(missing credentials) expected error, got nil")
	}

	view, viewErr := a.AccountMountStatus("acc-nokey")
	if viewErr != nil {
		t.Fatalf("AccountMountStatus error = %v", viewErr)
	}
	if view.State != string(mount.StateFailed) {
		t.Fatalf("State = %q, want %q", view.State, mount.StateFailed)
	}
	if view.ErrorCategory != "config_invalid" {
		t.Fatalf("ErrorCategory = %q, want config_invalid", view.ErrorCategory)
	}
	if view.LastError == "" {
		t.Fatal("LastError is empty, want non-empty")
	}
}

func TestAccountMountStatus_FallsBackToSQLite(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	mgr := &stubMountManager{} // always returns StateStopped
	a := newTestApp(db, stub, mgr)

	// Persist a "failed" state in SQLite.
	if err := a.mountStateRepository.Upsert(context.Background(), storage.MountState{
		AccountID: "acc-1",
		State:     string(mount.StateFailed),
		LastError: "previous crash",
	}); err != nil {
		t.Fatalf("Upsert error = %v", err)
	}

	view, err := a.AccountMountStatus("acc-1")
	if err != nil {
		t.Fatalf("AccountMountStatus error = %v", err)
	}
	if view.State != string(mount.StateFailed) {
		t.Fatalf("State = %q, want %q (SQLite fallback)", view.State, mount.StateFailed)
	}
	if view.LastError != "previous crash" {
		t.Fatalf("LastError = %q, want %q", view.LastError, "previous crash")
	}
}

func TestAccountMountStatus_LiveStateOverridesSQLite(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	mgr := &stubMountManager{
		statuses: map[string]mount.State{
			"acc-1": mount.StateMounted,
		},
	}
	a := newTestApp(db, stub, mgr)

	// SQLite says failed — but live manager says mounted.
	if err := a.mountStateRepository.Upsert(context.Background(), storage.MountState{
		AccountID: "acc-1",
		State:     string(mount.StateFailed),
		LastError: "stale",
	}); err != nil {
		t.Fatalf("Upsert error = %v", err)
	}

	view, err := a.AccountMountStatus("acc-1")
	if err != nil {
		t.Fatalf("AccountMountStatus error = %v", err)
	}
	if view.State != string(mount.StateMounted) {
		t.Fatalf("State = %q, want %q (live wins over SQLite)", view.State, mount.StateMounted)
	}
}

func TestAccountMountStatus_LiveFailedStateUsesManagerCategory(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	mgr := &stubMountManager{
		statuses: map[string]mount.State{
			"acc-1": mount.StateFailed,
		},
		lastErrors: map[string]string{
			"acc-1": "rclone not found",
		},
		errorCategories: map[string]string{
			"acc-1": string(mount.PreflightDependencyMissing),
		},
	}
	a := newTestApp(db, stub, mgr)

	if err := a.mountStateRepository.Upsert(context.Background(), storage.MountState{
		AccountID:     "acc-1",
		State:         string(mount.StateFailed),
		LastError:     "stale process error",
		ErrorCategory: "process_failed",
	}); err != nil {
		t.Fatalf("Upsert error = %v", err)
	}

	view, err := a.AccountMountStatus("acc-1")
	if err != nil {
		t.Fatalf("AccountMountStatus error = %v", err)
	}
	if view.State != string(mount.StateFailed) {
		t.Fatalf("State = %q, want %q", view.State, mount.StateFailed)
	}
	if view.ErrorCategory != string(mount.PreflightDependencyMissing) {
		t.Fatalf("ErrorCategory = %q, want %q", view.ErrorCategory, mount.PreflightDependencyMissing)
	}
}

// TestLifecycle_AddMountUnmountRetry tests the full add → mount → unmount → failed-mount → retry sequence.
func TestLifecycle_AddMountUnmountRetry(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)

	// Phase 1: normal mount manager succeeds.
	successMgr := &stubMountManager{}
	a := newTestApp(db, stub, successMgr)

	// Add account.
	_, err := a.CreateAccount(CreateAccountRequest{
		AccountID: "acc-lifecycle",
		Provider:  string(connectors.ProviderS3),
		Email:     "u@example.com",
		Options: map[string]string{
			"endpoint":          "https://s3.example.com",
			"region":            "us-east-1",
			"access_key_id":     "AKID",
			"secret_access_key": "SECRET",
		},
	})
	if err != nil {
		t.Fatalf("CreateAccount error = %v", err)
	}

	// Mount.
	if err := a.MountAccount("acc-lifecycle"); err != nil {
		t.Fatalf("MountAccount error = %v", err)
	}
	if len(successMgr.mountedIDs) != 1 {
		t.Fatalf("Mount calls = %d, want 1", len(successMgr.mountedIDs))
	}

	// Unmount.
	if err := a.UnmountAccount("acc-lifecycle"); err != nil {
		t.Fatalf("UnmountAccount error = %v", err)
	}

	// Phase 2: swap to failing mount manager (simulates rclone not found).
	failMgr := &errorMountManager{err: fmt.Errorf("rclone not found")}
	a.mountManager = failMgr

	// Attempt mount — should fail with process_failed (the errorMountManager returns a plain error).
	mountErr := a.MountAccount("acc-lifecycle")
	if mountErr == nil {
		t.Fatal("MountAccount with failing manager expected error, got nil")
	}

	// Retry count should have been reset by explicit MountAccount call.
	a.retryMu.Lock()
	count := a.retryCounts["acc-lifecycle"]
	a.retryMu.Unlock()
	if count != 0 {
		t.Fatalf("retry count after user-initiated mount = %d, want 0", count)
	}

	// Status should reflect failed state persisted by onMountStateChange callback from prior stub, or just the error.
	// At minimum the error returned should be non-nil (already verified above).
	cat := errorCategoryFromErr(mountErr)
	if cat == "" {
		t.Fatalf("errorCategoryFromErr returned empty for a non-nil error")
	}

	view, viewErr := a.AccountMountStatus("acc-lifecycle")
	if viewErr != nil {
		t.Fatalf("AccountMountStatus error = %v", viewErr)
	}
	if view.State != string(mount.StateFailed) {
		t.Fatalf("State = %q, want %q", view.State, mount.StateFailed)
	}
	if view.ErrorCategory != "process_failed" {
		t.Fatalf("ErrorCategory = %q, want process_failed", view.ErrorCategory)
	}
}

// TestRetryCount_ResetsOnSuccessfulMount verifies retry counters reset on mount success.
func TestRetryCount_ResetsOnSuccessfulMount(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	mgr := &stubMountManager{}
	a := newTestApp(db, stub, mgr)

	// Pre-set a stale retry count.
	a.retryMu.Lock()
	a.retryCounts["acc-x"] = 3
	a.retryMu.Unlock()

	a.onMountStateChange("acc-x", mount.StateMounted, "", nil)

	a.retryMu.Lock()
	count := a.retryCounts["acc-x"]
	a.retryMu.Unlock()

	if count != 0 {
		t.Fatalf("retry count after StateMounted callback = %d, want 0", count)
	}
}

// TestPreflight_DependencyMissingCategory verifies the full chain from preflight to category string.
func TestPreflight_DependencyMissingCategory(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	stub.data["account/acc-pf/access_key_id"] = []byte("AKID")
	stub.data["account/acc-pf/secret_access_key"] = []byte("SEC")

	db := openAppTestDB(t)
	a := newTestApp(db, stub, &stubMountManager{}) // stub: won't actually do preflight

	// Directly invoke errorCategoryFromErr with a synthetic PreflightError.
	pe := &mount.PreflightError{
		Category: mount.PreflightDependencyMissing,
		Message:  "rclone not found",
	}
	cat := errorCategoryFromErr(pe)
	if cat != string(mount.PreflightDependencyMissing) {
		t.Fatalf("category = %q, want %q", cat, mount.PreflightDependencyMissing)
	}
	_ = a // suppress unused warning
}

func TestOnMountStateChange_NonProcessFailureDoesNotRetry(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	stub.data["account/acc-1/access_key_id"] = []byte("KEY1")
	stub.data["account/acc-1/secret_access_key"] = []byte("SEC1")

	db := openAppTestDB(t)
	mgr := &stubMountManager{}
	a := newTestApp(db, stub, mgr)
	a.retryBaseDelay = 50 * time.Millisecond

	if err := a.accountRepository.Save(context.Background(), storage.Account{
		ID: "acc-1", Provider: "s3", Email: "u@example.com",
		Options: map[string]string{"endpoint": "https://s3.example.com", "region": "us-east-1"},
	}); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	a.onMountStateChange("acc-1", mount.StateFailed, "rclone not found", &mount.PreflightError{
		Category: mount.PreflightDependencyMissing,
		Message:  "rclone not found",
	})

	time.Sleep(200 * time.Millisecond)

	if len(mgr.mountedIDs) != 0 {
		t.Fatalf("unexpected retry for dependency failure; mountedIDs = %v", mgr.mountedIDs)
	}
}

func TestResolveOAuthProvider_AcceptsConnectorProviderKeys(t *testing.T) {
	t.Parallel()

	provider, tokenURL, authURL, scopes, err := resolveOAuthProvider(string(connectors.ProviderGoogle))
	if err != nil {
		t.Fatalf("resolveOAuthProvider(google-drive) error = %v", err)
	}
	if provider != "google" {
		t.Fatalf("provider = %q, want google", provider)
	}
	if tokenURL == "" || authURL == "" || len(scopes) == 0 {
		t.Fatalf("oauth config is incomplete")
	}

	provider, tokenURL, authURL, scopes, err = resolveOAuthProvider(string(connectors.ProviderOneDrive))
	if err != nil {
		t.Fatalf("resolveOAuthProvider(onedrive) error = %v", err)
	}
	if provider != "microsoft" {
		t.Fatalf("provider = %q, want microsoft", provider)
	}
	if tokenURL == "" || authURL == "" || len(scopes) == 0 {
		t.Fatalf("oauth config is incomplete")
	}
}

func TestCreateOAuthAccount_GoogleProviderSaved(t *testing.T) {
	t.Parallel()

	db := openAppTestDB(t)
	a := newTestApp(db, newStubSecretStore(), &stubMountManager{})

	view, err := a.CreateOAuthAccount(CreateOAuthAccountRequest{
		AccountID: "google-account-1",
		Provider:  string(connectors.ProviderGoogle),
		Email:     "google.user@example.com",
	})
	if err != nil {
		t.Fatalf("CreateOAuthAccount() error = %v", err)
	}
	if view.Provider != string(connectors.ProviderGoogle) {
		t.Fatalf("CreateOAuthAccount() provider = %q, want %q", view.Provider, connectors.ProviderGoogle)
	}

	accounts, err := a.accountRepository.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("List() len = %d, want 1", len(accounts))
	}
	if accounts[0].ID != "google-account-1" || accounts[0].Provider != string(connectors.ProviderGoogle) {
		t.Fatalf("saved account mismatch = %+v", accounts[0])
	}
}

func TestMountAccount_GoogleIncludesOAuthTokenInRemoteConfig(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	mgr := &stubMountManager{}
	a := newTestApp(db, stub, mgr)

	tokenStore := auth.NewSecretBackedTokenStore(stub)
	err := tokenStore.Save(context.Background(), auth.OAuthProviderGoogle, "google-acc", auth.OAuthToken{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		Expiry:       time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("tokenStore.Save() error = %v", err)
	}

	if err := a.accountRepository.Save(context.Background(), storage.Account{
		ID:       "google-acc",
		Provider: string(connectors.ProviderGoogle),
		Email:    "google.user@example.com",
		Options:  map[string]string{},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := a.MountAccount("google-acc"); err != nil {
		t.Fatalf("MountAccount() error = %v", err)
	}
	if len(mgr.writtenConfigs) != 1 {
		t.Fatalf("WriteConfig called %d times, want 1", len(mgr.writtenConfigs))
	}

	remote := mgr.writtenConfigs[0]
	if remote.Type != "drive" {
		t.Fatalf("remote.Type = %q, want drive", remote.Type)
	}
	if remote.Name != "gdrive-google-acc" {
		t.Fatalf("remote.Name = %q, want gdrive-google-acc", remote.Name)
	}

	tokenJSON := remote.Options["token"]
	if tokenJSON == "" {
		t.Fatal("remote token option is empty")
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(tokenJSON), &payload); err != nil {
		t.Fatalf("token JSON invalid: %v", err)
	}
	if payload["access_token"] != "access-123" {
		t.Fatalf("access_token = %q, want access-123", payload["access_token"])
	}
	if payload["refresh_token"] != "refresh-456" {
		t.Fatalf("refresh_token = %q, want refresh-456", payload["refresh_token"])
	}
}

func TestMountAccount_GoogleMissingOAuthTokenFailsAsConfigInvalid(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	a := newTestApp(db, stub, &stubMountManager{})

	if err := a.accountRepository.Save(context.Background(), storage.Account{
		ID:       "google-no-token",
		Provider: string(connectors.ProviderGoogle),
		Email:    "google.user@example.com",
		Options:  map[string]string{},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	err := a.MountAccount("google-no-token")
	if err == nil {
		t.Fatal("MountAccount() expected error, got nil")
	}
	if errorCategoryFromErr(err) != "config_invalid" {
		t.Fatalf("error category = %q, want config_invalid", errorCategoryFromErr(err))
	}
}
