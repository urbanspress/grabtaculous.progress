package compose

import (
	"fmt"
	"path/filepath"
	"strings"

	"grabtaculous/internal/safepath"
)

const (
	// MinFreeNVMeBytes enforces a strict >= 15 GB NVMe free-space safety reserve.
	MinFreeNVMeBytes int64 = 15 * 1024 * 1024 * 1024
	// MaxDiskUsedPercent refuses operations that would push disk utilization past 92%.
	MaxDiskUsedPercent float64 = 92.0
)

// DiskStats describes filesystem capacity and free space for an NVMe/host mount.
type DiskStats struct {
	Path           string  `json:"path"`
	TotalBytes     int64   `json:"total_bytes"`
	FreeBytes      int64   `json:"free_bytes"`
	AvailableBytes int64   `json:"available_bytes"`
	UsedBytes      int64   `json:"used_bytes"`
	UsedPercent    float64 `json:"used_percent"`
	FreeGB         float64 `json:"free_gb"`
}

// DiskStatFunc allows deterministic testing of disk reserve checks without filling disks.
type DiskStatFunc func(path string) (DiskStats, error)

// CheckDiskReserve verifies that path maintains at least 15 GB free and <= 92% utilization
// even after accounting for extraBytesNeeded.
func CheckDiskReserve(path string, extraBytesNeeded int64, statFn DiskStatFunc) (*DiskStats, error) {
	target := strings.TrimSpace(path)
	if target == "" {
		target = "."
	}
	absTarget, err := filepath.Abs(target)
	if err == nil {
		target = absTarget
	}
	if statFn == nil {
		statFn = PlatformDiskStats
	}
	st, err := statFn(target)
	if err != nil {
		return nil, safepath.WrapError(
			safepath.ErrSecurityPolicy,
			fmt.Sprintf("unable to check NVMe free space on %s: %v", target, err),
			"Verify mount path permissions before writing snapshots, backups, or migration bundles.",
			err,
		)
	}
	if extraBytesNeeded < 0 {
		extraBytesNeeded = 0
	}
	projectedAvail := st.AvailableBytes - extraBytesNeeded
	if st.TotalBytes > 0 {
		projectedUsed := (st.TotalBytes - st.AvailableBytes) + extraBytesNeeded
		projectedPct := (float64(projectedUsed) / float64(st.TotalBytes)) * 100.0
		if projectedPct > MaxDiskUsedPercent {
			return &st, safepath.NewError(
				safepath.ErrSecurityPolicy,
				fmt.Sprintf("NVMe safety reserve violation on %s: projected disk usage %.1f%% exceeds %.0f%% ceiling (available %.2f GB)", target, projectedPct, MaxDiskUsedPercent, float64(projectedAvail)/float64(1024*1024*1024)),
				"Offload large audiobooks to Backblaze B2 (/_protected_b2/) or prune old snapshots before continuing.",
			)
		}
	}
	if projectedAvail < MinFreeNVMeBytes {
		return &st, safepath.NewError(
			safepath.ErrSecurityPolicy,
			fmt.Sprintf("NVMe safety reserve violation on %s: projected free space %.2f GB is below mandatory 15.00 GB reserve", target, float64(projectedAvail)/float64(1024*1024*1024)),
			"Free disk space or offload media to Backblaze B2 so PostgreSQL 18 WAL checkpoints and Docker pulls never stall.",
		)
	}
	return &st, nil
}
