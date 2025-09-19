//go:build windows

package runner

import "os"

func trapSignals() []os.Signal {
	// Windows: os.Interrupt is supported; there is no SIGTERM in std syscall.
	return []os.Signal{os.Interrupt}
}
