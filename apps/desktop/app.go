package main

import (
	"KDrive/backend/auth"
	"KDrive/backend/connectors"
	"KDrive/backend/mount"
	"KDrive/backend/storage"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

const maxMountRetries = 3

type CreateAccountRequest struct {
	AccountID string            `json:"accountId"`
	Provider  string            `json:"provider"`
	Email     string            `json:"email"`
	Options   map[string]string `json:"options"`
}

type CapabilityFieldView struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
}

type ProviderCapabilityView struct {
	Provider   string                `json:"provider"`
	Label      string                `json:"label"`
	AuthScheme string                `json:"authScheme"`
	Fields     []CapabilityFieldView `json:"fields"`
}

type BeginOAuthRequest struct {
	Provider  string `json:"provider"`
	AccountID string `json:"accountId"`
	ClientID  string `json:"clientId"`
}

type BeginOAuthResultView struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	Expiry       string `json:"expiry"`
}

type AccountView struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Email    string `json:"email"`
}

type MountStatusView struct {
	AccountID     string `json:"accountId"`
	State         string `json:"state"`
	LastError     string `json:"lastError"`
	ErrorCategory string `json:"errorCategory"`
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
	authService          auth.Service

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
	registry.Register(connectors.NewGoogleDriveConnector())

	mountStateRepo := storage.NewSQLiteMountStateRepository(db)
	secretStore := storage.NewSQLiteSecretStore(db)

	a := &App{
		db:                   db,
		connectorRegistry:    registry,
		accountRepository:    storage.NewSQLiteAccountRepository(db),
		mountStateRepository: mountStateRepo,
		secretStore:          secretStore,
		authService:          auth.NewService(auth.NewSecretBackedTokenStore(secretStore)),
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
func (a *App) onMountStateChange(accountID string, state mount.State, lastError string, mountErr error) {
	_ = a.mountStateRepository.Upsert(context.Background(), storage.MountState{
		AccountID:     accountID,
		State:         string(state),
		LastError:     lastError,
		ErrorCategory: classifyMountError(state, lastError, mountErr),
	})

	switch state {
	case mount.StateFailed:
		if classifyMountError(state, lastError, mountErr) != "process_failed" {
			return
		}
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
					if err := a.doMount(accountID); err != nil {
						a.persistMountFailure(accountID, err)
					}
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
// Before attempting the remount it marks the persisted state as "stopped" so that a
// crash-then-restart does not leave stale "mounted" in SQLite if the remount fails.
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
		if state.State != string(mount.StateMounted) {
			continue
		}
		accountID := acc.ID

		// Mark as stopped before remounting so a crash mid-attempt leaves a clean state.
		_ = a.mountStateRepository.Upsert(a.ctx, storage.MountState{
			AccountID: accountID,
			State:     string(mount.StateStopped),
			LastError: "",
		})

		go func() {
			if err := a.doMount(accountID); err != nil {
				a.persistMountFailure(accountID, err)
			}
		}()
	}
}

func (a *App) shutdown(_ context.Context) {
	_ = a.db.Close()
}

// BeginOAuth opens the system browser for an OAuth 2.0 + PKCE flow, waits for
// the local callback, and returns the resulting tokens. The caller (typically the
// frontend) provides the provider key and a client_id registered for that provider.
// Token endpoint URLs are resolved internally per provider.
func (a *App) BeginOAuth(request BeginOAuthRequest) (BeginOAuthResultView, error) {
	oauthProvider, tokenURL, authURL, scopes, err := resolveOAuthProvider(request.Provider)
	if err != nil {
		return BeginOAuthResultView{}, err
	}

	result, err := a.authService.BeginOAuth(a.ctx, auth.OAuthRequest{
		Config: auth.OAuthConfig{
			Provider: oauthProvider,
			ClientID: request.ClientID,
			AuthURL:  authURL,
			TokenURL: tokenURL,
			Scopes:   scopes,
		},
		AccountID: request.AccountID,
	})
	if err != nil {
		return BeginOAuthResultView{}, err
	}

	return BeginOAuthResultView{
		AccessToken:  result.Token.AccessToken,
		RefreshToken: result.Token.RefreshToken,
		Expiry:       result.Token.Expiry.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

func resolveOAuthProvider(provider string) (auth.OAuthProvider, string, string, []string, error) {
	switch provider {
	case string(auth.OAuthProviderGoogle), string(connectors.ProviderGoogle):
		return auth.OAuthProviderGoogle,
			"https://oauth2.googleapis.com/token",
			"https://accounts.google.com/o/oauth2/v2/auth",
			[]string{"https://www.googleapis.com/auth/drive"},
			nil
	case string(auth.OAuthProviderMicrosoft), string(connectors.ProviderOneDrive):
		return auth.OAuthProviderMicrosoft,
			"https://login.microsoftonline.com/common/oauth2/v2.0/token",
			"https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			[]string{"Files.ReadWrite.All", "offline_access"},
			nil
	default:
		return "", "", "", nil, fmt.Errorf("OAuth provider %q is not supported", provider)
	}
}

type CreateOAuthAccountRequest struct {
	AccountID string `json:"accountId"`
	Provider  string `json:"provider"`
	Email     string `json:"email"`
}

// CreateOAuthAccount persists an account row for an OAuth provider whose tokens
// have already been stored by BeginOAuth. No credential options are needed here.
func (a *App) CreateOAuthAccount(request CreateOAuthAccountRequest) (AccountView, error) {
	if err := connectors.ValidateAccountID(request.AccountID); err != nil {
		return AccountView{}, err
	}
	if _, ok := a.connectorRegistry.Get(connectors.Provider(request.Provider)); !ok {
		return AccountView{}, fmt.Errorf("provider %q is not registered", request.Provider)
	}

	account := storage.Account{
		ID:       request.AccountID,
		Provider: request.Provider,
		Email:    request.Email,
		Options:  map[string]string{},
	}
	if err := a.accountRepository.Save(a.ctx, account); err != nil {
		return AccountView{}, err
	}
	return AccountView{ID: account.ID, Provider: account.Provider, Email: account.Email}, nil
}

func (a *App) CreateAccount(request CreateAccountRequest) (AccountView, error) {
	provider := connectors.Provider(request.Provider)
	connector, ok := a.connectorRegistry.Get(provider)
	if !ok {
		return AccountView{}, fmt.Errorf("provider %q is not registered", request.Provider)
	}

	if err := connectors.ValidateAccountID(request.AccountID); err != nil {
		return AccountView{}, err
	}

	_, err := connector.BuildRemoteConfig(a.ctx, connectors.AccountConfig{
		AccountID: request.AccountID,
		Provider:  provider,
		Options:   request.Options,
	})
	if err != nil {
		return AccountView{}, fmt.Errorf("invalid %s config: %w", request.Provider, err)
	}

	safeOptions := make(map[string]string, len(request.Options))
	for k, v := range request.Options {
		safeOptions[k] = v
	}
	for _, secretKey := range connectors.SecretKeys(connector.Capability()) {
		val := safeOptions[secretKey]
		if val == "" {
			continue
		}
		storeKey := secretStoreKey(request.AccountID, secretKey)
		if err := a.secretStore.Save(a.ctx, storeKey, []byte(val)); err != nil {
			return AccountView{}, fmt.Errorf("save secret %s: %w", secretKey, err)
		}
		delete(safeOptions, secretKey)
	}

	account := storage.Account{
		ID:       request.AccountID,
		Provider: request.Provider,
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

func (a *App) ProviderCapabilities() []ProviderCapabilityView {
	connectorList := a.connectorRegistry.List()
	capabilities := make([]ProviderCapabilityView, 0, len(connectorList))
	for _, connector := range connectorList {
		capability := connector.Capability()
		fields := make([]CapabilityFieldView, 0, len(capability.Fields))
		for _, field := range capability.Fields {
			fields = append(fields, CapabilityFieldView{
				Key:         field.Key,
				Label:       field.Label,
				Placeholder: field.Placeholder,
				Required:    field.Required,
				Secret:      field.Secret,
			})
		}
		capabilities = append(capabilities, ProviderCapabilityView{
			Provider:   string(capability.Provider),
			Label:      capability.Label,
			AuthScheme: capability.AuthScheme,
			Fields:     fields,
		})
	}
	return capabilities
}

func secretStoreKey(accountID, fieldKey string) string {
	return fmt.Sprintf("account/%s/%s", accountID, fieldKey)
}

func encodeRcloneTokenOption(token auth.OAuthToken) (string, error) {
	payload := map[string]string{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
	}
	if !token.Expiry.IsZero() {
		payload["expiry"] = token.Expiry.UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) accountByID(accountID string) (*storage.Account, error) {
	accounts, err := a.accountRepository.List(a.ctx)
	if err != nil {
		return nil, fmt.Errorf("load accounts: %w", err)
	}
	for i := range accounts {
		if accounts[i].ID == accountID {
			return &accounts[i], nil
		}
	}
	return nil, fmt.Errorf("account %q not found", accountID)
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

	err := a.doMount(accountID)
	if err != nil {
		a.persistMountFailure(accountID, err)
	}
	return err
}

// doMount is the shared implementation used by MountAccount, startup recovery, and retry goroutines.
func (a *App) doMount(accountID string) error {
	account, err := a.accountByID(accountID)
	if err != nil {
		return err
	}

	connector, ok := a.connectorRegistry.Get(connectors.Provider(account.Provider))
	if !ok {
		return fmt.Errorf("provider %q is not registered", account.Provider)
	}

	opts := make(map[string]string, len(account.Options))
	for k, v := range account.Options {
		opts[k] = v
	}
	for _, secretKey := range connectors.SecretKeys(connector.Capability()) {
		storeKey := secretStoreKey(accountID, secretKey)
		val, err := a.secretStore.Load(a.ctx, storeKey)
		if err != nil && err != storage.ErrSecretNotFound {
			return fmt.Errorf("load secret %s: %w", secretKey, err)
		}
		if len(val) > 0 {
			opts[secretKey] = string(val)
		}
	}

	if connectors.Provider(account.Provider) == connectors.ProviderGoogle {
		tokenStore := auth.NewSecretBackedTokenStore(a.secretStore)
		token, err := tokenStore.Load(a.ctx, auth.OAuthProviderGoogle, accountID)
		if err != nil {
			return &configInvalidError{cause: fmt.Errorf("load google oauth token: %w", err)}
		}
		encodedToken, err := encodeRcloneTokenOption(token)
		if err != nil {
			return &configInvalidError{cause: fmt.Errorf("encode google oauth token: %w", err)}
		}
		opts["token"] = encodedToken
	}

	remoteConfig, err := connector.BuildRemoteConfig(a.ctx, connectors.AccountConfig{
		AccountID: accountID,
		Provider:  connectors.Provider(account.Provider),
		Options:   opts,
	})
	if err != nil {
		return &configInvalidError{cause: fmt.Errorf("build remote config: %w", err)}
	}

	if err := a.mountManager.WriteConfig(remoteConfig); err != nil {
		return &configInvalidError{cause: fmt.Errorf("write rclone config: %w", err)}
	}

	return a.mountManager.Mount(a.ctx, accountID)
}

// configInvalidError wraps config-validation failures so errorCategoryFromErr can classify them.
type configInvalidError struct{ cause error }

func (e *configInvalidError) Error() string { return e.cause.Error() }
func (e *configInvalidError) Unwrap() error { return e.cause }

func (a *App) UnmountAccount(accountID string) error {
	if err := connectors.ValidateAccountID(accountID); err != nil {
		return err
	}

	account, err := a.accountByID(accountID)
	if err != nil {
		return err
	}

	a.retryMu.Lock()
	a.stoppedByUser[accountID] = true
	a.retryCounts[accountID] = 0
	a.retryMu.Unlock()

	if err := a.mountManager.Unmount(a.ctx, accountID); err != nil {
		return err
	}

	connector, ok := a.connectorRegistry.Get(connectors.Provider(account.Provider))
	if ok {
		_ = a.mountManager.DeleteConfig(connector.RemoteName(accountID))
	}

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
				AccountID:     accountID,
				State:         dbState.State,
				LastError:     dbState.LastError,
				ErrorCategory: dbState.ErrorCategory,
			}, nil
		}
	}

	return MountStatusView{
		AccountID:     status.AccountID,
		State:         string(status.State),
		LastError:     status.LastError,
		ErrorCategory: status.ErrorCategory,
	}, nil
}

func (a *App) persistMountFailure(accountID string, err error) {
	_ = a.mountStateRepository.Upsert(context.Background(), storage.MountState{
		AccountID:     accountID,
		State:         string(mount.StateFailed),
		LastError:     err.Error(),
		ErrorCategory: classifyMountError(mount.StateFailed, err.Error(), err),
	})
}

func classifyMountError(state mount.State, lastError string, err error) string {
	category := errorCategoryFromErr(err)
	if category != "" {
		return category
	}
	if state == mount.StateFailed && lastError != "" {
		return "process_failed"
	}
	return ""
}

// errorCategoryFromErr extracts the category from a *mount.PreflightError if present,
// returns "config_invalid" for configInvalidError, otherwise "process_failed" for
// generic mount errors, or "" for nil.
func errorCategoryFromErr(err error) string {
	if err == nil {
		return ""
	}
	var pe *mount.PreflightError
	if errors.As(err, &pe) {
		return string(pe.Category)
	}
	var ce *configInvalidError
	if errors.As(err, &ce) {
		return "config_invalid"
	}
	return "process_failed"
}
