package mount

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessManager_StatusDefaultStopped(t *testing.T) {
	t.Parallel()

	manager := newProcessManager(ProcessManagerConfig{
		ConfigManager: NewConfigManagerAt(filepath.Join(t.TempDir(), "rclone.conf")),
		RclonePath:    `C:\WINDOWS\system32\cmd.exe`,
		MountBaseDir:  t.TempDir(),
	})

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

	manager := newProcessManager(ProcessManagerConfig{
		ConfigManager: NewConfigManagerAt(filepath.Join(t.TempDir(), "rclone.conf")),
		RclonePath:    `C:\WINDOWS\system32\cmd.exe`,
		MountBaseDir:  t.TempDir(),
	})

	if err := manager.Mount(context.Background(), "acc-1"); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}

	time.Sleep(800 * time.Millisecond)

	status, err := manager.Status(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("Status(after mount) error = %v", err)
	}
	// cmd.exe exits quickly since no real mount happens; state will be stopped/failed/mounted.
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

	manager := newProcessManager(ProcessManagerConfig{
		ConfigManager: NewConfigManagerAt(filepath.Join(t.TempDir(), "rclone.conf")),
		RclonePath:    `nonexistent-binary-kdrive-test`,
		MountBaseDir:  t.TempDir(),
	})

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
