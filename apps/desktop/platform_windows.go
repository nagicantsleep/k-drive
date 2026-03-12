//go:build windows

package main

import "os/exec"

func openInFileExplorer(path string) error {
	return exec.Command("explorer.exe", path).Start()
}
