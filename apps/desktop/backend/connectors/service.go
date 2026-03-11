package connectors

import (
	"context"
	"fmt"
	"regexp"
	"sort"
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

type CapabilityField struct {
	Key         string
	Label       string
	Placeholder string
	Required    bool
	Secret      bool
}

type ProviderCapability struct {
	Provider   Provider
	Label      string
	AuthScheme string
	Fields     []CapabilityField
}

type Connector interface {
	Provider() Provider
	Capability() ProviderCapability
	RemoteName(accountID string) string
	BuildRemoteConfig(ctx context.Context, account AccountConfig) (RemoteConfig, error)
}

type Registry interface {
	Register(connector Connector)
	Get(provider Provider) (Connector, bool)
	List() []Connector
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

func (r *InMemoryRegistry) List() []Connector {
	providers := make([]string, 0, len(r.connectors))
	for provider := range r.connectors {
		providers = append(providers, string(provider))
	}
	sort.Strings(providers)

	connectors := make([]Connector, 0, len(providers))
	for _, provider := range providers {
		connectors = append(connectors, r.connectors[Provider(provider)])
	}

	return connectors
}

func SecretKeys(capability ProviderCapability) []string {
	keys := make([]string, 0)
	for _, field := range capability.Fields {
		if field.Secret {
			keys = append(keys, field.Key)
		}
	}
	return keys
}

type S3Connector struct{}

func NewS3Connector() *S3Connector {
	return &S3Connector{}
}

func (c *S3Connector) Provider() Provider {
	return ProviderS3
}

func (c *S3Connector) Capability() ProviderCapability {
	return ProviderCapability{
		Provider:   ProviderS3,
		Label:      "S3-Compatible",
		AuthScheme: "static",
		Fields: []CapabilityField{
			{Key: "endpoint", Label: "Endpoint URL", Placeholder: "S3 endpoint URL", Required: true},
			{Key: "region", Label: "Region", Placeholder: "Region", Required: true},
			{Key: "bucket", Label: "Bucket", Placeholder: "Bucket (optional)"},
			{Key: "access_key_id", Label: "Access Key ID", Placeholder: "Access Key ID", Required: true, Secret: true},
			{Key: "secret_access_key", Label: "Secret Access Key", Placeholder: "Secret Access Key", Required: true, Secret: true},
		},
	}
}

func (c *S3Connector) RemoteName(accountID string) string {
	return fmt.Sprintf("s3-%s", accountID)
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
		Name:    c.RemoteName(account.AccountID),
		Type:    string(ProviderS3),
		Options: normalized,
	}, nil
}

type GoogleDriveConnector struct{}

func NewGoogleDriveConnector() *GoogleDriveConnector {
	return &GoogleDriveConnector{}
}

func (c *GoogleDriveConnector) Provider() Provider {
	return ProviderGoogle
}

func (c *GoogleDriveConnector) Capability() ProviderCapability {
	return ProviderCapability{
		Provider:   ProviderGoogle,
		Label:      "Google Drive",
		AuthScheme: "oauth",
		Fields: []CapabilityField{
			{Key: "root_folder_id", Label: "Root Folder ID", Placeholder: "Root folder ID (optional)"},
			{Key: "shared_drive", Label: "Shared Drive ID", Placeholder: "Shared drive ID (optional)"},
		},
	}
}

func (c *GoogleDriveConnector) RemoteName(accountID string) string {
	return fmt.Sprintf("gdrive-%s", accountID)
}

func (c *GoogleDriveConnector) BuildRemoteConfig(_ context.Context, account AccountConfig) (RemoteConfig, error) {
	if strings.TrimSpace(account.AccountID) == "" {
		return RemoteConfig{}, fmt.Errorf("account ID is required")
	}

	normalized := map[string]string{
		"scope": "drive",
	}

	if rootFolderID := strings.TrimSpace(account.Options["root_folder_id"]); rootFolderID != "" {
		normalized["root_folder_id"] = rootFolderID
	}
	if sharedDriveID := strings.TrimSpace(account.Options["shared_drive"]); sharedDriveID != "" {
		normalized["team_drive"] = sharedDriveID
	}
	if token := strings.TrimSpace(account.Options["token"]); token != "" {
		normalized["token"] = token
	}

	return RemoteConfig{
		Name:    c.RemoteName(account.AccountID),
		Type:    "drive",
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
