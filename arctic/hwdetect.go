package arctic

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// gb is one gibibyte, the unit every RAM and disk figure reports in.
const gb = 1024 * 1024 * 1024

// existingAncestor walks up from path until it finds a directory that exists,
// so a statfs against a not-yet-created work directory still measures the right
// volume.
func existingAncestor(path string) string {
	p := path
	for p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	return "."
}

// HardwareProfile describes the machine the tool runs on. The budget reads it
// to decide how many downloads, decodes, and conversions can run at once without
// thrashing memory or disk.
type HardwareProfile struct {
	Hostname       string  `json:"hostname"`
	OS             string  `json:"os"`
	CPUs           int     `json:"cpus"`
	RAMTotalGB     float64 `json:"ram_total_gb"`
	RAMAvailableGB float64 `json:"ram_available_gb"`
	DiskFreeGB     float64 `json:"disk_free_gb"`
}

// DetectHardware probes the current machine. workDir picks the volume the disk
// check runs against, since that is where downloads and conversion land.
func DetectHardware(workDir string) HardwareProfile {
	hostname, _ := os.Hostname()
	total, avail := detectRAM()
	free := detectDiskFreeGB(workDir)
	return HardwareProfile{
		Hostname:       hostname,
		OS:             runtime.GOOS,
		CPUs:           runtime.NumCPU(),
		RAMTotalGB:     total,
		RAMAvailableGB: avail,
		DiskFreeGB:     free,
	}
}

// String renders a one-line summary for arctic info.
func (h HardwareProfile) String() string {
	return fmt.Sprintf("%s (%s): %d cores, %.0f GB RAM (%.0f avail), %.0f GB disk free",
		h.Hostname, h.OS, h.CPUs, h.RAMTotalGB, h.RAMAvailableGB, h.DiskFreeGB)
}
