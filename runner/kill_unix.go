//go:build !windows

package runner

import (
	"os"
	"syscall"
	"time"
)

func killTree(p *os.Process) {
	if p == nil {
		return
	}
	// Send to the whole process group (negative pid).
	_ = syscall.Kill(-p.Pid, syscall.SIGTERM)
	time.AfterFunc(1*time.Second, func() { _ = syscall.Kill(-p.Pid, syscall.SIGKILL) })
}
