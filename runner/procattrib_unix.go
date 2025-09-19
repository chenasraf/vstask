//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	// New process group so we can signal the whole tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
