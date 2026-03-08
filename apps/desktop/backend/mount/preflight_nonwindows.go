//go:build !windows

package mount

// checkWinFsp is a no-op on non-Windows platforms.
func checkWinFsp() error {
	return nil
}
