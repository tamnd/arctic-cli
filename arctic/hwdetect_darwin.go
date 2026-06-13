//go:build darwin

package arctic

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// detectRAM reads total RAM from sysctl hw.memsize and estimates available from
// vm_stat. macOS has no MemAvailable equivalent, so available is free plus
// inactive pages, which the kernel can reclaim under pressure.
func detectRAM() (total, avail float64) {
	if memsize, err := unix.SysctlUint64("hw.memsize"); err == nil {
		total = float64(memsize) / gb
	}
	avail = estimateAvailDarwin()
	if avail == 0 {
		// Without vm_stat assume a conservative 70 percent is reclaimable.
		avail = total * 0.7
	}
	return total, avail
}

func estimateAvailDarwin() float64 {
	var pageSize int64
	if buf, err := unix.SysctlRaw("hw.pagesize"); err == nil && len(buf) >= int(unsafe.Sizeof(int64(0))) {
		pageSize = *(*int64)(unsafe.Pointer(&buf[0]))
	}
	if pageSize == 0 {
		pageSize = 16384 // arm64 macOS default
	}

	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}
	var freePages, inactivePages int64
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "Pages free:"):
			freePages = parseVMStatPages(line)
		case strings.HasPrefix(line, "Pages inactive:"):
			inactivePages = parseVMStatPages(line)
		}
	}
	return float64((freePages+inactivePages)*pageSize) / gb
}

func parseVMStatPages(line string) int64 {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) < 2 {
		return 0
	}
	s := strings.TrimSuffix(strings.TrimSpace(parts[1]), ".")
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
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
