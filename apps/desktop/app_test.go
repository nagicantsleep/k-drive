package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

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
	writtenConfigs []connectors.RemoteConfig
	mountedIDs     []string
	statuses       map[string]mount.State
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
			return mount.Status{AccountID: accountID, State: s}, nil
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
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestApp(db *sql.DB, secretStore storage.SecretStore, mountMgr mount.Manager) *App {
	registry := connectors.NewRegistry()
	registry.Register(connectors.NewS3Connector())

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

func TestCreateS3Account_SecretSplitting(t *testing.T) {
	t.Parallel()

	stub := newStubSecretStore()
	db := openAppTestDB(t)
	a := newTestApp(db, stub, &stubMountManager{})

	view, err := a.CreateS3Account(CreateS3AccountRequest{
		AccountID: "acc-test",
		Email:     "user@example.com",
		Options: map[string]string{
			"endpoint":          "https://s3.example.com",
			"region":            "us-east-1",
			"access_key_id":     "AKID123",
			"secret_access_key": "SekRet456",
		},
	})
	if err != nil {
		t.Fatalf("CreateS3Account() error = %v", err)
	}
	if view.ID != "acc-test" || view.Email != "user@example.com" {
		t.Fatalf("CreateS3Account() view mismatch = %+v", view)
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
