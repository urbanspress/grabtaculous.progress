//go:build windows

package rollout

import "fmt"

// SendReloadSignal reports that POSIX SIGHUP is unavailable on Windows; supervised generation
// replacement (--reload / worker swap) should be used instead.
func SendReloadSignal(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID %d", pid)
	}
	return fmt.Errorf("POSIX SIGHUP is not supported on Windows for PID %d; use rolling worker generation swap or --reload", pid)
}
