package arctic

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadProgressMissing(t *testing.T) {
	m, err := LoadProgress(filepath.Join(t.TempDir(), "publish-progress.json"))
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("missing progress should load empty, got %v", m)
	}
}

func TestProgressRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "publish-progress.json")
	want := map[string]ShardProgress{
		"2021-05-submissions": {Engine: "duckdb", Shards: 12, Records: 6_000_000, Bytes: 4_200_000},
	}
	if err := SaveProgress(path, want); err != nil {
		t.Fatalf("SaveProgress: %v", err)
	}
	got, err := LoadProgress(path)
	if err != nil {
		t.Fatalf("LoadProgress: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %v, want %v", got, want)
	}
}

func TestResumeSeedEngineGuard(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Engine = EngineDuckDB
	p := &publisher{
		cfg:      cfg,
		cb:       func(string) {},
		progress: map[string]ShardProgress{"2021-05-submissions": {Engine: "duckdb", Shards: 12, Records: 100, Bytes: 200}},
	}
	if seed := p.resumeSeed("2021-05-submissions"); seed.Shards != 12 {
		t.Fatalf("same-engine seed Shards = %d, want 12", seed.Shards)
	}
	// A marker from the other engine is ignored: shard boundaries differ, so the
	// month restarts from zero rather than resuming onto misaligned shards.
	cfg.Engine = EngineGo
	p.cfg = cfg
	if seed := p.resumeSeed("2021-05-submissions"); seed.Shards != 0 {
		t.Fatalf("cross-engine seed Shards = %d, want 0", seed.Shards)
	}
	if seed := p.resumeSeed("2099-01-comments"); seed.Shards != 0 {
		t.Fatalf("unknown month seed Shards = %d, want 0", seed.Shards)
	}
}
