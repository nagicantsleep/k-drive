package connectors

import (
	"context"
	"fmt"
)

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
	if account.AccountID == "" {
		return RemoteConfig{}, fmt.Errorf("account ID is required")
	}

	name := fmt.Sprintf("s3-%s", account.AccountID)
	options := make(map[string]string)
	for key, value := range account.Options {
		options[key] = value
	}

	return RemoteConfig{
		Name:    name,
		Type:    string(ProviderS3),
		Options: options,
	}, nil
}
