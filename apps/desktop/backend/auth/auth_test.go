package auth_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"KDrive/backend/auth"
)

// --- PKCE tests ---

func TestGenerateVerifier_LengthAndCharset(t *testing.T) {
	t.Parallel()

	verifier, err := auth.GenerateVerifier()
	if err != nil {
		t.Fatalf("GenerateVerifier() error = %v", err)
	}
	if len(verifier) < 43 {
		t.Fatalf("verifier too short: len = %d", len(verifier))
	}
	// base64url chars only
	for _, c := range verifier {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", c) {
			t.Fatalf("verifier contains invalid char %q", c)
		}
	}
}

func TestGenerateVerifier_Unique(t *testing.T) {
	t.Parallel()

	v1, _ := auth.GenerateVerifier()
	v2, _ := auth.GenerateVerifier()
	if v1 == v2 {
		t.Fatalf("GenerateVerifier returned same value twice")
	}
}

func TestComputeChallenge_KnownVector(t *testing.T) {
	t.Parallel()

	// RFC 7636 Appendix B example value
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	got := auth.ComputeChallenge(verifier)
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got != want {
		t.Fatalf("ComputeChallenge() = %q, want %q", got, want)
	}
}

// --- Local callback listener tests ---

func TestStartLocalListener_ReceivesCode(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state := "test-state-123"
	listenAddr, resultCh, err := auth.StartLocalListener(ctx, state)
	if err != nil {
		t.Fatalf("StartLocalListener() error = %v", err)
	}
	if !strings.HasPrefix(listenAddr, "http://127.0.0.1:") {
		t.Fatalf("unexpected listen address: %q", listenAddr)
	}

	// Simulate browser redirect with correct state.
	callbackURL := fmt.Sprintf("%s?code=AUTH_CODE_XYZ&state=%s", listenAddr, state)
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d, want 200", resp.StatusCode)
	}

	select {
	case result := <-resultCh:
		if result.Code != "AUTH_CODE_XYZ" {
			t.Fatalf("result.Code = %q, want AUTH_CODE_XYZ", result.Code)
		}
		if result.State != state {
			t.Fatalf("result.State = %q, want %q", result.State, state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestStartLocalListener_RejectsWrongState(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listenAddr, _, err := auth.StartLocalListener(ctx, "correct-state")
	if err != nil {
		t.Fatalf("StartLocalListener() error = %v", err)
	}

	resp, err := http.Get(listenAddr + "?code=CODE&state=wrong-state")
	if err != nil {
		t.Fatalf("GET error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for wrong state, got %d", resp.StatusCode)
	}
}

// --- TokenStore tests ---

type inMemorySecretStore struct {
	data map[string][]byte
}

func newInMemorySecretStore() *inMemorySecretStore {
	return &inMemorySecretStore{data: make(map[string][]byte)}
}

func (s *inMemorySecretStore) Save(_ context.Context, key string, plaintext []byte) error {
	s.data[key] = plaintext
	return nil
}

func (s *inMemorySecretStore) Load(_ context.Context, key string) ([]byte, error) {
	v, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return v, nil
}

func (s *inMemorySecretStore) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func TestSecretBackedTokenStore_SaveAndLoad(t *testing.T) {
	t.Parallel()

	store := auth.NewSecretBackedTokenStore(newInMemorySecretStore())
	token := auth.OAuthToken{
		AccessToken:  "acc-123",
		RefreshToken: "ref-456",
		Expiry:       time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := store.Save(context.Background(), auth.OAuthProviderGoogle, "acct-1", token); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load(context.Background(), auth.OAuthProviderGoogle, "acct-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.AccessToken != token.AccessToken {
		t.Fatalf("AccessToken = %q, want %q", loaded.AccessToken, token.AccessToken)
	}
	if loaded.RefreshToken != token.RefreshToken {
		t.Fatalf("RefreshToken = %q, want %q", loaded.RefreshToken, token.RefreshToken)
	}
	if !loaded.Expiry.Equal(token.Expiry) {
		t.Fatalf("Expiry = %v, want %v", loaded.Expiry, token.Expiry)
	}
}

func TestSecretBackedTokenStore_Delete(t *testing.T) {
	t.Parallel()

	store := auth.NewSecretBackedTokenStore(newInMemorySecretStore())
	token := auth.OAuthToken{
		AccessToken:  "acc",
		RefreshToken: "ref",
		Expiry:       time.Now().Add(time.Hour),
	}

	_ = store.Save(context.Background(), auth.OAuthProviderGoogle, "acct-1", token)

	if err := store.Delete(context.Background(), auth.OAuthProviderGoogle, "acct-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err := store.Load(context.Background(), auth.OAuthProviderGoogle, "acct-1")
	if err == nil {
		t.Fatal("Load() after Delete should return error, got nil")
	}
}
