package arctic

import (
	"path/filepath"
	"testing"
)

func TestStatsRoundTripAndResume(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.csv")

	rows := []StatsRow{
		{Year: 2011, Month: 3, Type: "comments", Shards: 2, Count: 1000, SizeBytes: 4096,
			ZstBytes: 2048, DurDownloadS: 1.5, DurProcessS: 2.5, DurCommitS: 0.5, CommittedAt: nowRFC3339()},
		{Year: 2011, Month: 3, Type: "submissions", Shards: 1, Count: 200, SizeBytes: 512,
			CommittedAt: nowRFC3339()},
		// An uncommitted row should not count toward the committed set.
		{Year: 2011, Month: 4, Type: "comments", Shards: 0, Count: 0},
	}
	if err := WriteStats(path, rows); err != nil {
		t.Fatal(err)
	}

	got, err := ReadStats(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("read %d rows, want 3", len(got))
	}

	set, err := CommittedSet(path)
	if err != nil {
		t.Fatal(err)
	}
	if !set["2011-03-comments"] || !set["2011-03-submissions"] {
		t.Errorf("committed set missing committed rows: %v", set)
	}
	if set["2011-04-comments"] {
		t.Errorf("uncommitted row counted as committed")
	}

	// AppendStats replaces by key.
	if err := AppendStats(path, StatsRow{Year: 2011, Month: 4, Type: "comments", Shards: 3,
		Count: 99, CommittedAt: nowRFC3339()}); err != nil {
		t.Fatal(err)
	}
	set, _ = CommittedSet(path)
	if !set["2011-04-comments"] {
		t.Errorf("appended commit not in set")
	}
	got, _ = ReadStats(path)
	if len(got) != 3 {
		t.Fatalf("after append want 3 rows (dedup by key), got %d", len(got))
	}
}
