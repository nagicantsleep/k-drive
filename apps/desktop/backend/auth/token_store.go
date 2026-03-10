package auth

import (
	"context"
	"fmt"
	"time"
)

// TokenStore persists and loads OAuth tokens via the app-level SecretStore.
type TokenStore interface {
	Save(ctx context.Context, provider OAuthProvider, accountID string, token OAuthToken) error
	Load(ctx context.Context, provider OAuthProvider, accountID string) (OAuthToken, error)
	Delete(ctx context.Context, provider OAuthProvider, accountID string) error
}

// SecretBacked is a TokenStore that stores tokens in any key-value SecretStore.
type SecretBacked struct {
	store SecretKeyStore
}

// SecretKeyStore is the minimal interface from storage.SecretStore that TokenStore needs.
type SecretKeyStore interface {
	Save(ctx context.Context, key string, plaintext []byte) error
	Load(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

func NewSecretBackedTokenStore(store SecretKeyStore) *SecretBacked {
	return &SecretBacked{store: store}
}

func tokenKey(provider OAuthProvider, accountID, field string) string {
	return fmt.Sprintf("oauth/%s/%s/%s", provider, accountID, field)
}

func (s *SecretBacked) Save(ctx context.Context, provider OAuthProvider, accountID string, token OAuthToken) error {
	pairs := []struct {
		field string
		value []byte
	}{
		{"access_token", []byte(token.AccessToken)},
		{"refresh_token", []byte(token.RefreshToken)},
		{"expiry", []byte(token.Expiry.UTC().Format(time.RFC3339))},
	}

	for _, p := range pairs {
		if err := s.store.Save(ctx, tokenKey(provider, accountID, p.field), p.value); err != nil {
			return fmt.Errorf("save %s: %w", p.field, err)
		}
	}
	return nil
}

func (s *SecretBacked) Load(ctx context.Context, provider OAuthProvider, accountID string) (OAuthToken, error) {
	load := func(field string) ([]byte, error) {
		return s.store.Load(ctx, tokenKey(provider, accountID, field))
	}

	accessRaw, err := load("access_token")
	if err != nil {
		return OAuthToken{}, fmt.Errorf("load access_token: %w", err)
	}

	refreshRaw, err := load("refresh_token")
	if err != nil {
		return OAuthToken{}, fmt.Errorf("load refresh_token: %w", err)
	}

	expiryRaw, err := load("expiry")
	if err != nil {
		return OAuthToken{}, fmt.Errorf("load expiry: %w", err)
	}

	expiry, err := time.Parse(time.RFC3339, string(expiryRaw))
	if err != nil {
		return OAuthToken{}, fmt.Errorf("parse expiry: %w", err)
	}

	return OAuthToken{
		AccessToken:  string(accessRaw),
		RefreshToken: string(refreshRaw),
		Expiry:       expiry,
	}, nil
}

func (s *SecretBacked) Delete(ctx context.Context, provider OAuthProvider, accountID string) error {
	for _, field := range []string{"access_token", "refresh_token", "expiry"} {
		if err := s.store.Delete(ctx, tokenKey(provider, accountID, field)); err != nil {
			return fmt.Errorf("delete %s: %w", field, err)
		}
	}
	return nil
}
