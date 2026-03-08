//go:build windows

package storage

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteSecretStore_SaveLoadDelete(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "kdrive.db")
	store, err := NewSQLiteSecretStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteSecretStore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.db.Close()
	})

	key := "account/acc-1/access-key"
	plaintext := []byte("super-secret-value")

	if err := store.Save(context.Background(), key, plaintext); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	storedCiphertext := []byte{}
	if err := store.db.QueryRowContext(context.Background(), `SELECT ciphertext FROM secrets WHERE key = ?`, key).Scan(&storedCiphertext); err != nil {
		t.Fatalf("query ciphertext error = %v", err)
	}
	if bytes.Equal(storedCiphertext, plaintext) {
		t.Fatalf("ciphertext unexpectedly equals plaintext")
	}

	loaded, err := store.Load(context.Background(), key)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !bytes.Equal(loaded, plaintext) {
		t.Fatalf("Load() value mismatch = %q, want %q", loaded, plaintext)
	}

	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := store.Load(context.Background(), key); err != ErrSecretNotFound {
		t.Fatalf("Load(deleted) error = %v, want %v", err, ErrSecretNotFound)
	}
}
