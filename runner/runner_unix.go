//go:build !windows

package runner

import (
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
)

func trapSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func syscallSIGWINCH() os.Signal { return syscall.SIGWINCH }

func setProcessGroup(cmd *exec.Cmd) {
	// New process group so we can signal the whole tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killTree(p *os.Process) {
	if p == nil {
		return
	}
	// Send to the whole process group (negative pid).
	_ = syscall.Kill(-p.Pid, syscall.SIGTERM)
	time.AfterFunc(1*time.Second, func() { _ = syscall.Kill(-p.Pid, syscall.SIGKILL) })
}

// maybeStartWithPTY starts the command under a PTY.
// Returns (ptyMasterFile, true, nil) on success;
// Returns (nil, false, err) if starting under PTY failed;
// Callers may fall back to stdio.
func maybeStartWithPTY(cmd *exec.Cmd) (*os.File, bool, error) {
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, false, err
	}
	return f, true, nil
}

// inheritSizeFromStdin resizes the PTY to match our stdin terminal.
func inheritSizeFromStdin(f *os.File) {
	_ = pty.InheritSize(os.Stdin, f)
}

// terminateProcessTree best-effort kills cmd and its children on Unix.
// It does NOT call Wait() — caller is expected to be waiting elsewhere.
func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Try killing the *process group* first (requires Setpgid=true in setProcessGroup).
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil && pgid > 0 {
		// TERM first for graceful shutdown
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		time.Sleep(500 * time.Millisecond)
		// Ensure termination if still running
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return nil
	}

	// Fallback: signal only the main pid
	_ = cmd.Process.Signal(syscall.SIGTERM)
	time.Sleep(500 * time.Millisecond)
	_ = cmd.Process.Kill()
	return nil
}
