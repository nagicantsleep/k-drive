package connectors

import (
	"context"
	"testing"
)

func TestValidateAccountID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id      string
		wantErr bool
	}{
		{"", true},
		{"has space", true},
		{"has/slash", true},
		{"has.dot", true},
		{"valid-id", false},
		{"valid_id", false},
		{"ValidID123", false},
		{"123", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			err := ValidateAccountID(tc.id)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateAccountID(%q) = nil, want error", tc.id)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateAccountID(%q) = %v, want nil", tc.id, err)
			}
		})
	}
}

func TestSecretKeys_ReturnsOnlySecretFields(t *testing.T) {
	t.Parallel()

	keys := SecretKeys(NewS3Connector().Capability())
	if len(keys) != 2 {
		t.Fatalf("SecretKeys() len = %d, want 2", len(keys))
	}
	if keys[0] != "access_key_id" || keys[1] != "secret_access_key" {
		t.Fatalf("SecretKeys() = %v", keys)
	}

	googleKeys := SecretKeys(NewGoogleDriveConnector().Capability())
	if len(googleKeys) != 0 {
		t.Fatalf("SecretKeys(google) = %v, want empty", googleKeys)
	}
}

func TestS3Connector_BuildRemoteConfig_ValidationError(t *testing.T) {
	t.Parallel()

	connector := NewS3Connector()
	_, err := connector.BuildRemoteConfig(context.Background(), AccountConfig{
		AccountID: "acc-1",
		Provider:  ProviderS3,
		Options: map[string]string{
			"endpoint":          "https://s3.example.com",
			"region":            "ap-northeast-1",
			"access_key_id":     "key",
			"secret_access_key": "",
		},
	})
	if err == nil {
		t.Fatalf("BuildRemoteConfig() error = nil, want validation error")
	}
	if err.Error() != "missing required s3 option: secret_access_key" {
		t.Fatalf("BuildRemoteConfig() error = %q", err.Error())
	}
}

func TestS3Connector_BuildRemoteConfig_Success(t *testing.T) {
	t.Parallel()

	connector := NewS3Connector()
	config, err := connector.BuildRemoteConfig(context.Background(), AccountConfig{
		AccountID: "acc-1",
		Provider:  ProviderS3,
		Options: map[string]string{
			"endpoint":          "https://s3.example.com",
			"region":            "ap-northeast-1",
			"access_key_id":     "key",
			"secret_access_key": "secret",
			"bucket":            "team-bucket",
		},
	})
	if err != nil {
		t.Fatalf("BuildRemoteConfig() error = %v", err)
	}

	if config.Name != "s3-acc-1" || config.Type != "s3" {
		t.Fatalf("BuildRemoteConfig() config mismatch = %+v", config)
	}

	if config.Options["provider"] != "Other" ||
		config.Options["endpoint"] != "https://s3.example.com" ||
		config.Options["region"] != "ap-northeast-1" ||
		config.Options["access_key_id"] != "key" ||
		config.Options["secret_access_key"] != "secret" ||
		config.Options["env_auth"] != "false" ||
		config.Options["bucket"] != "team-bucket" {
		t.Fatalf("BuildRemoteConfig() options mismatch = %+v", config.Options)
	}
}

func TestGoogleDriveConnector_Capability(t *testing.T) {
	t.Parallel()

	capability := NewGoogleDriveConnector().Capability()
	if capability.Provider != ProviderGoogle {
		t.Fatalf("Provider = %q, want %q", capability.Provider, ProviderGoogle)
	}
	if capability.AuthScheme != "oauth" {
		t.Fatalf("AuthScheme = %q, want oauth", capability.AuthScheme)
	}
	if len(capability.Fields) != 2 {
		t.Fatalf("Fields len = %d, want 2", len(capability.Fields))
	}
}

func TestGoogleDriveConnector_BuildRemoteConfig_Success(t *testing.T) {
	t.Parallel()

	connector := NewGoogleDriveConnector()
	config, err := connector.BuildRemoteConfig(context.Background(), AccountConfig{
		AccountID: "google-1",
		Provider:  ProviderGoogle,
		Options: map[string]string{
			"root_folder_id": "root123",
			"shared_drive":   "drive123",
			"token":          "token-json",
		},
	})
	if err != nil {
		t.Fatalf("BuildRemoteConfig() error = %v", err)
	}

	if config.Name != "gdrive-google-1" {
		t.Fatalf("Name = %q, want gdrive-google-1", config.Name)
	}
	if config.Type != "drive" {
		t.Fatalf("Type = %q, want drive", config.Type)
	}
	if config.Options["scope"] != "drive" {
		t.Fatalf("scope = %q, want drive", config.Options["scope"])
	}
	if config.Options["root_folder_id"] != "root123" {
		t.Fatalf("root_folder_id = %q, want root123", config.Options["root_folder_id"])
	}
	if config.Options["team_drive"] != "drive123" {
		t.Fatalf("team_drive = %q, want drive123", config.Options["team_drive"])
	}
	if config.Options["token"] != "token-json" {
		t.Fatalf("token = %q, want token-json", config.Options["token"])
	}
}
