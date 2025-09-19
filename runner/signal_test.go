//go:build !windows
// +build !windows

package runner

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestKillTreeKillsProcessGroup starts a helper that spawns a child,
// then uses killTree to terminate the parent's *process group* and
// verifies the child is also gone.
func TestKillTreeKillsProcessGroup(t *testing.T) {
	helperPath := buildSignalHelper(t)

	// Start helper in its own process group so killTree(-pgid) targets the group.
	cmd := exec.Command(helperPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// Read CHILD pid line
	childPID, err := waitForChildPID(&out, 2*time.Second, cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("failed to read child PID: %v\nstdout/stderr:\n%s", err, out.String())
	}

	// Give the processes a moment to settle.
	time.Sleep(50 * time.Millisecond)

	// Kill the whole process group via killTree.
	killTree(cmd.Process)

	// The parent should exit shortly.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// The helper exits because its child exits (or due to group signal).
		_ = err // may be *exec.ExitError; we only care that it's done
	case <-time.After(2 * time.Second):
		// Try a hard kill to avoid leaking processes on failure.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatalf("helper did not exit after killTree")
	}

	// Verify the child process is gone (poll for ESRCH up to ~3s).
	gone := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(childPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			gone = true
			break
		}
		// If we get EPERM or nil, the process may still exist or be a zombie; wait a bit and retry.
		time.Sleep(25 * time.Millisecond)
	}
	if !gone {
		t.Fatalf("child PID %d still exists after killTree", childPID)
	}
}

// buildSignalHelper writes and builds a tiny program that spawns a long-running child
// and prints "CHILD=<pid>" to stdout, then waits for the child (exits when killed).

func buildSignalHelper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "helper.go")
	bin := filepath.Join(dir, "helper_bin")

	code := `package main

import (
    "fmt"
    "os"
    "os/exec"
    "os/signal"
    "runtime"
    "syscall"
    "time"
)

func main() {
    if runtime.GOOS == "windows" {
        time.Sleep(60 * time.Second)
        return
    }
    // Start a long-lived child in the SAME process group.
    child := exec.Command("/bin/sh", "-c", "sleep 60")
    if err := child.Start(); err != nil {
        fmt.Printf("ERR: %v\n", err)
        os.Exit(1)
    }
    fmt.Printf("CHILD=%d\n", child.Process.Pid)
    _ = os.Stdout.Sync()

    // Best-effort: on SIGTERM/INT, forward to child, then reap.
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGTERM, os.Interrupt)
    go func() {
        <-sigs
        // Forward SIGTERM; ignore error if already exiting.
        _ = child.Process.Signal(syscall.SIGTERM)
    }()

    // Wait until killed or signalled.
    _ = child.Wait()
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	build := exec.Command("go", "build", "-o", bin, src)
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("go build helper: %v", err)
	}
	return bin
}

func waitForChildPID(buf *bytes.Buffer, timeout time.Duration, cmd *exec.Cmd) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		lines := strings.SplitSeq(buf.String(), "\n")
		for ln := range lines {
			ln = strings.TrimSpace(ln)
			if after, ok := strings.CutPrefix(ln, "CHILD="); ok {
				p := after
				pid, err := strconv.Atoi(p)
				if err != nil {
					return 0, fmt.Errorf("bad CHILD pid line %q: %w", ln, err)
				}
				return pid, nil
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	// If the helper already exited, return its output to aid debugging.
	_ = cmd.Process.Kill()
	return 0, fmt.Errorf("timeout waiting for CHILD pid; output:\n%s", buf.String())
}
