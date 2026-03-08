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
