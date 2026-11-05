//go:build windows

package workbench

import (
	"os/exec"
	"syscall"
)

func hideCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func browserCommand(link string) *exec.Cmd {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", link)
}
