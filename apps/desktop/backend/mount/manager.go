package mount

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
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
	Mount(ctx context.Context, accountID string) error
	Unmount(ctx context.Context, accountID string) error
	Status(ctx context.Context, accountID string) (Status, error)
}

type mountEntry struct {
	state     State
	lastError string
	cmd       *exec.Cmd
	cancel    context.CancelFunc
}

type ProcessManager struct {
	mu           sync.RWMutex
	entries      map[string]*mountEntry
	configMgr    *ConfigManager
	rclonePath   string
	mountBaseDir string
}

type ProcessManagerConfig struct {
	ConfigManager *ConfigManager
	RclonePath    string
	MountBaseDir  string
}

func NewManager() Manager {
	return newProcessManager(ProcessManagerConfig{
		ConfigManager: NewConfigManager(),
		RclonePath:    "rclone",
		MountBaseDir:  defaultMountBaseDir(),
	})
}

func newProcessManager(cfg ProcessManagerConfig) *ProcessManager {
	rclonePath := cfg.RclonePath
	if rclonePath == "" {
		rclonePath = "rclone"
	}

	return &ProcessManager{
		entries:      make(map[string]*mountEntry),
		configMgr:    cfg.ConfigManager,
		rclonePath:   rclonePath,
		mountBaseDir: cfg.MountBaseDir,
	}
}

func (m *ProcessManager) Mount(ctx context.Context, accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.entries[accountID]; ok {
		if entry.state == StateMounting || entry.state == StateMounted {
			return nil
		}
	}

	mountPoint := mountPointPath(m.mountBaseDir, accountID)
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

	entry := &mountEntry{
		state:  StateMounting,
		cmd:    cmd,
		cancel: cancel,
	}
	m.entries[accountID] = entry

	if err := cmd.Start(); err != nil {
		cancel()
		entry.state = StateFailed
		entry.lastError = err.Error()
		return fmt.Errorf("start rclone mount: %w", err)
	}

	go m.watchProcess(accountID, entry, cmd)

	return nil
}

func (m *ProcessManager) watchProcess(accountID string, entry *mountEntry, cmd *exec.Cmd) {
	// Allow brief startup window before declaring mounted.
	time.Sleep(500 * time.Millisecond)

	m.mu.Lock()
	if m.entries[accountID] == entry && entry.state == StateMounting {
		entry.state = StateMounted
	}
	m.mu.Unlock()

	// Block on process exit.
	err := cmd.Wait()

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
}

func (m *ProcessManager) Unmount(_ context.Context, accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[accountID]
	if !ok {
		return nil
	}

	entry.state = StateStopped
	entry.cancel()

	return nil
}

func (m *ProcessManager) Status(_ context.Context, accountID string) (Status, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

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

func defaultMountBaseDir() string {
	return `C:\KDrive`
}

func mountPointPath(baseDir, accountID string) string {
	return fmt.Sprintf("%s\\%s", baseDir, accountID)
}
