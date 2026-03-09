package mount

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestManager(t *testing.T, rclonePath string) *ProcessManager {
	t.Helper()
	return newProcessManager(ProcessManagerConfig{
		ConfigManager: NewConfigManagerAt(filepath.Join(t.TempDir(), "rclone.conf")),
		RclonePath:    rclonePath,
		MountBaseDir:  t.TempDir(),
	})
}

func TestProcessManager_StatusDefaultStopped(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, `C:\WINDOWS\system32\cmd.exe`)

	status, err := manager.Status(context.Background(), "acc-nonexistent")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State != StateStopped {
		t.Fatalf("Status().State = %q, want %q", status.State, StateStopped)
	}
}

func TestProcessManager_MountUnmount_StateTransitions(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, `C:\WINDOWS\system32\cmd.exe`)

	if err := manager.Mount(context.Background(), "acc-1"); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}

	// cmd.exe exits quickly — it should land on failed (exited before probe window).
	time.Sleep(1500 * time.Millisecond)

	status, err := manager.Status(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("Status(after mount) error = %v", err)
	}
	if status.AccountID != "acc-1" {
		t.Fatalf("Status().AccountID = %q, want acc-1", status.AccountID)
	}

	if err := manager.Unmount(context.Background(), "acc-1"); err != nil {
		t.Fatalf("Unmount() error = %v", err)
	}

	status2, err := manager.Status(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("Status(after unmount) error = %v", err)
	}
	if status2.State != StateStopped {
		t.Fatalf("Status(after unmount).State = %q, want %q", status2.State, StateStopped)
	}
}

func TestProcessManager_MountFailure_BadBinary(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, `nonexistent-binary-kdrive-test`)

	err := manager.Mount(context.Background(), "acc-1")
	if err == nil {
		t.Fatalf("Mount(bad binary) expected error, got nil")
	}

	status, statErr := manager.Status(context.Background(), "acc-1")
	if statErr != nil {
		t.Fatalf("Status() error = %v", statErr)
	}
	if status.State != StateFailed {
		t.Fatalf("Status().State = %q, want %q", status.State, StateFailed)
	}
	if status.LastError == "" {
		t.Fatalf("Status().LastError is empty, want non-empty")
	}
}

func TestProcessManager_FastExit_MarkedFailed(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, `C:\WINDOWS\system32\cmd.exe`)

	// cmd.exe exits nearly immediately — should end up failed, not mounted.
	if err := manager.Mount(context.Background(), "acc-1"); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}

	// Wait enough for the probe window + process exit to resolve.
	time.Sleep(1500 * time.Millisecond)

	status, err := manager.Status(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State == StateMounted {
		t.Fatalf("Status().State = %q after fast exit, want stopped or failed", status.State)
	}
}

func TestProcessManager_UnmountAfterPreflightFailure_SafeStoppedState(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, `nonexistent-binary-kdrive-test`)

	err := manager.Mount(context.Background(), "acc-1")
	if err == nil {
		t.Fatal("Mount() expected preflight error, got nil")
	}

	if unmountErr := manager.Unmount(context.Background(), "acc-1"); unmountErr != nil {
		t.Fatalf("Unmount() error = %v", unmountErr)
	}

	status, statErr := manager.Status(context.Background(), "acc-1")
	if statErr != nil {
		t.Fatalf("Status() error = %v", statErr)
	}
	if status.State != StateStopped {
		t.Fatalf("Status().State = %q, want %q", status.State, StateStopped)
	}
	if status.LastError != "" {
		t.Fatalf("Status().LastError = %q, want empty", status.LastError)
	}
	if status.ErrorCategory != "" {
		t.Fatalf("Status().ErrorCategory = %q, want empty", status.ErrorCategory)
	}
}
