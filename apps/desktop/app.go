package main

import (
	"KDrive/backend/connectors"
	"KDrive/backend/mount"
	"KDrive/backend/storage"
	"context"
	"database/sql"
	"fmt"
)

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

	manager := mount.NewManagerWithConfig(mount.ProcessManagerConfig{
		ConfigManager: mount.NewConfigManager(),
		RclonePath:    "rclone",
		MountBaseDir:  mount.DefaultMountBaseDir(),
		OnStateChange: func(accountID string, state mount.State, lastError string) {
			_ = mountStateRepo.Upsert(context.Background(), storage.MountState{
				AccountID: accountID,
				State:     string(state),
				LastError: lastError,
			})
		},
	})

	return &App{
		db:                   db,
		connectorRegistry:    registry,
		mountManager:         manager,
		accountRepository:    storage.NewSQLiteAccountRepository(db),
		mountStateRepository: mountStateRepo,
		secretStore:          storage.NewSQLiteSecretStore(db),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
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

func (a *App) MountAccount(accountID string) error {
	if err := connectors.ValidateAccountID(accountID); err != nil {
		return err
	}

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

	if err := a.mountManager.Unmount(a.ctx, accountID); err != nil {
		return err
	}

	remoteName := fmt.Sprintf("s3-%s", accountID)
	_ = a.mountManager.DeleteConfig(remoteName)

	return nil
}

func (a *App) AccountMountStatus(accountID string) (MountStatusView, error) {
	status, err := a.mountManager.Status(a.ctx, accountID)
	if err != nil {
		return MountStatusView{}, err
	}

	return MountStatusView{
		AccountID: status.AccountID,
		State:     string(status.State),
		LastError: status.LastError,
	}, nil
}
