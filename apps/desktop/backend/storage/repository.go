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
var ErrSyncStateNotFound = errors.New("sync state not found")

type Account struct {
	ID       string
	Provider string
	Email    string
	Options  map[string]string
}

type MountState struct {
	AccountID     string
	State         string
	LastError     string
	ErrorCategory string
	MountPath     string
	UpdatedAt     time.Time
}

type SyncState string

const (
	SyncStateIdle         SyncState = "idle"
	SyncStateSyncing      SyncState = "syncing"
	SyncStateSuccess      SyncState = "success"
	SyncStateError        SyncState = "error"
	SyncStateConflict     SyncState = "conflict"
	SyncStateNeedsResolve SyncState = "needs_resolve"
	SyncStateRetrying     SyncState = "retrying"
	SyncStateOffline      SyncState = "offline"
)

type SyncStatus struct {
	AccountID       string
	State           SyncState
	LastSyncAt      time.Time
	LastError       string
	ConflictCount   int
	FilesSynced     int
	BytesTransferred int64
	UpdatedAt       time.Time
}

type SyncConflict struct {
	ID         string
	AccountID  string
	FilePath   string
	LocalModTime  time.Time
	RemoteModTime time.Time
	Resolution string
	CreatedAt  time.Time
}

type AccountRepository interface {
	Save(ctx context.Context, account Account) error
	List(ctx context.Context) ([]Account, error)
	Delete(ctx context.Context, accountID string) error
}

type MountStateRepository interface {
	Upsert(ctx context.Context, state MountState) error
	Get(ctx context.Context, accountID string) (MountState, error)
	Delete(ctx context.Context, accountID string) error
}

type SyncStateRepository interface {
	UpsertSyncStatus(ctx context.Context, status SyncStatus) error
	GetSyncStatus(ctx context.Context, accountID string) (SyncStatus, error)
	ListSyncStatuses(ctx context.Context) ([]SyncStatus, error)
}

type SyncConflictRepository interface {
	SaveConflict(ctx context.Context, conflict SyncConflict) error
	ListConflicts(ctx context.Context, accountID string) ([]SyncConflict, error)
	DeleteConflict(ctx context.Context, conflictID string) error
}

type SQLiteAccountRepository struct {
	db *sql.DB
}

type SQLiteMountStateRepository struct {
	db *sql.DB
}

type SQLiteSyncStateRepository struct {
	db *sql.DB
}

type SQLiteSyncConflictRepository struct {
	db *sql.DB
}

func NewSQLiteAccountRepository(db *sql.DB) *SQLiteAccountRepository {
	return &SQLiteAccountRepository{db: db}
}

func NewSQLiteMountStateRepository(db *sql.DB) *SQLiteMountStateRepository {
	return &SQLiteMountStateRepository{db: db}
}

func NewSQLiteSyncStateRepository(db *sql.DB) *SQLiteSyncStateRepository {
	return &SQLiteSyncStateRepository{db: db}
}

func NewSQLiteSyncConflictRepository(db *sql.DB) *SQLiteSyncConflictRepository {
	return &SQLiteSyncConflictRepository{db: db}
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

func (r *SQLiteAccountRepository) Delete(ctx context.Context, accountID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, accountID)
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return nil
}

func (r *SQLiteMountStateRepository) Upsert(ctx context.Context, state MountState) error {
	updatedAt := state.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO mount_states (account_id, state, last_error, error_category, mount_path, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET state = excluded.state, last_error = excluded.last_error, error_category = excluded.error_category, mount_path = excluded.mount_path, updated_at = excluded.updated_at`,
		state.AccountID,
		state.State,
		state.LastError,
		state.ErrorCategory,
		state.MountPath,
		updatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert mount state: %w", err)
	}

	return nil
}

func (r *SQLiteMountStateRepository) Get(ctx context.Context, accountID string) (MountState, error) {
	row := r.db.QueryRowContext(ctx, `SELECT account_id, state, last_error, error_category, COALESCE(mount_path, '') AS mount_path, updated_at FROM mount_states WHERE account_id = ?`, accountID)

	var state MountState
	var updatedAtRaw string
	if err := row.Scan(&state.AccountID, &state.State, &state.LastError, &state.ErrorCategory, &state.MountPath, &updatedAtRaw); err != nil {
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

func (r *SQLiteMountStateRepository) Delete(ctx context.Context, accountID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM mount_states WHERE account_id = ?`, accountID)
	if err != nil {
		return fmt.Errorf("delete mount state: %w", err)
	}
	return nil
}

func (r *SQLiteSyncStateRepository) UpsertSyncStatus(ctx context.Context, status SyncStatus) error {
	updatedAt := status.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	lastSyncAt := status.LastSyncAt
	lastSyncAtStr := ""
	if !lastSyncAt.IsZero() {
		lastSyncAtStr = lastSyncAt.Format(time.RFC3339Nano)
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO sync_states (account_id, state, last_sync_at, last_error, conflict_count, files_synced, bytes_transferred, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(account_id) DO UPDATE SET
		   state = excluded.state,
		   last_sync_at = excluded.last_sync_at,
		   last_error = excluded.last_error,
		   conflict_count = excluded.conflict_count,
		   files_synced = excluded.files_synced,
		   bytes_transferred = excluded.bytes_transferred,
		   updated_at = excluded.updated_at`,
		status.AccountID,
		string(status.State),
		lastSyncAtStr,
		status.LastError,
		status.ConflictCount,
		status.FilesSynced,
		status.BytesTransferred,
		updatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert sync status: %w", err)
	}

	return nil
}

func (r *SQLiteSyncStateRepository) GetSyncStatus(ctx context.Context, accountID string) (SyncStatus, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT account_id, state, last_sync_at, last_error, conflict_count, files_synced, bytes_transferred, updated_at
		 FROM sync_states WHERE account_id = ?`,
		accountID,
	)

	var status SyncStatus
	var lastSyncAtRaw, updatedAtRaw sql.NullString
	if err := row.Scan(
		&status.AccountID,
		&status.State,
		&lastSyncAtRaw,
		&status.LastError,
		&status.ConflictCount,
		&status.FilesSynced,
		&status.BytesTransferred,
		&updatedAtRaw,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SyncStatus{}, ErrSyncStateNotFound
		}
		return SyncStatus{}, fmt.Errorf("get sync status: %w", err)
	}

	if lastSyncAtRaw.Valid && lastSyncAtRaw.String != "" {
		lastSyncAt, err := time.Parse(time.RFC3339Nano, lastSyncAtRaw.String)
		if err != nil {
			return SyncStatus{}, fmt.Errorf("parse last_sync_at: %w", err)
		}
		status.LastSyncAt = lastSyncAt
	}

	if updatedAtRaw.Valid && updatedAtRaw.String != "" {
		updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw.String)
		if err != nil {
			return SyncStatus{}, fmt.Errorf("parse updated_at: %w", err)
		}
		status.UpdatedAt = updatedAt
	}

	return status, nil
}

func (r *SQLiteSyncStateRepository) ListSyncStatuses(ctx context.Context) ([]SyncStatus, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT account_id, state, last_sync_at, last_error, conflict_count, files_synced, bytes_transferred, updated_at
		 FROM sync_states ORDER BY account_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list sync statuses: %w", err)
	}
	defer rows.Close()

	statuses := make([]SyncStatus, 0)
	for rows.Next() {
		var status SyncStatus
		var lastSyncAtRaw, updatedAtRaw sql.NullString
		if err := rows.Scan(
			&status.AccountID,
			&status.State,
			&lastSyncAtRaw,
			&status.LastError,
			&status.ConflictCount,
			&status.FilesSynced,
			&status.BytesTransferred,
			&updatedAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan sync status row: %w", err)
		}

		if lastSyncAtRaw.Valid && lastSyncAtRaw.String != "" {
			lastSyncAt, err := time.Parse(time.RFC3339Nano, lastSyncAtRaw.String)
			if err != nil {
				return nil, fmt.Errorf("parse last_sync_at: %w", err)
			}
			status.LastSyncAt = lastSyncAt
		}

		if updatedAtRaw.Valid && updatedAtRaw.String != "" {
			updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw.String)
			if err != nil {
				return nil, fmt.Errorf("parse updated_at: %w", err)
			}
			status.UpdatedAt = updatedAt
		}

		statuses = append(statuses, status)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync status rows: %w", err)
	}

	return statuses, nil
}

func (r *SQLiteSyncConflictRepository) SaveConflict(ctx context.Context, conflict SyncConflict) error {
	createdAt := conflict.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO sync_conflicts (id, account_id, file_path, local_mod_time, remote_mod_time, resolution, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   file_path = excluded.file_path,
		   local_mod_time = excluded.local_mod_time,
		   remote_mod_time = excluded.remote_mod_time,
		   resolution = excluded.resolution`,
		conflict.ID,
		conflict.AccountID,
		conflict.FilePath,
		conflict.LocalModTime.Format(time.RFC3339Nano),
		conflict.RemoteModTime.Format(time.RFC3339Nano),
		conflict.Resolution,
		createdAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save sync conflict: %w", err)
	}

	return nil
}

func (r *SQLiteSyncConflictRepository) ListConflicts(ctx context.Context, accountID string) ([]SyncConflict, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, account_id, file_path, local_mod_time, remote_mod_time, resolution, created_at
		 FROM sync_conflicts WHERE account_id = ? ORDER BY created_at DESC`,
		accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("list sync conflicts: %w", err)
	}
	defer rows.Close()

	conflicts := make([]SyncConflict, 0)
	for rows.Next() {
		var conflict SyncConflict
		var localModTimeRaw, remoteModTimeRaw, createdAtRaw string
		if err := rows.Scan(
			&conflict.ID,
			&conflict.AccountID,
			&conflict.FilePath,
			&localModTimeRaw,
			&remoteModTimeRaw,
			&conflict.Resolution,
			&createdAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan sync conflict row: %w", err)
		}

		conflict.LocalModTime, _ = time.Parse(time.RFC3339Nano, localModTimeRaw)
		conflict.RemoteModTime, _ = time.Parse(time.RFC3339Nano, remoteModTimeRaw)
		conflict.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtRaw)

		conflicts = append(conflicts, conflict)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync conflict rows: %w", err)
	}

	return conflicts, nil
}

func (r *SQLiteSyncConflictRepository) DeleteConflict(ctx context.Context, conflictID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sync_conflicts WHERE id = ?`, conflictID)
	if err != nil {
		return fmt.Errorf("delete sync conflict: %w", err)
	}
	return nil
}

func DefaultDBPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return defaultDBFileName
	}
	return filepath.Join(configDir, "KDrive", defaultDBFileName)
}

func OpenDatabase(dbPath string) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure sqlite pragmas: %w", err)
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
			error_category TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS secrets (
			key TEXT PRIMARY KEY,
			ciphertext BLOB NOT NULL
		);
		CREATE TABLE IF NOT EXISTS sync_states (
			account_id TEXT PRIMARY KEY,
			state TEXT NOT NULL,
			last_sync_at TEXT,
			last_error TEXT NOT NULL DEFAULT '',
			conflict_count INTEGER NOT NULL DEFAULT 0,
			files_synced INTEGER NOT NULL DEFAULT 0,
			bytes_transferred INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS sync_conflicts (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			local_mod_time TEXT NOT NULL,
			remote_mod_time TEXT NOT NULL,
			resolution TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("create sqlite schema: %w", err)
	}

	// Migrate: add error_category column to mount_states if it was created without it.
	_, _ = db.Exec(`ALTER TABLE mount_states ADD COLUMN error_category TEXT NOT NULL DEFAULT ''`)

	// Migrate: add mount_path column to mount_states.
	_, _ = db.Exec(`ALTER TABLE mount_states ADD COLUMN mount_path TEXT NOT NULL DEFAULT ''`)

	return nil
}
