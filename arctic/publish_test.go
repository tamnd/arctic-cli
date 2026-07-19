package arctic

import (
	"testing"
	"time"
)

// A month can take longer to process than MaxCommitStall. Processing progress
// must reset the stall clock, otherwise the watchdog cancels a healthy month
// before it ever reaches its commit and the publish deadlocks on retry.
func TestMarkProgressResetsStall(t *testing.T) {
	p := &publisher{cfg: Config{MaxCommitStall: 40 * time.Millisecond}}
	p.markProgress()

	if p.stalledOut() {
		t.Fatal("stalled immediately after progress")
	}

	time.Sleep(60 * time.Millisecond)
	if !p.stalledOut() {
		t.Fatal("did not stall after idle exceeded MaxCommitStall")
	}

	// Forward progress (a processed shard) clears the stall, the same way a long
	// month keeps the watchdog quiet while it works.
	p.markProgress()
	if p.stalledOut() {
		t.Fatal("progress did not reset the stall clock")
	}
}

func TestStalledOutDisabled(t *testing.T) {
	p := &publisher{cfg: Config{MaxCommitStall: 0}}
	// lastCommit stays at the zero time, so idle is effectively infinite; a
	// disabled watchdog must still never report a stall.
	if p.stalledOut() {
		t.Fatal("stall reported with the watchdog disabled")
	}
}
