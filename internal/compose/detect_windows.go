//go:build windows

package compose

import (
	"runtime"
	"syscall"
	"unsafe"

	"grabtaculous/internal/safepath"
)

var procGlobalMemoryStatusEx = kernel32DLL.NewProc("GlobalMemoryStatusEx")

type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

func platformRootPath() string {
	return "."
}

func platformCPUAndRAM() (vcpus int, totalRAMMB uint64, availRAMMB uint64, err error) {
	vcpus = runtime.NumCPU()
	var st memoryStatusEx
	st.dwLength = uint32(unsafe.Sizeof(st))
	r1, _, callErr := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&st)))
	if r1 == 0 {
		if callErr == syscall.Errno(0) {
			callErr = nil
		}
		return 0, 0, 0, safepath.WrapError(
			safepath.ErrPreflightFailed,
			"unable to query host physical memory via GlobalMemoryStatusEx",
			"Supply --vcpus and --ram-mb overrides if running inside a restricted sandbox.",
			callErr,
		)
	}
	const bytesPerMB = uint64(1024 * 1024)
	return vcpus, st.ullTotalPhys / bytesPerMB, st.ullAvailPhys / bytesPerMB, nil
}
