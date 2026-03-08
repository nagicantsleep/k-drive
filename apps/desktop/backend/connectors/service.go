package connectors

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var accountIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateAccountID ensures accountID is safe for use in file paths and config section names.
func ValidateAccountID(id string) error {
	if id == "" {
		return fmt.Errorf("account ID is required")
	}
	if !accountIDPattern.MatchString(id) {
		return fmt.Errorf("account ID must contain only letters, digits, hyphens, and underscores")
	}
	return nil
}

type Provider string

const (
	ProviderS3       Provider = "s3"
	ProviderGoogle   Provider = "google-drive"
	ProviderOneDrive Provider = "onedrive"
)

type AccountConfig struct {
	AccountID string
	Provider  Provider
	Options   map[string]string
}

type RemoteConfig struct {
	Name    string
	Type    string
	Options map[string]string
}

type Connector interface {
	Provider() Provider
	BuildRemoteConfig(ctx context.Context, account AccountConfig) (RemoteConfig, error)
}

type Registry interface {
	Register(connector Connector)
	Get(provider Provider) (Connector, bool)
}

type InMemoryRegistry struct {
	connectors map[Provider]Connector
}

func NewRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{connectors: make(map[Provider]Connector)}
}

func (r *InMemoryRegistry) Register(connector Connector) {
	r.connectors[connector.Provider()] = connector
}

func (r *InMemoryRegistry) Get(provider Provider) (Connector, bool) {
	connector, ok := r.connectors[provider]
	return connector, ok
}

type S3Connector struct{}

func NewS3Connector() *S3Connector {
	return &S3Connector{}
}

func (c *S3Connector) Provider() Provider {
	return ProviderS3
}

func (c *S3Connector) BuildRemoteConfig(_ context.Context, account AccountConfig) (RemoteConfig, error) {
	if strings.TrimSpace(account.AccountID) == "" {
		return RemoteConfig{}, fmt.Errorf("account ID is required")
	}

	normalized := make(map[string]string)

	endpoint, err := requiredOption(account.Options, "endpoint")
	if err != nil {
		return RemoteConfig{}, err
	}
	region, err := requiredOption(account.Options, "region")
	if err != nil {
		return RemoteConfig{}, err
	}
	accessKeyID, err := requiredOption(account.Options, "access_key_id")
	if err != nil {
		return RemoteConfig{}, err
	}
	secretAccessKey, err := requiredOption(account.Options, "secret_access_key")
	if err != nil {
		return RemoteConfig{}, err
	}

	normalized["provider"] = "Other"
	normalized["endpoint"] = endpoint
	normalized["region"] = region
	normalized["access_key_id"] = accessKeyID
	normalized["secret_access_key"] = secretAccessKey
	normalized["env_auth"] = "false"

	if bucket := strings.TrimSpace(account.Options["bucket"]); bucket != "" {
		normalized["bucket"] = bucket
	}

	return RemoteConfig{
		Name:    fmt.Sprintf("s3-%s", account.AccountID),
		Type:    string(ProviderS3),
		Options: normalized,
	}, nil
}

func requiredOption(options map[string]string, key string) (string, error) {
	value := strings.TrimSpace(options[key])
	if value == "" {
		return "", fmt.Errorf("missing required s3 option: %s", key)
	}

	return value, nil
}
