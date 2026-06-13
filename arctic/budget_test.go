package arctic

import "testing"

func TestComputeBudget(t *testing.T) {
	cases := []struct {
		name        string
		hw          HardwareProfile
		seq         bool
		minDownload int
		maxDownload int
		process     int
		duckMin     int
		duckMax     int
	}{
		{
			name:        "tiny ram falls back to sequential",
			hw:          HardwareProfile{CPUs: 2, RAMTotalGB: 1, RAMAvailableGB: 0.5, DiskFreeGB: 100},
			seq:         true,
			minDownload: 1, maxDownload: 1, process: 1,
			duckMin: 512, duckMax: 512,
		},
		{
			name:        "tiny disk falls back to sequential",
			hw:          HardwareProfile{CPUs: 8, RAMTotalGB: 16, RAMAvailableGB: 12, DiskFreeGB: 20},
			seq:         true,
			minDownload: 1, maxDownload: 1, process: 1,
			duckMin: 512, duckMax: 512,
		},
		{
			name:        "laptop class",
			hw:          HardwareProfile{CPUs: 8, RAMTotalGB: 16, RAMAvailableGB: 10, DiskFreeGB: 120},
			seq:         false,
			minDownload: 1, maxDownload: 1, process: 4,
			duckMin: 512, duckMax: 512,
		},
		{
			name:        "small server, two cores",
			hw:          HardwareProfile{CPUs: 2, RAMTotalGB: 8, RAMAvailableGB: 6, DiskFreeGB: 60},
			seq:         false,
			minDownload: 1, maxDownload: 1, process: 1,
			duckMin: 512, duckMax: 512,
		},
		{
			name:        "big server scales downloads and duckdb",
			hw:          HardwareProfile{CPUs: 32, RAMTotalGB: 256, RAMAvailableGB: 240, DiskFreeGB: 4000},
			seq:         false,
			minDownload: 2, maxDownload: 2, process: 4,
			duckMin: 2048, duckMax: 2048,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := ComputeBudget(c.hw)
			if b.Sequential != c.seq {
				t.Errorf("Sequential = %v, want %v", b.Sequential, c.seq)
			}
			if b.MaxDownloads < c.minDownload || b.MaxDownloads > c.maxDownload {
				t.Errorf("MaxDownloads = %d, want %d..%d", b.MaxDownloads, c.minDownload, c.maxDownload)
			}
			if b.MaxProcess != c.process {
				t.Errorf("MaxProcess = %d, want %d", b.MaxProcess, c.process)
			}
			if b.DuckDBMemoryMB < c.duckMin || b.DuckDBMemoryMB > c.duckMax {
				t.Errorf("DuckDBMemoryMB = %d, want %d..%d", b.DuckDBMemoryMB, c.duckMin, c.duckMax)
			}
			if b.MaxConvertWorkers < 1 {
				t.Errorf("MaxConvertWorkers = %d, want >= 1", b.MaxConvertWorkers)
			}
		})
	}
}
