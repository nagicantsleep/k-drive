package mount

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"KDrive/backend/connectors"
)

type State string

const (
	StateStopped  State = "stopped"
	StateMounting State = "mounting"
	StateMounted  State = "mounted"
	StateFailed   State = "failed"
)

type Status struct {
	AccountID string
	State     State
	LastError string
}

type Manager interface {
	WriteConfig(remote connectors.RemoteConfig) error
	DeleteConfig(remoteName string) error
	Mount(ctx context.Context, accountID string) error
	Unmount(ctx context.Context, accountID string) error
	Status(ctx context.Context, accountID string) (Status, error)
}

type mountEntry struct {
	state     State
	lastError string
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	done      chan struct{}
}

type ProcessManager struct {
	mu            sync.Mutex
	entries       map[string]*mountEntry
	configMgr     *ConfigManager
	rclonePath    string
	mountBaseDir  string
	onStateChange func(accountID string, state State, lastError string, mountErr error)
}

type ProcessManagerConfig struct {
	ConfigManager *ConfigManager
	RclonePath    string
	MountBaseDir  string
	OnStateChange func(accountID string, state State, lastError string, mountErr error)
}

func NewManager() Manager {
	return newProcessManager(ProcessManagerConfig{
		ConfigManager: NewConfigManager(),
		RclonePath:    "rclone",
		MountBaseDir:  DefaultMountBaseDir(),
	})
}

func NewManagerWithConfig(cfg ProcessManagerConfig) Manager {
	return newProcessManager(cfg)
}

func newProcessManager(cfg ProcessManagerConfig) *ProcessManager {
	rclonePath := cfg.RclonePath
	if rclonePath == "" {
		rclonePath = "rclone"
	}

	return &ProcessManager{
		entries:       make(map[string]*mountEntry),
		configMgr:     cfg.ConfigManager,
		rclonePath:    rclonePath,
		mountBaseDir:  cfg.MountBaseDir,
		onStateChange: cfg.OnStateChange,
	}
}

func (m *ProcessManager) WriteConfig(remote connectors.RemoteConfig) error {
	return m.configMgr.WriteRemote(remote.Name, remote.Type, remote.Options)
}

func (m *ProcessManager) DeleteConfig(remoteName string) error {
	return m.configMgr.DeleteRemote(remoteName)
}

func (m *ProcessManager) Mount(ctx context.Context, accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.entries[accountID]; ok {
		if entry.state == StateMounting || entry.state == StateMounted {
			return nil
		}
		// Wait for any in-flight stop to complete before remounting.
		if entry.done != nil {
			done := entry.done
			m.mu.Unlock()
			select {
			case <-done:
			case <-ctx.Done():
				m.mu.Lock()
				return ctx.Err()
			}
			m.mu.Lock()
		}
	}

	if err := runPreflight(m.rclonePath, m.mountBaseDir); err != nil {
		entry := &mountEntry{state: StateFailed, lastError: err.Error()}
		m.entries[accountID] = entry
		m.notifyStateChange(accountID, StateFailed, entry.lastError, err)
		return err
	}

	mountPoint := mountPointPath(m.mountBaseDir, accountID)
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		return fmt.Errorf("create mount point: %w", err)
	}

	remoteName := fmt.Sprintf("s3-%s", accountID)

	cmdCtx, cancel := context.WithCancel(context.Background())

	args := []string{
		"mount",
		fmt.Sprintf("%s:", remoteName),
		mountPoint,
		"--config", m.configMgr.ConfigPath(),
		"--vfs-cache-mode", "writes",
	}

	cmd := exec.CommandContext(cmdCtx, m.rclonePath, args...)

	done := make(chan struct{})
	entry := &mountEntry{
		state:  StateMounting,
		cmd:    cmd,
		cancel: cancel,
		done:   done,
	}
	m.entries[accountID] = entry
	m.notifyStateChange(accountID, StateMounting, "", nil)

	if err := cmd.Start(); err != nil {
		cancel()
		close(done)
		entry.state = StateFailed
		entry.lastError = err.Error()
		startErr := fmt.Errorf("start rclone mount: %w", err)
		m.notifyStateChange(accountID, StateFailed, entry.lastError, startErr)
		return startErr
	}

	go m.watchProcess(accountID, entry, cmd, done)

	return nil
}

// watchProcess waits for rclone to either stabilize or exit.
// It uses a short probe window: if the process is still running after the window, it is considered mounted.
func (m *ProcessManager) watchProcess(accountID string, entry *mountEntry, cmd *exec.Cmd, done chan struct{}) {
	defer close(done)

	// Channel receives the process exit error (or nil) when Wait returns.
	exitCh := make(chan error, 1)
	go func() {
		exitCh <- cmd.Wait()
	}()

	// Probe window: if still running after 1s, declare mounted.
	probeTimer := time.NewTimer(1 * time.Second)
	defer probeTimer.Stop()

	select {
	case err := <-exitCh:
		// Process exited before probe window — treat as failure.
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.entries[accountID] != entry {
			return
		}
		if entry.state == StateStopped {
			return
		}
		if err != nil {
			entry.state = StateFailed
			entry.lastError = err.Error()
		} else {
			entry.state = StateFailed
			entry.lastError = "rclone exited unexpectedly"
		}
		m.notifyStateChange(accountID, entry.state, entry.lastError, nil)
		return

	case <-probeTimer.C:
		// Process survived probe window — mark mounted.
		m.mu.Lock()
		if m.entries[accountID] == entry && entry.state == StateMounting {
			entry.state = StateMounted
			m.notifyStateChange(accountID, StateMounted, "", nil)
		}
		m.mu.Unlock()
	}

	// Now wait for eventual process exit (stopped or failed).
	err := <-exitCh

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.entries[accountID] != entry {
		return
	}
	if entry.state == StateStopped {
		return
	}
	if err != nil {
		entry.state = StateFailed
		entry.lastError = err.Error()
	} else {
		entry.state = StateStopped
	}
	m.notifyStateChange(accountID, entry.state, entry.lastError, nil)
}

func (m *ProcessManager) Unmount(_ context.Context, accountID string) error {
	m.mu.Lock()

	entry, ok := m.entries[accountID]
	if !ok {
		m.mu.Unlock()
		return nil
	}

	entry.state = StateStopped
	entry.cancel()
	done := entry.done
	m.notifyStateChange(accountID, StateStopped, "", nil)
	m.mu.Unlock()

	// Wait (bounded) for process to exit.
	if done != nil {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		}
	}

	return nil
}

func (m *ProcessManager) Status(_ context.Context, accountID string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[accountID]
	if !ok {
		return Status{AccountID: accountID, State: StateStopped}, nil
	}

	return Status{
		AccountID: accountID,
		State:     entry.state,
		LastError: entry.lastError,
	}, nil
}

func (m *ProcessManager) notifyStateChange(accountID string, state State, lastError string, mountErr error) {
	if m.onStateChange != nil {
		m.onStateChange(accountID, state, lastError, mountErr)
	}
}

func DefaultMountBaseDir() string {
	return `C:\KDrive`
}

func mountPointPath(baseDir, accountID string) string {
	return fmt.Sprintf(`%s\%s`, baseDir, accountID)
}
