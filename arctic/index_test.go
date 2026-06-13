package arctic

import (
	"path/filepath"
	"testing"
)

func TestIndexRecordAndStats(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(filepath.Join(dir, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	if err := idx.RecordShard(TypeComments, 2011, 3, "", "/data/comments/2011/03/000.parquet", 500, 1024); err != nil {
		t.Fatal(err)
	}
	if err := idx.RecordShard(TypeComments, 2011, 3, "", "/data/comments/2011/03/001.parquet", 700, 2048); err != nil {
		t.Fatal(err)
	}
	if err := idx.RecordShard(TypeSubmissions, 0, 0, "golang", "/data/sub/golang/000.parquet", 50, 256); err != nil {
		t.Fatal(err)
	}

	byMonth, err := idx.Stats("month")
	if err != nil {
		t.Fatal(err)
	}
	if len(byMonth) != 1 || byMonth[0].Group != "2011-03" || byMonth[0].Count != 1200 || byMonth[0].Shards != 2 {
		t.Fatalf("month rollup wrong: %+v", byMonth)
	}

	byType, err := idx.Stats("type")
	if err != nil {
		t.Fatal(err)
	}
	if len(byType) != 2 {
		t.Fatalf("type rollup want 2 groups, got %d", len(byType))
	}

	bySub, err := idx.Stats("subreddit")
	if err != nil {
		t.Fatal(err)
	}
	if len(bySub) != 1 || bySub[0].Group != "golang" {
		t.Fatalf("subreddit rollup wrong: %+v", bySub)
	}

	// Re-recording the same path updates rather than duplicates.
	if err := idx.RecordShard(TypeComments, 2011, 3, "", "/data/comments/2011/03/000.parquet", 999, 4096); err != nil {
		t.Fatal(err)
	}
	byMonth, _ = idx.Stats("month")
	if byMonth[0].Count != 1699 {
		t.Fatalf("after upsert count = %d, want 1699", byMonth[0].Count)
	}
}
