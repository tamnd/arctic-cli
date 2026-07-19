package arctic

import "testing"

func TestGenerateREADMEStructure(t *testing.T) {
	rows := []StatsRow{
		{Year: 2005, Month: 12, Type: "comments", Shards: 1, Count: 1075, SizeBytes: 141717, CommittedAt: "2026-03-14T19:45:54Z", DurDownloadS: 36.67, DurProcessS: 1.04, DurCommitS: 17.02},
		{Year: 2011, Month: 1, Type: "comments", Shards: 14, Count: 6603329, SizeBytes: 598091268, ZstBytes: 621585706, DurProcessS: 734.20, DurCommitS: 208.39, CommittedAt: "2026-03-16T06:21:49Z"},
	}
	out := GenerateREADME(DefaultConfig(), rows)

	wants := []string{
		"---\nconfigs:\n- config_name: comments",
		"pretty_name: Arctic Shift Reddit Archive",
		"license: other",
		"# Arctic Shift Reddit Archive",
		"| Month | Type | .zst Size | Download | Process | Upload | Shards | Rows | Parquet |",
		// 2011-01 comments: zst 621585706 -> 592.8 MB, process 734.20s -> 12m14s, commit 208.39s -> 3m28s.
		"| 2011-01 | comments | 592.8 MB | - | 12m14s | 3m28s | 14 | 6,603,329 | 570.4 MB |",
		// zero durations render as "-".
		"| 2005-12 | comments | - | 36.7s | 1.0s | 17.0s | 1 | 1,075 | 138.4 KB |",
		"# Dataset card for Arctic Shift Reddit Archive",
	}
	for _, w := range wants {
		if !contains(out, w) {
			t.Errorf("README missing:\n%q", w)
		}
	}
	// A live/pipeline section must never appear: this publisher emits no live telemetry.
	if contains(out, "## Pipeline Status") {
		t.Error("README should not carry a Pipeline Status section")
	}
}

func TestGenerateREADMEEmpty(t *testing.T) {
	out := GenerateREADME(DefaultConfig(), nil)
	if !contains(out, "pretty_name: Arctic Shift Reddit Archive") {
		t.Error("empty ledger must still produce a valid card")
	}
	if !contains(out, "(no data yet)") {
		t.Error("empty ledger monthly table should read (no data yet)")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
