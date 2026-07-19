package arctic

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestPublishIncrementalCommits drives one month through process + commit with a
// fake uploader and asserts shards land in batches, local copies are dropped,
// the progress marker tracks then clears, and the ledger row is written once.
func TestPublishIncrementalCommits(t *testing.T) {
	tmp := t.TempDir()
	zst := buildComments(t, tmp, 1200) // 3 shards at ChunkLines 500: 500, 500, 200

	cfg := DefaultConfig()
	cfg.Engine = EngineGo
	cfg.ChunkLines = 500
	cfg.CommitEveryShards = 2
	cfg.RepoRoot = filepath.Join(tmp, "repo")
	if err := os.MkdirAll(cfg.RepoRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	var uploads [][]HFOp
	p := &publisher{
		cfg:          cfg,
		opts:         PublishOptions{HFCommit: true, Types: []Type{TypeComments}},
		statsPath:    filepath.Join(cfg.RepoRoot, "stats.csv"),
		progressPath: ProgressPath(cfg),
		cb:           func(string) {},
		progress:     map[string]ShardProgress{},
	}
	p.upload = func(_ context.Context, ops []HFOp) error {
		uploads = append(uploads, append([]HFOp(nil), ops...))
		return nil
	}
	p.markProgress()

	job := &publishJob{Month: Month{Year: 2021, Month: 5}, Type: TypeComments, zstPath: zst}
	if err := p.process(context.Background(), job); err != nil {
		t.Fatalf("process: %v", err)
	}

	// Two shard commits: a batch of 2 (shards 0,1), then the trailing shard 2.
	if len(uploads) != 2 {
		t.Fatalf("shard commits = %d, want 2", len(uploads))
	}
	if len(uploads[0]) != 2 || len(uploads[1]) != 1 {
		t.Fatalf("batch sizes = %d,%d, want 2,1", len(uploads[0]), len(uploads[1]))
	}
	// Uploaded shards were removed locally once on the hub.
	for _, ops := range uploads {
		for _, op := range ops {
			if _, err := os.Stat(op.LocalPath); !os.IsNotExist(err) {
				t.Errorf("shard %s should be gone locally, stat err = %v", op.PathInRepo, err)
			}
		}
	}
	// Progress marker tracks the whole month while it is in flight.
	if sp := p.progress[statsKey(job)]; sp.Shards != 3 || sp.Records != 1200 {
		t.Fatalf("progress = %+v, want Shards 3 Records 1200", sp)
	}

	if err := p.commit(context.Background(), job); err != nil {
		t.Fatalf("commit: %v", err)
	}
	// Finalize pushes stats.csv + README, then clears the marker.
	if len(uploads) != 3 || len(uploads[2]) != 2 {
		t.Fatalf("finalize upload = %v, want one call of 2 files", uploads[2:])
	}
	if _, ok := p.progress[statsKey(job)]; ok {
		t.Fatalf("progress marker should be cleared after finalize")
	}
	rows, err := ReadStats(p.statsPath)
	if err != nil {
		t.Fatalf("ReadStats: %v", err)
	}
	if len(rows) != 1 || rows[0].Shards != 3 || rows[0].Count != 1200 {
		t.Fatalf("ledger = %+v, want one row Shards 3 Count 1200", rows)
	}
}

// TestPublishResumesFromProgress seeds a committed-shard marker and checks the
// next run skips the finished shards instead of redoing them.
func TestPublishResumesFromProgress(t *testing.T) {
	tmp := t.TempDir()
	zst := buildComments(t, tmp, 1200)

	cfg := DefaultConfig()
	cfg.Engine = EngineGo
	cfg.ChunkLines = 500
	cfg.CommitEveryShards = 2
	cfg.RepoRoot = filepath.Join(tmp, "repo")
	if err := os.MkdirAll(cfg.RepoRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	job := &publishJob{Month: Month{Year: 2021, Month: 5}, Type: TypeComments, zstPath: zst}
	var uploads [][]HFOp
	p := &publisher{
		cfg:          cfg,
		opts:         PublishOptions{HFCommit: true, Types: []Type{TypeComments}},
		statsPath:    filepath.Join(cfg.RepoRoot, "stats.csv"),
		progressPath: ProgressPath(cfg),
		cb:           func(string) {},
		// Two shards already committed by a prior run with the same engine.
		progress: map[string]ShardProgress{
			statsKey(job): {Engine: "go", Shards: 2, Records: 1000, Bytes: 4096},
		},
	}
	p.upload = func(_ context.Context, ops []HFOp) error {
		uploads = append(uploads, append([]HFOp(nil), ops...))
		return nil
	}
	p.markProgress()

	if err := p.process(context.Background(), job); err != nil {
		t.Fatalf("process: %v", err)
	}
	// Only shard 2 remains: one commit of one shard.
	if len(uploads) != 1 || len(uploads[0]) != 1 {
		t.Fatalf("resumed uploads = %v, want a single 1-shard commit", uploads)
	}
	if uploads[0][0].PathInRepo != filepath.ToSlash(ShardHFPath(TypeComments, job.Month, 2)) {
		t.Fatalf("resumed shard path = %s, want shard 2", uploads[0][0].PathInRepo)
	}
	// Totals fold the seeded shards into the final count.
	if job.result.Shards != 3 || job.result.Records != 1200 {
		t.Fatalf("totals = Shards %d Records %d, want 3 and 1200", job.result.Shards, job.result.Records)
	}
}
