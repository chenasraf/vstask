//go:build windows

package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func trapSignals() []os.Signal {
	// Windows: os.Interrupt is supported; there is no SIGTERM in std syscall.
	return []os.Signal{os.Interrupt}
}

func setProcessGroup(cmd *exec.Cmd) {
	// Nothing to do on Windows here.
}

func killTree(p *os.Process) {
	if p == nil {
		return
	}
	// Best-effort kill process tree on Windows.
	_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", p.Pid)).Run()
}

func syscallSIGWINCH() os.Signal { return nil }

// ---- PTY helpers (Windows: none) ----
func maybeStartWithPTY(cmd *exec.Cmd) (*os.File, bool, error) {
	return nil, false, errors.New("pty not available on windows")
}
func inheritSizeFromStdin(_ *os.File) {}
