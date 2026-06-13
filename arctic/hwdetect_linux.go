//go:build linux

package arctic

import (
	"bufio"
	"os"
	"strings"
	"syscall"
)

// detectRAM reads /proc/meminfo. MemAvailable is the kernel's own estimate of
// what a new workload can use without swapping; on kernels too old to report it
// we fall back to free plus buffers plus cache.
func detectRAM() (total, avail float64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()

	var totalKB, availKB, freeKB, buffersKB, cachedKB int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			totalKB = parseMeminfoKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			availKB = parseMeminfoKB(line)
		case strings.HasPrefix(line, "MemFree:"):
			freeKB = parseMeminfoKB(line)
		case strings.HasPrefix(line, "Buffers:"):
			buffersKB = parseMeminfoKB(line)
		case strings.HasPrefix(line, "Cached:") && !strings.HasPrefix(line, "CachedSwap"):
			cachedKB = parseMeminfoKB(line)
		}
	}

	total = float64(totalKB) * 1024 / gb
	if availKB > 0 {
		avail = float64(availKB) * 1024 / gb
	} else {
		avail = float64(freeKB+buffersKB+cachedKB) * 1024 / gb
	}
	return total, avail
}

func parseMeminfoKB(line string) int64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	var v int64
	for _, c := range fields[1] {
		if c >= '0' && c <= '9' {
			v = v*10 + int64(c-'0')
		}
	}
	return v
}

func detectDiskFreeGB(path string) float64 {
	if path == "" {
		return 0
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(existingAncestor(path), &st); err != nil {
		return 0
	}
	return float64(st.Bavail) * float64(st.Bsize) / gb
}
