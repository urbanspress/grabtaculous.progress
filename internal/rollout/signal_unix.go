//go:build !windows

package rollout

import (
	"fmt"
	"syscall"
)

// SendReloadSignal delivers POSIX SIGHUP to a live Grisp process for zero-downtime PreparedApp reload.
func SendReloadSignal(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID %d for SIGHUP", pid)
	}
	return syscall.Kill(pid, syscall.SIGHUP)
}
