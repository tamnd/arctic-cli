package arctic

import (
	"fmt"
	"math"
)

// Budget caps how much work runs at once. The 2 GB zstd window for the bulk
// dumps is the dominant memory cost, so the caps are sized to keep a small
// machine from thrashing or getting OOM-killed in the middle of a decode.
type Budget struct {
	// MaxDownloads is how many torrent downloads run concurrently.
	MaxDownloads int
	// MaxProcess is how many months decode concurrently. A process-wide
	// semaphore still holds only one 2 GB decoder at a time, but each slot needs
	// headroom for scanner buffers and the conversion that follows.
	MaxProcess int
	// MaxConvertWorkers is how many Parquet conversions run per decoded month.
	MaxConvertWorkers int
	// DuckDBMemoryMB caps the DuckDB engine when the build carries it.
	DuckDBMemoryMB int
	// Sequential is set when the machine is too small for any overlap: one
	// download, one decode, one conversion, fully serial.
	Sequential bool
}

// ComputeBudget derives the caps from a hardware profile. The shape follows the
// spec: a hard sequential floor on tiny machines, then download, process, and
// convert caps scaled by RAM and CPU, and DuckDB memory that grows on large
// hosts.
func ComputeBudget(hw HardwareProfile) Budget {
	b := Budget{DuckDBMemoryMB: 512}

	// A machine with under 2 GB RAM or under 30 GB free disk has no safe
	// parallel plan: one 2 GB decode plus the OS already runs it close to the
	// edge, and there is nowhere to land a second multi-gigabyte file.
	if hw.RAMTotalGB < 2 || hw.DiskFreeGB < 30 {
		b.MaxDownloads = 1
		b.MaxProcess = 1
		b.MaxConvertWorkers = 1
		b.Sequential = true
		return b
	}

	// Two concurrent downloads each need a multi-gigabyte file's worth of room,
	// so only allow the second when disk is comfortable.
	b.MaxDownloads = 1
	if hw.DiskFreeGB >= 200 {
		b.MaxDownloads = 2
	}

	// Reserve 4 GB for the OS, the torrent client, and uploads. Each decode slot
	// wants about 3 GB of headroom (the 2 GB window plus scanner and overhead),
	// capped by half the cores and a hard ceiling of four so even a big server
	// does not run the swarm out of peers.
	usableRAM := hw.RAMTotalGB - 4
	if usableRAM < 1.5 {
		usableRAM = 1.5
	}
	b.MaxProcess = clamp(int(usableRAM/3.0), 1, 4)
	if cpuLimit := hw.CPUs / 2; cpuLimit >= 1 && b.MaxProcess > cpuLimit {
		b.MaxProcess = cpuLimit
	}
	if b.MaxProcess < 1 {
		b.MaxProcess = 1
	}

	// Conversion is CPU-bound and cheaper on memory than decoding. Budget it
	// from the RAM left over after the decoder and scanner, around 0.4 GB per
	// worker, capped by core count and a ceiling of eight.
	convertRAM := usableRAM - 2.5
	if convertRAM < 0.5 {
		convertRAM = 0.5
	}
	const perWorkerGB = 0.4
	b.MaxConvertWorkers = clamp(int(convertRAM/perWorkerGB), 1, 8)
	if hw.CPUs >= 1 && b.MaxConvertWorkers > hw.CPUs {
		b.MaxConvertWorkers = hw.CPUs
	}
	if b.MaxConvertWorkers < 1 {
		b.MaxConvertWorkers = 1
	}

	// Give DuckDB more memory only on a host with real headroom, and never more
	// than 2 GB per instance.
	if hw.RAMTotalGB >= 24 {
		perInstance := int(usableRAM / float64(b.MaxProcess) / 2 * 1024)
		if perInstance > b.DuckDBMemoryMB {
			b.DuckDBMemoryMB = perInstance
		}
		if b.DuckDBMemoryMB > 2048 {
			b.DuckDBMemoryMB = 2048
		}
	}

	return b
}

// applyBudgetOverrides lets Config pin the concurrency knobs above or below the
// hardware-derived budget. A positive override wins; zero keeps the computed
// value. Raising process concurrency also leaves sequential mode.
func applyBudgetOverrides(b Budget, cfg Config) Budget {
	if cfg.MaxDownloads > 0 {
		b.MaxDownloads = cfg.MaxDownloads
	}
	if cfg.MaxProcess > 0 {
		b.MaxProcess = cfg.MaxProcess
	}
	if cfg.MaxConvertWorkers > 0 {
		b.MaxConvertWorkers = cfg.MaxConvertWorkers
	}
	if b.MaxProcess > 1 {
		b.Sequential = false
	}
	return b
}

// String renders the budget for arctic info.
func (b Budget) String() string {
	if b.Sequential {
		return "sequential (1 download, 1 process, 1 convert)"
	}
	return fmt.Sprintf("pipeline: %d download, %d process, %d convert, DuckDB %d MB",
		b.MaxDownloads, b.MaxProcess, b.MaxConvertWorkers, b.DuckDBMemoryMB)
}

func clamp(v, lo, hi int) int {
	return int(math.Min(math.Max(float64(v), float64(lo)), float64(hi)))
}
