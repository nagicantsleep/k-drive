package sync

import (
	"testing"

	"KDrive/backend/storage"
)

func TestParseConflictLine_Match(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		found bool
	}{
		{
			name:  "rclone conflict rename",
			line:  "NOTICE: path/to/file.txt: Renamed to file.txt.conflict1",
			found: true,
		},
		{
			name:  "non-conflict line",
			line:  "INFO: Transferred 5 files",
			found: false,
		},
		{
			name:  "empty line",
			line:  "",
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conflict := parseConflictLine(tt.line)
			if tt.found && conflict == nil {
				t.Error("expected conflict info, got nil")
			}
			if !tt.found && conflict != nil {
				t.Errorf("expected no conflict, got %+v", conflict)
			}
		})
	}
}

func TestResolveStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"newer", "newer"},
		{"path1", "path1"},
		{"path2", "path2"},
		{"older", "older"},
		{"larger", "larger"},
		{"smaller", "smaller"},
		{"", "newer"},
		{"invalid", "newer"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := resolveStrategy(tt.input)
			if got != tt.expected {
				t.Errorf("resolveStrategy(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsRecoverableError_Patterns(t *testing.T) {
	tests := []struct {
		errMsg      string
		recoverable bool
	}{
		{"connection refused", true},
		{"timeout occurred", true},
		{"rate limit exceeded", true},
		{"503 service unavailable", true},
		{"invalid config", false},
		{"permission denied", false},
	}

	for _, tt := range tests {
		t.Run(tt.errMsg, func(t *testing.T) {
			err := &testError{msg: tt.errMsg}
			got := isRecoverableError(err)
			if got != tt.recoverable {
				t.Errorf("isRecoverableError(%q) = %v, want %v", tt.errMsg, got, tt.recoverable)
			}
		})
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string { return e.msg }

func TestValidateConflictResolve(t *testing.T) {
	valid := []string{"newer", "path1", "path2", "older", "larger", "smaller", "none"}
	for _, v := range valid {
		if err := ValidateConflictResolve(v); err != nil {
			t.Errorf("ValidateConflictResolve(%q) returned error: %v", v, err)
		}
	}

	if err := ValidateConflictResolve("invalid"); err == nil {
		t.Error("expected error for invalid strategy")
	}
}

func TestStatusForConflicts(t *testing.T) {
	// No conflicts -> success
	status := StatusForConflicts(nil)
	if status != storage.SyncStateSuccess {
		t.Errorf("expected %s, got %s", storage.SyncStateSuccess, status)
	}

	// All resolved -> success
	resolved := []storage.SyncConflict{
		{ID: "1", Resolution: "kept_local"},
	}
	status = StatusForConflicts(resolved)
	if status != storage.SyncStateSuccess {
		t.Errorf("expected %s for resolved conflicts, got %s", storage.SyncStateSuccess, status)
	}

	// Unresolved -> conflict
	unresolved := []storage.SyncConflict{
		{ID: "1", Resolution: ""},
	}
	status = StatusForConflicts(unresolved)
	if status != storage.SyncStateConflict {
		t.Errorf("expected %s for unresolved conflicts, got %s", storage.SyncStateConflict, status)
	}
}

func TestDefaultSyncConfig(t *testing.T) {
	config := DefaultSyncConfig()
	if config.Enabled {
		t.Error("default config should not be enabled")
	}
	if config.IntervalMinutes != 10 {
		t.Errorf("expected interval 10, got %d", config.IntervalMinutes)
	}
	if config.ConflictResolve != "newer" {
		t.Errorf("expected conflict resolve 'newer', got %q", config.ConflictResolve)
	}
}

func TestBuildArgs(t *testing.T) {
	runner := &BisyncRunner{rclonePath: "rclone"}
	config := BisyncConfig{
		AccountID:       "test",
		RemoteName:      "remote",
		LocalPath:       "/tmp/mount",
		ConflictResolve: "newer",
		MaxDeletePct:    75,
		DryRun:          true,
	}

	args := runner.buildArgs(config)

	// Check key arguments
	found := map[string]bool{}
	for _, arg := range args {
		found[arg] = true
	}

	if !found["bisync"] {
		t.Error("expected 'bisync' in args")
	}
	if !found["--dry-run"] {
		t.Error("expected '--dry-run' in args")
	}
	if !found["--recover"] {
		t.Error("expected '--recover' in args")
	}
	if !found["--resilient"] {
		t.Error("expected '--resilient' in args")
	}
}
