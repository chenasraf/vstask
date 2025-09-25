package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

// startAndWait starts cmd with PTY if interactive==true and allowed.
// Robust macOS-friendly fallback order:
//  1. PTY + current SysProcAttr
//  2. PTY + no SysProcAttr
//  3. stdio + no SysProcAttr
//  4. (if bash) stdio + no SysProcAttr + swap to /bin/sh
func startAndWait(ctx context.Context, cmd *exec.Cmd, interactive bool) (retErr error) {
	// Try PTY path first if permitted
	if interactive && canUsePTY() {
		// (1) PTY + current SysProcAttr
		if ptmx, ok, err := maybeStartWithPTY(cmd); err == nil && ok && ptmx != nil {
			return waitWithPTY(ctx, cmd, ptmx)
		} else if isExecPermissionError(err) {
			// (2) PTY + NO SysProcAttr
			clone := cloneCmdNoSysProc(cmd)
			if ptmx2, ok2, err2 := maybeStartWithPTY(clone); err2 == nil && ok2 && ptmx2 != nil {
				return waitWithPTY(ctx, clone, ptmx2)
			}
			// (3) stdio + NO SysProcAttr
			if err3 := startAndWaitStdio(ctx, clone); err3 == nil {
				return nil
			} else if shouldFallbackToSh(clone, err3) {
				// (4) stdio + NO SysProcAttr + swap to /bin/sh
				return startAndWaitStdio(ctx, rebuildWithSh(clone))
			} else {
				return err3
			}
		}
		// If PTY failed for any other reason, fall through to stdio with the original cmd.
	}

	// Stdio path (original cmd + current SysProcAttr)
	if err := startAndWaitStdio(ctx, cmd); err == nil {
		return nil
	}
	// Fallback: /bin/bash -> /bin/sh swap if appropriate
	if shouldFallbackToSh(cmd, retErr) {
		return startAndWaitStdio(ctx, rebuildWithSh(cmd))
	}
	return retErr
}

// startAndWaitStdio runs the command with plain stdio and cancel/kill logic.
func startAndWaitStdio(ctx context.Context, cmd *exec.Cmd) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		killTree(cmd.Process)
		select {
		case err := <-waitErr:
			return err
		case <-time.After(2 * time.Second):
			return errors.New("killed")
		}
	case err := <-waitErr:
		return err
	}
}

// waitWithPTY waits for an already-started PTY command and wires io + resize + raw mode.
// IMPORTANT: we DO NOT wait for the stdin->PTY copier to finish, to avoid
// needing an extra keypress after the child exits.
func waitWithPTY(ctx context.Context, cmd *exec.Cmd, ptmx *os.File) error {
	defer func() { _ = ptmx.Close() }()

	// Keep PTY sized to our terminal
	resize := func() { inheritSizeFromStdin(ptmx) }
	resize()

	winch := make(chan os.Signal, 1)
	if sig := syscallSIGWINCH(); sig != nil {
		signal.Notify(winch, sig)
		defer signal.Stop(winch)
		go func() {
			for range winch {
				resize()
			}
		}()
	}

	// Put local stdin into raw mode so Enter/^C/etc. pass through cleanly
	var oldState *term.State
	if term.IsTerminal(int(os.Stdin.Fd())) {
		if s, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
			oldState = s
			defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()
		}
	}

	// Pump I/O
	// stdin -> PTY (do NOT wait for this goroutine on exit)
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()

	// PTY -> stdout (we'll give this a brief chance to flush)
	outDone := make(chan struct{})
	go func() { _, _ = io.Copy(os.Stdout, ptmx); close(outDone) }()

	// Wait in a goroutine so we can cancel.
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		_ = ptmx.Close() // unblock io.Copy
		killTree(cmd.Process)
		select {
		case err := <-waitErr:
			return err
		case <-time.After(2 * time.Second):
			return errors.New("killed")
		}
	case err := <-waitErr:
		// Close PTY to stop output copier; don't wait for stdin copier (avoids extra Enter)
		_ = ptmx.Close()
		// give output copier a short moment to drain
		select {
		case <-outDone:
		case <-time.After(150 * time.Millisecond):
		}
		return err
	}
}

func canUsePTY() bool {
	if os.Getenv("VSTASK_DISABLE_PTY") == "1" {
		return false
	}
	if os.Getenv("VSTASK_FORCE_PTY") == "1" {
		return true
	}
	// Use PTY only when we have real TTYs on both ends.
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// shouldFallbackToSh decides whether to replace /bin/bash with /bin/sh and retry.
func shouldFallbackToSh(cmd *exec.Cmd, startErr error) bool {
	if startErr == nil {
		return false
	}
	// Only for bash-like paths
	path := cmd.Path
	if path == "" && len(cmd.Args) > 0 {
		path = cmd.Args[0]
	}
	if !strings.HasSuffix(path, "/bash") && !strings.HasSuffix(path, "bash") {
		return false
	}
	// Exec start errors that suggest policy/path issues
	return errors.Is(startErr, syscall.EPERM) ||
		errors.Is(startErr, syscall.EACCES) ||
		errors.Is(startErr, syscall.ENOENT) ||
		strings.Contains(strings.ToLower(startErr.Error()), "operation not permitted")
}

// rebuildWithSh reconstructs cmd to use /bin/sh while preserving args/env/cwd and SysProcAttr.
func rebuildWithSh(orig *exec.Cmd) *exec.Cmd {
	if orig == nil {
		return nil
	}
	args := append([]string(nil), orig.Args...) // first element is the executable
	if len(args) == 0 {
		return nil
	}
	// swap executable to /bin/sh, keep the rest of the args the same
	args[0] = "/bin/sh"
	c := exec.Command(args[0], args[1:]...)
	c.Dir = orig.Dir
	c.Env = orig.Env
	c.SysProcAttr = orig.SysProcAttr
	return c
}

// cloneCmdNoSysProc clones a command but drops SysProcAttr (helps on macOS with EPERM policies).
func cloneCmdNoSysProc(orig *exec.Cmd) *exec.Cmd {
	if orig == nil {
		return nil
	}
	c := exec.Command(orig.Path, orig.Args[1:]...)
	c.Dir = orig.Dir
	c.Env = orig.Env
	// intentionally do NOT copy SysProcAttr
	return c
}

func isExecPermissionError(err error) bool {
	if err == nil {
		return false
	}
	// Common blockers on macOS / restricted envs
	return errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.ENOENT) ||
		strings.Contains(strings.ToLower(err.Error()), "operation not permitted")
}
