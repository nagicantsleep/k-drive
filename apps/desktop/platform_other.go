//go:build !windows && !darwin

package main

import "os/exec"

func openInFileExplorer(path string) error {
	return exec.Command("xdg-open", path).Start()
}
