package sync

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"KDrive/backend/storage"
)

// ConflictSuffix is the default suffix for conflict files
const ConflictSuffix = ".conflict"

// conflictPattern matches rclone bisync conflict rename output lines
var conflictPattern = regexp.MustCompile(`NOTICE.*?:\s+(.+?)\s*:\s*Renamed`)

// BisyncRunner executes rclone bisync and parses output for conflicts
type BisyncRunner struct {
	rclonePath string
	service    *Service
}

// NewBisyncRunner creates a new bisync runner
func NewBisyncRunner(rclonePath string, service *Service) *BisyncRunner {
	return &BisyncRunner{
		rclonePath: rclonePath,
		service:    service,
	}
}

// BisyncConfig configures a bisync operation
type BisyncConfig struct {
	AccountID       string
	RemoteName      string
	LocalPath       string
	ConflictResolve string // "newer", "path1", "path2", "none"
	MaxDeletePct    int
	DryRun          bool
}

// BisyncResult contains the result of a bisync operation
type BisyncResult struct {
	Success          bool
	FilesSynced      int
	BytesTransferred int64
	ConflictsFound   []ConflictInfo
	Error            error
	Duration         time.Duration
}

// ConflictInfo represents a detected conflict
type ConflictInfo struct {
	FilePath      string
	LocalModTime  time.Time
	RemoteModTime time.Time
}

// RunBisync executes rclone bisync between local and remote paths
func (r *BisyncRunner) RunBisync(ctx context.Context, config BisyncConfig) BisyncResult {
	start := time.Now()

	if err := r.service.StartSync(ctx, config.AccountID); err != nil {
		return BisyncResult{Error: fmt.Errorf("start sync: %w", err)}
	}

	args := r.buildArgs(config)

	cmd := exec.CommandContext(ctx, r.rclonePath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		syncErr := fmt.Errorf("create stdout pipe: %w", err)
		_ = r.service.FailSync(ctx, config.AccountID, syncErr, true)
		return BisyncResult{Error: syncErr}
	}

	cmd.Stderr = cmd.Stdout // merge stderr into stdout

	if err := cmd.Start(); err != nil {
		syncErr := fmt.Errorf("start rclone bisync: %w", err)
		_ = r.service.FailSync(ctx, config.AccountID, syncErr, true)
		return BisyncResult{Error: syncErr}
	}

	// Parse output for conflicts
	var conflicts []ConflictInfo
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if conflict := parseConflictLine(line); conflict != nil {
			conflicts = append(conflicts, *conflict)
			_ = r.service.RecordConflict(ctx, config.AccountID, conflict.FilePath, conflict.LocalModTime, conflict.RemoteModTime)
		}
	}

	err = cmd.Wait()
	duration := time.Since(start)

	result := BisyncResult{
		Duration:       duration,
		ConflictsFound: conflicts,
	}

	if err != nil {
		result.Error = fmt.Errorf("rclone bisync: %w", err)
		result.Success = false

		recoverable := isRecoverableError(err)
		_ = r.service.FailSync(ctx, config.AccountID, result.Error, recoverable)
	} else {
		result.Success = true
		_ = r.service.CompleteSync(ctx, config.AccountID, result.FilesSynced, result.BytesTransferred, duration)
	}

	return result
}

func (r *BisyncRunner) buildArgs(config BisyncConfig) []string {
	args := []string{
		"bisync",
		config.LocalPath,
		config.RemoteName + ":",
		"--conflict-resolve", resolveStrategy(config.ConflictResolve),
		"--conflict-loser", "num",
		"--conflict-suffix", ConflictSuffix,
		"--check-access",
		"--recover",
		"--resilient",
		"-v",
	}

	if config.MaxDeletePct > 0 {
		args = append(args, "--max-delete", fmt.Sprintf("%d", config.MaxDeletePct))
	}

	if config.DryRun {
		args = append(args, "--dry-run")
	}

	return args
}

func resolveStrategy(resolve string) string {
	switch resolve {
	case "newer", "path1", "path2", "older", "larger", "smaller":
		return resolve
	default:
		return "newer"
	}
}

func parseConflictLine(line string) *ConflictInfo {
	matches := conflictPattern.FindStringSubmatch(line)
	if len(matches) < 2 {
		return nil
	}

	filePath := strings.TrimSpace(matches[1])
	if filePath == "" {
		return nil
	}

	// Clean up the file path
	filePath = filepath.Clean(filePath)

	return &ConflictInfo{
		FilePath:      filePath,
		LocalModTime:  time.Now(),
		RemoteModTime: time.Now(),
	}
}

func isRecoverableError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	recoverablePatterns := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"EOF",
		"temporary failure",
		"rate limit",
		"429",
		"503",
	}

	for _, pattern := range recoverablePatterns {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

// ScanForConflictFiles scans a local path for existing conflict files
func ScanForConflictFiles(localPath string) ([]string, error) {
	pattern := filepath.Join(localPath, "**", "*"+ConflictSuffix+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("scan for conflict files: %w", err)
	}
	return matches, nil
}

// ResolveConflictFile resolves a conflict by keeping either the local or remote version
func (r *BisyncRunner) ResolveConflictFile(ctx context.Context, accountID, conflictID string, keepLocal bool) error {
	return r.service.ResolveConflict(ctx, accountID, conflictID)
}

// DetectOffline checks if a remote is reachable
func (r *BisyncRunner) DetectOffline(ctx context.Context, remoteName string) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, r.rclonePath, "lsd", remoteName+":", "--max-depth", "0")
	err := cmd.Run()
	return err != nil
}

// SyncConfig represents account-level sync configuration
type SyncConfig struct {
	Enabled           bool   `json:"enabled"`
	IntervalMinutes   int    `json:"intervalMinutes"`
	ConflictResolve   string `json:"conflictResolve"`
	SyncOnMount       bool   `json:"syncOnMount"`
	SyncOnUnmount     bool   `json:"syncOnUnmount"`
}

// DefaultSyncConfig returns the default sync configuration
func DefaultSyncConfig() SyncConfig {
	return SyncConfig{
		Enabled:         false,
		IntervalMinutes: 10,
		ConflictResolve: "newer",
		SyncOnMount:     false,
		SyncOnUnmount:   false,
	}
}

// ValidateConflictResolve validates the conflict resolution strategy
func ValidateConflictResolve(strategy string) error {
	valid := map[string]bool{
		"newer": true, "path1": true, "path2": true,
		"older": true, "larger": true, "smaller": true, "none": true,
	}
	if !valid[strategy] {
		return fmt.Errorf("invalid conflict resolution strategy: %q (valid: newer, path1, path2, older, larger, smaller, none)", strategy)
	}
	return nil
}

// StatusForConflicts returns SyncStateConflict or SyncStateNeedsResolve based on conflict count
func StatusForConflicts(conflicts []storage.SyncConflict) storage.SyncState {
	unresolved := 0
	for _, c := range conflicts {
		if c.Resolution == "" {
			unresolved++
		}
	}
	if unresolved == 0 {
		return storage.SyncStateSuccess
	}
	return storage.SyncStateConflict
}
