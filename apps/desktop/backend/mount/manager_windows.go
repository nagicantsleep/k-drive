//go:build windows

package mount

// DefaultMountBaseDir returns the default base directory for mounts on Windows.
func DefaultMountBaseDir() string {
	return `C:\KDrive`
}
