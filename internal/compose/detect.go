package compose

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"grabtaculous/internal/safepath"
)

// HostHardware captures physical host CPU, RAM, and root NVMe/disk capacity probed without shell calls.
type HostHardware struct {
	OS              string  `json:"os"`
	Arch            string  `json:"arch"`
	VCPUs           int     `json:"vcpus"`
	TotalRAMMB      uint64  `json:"total_ram_mb"`
	AvailableRAMMB  uint64  `json:"available_ram_mb"`
	TotalDiskGB     uint64  `json:"total_disk_gb"`
	FreeDiskGB      uint64  `json:"free_disk_gb"`
	DiskUsedPercent float64 `json:"disk_used_percent"`
	DiskReserveOK   bool    `json:"disk_reserve_ok"`
	DiskReserveCode string  `json:"disk_reserve_code,omitempty"`
	DiskReserveErr  string  `json:"disk_reserve_error,omitempty"`
}

// HardwareOverride allows deterministic dry-run testing of profile detection without mutating the host.
type HardwareOverride struct {
	VCPUs      int
	TotalRAMMB uint64
	TotalDiskGB uint64
	FreeDiskGB uint64
}

// MemoryBudgetSummary summarizes container memory ceilings and host Linux page-cache headroom.
type MemoryBudgetSummary struct {
	GrispWorkerCount       int     `json:"grisp_worker_count"`
	GrispPerWorkerLimitMB  int     `json:"grisp_per_worker_limit_mb"`
	GrispTotalLimitMB      int     `json:"grisp_total_limit_mb"`
	PostgresLimitMB        int     `json:"postgres_limit_mb"`
	PostgresSharedBuffers  string  `json:"postgres_shared_buffers"`
	PostgresEffectiveCache string  `json:"postgres_effective_cache_size"`
	PostgresIOWorkers      int     `json:"postgres_io_workers"`
	RedisLimitMB           int     `json:"redis_limit_mb"`
	RedisMaxMemory         string  `json:"redis_maxmemory"`
	GroxyLimitMB           int     `json:"groxy_limit_mb"`
	GrexBuilderLimitMB     int     `json:"grex_builder_limit_mb"`
	TotalStackLimitMB      int     `json:"total_stack_limit_mb"`
	HostHeadroomMB         int64   `json:"host_headroom_mb"`
	HostHeadroomPercent    float64 `json:"host_headroom_percent"`
}

// ProfileDetectionResult combines probed host hardware, selected optimal profile, memory math, and NVMe reserve status.
type ProfileDetectionResult struct {
	Hardware          HostHardware        `json:"hardware"`
	Profile           *HardwareProfile    `json:"profile"`
	ProfileID         string              `json:"profile_id"`
	WorkerCount       int                 `json:"worker_count"`
	WorkerPorts       []int               `json:"worker_ports"`
	MemoryMath        MemoryBudgetSummary `json:"memory_math"`
	NVMeReserveOK     bool                `json:"nvme_reserve_ok"`
	NVMeReserveStatus string              `json:"nvme_reserve_status"`
	Simulated         bool                `json:"simulated"`
}

// DetectHostHardware probes host CPU cores, RAM, and root disk capacity with zero sh -c shell calls (ADR-017).
func DetectHostHardware() (HostHardware, error) {
	vcpus, totalRAMMB, availRAMMB, err := platformCPUAndRAM()
	if err != nil {
		return HostHardware{}, err
	}
	if vcpus <= 0 {
		vcpus = runtime.NumCPU()
	}

	probePath := platformRootPath()
	st, diskErr := PlatformDiskStats(probePath)
	if diskErr != nil {
		return HostHardware{}, safepath.WrapError(
			safepath.ErrSnapshotFailed,
			fmt.Sprintf("unable to inspect root filesystem capacity on %s: %v", probePath, diskErr),
			"Ensure the root mount is accessible for statfs capacity checks.",
			diskErr,
		)
	}

	const bytesPerGB = uint64(1024 * 1024 * 1024)
	var totalDiskGB, freeDiskGB uint64
	if st.TotalBytes > 0 {
		totalDiskGB = uint64(st.TotalBytes) / bytesPerGB
	}
	if st.AvailableBytes > 0 {
		freeDiskGB = uint64(st.AvailableBytes) / bytesPerGB
	}

	hw := HostHardware{
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		VCPUs:           vcpus,
		TotalRAMMB:      totalRAMMB,
		AvailableRAMMB:  availRAMMB,
		TotalDiskGB:     totalDiskGB,
		FreeDiskGB:      freeDiskGB,
		DiskUsedPercent: st.UsedPercent,
		DiskReserveOK:   true,
	}

	if _, resErr := CheckDiskReserve(probePath, 0, func(string) (DiskStats, error) { return st, nil }); resErr != nil {
		hw.DiskReserveOK = false
		hw.DiskReserveCode = string(safepath.ErrSnapshotFailed)
		hw.DiskReserveErr = resErr.Error()
	}
	return hw, nil
}

// SelectOptimalProfile deterministically maps host hardware specs to the optimal Grabtaculous hardware profile
// and enforces the mandatory >= 15 GB free NVMe / <= 92% utilization reserve guard (GRABTAC-E701).
func SelectOptimalProfile(vcpus int, totalRAMMB uint64, totalDiskGB uint64, freeDiskGB uint64) (*HardwareProfile, error) {
	if vcpus <= 0 {
		return nil, safepath.NewError(
			safepath.ErrPreflightFailed,
			fmt.Sprintf("invalid vcpu count %d", vcpus),
			"Provide a positive logical CPU core count (>= 1).",
		)
	}
	if totalRAMMB == 0 {
		return nil, safepath.NewError(
			safepath.ErrPreflightFailed,
			"invalid total RAM (0 MB)",
			"Provide detected or simulated host physical RAM in MB.",
		)
	}

	if err := checkDiskGBReserve(totalDiskGB, freeDiskGB); err != nil {
		return nil, err
	}

	profileID := selectProfileIDBySpecs(vcpus, totalRAMMB)
	return GetProfile(profileID)
}

func selectProfileIDBySpecs(vcpus int, totalRAMMB uint64) string {
	// Guard small-CPU or < 14 GB nodes against OOM regardless of core count.
	if vcpus <= 6 || totalRAMMB < 14000 {
		return "vps-6c-12g"
	}
	if vcpus <= 8 {
		switch {
		case totalRAMMB < 21000:
			return "vps-8c-16g"
		case totalRAMMB < 28000:
			return "vps-8c-24g"
		case totalRAMMB <= 46080:
			return "vps-8c-32g"
		default:
			return "vps-16c-64g"
		}
	}
	return "vps-16c-64g"
}

func checkDiskGBReserve(totalDiskGB, freeDiskGB uint64) error {
	if totalDiskGB == 0 && freeDiskGB == 0 {
		return nil
	}
	if freeDiskGB < 15 {
		return safepath.NewError(
			safepath.ErrSnapshotFailed,
			fmt.Sprintf("NVMe safety reserve violation: free disk %d GB is below mandatory 15 GB reserve", freeDiskGB),
			"Free disk space or offload media to Backblaze B2 so PostgreSQL 18 WAL checkpoints and Docker pulls never stall.",
		)
	}
	if totalDiskGB > 0 {
		if freeDiskGB > totalDiskGB {
			return safepath.NewError(
				safepath.ErrPreflightFailed,
				fmt.Sprintf("free disk %d GB exceeds total disk %d GB", freeDiskGB, totalDiskGB),
				"Provide valid total and free disk values.",
			)
		}
		usedPct := (float64(totalDiskGB-freeDiskGB) / float64(totalDiskGB)) * 100.0
		if usedPct > MaxDiskUsedPercent {
			return safepath.NewError(
				safepath.ErrSnapshotFailed,
				fmt.Sprintf("NVMe safety reserve violation: disk usage %.1f%% exceeds %.0f%% ceiling (%d GB free of %d GB)", usedPct, MaxDiskUsedPercent, freeDiskGB, totalDiskGB),
				"Offload large media to Backblaze B2 (/_protected_b2/) or prune old snapshots before continuing.",
			)
		}
	}
	return nil
}

// DetectAndSelectProfile probes host hardware (or applies dry-run overrides) and builds a complete sizing summary.
func DetectAndSelectProfile(override *HardwareOverride) (*ProfileDetectionResult, error) {
	var hw HostHardware
	simulated := false

	hasOverride := override != nil && (override.VCPUs > 0 || override.TotalRAMMB > 0 || override.TotalDiskGB > 0 || override.FreeDiskGB > 0)
	if !hasOverride {
		probed, err := DetectHostHardware()
		if err != nil {
			return nil, err
		}
		hw = probed
	} else {
		simulated = true
		hw = HostHardware{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			VCPUs:      8,
			TotalRAMMB: 16384,
		}
		if override.VCPUs > 0 {
			hw.VCPUs = override.VCPUs
		}
		if override.TotalRAMMB > 0 {
			hw.TotalRAMMB = override.TotalRAMMB
		}
		hw.AvailableRAMMB = hw.TotalRAMMB
		if override.TotalDiskGB > 0 || override.FreeDiskGB > 0 {
			hw.TotalDiskGB = override.TotalDiskGB
			hw.FreeDiskGB = override.FreeDiskGB
			if hw.TotalDiskGB == 0 && hw.FreeDiskGB > 0 {
				hw.TotalDiskGB = hw.FreeDiskGB + 20
			}
			if hw.TotalDiskGB > 0 && hw.FreeDiskGB <= hw.TotalDiskGB {
				hw.DiskUsedPercent = (float64(hw.TotalDiskGB-hw.FreeDiskGB) / float64(hw.TotalDiskGB)) * 100.0
			}
		} else {
			hw.TotalDiskGB = 200
			hw.FreeDiskGB = 100
			hw.DiskUsedPercent = 50.0
		}
		if err := checkDiskGBReserve(hw.TotalDiskGB, hw.FreeDiskGB); err != nil {
			hw.DiskReserveOK = false
			hw.DiskReserveCode = string(safepath.ExtractCode(err))
			hw.DiskReserveErr = err.Error()
		} else {
			hw.DiskReserveOK = true
		}
	}

	if hw.VCPUs <= 0 || hw.TotalRAMMB == 0 {
		return nil, safepath.NewError(safepath.ErrPreflightFailed, "host hardware probe returned incomplete CPU/RAM metrics", "Pass explicit --vcpus and --ram-mb overrides or inspect /proc/meminfo.")
	}

	profID := selectProfileIDBySpecs(hw.VCPUs, hw.TotalRAMMB)
	prof, err := GetProfile(profID)
	if err != nil {
		return nil, err
	}

	math := computeMemoryBudget(prof, hw.TotalRAMMB)
	reserveStatus := fmt.Sprintf("PASS (%d GB free, %.1f%% used; >= 15 GB free & <= 92%% required)", hw.FreeDiskGB, hw.DiskUsedPercent)
	if !hw.DiskReserveOK {
		reserveStatus = fmt.Sprintf("FAIL [%s]: %s", hw.DiskReserveCode, hw.DiskReserveErr)
	}

	return &ProfileDetectionResult{
		Hardware:          hw,
		Profile:           prof,
		ProfileID:         prof.ID,
		WorkerCount:       prof.GrispWorkers,
		WorkerPorts:       append([]int(nil), prof.GrispPorts...),
		MemoryMath:        math,
		NVMeReserveOK:     hw.DiskReserveOK,
		NVMeReserveStatus: reserveStatus,
		Simulated:         simulated,
	}, nil
}

// ResolveProfile resolves an explicit profile ID or "auto" (empty or "auto") via hardware detection.
// When auto-detecting, it enforces the mandatory 15 GB NVMe reserve guard (GRABTAC-E701).
func ResolveProfile(id string, override *HardwareOverride) (*HardwareProfile, *ProfileDetectionResult, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" || strings.EqualFold(trimmed, "auto") {
		res, err := DetectAndSelectProfile(override)
		if err != nil {
			return nil, nil, err
		}
		if !res.NVMeReserveOK {
			return nil, res, safepath.NewError(
				safepath.ErrSnapshotFailed,
				res.Hardware.DiskReserveErr,
				"Free disk space or offload media to Backblaze B2 before rendering or applying auto-detected stack templates.",
			)
		}
		return res.Profile, res, nil
	}
	prof, err := GetProfile(trimmed)
	if err != nil {
		return nil, nil, err
	}
	return prof, nil, nil
}

func computeMemoryBudget(p *HardwareProfile, hostRAMMB uint64) MemoryBudgetSummary {
	grispPerWorker := parseMemMB(p.GrispMemoryLimit)
	grispTotal := grispPerWorker * p.GrispWorkers
	pgMB := parseMemMB(p.PostgresMemoryLimit)
	redisMB := parseMemMB(p.RedisMemoryLimit)
	groxyMB := parseMemMB(p.GroxyMemoryLimit)
	builderMB := 512
	totalStack := grispTotal + pgMB + redisMB + groxyMB + builderMB
	headroom := int64(hostRAMMB) - int64(totalStack)
	headroomPct := 0.0
	if hostRAMMB > 0 {
		headroomPct = (float64(headroom) / float64(hostRAMMB)) * 100.0
	}
	return MemoryBudgetSummary{
		GrispWorkerCount:       p.GrispWorkers,
		GrispPerWorkerLimitMB:  grispPerWorker,
		GrispTotalLimitMB:      grispTotal,
		PostgresLimitMB:        pgMB,
		PostgresSharedBuffers:  p.PostgresSharedBuffers,
		PostgresEffectiveCache: p.PostgresEffectiveCache,
		PostgresIOWorkers:      p.PostgresIOWorkers,
		RedisLimitMB:           redisMB,
		RedisMaxMemory:         p.RedisMaxMemory,
		GroxyLimitMB:           groxyMB,
		GrexBuilderLimitMB:     builderMB,
		TotalStackLimitMB:      totalStack,
		HostHeadroomMB:         headroom,
		HostHeadroomPercent:    headroomPct,
	}
}

func parseMemMB(s string) int {
	val := strings.ToUpper(strings.TrimSpace(s))
	if strings.HasSuffix(val, "G") {
		if n, err := strconv.Atoi(strings.TrimSuffix(val, "G")); err == nil {
			return n * 1024
		}
	}
	val = strings.TrimSuffix(val, "MIB")
	val = strings.TrimSuffix(val, "MB")
	val = strings.TrimSuffix(val, "M")
	if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
		return n
	}
	return 0
}

// ParseProcCPUInfo counts logical processor entries from Linux /proc/cpuinfo content.
func ParseProcCPUInfo(raw string) int {
	count := 0
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "processor") && strings.Contains(trimmed, ":") {
			count++
		}
	}
	return count
}

// ParseProcMeminfo parses MemTotal and MemAvailable (in MB) from Linux /proc/meminfo content.
func ParseProcMeminfo(raw string) (totalMB uint64, availMB uint64, err error) {
	var totalKB, availKB, freeKB, buffersKB, cachedKB uint64
	var sawTotal, sawAvail bool
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		kb, convErr := strconv.ParseUint(fields[1], 10, 64)
		if convErr != nil {
			continue
		}
		switch key {
		case "MemTotal":
			totalKB = kb
			sawTotal = true
		case "MemAvailable":
			availKB = kb
			sawAvail = true
		case "MemFree":
			freeKB = kb
		case "Buffers":
			buffersKB = kb
		case "Cached":
			cachedKB = kb
		}
	}
	if !sawTotal || totalKB == 0 {
		return 0, 0, safepath.NewError(safepath.ErrPreflightFailed, "unable to parse MemTotal from /proc/meminfo", "Verify /proc/meminfo readability or supply --ram-mb.")
	}
	if !sawAvail {
		availKB = freeKB + buffersKB + cachedKB
	}
	return totalKB / 1024, availKB / 1024, nil
}
