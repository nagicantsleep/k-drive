package auth

import "context"

type OAuthProvider string

const (
	OAuthProviderGoogle    OAuthProvider = "google"
	OAuthProviderMicrosoft OAuthProvider = "microsoft"
)

type OAuthRequest struct {
	Provider   OAuthProvider
	RedirectTo string
}

type OAuthResult struct {
	AuthorizationURL string
}

type Service interface {
	BeginOAuth(ctx context.Context, request OAuthRequest) (OAuthResult, error)
}

type NoopService struct{}

func NewService() *NoopService {
	return &NoopService{}
}

func (s *NoopService) BeginOAuth(_ context.Context, request OAuthRequest) (OAuthResult, error) {
	return OAuthResult{AuthorizationURL: request.RedirectTo}, nil
}
