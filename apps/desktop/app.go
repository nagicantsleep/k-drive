package main

import (
	"KDrive/backend/connectors"
	"KDrive/backend/mount"
	"KDrive/backend/storage"
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

const maxMountRetries = 3

type CreateS3AccountRequest struct {
	AccountID string            `json:"accountId"`
	Email     string            `json:"email"`
	Options   map[string]string `json:"options"`
}

type AccountView struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Email    string `json:"email"`
}

type MountStatusView struct {
	AccountID string `json:"accountId"`
	State     string `json:"state"`
	LastError string `json:"lastError"`
}

// App struct
type App struct {
	ctx                  context.Context
	db                   *sql.DB
	connectorRegistry    connectors.Registry
	mountManager         mount.Manager
	accountRepository    storage.AccountRepository
	mountStateRepository storage.MountStateRepository
	secretStore          storage.SecretStore

	retryMu        sync.Mutex
	retryCounts    map[string]int
	stoppedByUser  map[string]bool
	retryBaseDelay time.Duration
}

// NewApp creates a new App application struct
func NewApp() *App {
	db, err := storage.OpenDatabase(storage.DefaultDBPath())
	if err != nil {
		panic(fmt.Errorf("open database: %w", err))
	}

	registry := connectors.NewRegistry()
	registry.Register(connectors.NewS3Connector())

	mountStateRepo := storage.NewSQLiteMountStateRepository(db)

	a := &App{
		db:                   db,
		connectorRegistry:    registry,
		accountRepository:    storage.NewSQLiteAccountRepository(db),
		mountStateRepository: mountStateRepo,
		secretStore:          storage.NewSQLiteSecretStore(db),
		retryCounts:          make(map[string]int),
		stoppedByUser:        make(map[string]bool),
		retryBaseDelay:       5 * time.Second,
	}

	a.mountManager = mount.NewManagerWithConfig(mount.ProcessManagerConfig{
		ConfigManager: mount.NewConfigManager(),
		RclonePath:    "rclone",
		MountBaseDir:  mount.DefaultMountBaseDir(),
		OnStateChange: a.onMountStateChange,
	})

	return a
}

// onMountStateChange persists the new state to SQLite and schedules retries on unexpected failure.
func (a *App) onMountStateChange(accountID string, state mount.State, lastError string) {
	_ = a.mountStateRepository.Upsert(context.Background(), storage.MountState{
		AccountID: accountID,
		State:     string(state),
		LastError: lastError,
	})

	switch state {
	case mount.StateFailed:
		a.retryMu.Lock()
		if a.stoppedByUser[accountID] {
			a.retryMu.Unlock()
			return
		}
		a.retryCounts[accountID]++
		count := a.retryCounts[accountID]
		a.retryMu.Unlock()

		if count <= maxMountRetries {
			delay := time.Duration(count) * a.retryBaseDelay
			go func() {
				time.Sleep(delay)
				a.retryMu.Lock()
				skip := a.stoppedByUser[accountID]
				a.retryMu.Unlock()
				if !skip {
					_ = a.doMount(accountID)
				}
			}()
		}

	case mount.StateMounted:
		a.retryMu.Lock()
		a.retryCounts[accountID] = 0
		a.retryMu.Unlock()
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.autoRemountOnStartup()
}

// autoRemountOnStartup re-mounts any account whose last persisted state was "mounted".
func (a *App) autoRemountOnStartup() {
	accounts, err := a.accountRepository.List(a.ctx)
	if err != nil {
		return
	}
	for _, acc := range accounts {
		state, err := a.mountStateRepository.Get(a.ctx, acc.ID)
		if err != nil {
			continue
		}
		if state.State == string(mount.StateMounted) {
			accountID := acc.ID
			go func() {
				_ = a.doMount(accountID)
			}()
		}
	}
}

func (a *App) shutdown(_ context.Context) {
	_ = a.db.Close()
}

func (a *App) CreateS3Account(request CreateS3AccountRequest) (AccountView, error) {
	connector, ok := a.connectorRegistry.Get(connectors.ProviderS3)
	if !ok {
		return AccountView{}, fmt.Errorf("s3 connector is not registered")
	}

	if err := connectors.ValidateAccountID(request.AccountID); err != nil {
		return AccountView{}, err
	}

	_, err := connector.BuildRemoteConfig(a.ctx, connectors.AccountConfig{
		AccountID: request.AccountID,
		Provider:  connectors.ProviderS3,
		Options:   request.Options,
	})
	if err != nil {
		return AccountView{}, fmt.Errorf("invalid s3 config: %w", err)
	}

	secretKeys := []string{"access_key_id", "secret_access_key"}
	safeOptions := make(map[string]string, len(request.Options))
	for k, v := range request.Options {
		safeOptions[k] = v
	}
	for _, secretKey := range secretKeys {
		val := safeOptions[secretKey]
		if val == "" {
			continue
		}
		storeKey := fmt.Sprintf("account/%s/%s", request.AccountID, secretKey)
		if err := a.secretStore.Save(a.ctx, storeKey, []byte(val)); err != nil {
			return AccountView{}, fmt.Errorf("save secret %s: %w", secretKey, err)
		}
		delete(safeOptions, secretKey)
	}

	account := storage.Account{
		ID:       request.AccountID,
		Provider: string(connectors.ProviderS3),
		Email:    request.Email,
		Options:  safeOptions,
	}

	if err := a.accountRepository.Save(a.ctx, account); err != nil {
		return AccountView{}, err
	}

	return AccountView{
		ID:       account.ID,
		Provider: account.Provider,
		Email:    account.Email,
	}, nil
}

func (a *App) ListAccounts() ([]AccountView, error) {
	accounts, err := a.accountRepository.List(a.ctx)
	if err != nil {
		return nil, err
	}

	result := make([]AccountView, 0, len(accounts))
	for _, account := range accounts {
		result = append(result, AccountView{
			ID:       account.ID,
			Provider: account.Provider,
			Email:    account.Email,
		})
	}

	return result, nil
}

// MountAccount is the Wails-bound user-initiated mount. It resets retry state before mounting.
func (a *App) MountAccount(accountID string) error {
	if err := connectors.ValidateAccountID(accountID); err != nil {
		return err
	}

	a.retryMu.Lock()
	delete(a.stoppedByUser, accountID)
	a.retryCounts[accountID] = 0
	a.retryMu.Unlock()

	return a.doMount(accountID)
}

// doMount is the shared implementation used by MountAccount, startup recovery, and retry goroutines.
func (a *App) doMount(accountID string) error {
	accounts, err := a.accountRepository.List(a.ctx)
	if err != nil {
		return fmt.Errorf("load accounts: %w", err)
	}
	var account *storage.Account
	for i := range accounts {
		if accounts[i].ID == accountID {
			account = &accounts[i]
			break
		}
	}
	if account == nil {
		return fmt.Errorf("account %q not found", accountID)
	}

	opts := make(map[string]string, len(account.Options))
	for k, v := range account.Options {
		opts[k] = v
	}
	for _, secretKey := range []string{"access_key_id", "secret_access_key"} {
		storeKey := fmt.Sprintf("account/%s/%s", accountID, secretKey)
		val, err := a.secretStore.Load(a.ctx, storeKey)
		if err != nil && err != storage.ErrSecretNotFound {
			return fmt.Errorf("load secret %s: %w", secretKey, err)
		}
		if len(val) > 0 {
			opts[secretKey] = string(val)
		}
	}

	connector, _ := a.connectorRegistry.Get(connectors.ProviderS3)
	remoteConfig, err := connector.BuildRemoteConfig(a.ctx, connectors.AccountConfig{
		AccountID: accountID,
		Provider:  connectors.ProviderS3,
		Options:   opts,
	})
	if err != nil {
		return fmt.Errorf("build remote config: %w", err)
	}

	if err := a.mountManager.WriteConfig(remoteConfig); err != nil {
		return fmt.Errorf("write rclone config: %w", err)
	}

	return a.mountManager.Mount(a.ctx, accountID)
}

func (a *App) UnmountAccount(accountID string) error {
	if err := connectors.ValidateAccountID(accountID); err != nil {
		return err
	}

	a.retryMu.Lock()
	a.stoppedByUser[accountID] = true
	a.retryCounts[accountID] = 0
	a.retryMu.Unlock()

	if err := a.mountManager.Unmount(a.ctx, accountID); err != nil {
		return err
	}

	remoteName := fmt.Sprintf("s3-%s", accountID)
	_ = a.mountManager.DeleteConfig(remoteName)

	return nil
}

// AccountMountStatus returns the current mount status from the process manager. When the
// process manager has no active entry (state = stopped), it falls back to the last known
// state persisted in SQLite so the frontend reflects state across restarts.
func (a *App) AccountMountStatus(accountID string) (MountStatusView, error) {
	status, err := a.mountManager.Status(a.ctx, accountID)
	if err != nil {
		return MountStatusView{}, err
	}

	if status.State == mount.StateStopped {
		if dbState, dbErr := a.mountStateRepository.Get(a.ctx, accountID); dbErr == nil {
			return MountStatusView{
				AccountID: accountID,
				State:     dbState.State,
				LastError: dbState.LastError,
			}, nil
		}
	}

	return MountStatusView{
		AccountID: status.AccountID,
		State:     string(status.State),
		LastError: status.LastError,
	}, nil
}
