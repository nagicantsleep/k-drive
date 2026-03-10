package auth

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/pkg/browser"
)

// OAuthConfig holds the provider-specific parameters needed to drive an OAuth 2.0 + PKCE flow.
type OAuthConfig struct {
	Provider     OAuthProvider
	ClientID     string
	AuthURL      string
	TokenURL     string
	Scopes       []string
}

// OAuthProvider identifies which OAuth provider to use.
type OAuthProvider string

const (
	OAuthProviderGoogle    OAuthProvider = "google"
	OAuthProviderMicrosoft OAuthProvider = "microsoft"
)

// OAuthRequest asks the service to begin an OAuth flow for a given account.
type OAuthRequest struct {
	Config    OAuthConfig
	AccountID string
}

// OAuthResult is returned once the full OAuth flow completes successfully.
type OAuthResult struct {
	Token OAuthToken
}

// Service drives OAuth 2.0 + PKCE flows and stores the resulting tokens.
type Service interface {
	BeginOAuth(ctx context.Context, request OAuthRequest) (OAuthResult, error)
}

// RealService implements Service with a local loopback callback listener and PKCE.
type RealService struct {
	TokenStore TokenStore
}

func NewService(tokenStore TokenStore) *RealService {
	return &RealService{TokenStore: tokenStore}
}

// BeginOAuth opens the system browser for the provider's auth page, waits for the
// local callback, and persists the resulting tokens via TokenStore.
func (s *RealService) BeginOAuth(ctx context.Context, req OAuthRequest) (OAuthResult, error) {
	verifier, err := GenerateVerifier()
	if err != nil {
		return OAuthResult{}, err
	}
	challenge := ComputeChallenge(verifier)

	state, err := GenerateState()
	if err != nil {
		return OAuthResult{}, err
	}

	redirectURI, resultCh, err := StartLocalListener(ctx, state)
	if err != nil {
		return OAuthResult{}, err
	}

	authURL := buildAuthURL(req.Config, challenge, state, redirectURI)

	if err := browser.OpenURL(authURL); err != nil {
		return OAuthResult{}, fmt.Errorf("open browser: %w", err)
	}

	var cbResult CallbackResult
	select {
	case cbResult = <-resultCh:
	case <-ctx.Done():
		return OAuthResult{}, fmt.Errorf("oauth flow cancelled")
	}

	token, err := exchangeCode(ctx, req.Config, cbResult.Code, verifier, redirectURI)
	if err != nil {
		return OAuthResult{}, fmt.Errorf("token exchange: %w", err)
	}

	if err := s.TokenStore.Save(ctx, req.Config.Provider, req.AccountID, token); err != nil {
		return OAuthResult{}, fmt.Errorf("save token: %w", err)
	}

	return OAuthResult{Token: token}, nil
}

func buildAuthURL(cfg OAuthConfig, challenge, state, redirectURI string) string {
	params := url.Values{}
	params.Set("client_id", cfg.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", joinScopes(cfg.Scopes))
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	return cfg.AuthURL + "?" + params.Encode()
}

func joinScopes(scopes []string) string {
	out := ""
	for i, s := range scopes {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}

// exchangeCode sends the authorization code to the token endpoint and returns an OAuthToken.
// For providers that do not return a refresh_token (e.g. first Google Drive requests), an
// empty string is stored and the caller is expected to re-authorise when needed.
func exchangeCode(ctx context.Context, cfg OAuthConfig, code, verifier, redirectURI string) (OAuthToken, error) {
	params := url.Values{}
	params.Set("grant_type", "authorization_code")
	params.Set("code", code)
	params.Set("client_id", cfg.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("code_verifier", verifier)

	body, err := postForm(ctx, cfg.TokenURL, params)
	if err != nil {
		return OAuthToken{}, err
	}

	accessToken, _ := body["access_token"].(string)
	refreshToken, _ := body["refresh_token"].(string)

	var expiry time.Time
	if expiresIn, ok := body["expires_in"].(float64); ok && expiresIn > 0 {
		expiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}

	if accessToken == "" {
		return OAuthToken{}, fmt.Errorf("token endpoint did not return access_token")
	}

	return OAuthToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       expiry,
	}, nil
}
