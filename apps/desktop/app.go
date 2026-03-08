package main

import (
	"KDrive/backend/auth"
	"KDrive/backend/connectors"
	"KDrive/backend/mount"
	"KDrive/backend/storage"
	"context"
	"fmt"
)

// App struct
type App struct {
	ctx               context.Context
	connectorRegistry connectors.Registry
	mountManager      mount.Manager
	authService       auth.Service
	accountRepository storage.AccountRepository
}

// NewApp creates a new App application struct
func NewApp() *App {
	registry := connectors.NewRegistry()
	registry.Register(connectors.NewS3Connector())

	return &App{
		connectorRegistry: registry,
		mountManager:      mount.NewManager(),
		authService:       auth.NewService(),
		accountRepository: storage.NewAccountRepository(),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
