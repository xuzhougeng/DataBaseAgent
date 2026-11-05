//go:build !windows

package workbench

import "os/exec"

func hideCommand(cmd *exec.Cmd) {}
