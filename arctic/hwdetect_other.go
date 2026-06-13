//go:build !darwin && !linux

package arctic

// detectRAM has no portable probe outside macOS and Linux, so it returns a
// conservative guess that keeps the budget on the safe side: assume 8 GB total
// with 4 GB available. An operator who knows their box overrides the caps with
// flags.
func detectRAM() (total, avail float64) {
	return 8, 4
}

// detectDiskFreeGB reports free space on the work volume using whatever probe
// the platform offers; diskFreeGB is supplied per-OS below this build tag.
func detectDiskFreeGB(path string) float64 {
	if path == "" {
		return 0
	}
	return diskFreeGB(existingAncestor(path))
}
