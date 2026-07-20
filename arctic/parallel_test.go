package arctic

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestApplyBudgetOverrides(t *testing.T) {
	base := Budget{MaxDownloads: 1, MaxProcess: 4, MaxConvertWorkers: 8, MaxDecodes: 1}

	// A zero override keeps the computed value; a positive one wins.
	got := applyBudgetOverrides(base, Config{MaxProcess: 2})
	if got.MaxProcess != 2 || got.MaxDownloads != 1 || got.MaxConvertWorkers != 8 {
		t.Fatalf("override = %+v, want process pinned to 2 and the rest unchanged", got)
	}

	// Raising process concurrency on a sequential box switches it to pipeline.
	seq := Budget{MaxDownloads: 1, MaxProcess: 1, MaxConvertWorkers: 1, Sequential: true}
	got = applyBudgetOverrides(seq, Config{MaxProcess: 3})
	if got.Sequential || got.MaxProcess != 3 {
		t.Fatalf("override = %+v, want pipeline with process 3", got)
	}

	// No overrides leaves the budget untouched.
	got = applyBudgetOverrides(base, Config{})
	if got != base {
		t.Fatalf("empty override changed the budget: %+v", got)
	}
}

func TestWaitForDiskGateReleases(t *testing.T) {
	var calls atomic.Int32
	p := &publisher{
		cfg:      Config{DownloadFloorGB: 40},
		cb:       func(string) {},
		diskPoll: time.Millisecond,
		// Below the floor for the first two probes, then clears.
		diskFreeGB: func(string) float64 {
			if calls.Add(1) <= 2 {
				return 10
			}
			return 50
		},
	}
	if err := p.waitForDisk(context.Background()); err != nil {
		t.Fatalf("waitForDisk: %v", err)
	}
	if calls.Load() < 3 {
		t.Fatalf("gate released after %d probes, want it to wait for the floor to clear", calls.Load())
	}
}

func TestWaitForDiskGateDisabledAndUnsure(t *testing.T) {
	// A zero floor disables the gate: no probe, immediate return.
	p := &publisher{cfg: Config{DownloadFloorGB: 0}, cb: func(string) {}, diskFreeGB: func(string) float64 { t.Fatal("probed with gate disabled"); return 0 }}
	if err := p.waitForDisk(context.Background()); err != nil {
		t.Fatalf("disabled gate: %v", err)
	}
	// An unsure probe (<= 0) must not block the pipeline forever.
	p = &publisher{cfg: Config{DownloadFloorGB: 40}, cb: func(string) {}, diskFreeGB: func(string) float64 { return 0 }}
	if err := p.waitForDisk(context.Background()); err != nil {
		t.Fatalf("unsure probe should proceed: %v", err)
	}
}

func TestWaitForDiskGateContextCancel(t *testing.T) {
	p := &publisher{
		cfg:        Config{DownloadFloorGB: 40},
		cb:         func(string) {},
		diskPoll:   time.Hour, // would hang if cancellation were ignored
		diskFreeGB: func(string) float64 { return 1 },
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.waitForDisk(ctx); err == nil {
		t.Fatal("waitForDisk should return the context error when cancelled")
	}
}
