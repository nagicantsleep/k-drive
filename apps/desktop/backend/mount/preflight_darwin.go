//go:build darwin

package mount

import (
	"fmt"
	"os"
)

// checkFUSE verifies macFUSE is installed by checking for known filesystem paths.
func checkFUSE() error {
	candidates := []string{
		"/Library/Filesystems/macfuse.fs",
		"/usr/local/lib/libfuse.dylib",
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}

	return &PreflightError{
		Category: PreflightDependencyMissing,
		Message:  fmt.Sprintf("macFUSE is not installed: install macFUSE from https://osxfuse.github.io and retry"),
	}
}

func fuseDependencyStatus() DependencyStatus {
	return DependencyStatus{
		Name:       "macFUSE",
		Installed:  true,
		InstallURL: "https://osxfuse.github.io",
	}
}
