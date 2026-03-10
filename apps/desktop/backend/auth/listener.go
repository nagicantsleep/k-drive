package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"time"
)

// OAuthToken holds the credentials returned after a completed OAuth flow.
type OAuthToken struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// CallbackResult is produced by the local redirect listener once a code arrives.
type CallbackResult struct {
	Code  string
	State string
}

// StartLocalListener binds an HTTP server on a random loopback port, waits for
// the OAuth redirect with matching state, and sends the result on the returned channel.
// The listener shuts itself down after receiving one request or when ctx is cancelled.
// The listen address (e.g. "http://localhost:54321") is returned so callers can use it
// as the redirect_uri.
func StartLocalListener(ctx context.Context, expectedState string) (listenAddr string, resultCh <-chan CallbackResult, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("oauth listener: %w", err)
	}

	ch := make(chan CallbackResult, 1)
	srv := &http.Server{}

	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		if state != expectedState {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><p>Authentication complete. You may close this tab.</p></body></html>`))

		ch <- CallbackResult{Code: code, State: state}

		go func() {
			_ = srv.Shutdown(context.Background())
		}()
	})

	go func() {
		_ = srv.Serve(ln)
	}()

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	addr := fmt.Sprintf("http://%s", ln.Addr().String())
	return addr, ch, nil
}

// GenerateState returns a random opaque string suitable for use as the OAuth state parameter.
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate oauth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
