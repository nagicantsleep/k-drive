package main

import (
	"KDrive/backend/auth"
	"KDrive/backend/connectors"
	"KDrive/backend/mount"
	"KDrive/backend/storage"
	"context"
	"fmt"
)

type CreateS3AccountRequest struct {
	AccountID string            `json:"accountId"`
	Email     string            `json:"email"`
	Options   map[string]string `json:"options"`
}

type AccountView struct {
	ID       string            `json:"id"`
	Provider string            `json:"provider"`
	Email    string            `json:"email"`
	Options  map[string]string `json:"options"`
}

type MountStatusView struct {
	AccountID string `json:"accountId"`
	State     string `json:"state"`
}

// App struct
type App struct {
	ctx                  context.Context
	connectorRegistry    connectors.Registry
	mountManager         mount.Manager
	authService          auth.Service
	accountRepository    storage.AccountRepository
	mountStateRepository storage.MountStateRepository
	secretStore          storage.SecretStore
}

// NewApp creates a new App application struct
func NewApp() *App {
	registry := connectors.NewRegistry()
	registry.Register(connectors.NewS3Connector())

	return &App{
		connectorRegistry:    registry,
		mountManager:         mount.NewManager(),
		authService:          auth.NewService(),
		accountRepository:    storage.NewAccountRepository(),
		mountStateRepository: storage.NewMountStateRepository(),
		secretStore:          storage.NewSecretStore(),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) CreateS3Account(request CreateS3AccountRequest) (AccountView, error) {
	_, ok := a.connectorRegistry.Get(connectors.ProviderS3)
	if !ok {
		return AccountView{}, fmt.Errorf("s3 connector is not registered")
	}

	account := storage.Account{
		ID:       request.AccountID,
		Provider: string(connectors.ProviderS3),
		Email:    request.Email,
		Options:  request.Options,
	}

	if err := a.accountRepository.Save(a.ctx, account); err != nil {
		return AccountView{}, err
	}

	return AccountView{
		ID:       account.ID,
		Provider: account.Provider,
		Email:    account.Email,
		Options:  account.Options,
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
			Options:  account.Options,
		})
	}

	return result, nil
}

func (a *App) MountAccount(accountID string) error {
	return a.mountManager.Mount(a.ctx, accountID)
}

func (a *App) UnmountAccount(accountID string) error {
	return a.mountManager.Unmount(a.ctx, accountID)
}

func (a *App) AccountMountStatus(accountID string) (MountStatusView, error) {
	status, err := a.mountManager.Status(a.ctx, accountID)
	if err != nil {
		return MountStatusView{}, err
	}

	return MountStatusView{
		AccountID: status.AccountID,
		State:     string(status.State),
	}, nil
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
