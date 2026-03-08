package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrSecretNotFound = errors.New("secret not found")

type SecretStore interface {
	Save(ctx context.Context, key string, plaintext []byte) error
	Load(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

type SQLiteSecretStore struct {
	db *sql.DB
}

func NewSecretStore() *SQLiteSecretStore {
	store, err := NewSQLiteSecretStore(defaultDBPath())
	if err != nil {
		panic(err)
	}
	return store
}

func NewSQLiteSecretStore(dbPath string) (*SQLiteSecretStore, error) {
	db, err := openDatabase(dbPath)
	if err != nil {
		return nil, err
	}

	return &SQLiteSecretStore{db: db}, nil
}

func (s *SQLiteSecretStore) Save(ctx context.Context, key string, plaintext []byte) error {
	ciphertext, err := protectData(plaintext)
	if err != nil {
		return fmt.Errorf("protect secret: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO secrets (key, ciphertext) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET ciphertext = excluded.ciphertext`,
		key,
		ciphertext,
	)
	if err != nil {
		return fmt.Errorf("save secret: %w", err)
	}

	return nil
}

func (s *SQLiteSecretStore) Load(ctx context.Context, key string) ([]byte, error) {
	row := s.db.QueryRowContext(ctx, `SELECT ciphertext FROM secrets WHERE key = ?`, key)

	var ciphertext []byte
	if err := row.Scan(&ciphertext); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSecretNotFound
		}
		return nil, fmt.Errorf("load secret: %w", err)
	}

	plaintext, err := unprotectData(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("unprotect secret: %w", err)
	}

	return plaintext, nil
}

func (s *SQLiteSecretStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}

	return nil
}
