//go:build windows

package runner

import (
	"fmt"
	"os"
	"os/exec"
)

func killTree(p *os.Process) {
	if p == nil {
		return
	}
	// Best-effort kill process tree on Windows.
	_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", p.Pid)).Run()
}
