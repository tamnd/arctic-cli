//go:build !darwin && !linux && !windows

package arctic

import "syscall"

// diskFreeGB measures free space via statfs on the remaining unix platforms
// (freebsd and friends), which all carry it.
func diskFreeGB(path string) float64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return float64(st.Bavail) * float64(st.Bsize) / gb
}
