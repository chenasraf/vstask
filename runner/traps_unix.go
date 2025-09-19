//go:build !windows

package runner

import (
	"os"
	"syscall"
)

func trapSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
