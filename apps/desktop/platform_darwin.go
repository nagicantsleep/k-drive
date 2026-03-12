//go:build darwin

package main

import "os/exec"

func openInFileExplorer(path string) error {
	return exec.Command("open", path).Start()
}
