package mount

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// PreflightCategory classifies why a preflight check failed.
type PreflightCategory string

const (
	PreflightDependencyMissing PreflightCategory = "dependency_missing"
	PreflightPathError         PreflightCategory = "path_error"
)

// PreflightError is returned when a dependency or environment check fails before
// a mount attempt is made. The Category field lets callers present targeted guidance.
type PreflightError struct {
	Category PreflightCategory
	Message  string
}

func (e *PreflightError) Error() string {
	return e.Message
}

func preflightCategory(err error) string {
	var pe *PreflightError
	if errors.As(err, &pe) {
		return string(pe.Category)
	}
	return ""
}

// runPreflight checks rclone availability, WinFsp (platform-specific), and mount
// base directory writability. Returns a *PreflightError on the first failure.
func runPreflight(rclonePath, mountBaseDir string) error {
	if err := checkRclone(rclonePath); err != nil {
		return err
	}
	if err := checkWinFsp(); err != nil {
		return err
	}
	if err := checkMountBaseDir(mountBaseDir); err != nil {
		return err
	}
	return nil
}

// checkRclone verifies the rclone binary is present and executable.
func checkRclone(rclonePath string) error {
	resolved, err := exec.LookPath(rclonePath)
	if err != nil {
		return &PreflightError{
			Category: PreflightDependencyMissing,
			Message:  fmt.Sprintf("rclone not found: install rclone and ensure it is on your PATH (looked for %q)", rclonePath),
		}
	}

	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() {
		return &PreflightError{
			Category: PreflightDependencyMissing,
			Message:  fmt.Sprintf("rclone binary not executable: %s", resolved),
		}
	}

	return nil
}

// checkMountBaseDir verifies the mount base directory can be created and written to.
func checkMountBaseDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &PreflightError{
			Category: PreflightPathError,
			Message:  fmt.Sprintf("cannot create mount base directory %q: %v", dir, err),
		}
	}

	probe := dir + `\.kdrive-probe`
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		return &PreflightError{
			Category: PreflightPathError,
			Message:  fmt.Sprintf("mount base directory %q is not writable: %v", dir, err),
		}
	}
	_ = os.Remove(probe)

	return nil
}
