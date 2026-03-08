package mount

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigManager_WriteRemoteDeleteRemote(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "rclone.conf")
	manager := NewConfigManagerAt(configPath)

	options := map[string]string{
		"endpoint":          "https://s3.example.com",
		"region":            "ap-northeast-1",
		"access_key_id":     "key",
		"secret_access_key": "secret",
		"env_auth":          "false",
		"provider":          "Other",
	}

	if err := manager.WriteRemote("s3-acc-1", "s3", options); err != nil {
		t.Fatalf("WriteRemote() error = %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)

	for _, expected := range []string{
		"[s3-acc-1]",
		"type = s3",
		"endpoint = https://s3.example.com",
		"region = ap-northeast-1",
		"access_key_id = key",
		"secret_access_key = secret",
	} {
		if !containsLine(content, expected) {
			t.Fatalf("WriteRemote() config missing line %q\ncontent:\n%s", expected, content)
		}
	}

	options2 := map[string]string{
		"endpoint": "https://other.example.com",
		"region":   "us-west-2",
		"env_auth": "true",
		"provider": "AWS",
	}
	if err := manager.WriteRemote("s3-acc-2", "s3", options2); err != nil {
		t.Fatalf("WriteRemote(second) error = %v", err)
	}

	data2, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(second) error = %v", err)
	}
	if !containsLine(string(data2), "[s3-acc-1]") || !containsLine(string(data2), "[s3-acc-2]") {
		t.Fatalf("WriteRemote(second) missing sections\ncontent:\n%s", string(data2))
	}

	if err := manager.DeleteRemote("s3-acc-1"); err != nil {
		t.Fatalf("DeleteRemote() error = %v", err)
	}

	data3, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(after delete) error = %v", err)
	}
	if containsLine(string(data3), "[s3-acc-1]") {
		t.Fatalf("DeleteRemote() did not remove s3-acc-1\ncontent:\n%s", string(data3))
	}
	if !containsLine(string(data3), "[s3-acc-2]") {
		t.Fatalf("DeleteRemote() removed s3-acc-2 unexpectedly\ncontent:\n%s", string(data3))
	}

	if err := manager.DeleteRemote("nonexistent"); err != nil {
		t.Fatalf("DeleteRemote(nonexistent) error = %v", err)
	}
}

func containsLine(content, expected string) bool {
	for _, line := range splitLines(content) {
		if line == expected {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	lines := []string{}
	current := ""
	for _, ch := range s {
		if ch == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
