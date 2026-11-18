//go:build !windows

package compose

import (
	"os"
	"runtime"

	"grabtaculous/internal/safepath"
)

func platformRootPath() string {
	return "/"
}

func platformCPUAndRAM() (vcpus int, totalRAMMB uint64, availRAMMB uint64, err error) {
	vcpus = runtime.NumCPU()
	if cpuRaw, readErr := os.ReadFile("/proc/cpuinfo"); readErr == nil {
		if parsed := ParseProcCPUInfo(string(cpuRaw)); parsed > 0 {
			vcpus = parsed
		}
	}

	memRaw, readErr := os.ReadFile("/proc/meminfo")
	if readErr != nil {
		return 0, 0, 0, safepath.WrapError(
			safepath.ErrPreflightFailed,
			"unable to read /proc/meminfo for host RAM detection",
			"Run on Linux or supply --vcpus and --ram-mb overrides.",
			readErr,
		)
	}
	totalRAMMB, availRAMMB, err = ParseProcMeminfo(string(memRaw))
	if err != nil {
		return 0, 0, 0, err
	}
	return vcpus, totalRAMMB, availRAMMB, nil
}
