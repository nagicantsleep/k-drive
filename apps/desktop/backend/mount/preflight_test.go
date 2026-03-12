package mount

import (
	"errors"
	"os"
	"testing"
)

func TestRunPreflight_RcloneMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := runPreflight("nonexistent-rclone-binary-kdrive", dir)
	if err == nil {
		t.Fatal("expected preflight error for missing rclone, got nil")
	}

	var pe *PreflightError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PreflightError, got %T: %v", err, err)
	}
	if pe.Category != PreflightDependencyMissing {
		t.Fatalf("Category = %q, want %q", pe.Category, PreflightDependencyMissing)
	}
}

func TestRunPreflight_MountBaseDirNotWritable(t *testing.T) {
	t.Parallel()

	// Use a file path (not a directory) as the mount base — MkdirAll should fail.
	f, err := os.CreateTemp("", "kdrive-probe-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	// Point mountBaseDir to a path inside the temp file (impossible to create).
	impossibleDir := f.Name() + `\subdir`

	// rclone check will pass because cmd.exe exists on Windows; we only care about the path check.
	err = checkMountBaseDir(impossibleDir)
	if err == nil {
		t.Fatal("expected preflight error for unwritable mount dir, got nil")
	}

	var pe *PreflightError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PreflightError, got %T: %v", err, err)
	}
	if pe.Category != PreflightPathError {
		t.Fatalf("Category = %q, want %q", pe.Category, PreflightPathError)
	}
}

func TestProcessManager_Mount_PreflightRcloneMissing(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, "nonexistent-rclone-binary-kdrive")

	err := manager.Mount(nil, "acc-preflight", "test-remote", "") //nolint:staticcheck // intentional nil ctx for test
	if err == nil {
		t.Fatal("Mount() expected preflight error, got nil")
	}

	var pe *PreflightError
	if !errors.As(err, &pe) {
		t.Fatalf("Mount() error should be *PreflightError, got %T: %v", err, err)
	}
	if pe.Category != PreflightDependencyMissing {
		t.Fatalf("PreflightError.Category = %q, want %q", pe.Category, PreflightDependencyMissing)
	}

	status, statErr := manager.Status(nil, "acc-preflight") //nolint:staticcheck
	if statErr != nil {
		t.Fatalf("Status() error = %v", statErr)
	}
	if status.State != StateFailed {
		t.Fatalf("Status().State = %q, want %q", status.State, StateFailed)
	}
}
