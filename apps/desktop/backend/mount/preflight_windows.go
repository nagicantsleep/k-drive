//go:build windows

package mount

import (
	"fmt"
	"os"
	"path/filepath"
)

// checkFUSE verifies WinFsp is installed by checking for its DLL in the expected location.
func checkFUSE() error {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), `WinFsp\bin\winfsp-x64.dll`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `WinFsp\bin\winfsp-x86.dll`),
	}

	for _, path := range candidates {
		if path == `\WinFsp\bin\winfsp-x64.dll` || path == `\WinFsp\bin\winfsp-x86.dll` {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}

	return &PreflightError{
		Category: PreflightDependencyMissing,
		Message:  fmt.Sprintf("WinFsp is not installed: install WinFsp from https://winfsp.dev and retry"),
	}
}

func fuseDependencyStatus() DependencyStatus {
	return DependencyStatus{
		Name:       "WinFsp",
		Installed:  true,
		InstallURL: "https://winfsp.dev/rel/",
	}
}
