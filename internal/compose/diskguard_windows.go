//go:build windows

package compose

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32DLL         = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeExW  = kernel32DLL.NewProc("GetDiskFreeSpaceExW")
)

// PlatformDiskStats queries filesystem free space via GetDiskFreeSpaceExW on Windows.
func PlatformDiskStats(path string) (DiskStats, error) {
	probe := existingProbeDir(path)
	ptr, err := syscall.UTF16PtrFromString(probe)
	if err != nil {
		return DiskStats{}, err
	}
	var freeBytesAvail, totalBytes, totalFreeBytes uint64
	r1, _, callErr := procGetDiskFreeExW.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeBytesAvail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if r1 == 0 {
		return DiskStats{}, callErr
	}
	used := int64(totalBytes - totalFreeBytes)
	pct := 0.0
	if totalBytes > 0 {
		pct = (float64(totalBytes-freeBytesAvail) / float64(totalBytes)) * 100.0
	}
	return DiskStats{
		Path:           probe,
		TotalBytes:     int64(totalBytes),
		FreeBytes:      int64(totalFreeBytes),
		AvailableBytes: int64(freeBytesAvail),
		UsedBytes:      used,
		UsedPercent:    pct,
		FreeGB:         float64(freeBytesAvail) / float64(1024*1024*1024),
	}, nil
}

func existingProbeDir(path string) string {
	cur := path
	for {
		if st, err := os.Stat(cur); err == nil && st.IsDir() {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "."
		}
		cur = parent
	}
}
