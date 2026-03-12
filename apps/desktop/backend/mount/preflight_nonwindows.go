//go:build !windows && !darwin

package mount

// checkFUSE is a no-op on unsupported platforms (e.g. Linux).
func checkFUSE() error {
	return nil
}

func fuseDependencyStatus() DependencyStatus {
	return DependencyStatus{
		Name:      "FUSE",
		Installed: true,
	}
}
