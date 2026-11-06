//go:build !windows

package workbench

import (
	"os/exec"
	"runtime"
)

func hideCommand(cmd *exec.Cmd) {}

func browserCommand(link string) *exec.Cmd {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", link)
	}
	return exec.Command("xdg-open", link)
}
