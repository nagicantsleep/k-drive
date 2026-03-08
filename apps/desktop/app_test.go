package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

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
