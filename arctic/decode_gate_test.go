package arctic

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDecodeGateLimitsConcurrency checks the gate never lets more than its sized
// number of decodes hold a slot at once, and that a second decode proceeds only
// after the first releases.
func TestDecodeGateLimitsConcurrency(t *testing.T) {
	t.Cleanup(func() { SetDecodeConcurrency(1) }) // restore the package default

	SetDecodeConcurrency(2)

	var live, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := acquireDecoder()
			n := live.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			live.Add(-1)
			release()
		}()
	}
	wg.Wait()

	if peak.Load() > 2 {
		t.Fatalf("peak concurrent decodes = %d, want <= 2", peak.Load())
	}
	if peak.Load() < 2 {
		t.Fatalf("peak concurrent decodes = %d, want the gate to allow 2", peak.Load())
	}
}

// TestDecodeGateSerializesAtOne is the small-box default: strictly one at a time.
func TestDecodeGateSerializesAtOne(t *testing.T) {
	t.Cleanup(func() { SetDecodeConcurrency(1) })
	SetDecodeConcurrency(1)

	release := acquireDecoder()
	proceeded := make(chan struct{})
	go func() {
		r2 := acquireDecoder()
		close(proceeded)
		r2()
	}()

	select {
	case <-proceeded:
		t.Fatal("second decode acquired a slot while the first still held it")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case <-proceeded:
	case <-time.After(time.Second):
		t.Fatal("second decode never acquired after release")
	}
}

// TestSetDecodeConcurrencyFloor guards against a zero or negative size.
func TestSetDecodeConcurrencyFloor(t *testing.T) {
	t.Cleanup(func() { SetDecodeConcurrency(1) })
	SetDecodeConcurrency(0)
	release := acquireDecoder() // must not deadlock; a zero size floors to one
	release()
}

func TestBudgetMaxDecodes(t *testing.T) {
	// A big host earns a second decode slot but never more than its process slots.
	big := ComputeBudget(HardwareProfile{RAMTotalGB: 23, CPUs: 8, DiskFreeGB: 300})
	if big.MaxDecodes < 2 {
		t.Fatalf("MaxDecodes = %d on a 23 GB host, want >= 2", big.MaxDecodes)
	}
	if big.MaxDecodes > big.MaxProcess {
		t.Fatalf("MaxDecodes %d exceeds MaxProcess %d", big.MaxDecodes, big.MaxProcess)
	}

	// A tiny box stays at one.
	small := ComputeBudget(HardwareProfile{RAMTotalGB: 4, CPUs: 2, DiskFreeGB: 50})
	if small.MaxDecodes != 1 {
		t.Fatalf("MaxDecodes = %d on a 4 GB host, want 1", small.MaxDecodes)
	}

	// An explicit override wins and floors to one.
	over := applyBudgetOverrides(big, Config{MaxDecodes: 3})
	if over.MaxDecodes != 3 {
		t.Fatalf("override MaxDecodes = %d, want 3", over.MaxDecodes)
	}
	floored := applyBudgetOverrides(Budget{MaxProcess: 2, MaxDecodes: 0}, Config{})
	if floored.MaxDecodes != 1 {
		t.Fatalf("zero MaxDecodes floored to %d, want 1", floored.MaxDecodes)
	}
}
