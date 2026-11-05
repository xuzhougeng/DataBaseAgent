//go:build windows

package workbench

import (
	"os/exec"
	"syscall"
)

func hideCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
