package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const defaultDBFileName = "kdrive.db"

var ErrMountStateNotFound = errors.New("mount state not found")

type Account struct {
	ID       string
	Provider string
	Email    string
	Options  map[string]string
}

type MountState struct {
	AccountID string
	State     string
	LastError string
	UpdatedAt time.Time
}

type AccountRepository interface {
	Save(ctx context.Context, account Account) error
	List(ctx context.Context) ([]Account, error)
}

type MountStateRepository interface {
	Upsert(ctx context.Context, state MountState) error
	Get(ctx context.Context, accountID string) (MountState, error)
}

type SQLiteAccountRepository struct {
	db *sql.DB
}

type SQLiteMountStateRepository struct {
	db *sql.DB
}

func NewAccountRepository() *SQLiteAccountRepository {
	repo, err := NewSQLiteAccountRepository(defaultDBPath())
	if err != nil {
		panic(err)
	}
	return repo
}

func NewMountStateRepository() *SQLiteMountStateRepository {
	repo, err := NewSQLiteMountStateRepository(defaultDBPath())
	if err != nil {
		panic(err)
	}
	return repo
}

func NewSQLiteAccountRepository(dbPath string) (*SQLiteAccountRepository, error) {
	db, err := openDatabase(dbPath)
	if err != nil {
		return nil, err
	}
	return &SQLiteAccountRepository{db: db}, nil
}

func NewSQLiteMountStateRepository(dbPath string) (*SQLiteMountStateRepository, error) {
	db, err := openDatabase(dbPath)
	if err != nil {
		return nil, err
	}
	return &SQLiteMountStateRepository{db: db}, nil
}

func (r *SQLiteAccountRepository) Save(ctx context.Context, account Account) error {
	optionsJSON, err := json.Marshal(account.Options)
	if err != nil {
		return fmt.Errorf("marshal account options: %w", err)
	}

	_, err = r.db.ExecContext(
		ctx,
		`INSERT INTO accounts (id, provider, email, options_json) VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET provider = excluded.provider, email = excluded.email, options_json = excluded.options_json`,
		account.ID,
		account.Provider,
		account.Email,
		string(optionsJSON),
	)
	if err != nil {
		return fmt.Errorf("save account: %w", err)
	}

	return nil
}

func (r *SQLiteAccountRepository) List(ctx context.Context) ([]Account, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, provider, email, options_json FROM accounts ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	accounts := make([]Account, 0)
	for rows.Next() {
		var account Account
		var optionsJSON string
		if err := rows.Scan(&account.ID, &account.Provider, &account.Email, &optionsJSON); err != nil {
			return nil, fmt.Errorf("scan account row: %w", err)
		}

		account.Options = make(map[string]string)
		if err := json.Unmarshal([]byte(optionsJSON), &account.Options); err != nil {
			return nil, fmt.Errorf("unmarshal account options: %w", err)
		}

		accounts = append(accounts, account)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account rows: %w", err)
	}

	return accounts, nil
}

func (r *SQLiteMountStateRepository) Upsert(ctx context.Context, state MountState) error {
	updatedAt := state.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO mount_states (account_id, state, last_error, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET state = excluded.state, last_error = excluded.last_error, updated_at = excluded.updated_at`,
		state.AccountID,
		state.State,
		state.LastError,
		updatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert mount state: %w", err)
	}

	return nil
}

func (r *SQLiteMountStateRepository) Get(ctx context.Context, accountID string) (MountState, error) {
	row := r.db.QueryRowContext(ctx, `SELECT account_id, state, last_error, updated_at FROM mount_states WHERE account_id = ?`, accountID)

	var state MountState
	var updatedAtRaw string
	if err := row.Scan(&state.AccountID, &state.State, &state.LastError, &updatedAtRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MountState{}, ErrMountStateNotFound
		}
		return MountState{}, fmt.Errorf("get mount state: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return MountState{}, fmt.Errorf("parse mount state updated_at: %w", err)
	}
	state.UpdatedAt = updatedAt

	return state, nil
}

func defaultDBPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return defaultDBFileName
	}
	return filepath.Join(configDir, "KDrive", defaultDBFileName)
}

func openDatabase(dbPath string) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if err := createSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func createSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS accounts (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			email TEXT NOT NULL,
			options_json TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS mount_states (
			account_id TEXT PRIMARY KEY,
			state TEXT NOT NULL,
			last_error TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS secrets (
			key TEXT PRIMARY KEY,
			ciphertext BLOB NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("create sqlite schema: %w", err)
	}

	return nil
}
