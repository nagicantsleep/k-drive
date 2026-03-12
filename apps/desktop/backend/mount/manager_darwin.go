//go:build darwin

package mount

import "path/filepath"
import "os"

// DefaultMountBaseDir returns the default base directory for mounts on macOS.
func DefaultMountBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/KDrive"
	}
	return filepath.Join(home, "KDrive")
}
