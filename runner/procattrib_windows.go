//go:build windows

package runner

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {
	// Nothing to do on Windows here.
}
