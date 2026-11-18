//go:build !windows

package compose

import (
	"os"
	"path/filepath"
	"syscall"
)

// PlatformDiskStats queries filesystem free space via syscall.Statfs on Unix/Linux/macOS.
func PlatformDiskStats(path string) (DiskStats, error) {
	probe := existingProbeDir(path)
	var fs syscall.Statfs_t
	if err := syscall.Statfs(probe, &fs); err != nil {
		return DiskStats{}, err
	}
	bsize := int64(fs.Bsize)
	total := int64(fs.Blocks) * bsize
	free := int64(fs.Bfree) * bsize
	avail := int64(fs.Bavail) * bsize
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = (float64(total-avail) / float64(total)) * 100.0
	}
	return DiskStats{
		Path:           probe,
		TotalBytes:     total,
		FreeBytes:      free,
		AvailableBytes: avail,
		UsedBytes:      used,
		UsedPercent:    pct,
		FreeGB:         float64(avail) / float64(1024*1024*1024),
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
