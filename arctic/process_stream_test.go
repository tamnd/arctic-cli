package arctic

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// buildComments writes n comment rows to a .zst and returns its path.
func buildComments(t *testing.T, dir string, n int) string {
	t.Helper()
	var lines [][]byte
	for i := 0; i < n; i++ {
		row := map[string]any{
			"id":          "c" + itoa(i),
			"author":      "user_" + itoa(i%50),
			"subreddit":   "golang",
			"body":        "comment " + itoa(i),
			"score":       i % 100,
			"created_utc": 1300000000 + i,
		}
		b, _ := json.Marshal(row)
		lines = append(lines, b)
	}
	zst := filepath.Join(dir, "RC_2021-05.zst")
	writeZst(t, zst, lines)
	return zst
}

func TestProcessFileStreamAllShards(t *testing.T) {
	tmp := t.TempDir()
	zst := buildComments(t, tmp, 1200)

	cfg := DefaultConfig()
	cfg.ChunkLines = 500 // 1200 -> shards of 500, 500, 200
	pathFn := func(n int) string { return filepath.Join(tmp, "out", padShard(n)+".parquet") }

	var got []ShardDone
	res, err := ProcessFileStream(context.Background(), cfg, zst, TypeComments, pathFn, ProcessConfig{
		OnShard: func(s ShardDone) error { got = append(got, s); return nil },
	})
	if err != nil {
		t.Fatalf("ProcessFileStream: %v", err)
	}
	if res.Shards != 3 || res.Records != 1200 {
		t.Fatalf("res Shards=%d Records=%d, want 3 and 1200", res.Shards, res.Records)
	}
	if len(got) != 3 {
		t.Fatalf("OnShard fired %d times, want 3", len(got))
	}
	wantRecords := []int64{500, 500, 200}
	for i, s := range got {
		if s.N != i {
			t.Errorf("shard %d reported N=%d", i, s.N)
		}
		if s.Records != wantRecords[i] {
			t.Errorf("shard %d Records=%d, want %d", i, s.Records, wantRecords[i])
		}
		if _, err := os.Stat(s.Path); err != nil {
			t.Errorf("shard %d not written: %v", i, err)
		}
	}
}

func TestProcessFileStreamResumeSkips(t *testing.T) {
	tmp := t.TempDir()
	zst := buildComments(t, tmp, 1200)

	cfg := DefaultConfig()
	cfg.ChunkLines = 500
	pathFn := func(n int) string { return filepath.Join(tmp, "out", padShard(n)+".parquet") }

	// Resume after the first two shards: only shard 2 should be produced.
	var got []ShardDone
	res, err := ProcessFileStream(context.Background(), cfg, zst, TypeComments, pathFn, ProcessConfig{
		StartShard: 2,
		OnShard:    func(s ShardDone) error { got = append(got, s); return nil },
	})
	if err != nil {
		t.Fatalf("ProcessFileStream: %v", err)
	}
	if res.Shards != 1 || res.Records != 200 {
		t.Fatalf("res Shards=%d Records=%d, want 1 and 200", res.Shards, res.Records)
	}
	if len(got) != 1 || got[0].N != 2 {
		t.Fatalf("OnShard = %+v, want a single shard N=2", got)
	}
	// The skipped shards must not have been written.
	if _, err := os.Stat(pathFn(0)); !os.IsNotExist(err) {
		t.Errorf("shard 0 should not exist, stat err = %v", err)
	}
	if _, err := os.Stat(pathFn(1)); !os.IsNotExist(err) {
		t.Errorf("shard 1 should not exist, stat err = %v", err)
	}
	// Shard 2 holds the same rows it would have with no resume: the tail 200.
	rows := readComments(t, pathFn(2))
	if len(rows) != 200 {
		t.Fatalf("shard 2 has %d rows, want 200", len(rows))
	}
	if rows[0].ID != "c1000" {
		t.Fatalf("shard 2 first id = %q, want c1000", rows[0].ID)
	}
}
