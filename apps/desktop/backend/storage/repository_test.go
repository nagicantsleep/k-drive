package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteAccountRepository_SaveList(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "kdrive.db")
	repo, err := NewSQLiteAccountRepository(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteAccountRepository() error = %v", err)
	}
	t.Cleanup(func() {
		_ = repo.db.Close()
	})

	account := Account{
		ID:       "acc-1",
		Provider: "s3",
		Email:    "user@example.com",
		Options: map[string]string{
			"region": "ap-northeast-1",
			"bucket": "team-bucket",
		},
	}
	if err := repo.Save(context.Background(), account); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reopenedRepo, err := NewSQLiteAccountRepository(dbPath)
	if err != nil {
		t.Fatalf("reopen NewSQLiteAccountRepository() error = %v", err)
	}
	t.Cleanup(func() {
		_ = reopenedRepo.db.Close()
	})

	accounts, err := reopenedRepo.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(accounts) != 1 {
		t.Fatalf("List() len = %d, want 1", len(accounts))
	}

	if accounts[0].ID != account.ID || accounts[0].Provider != account.Provider || accounts[0].Email != account.Email {
		t.Fatalf("List() account mismatch = %+v", accounts[0])
	}

	if accounts[0].Options["region"] != "ap-northeast-1" || accounts[0].Options["bucket"] != "team-bucket" {
		t.Fatalf("List() options mismatch = %+v", accounts[0].Options)
	}
}

func TestSQLiteMountStateRepository_UpsertGet(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "kdrive.db")
	repo, err := NewSQLiteMountStateRepository(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteMountStateRepository() error = %v", err)
	}
	t.Cleanup(func() {
		_ = repo.db.Close()
	})

	if _, err := repo.Get(context.Background(), "missing"); err != ErrMountStateNotFound {
		t.Fatalf("Get(missing) error = %v, want %v", err, ErrMountStateNotFound)
	}

	firstTime := time.Now().UTC().Round(time.Second)
	if err := repo.Upsert(context.Background(), MountState{
		AccountID: "acc-1",
		State:     "mounted",
		LastError: "",
		UpdatedAt: firstTime,
	}); err != nil {
		t.Fatalf("Upsert(initial) error = %v", err)
	}

	state, err := repo.Get(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("Get(initial) error = %v", err)
	}
	if state.State != "mounted" || state.LastError != "" || !state.UpdatedAt.Equal(firstTime) {
		t.Fatalf("Get(initial) state mismatch = %+v", state)
	}

	secondTime := firstTime.Add(2 * time.Minute)
	if err := repo.Upsert(context.Background(), MountState{
		AccountID: "acc-1",
		State:     "failed",
		LastError: "rclone exited",
		UpdatedAt: secondTime,
	}); err != nil {
		t.Fatalf("Upsert(update) error = %v", err)
	}

	updatedState, err := repo.Get(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("Get(updated) error = %v", err)
	}
	if updatedState.State != "failed" || updatedState.LastError != "rclone exited" || !updatedState.UpdatedAt.Equal(secondTime) {
		t.Fatalf("Get(updated) state mismatch = %+v", updatedState)
	}
}
