package arctic

import "testing"

func pf(path string, size int64, del bool) preparedFile {
	return preparedFile{op: HFOp{PathInRepo: path, Delete: del}, size: size}
}

func TestCommitMessageAddRange(t *testing.T) {
	files := []preparedFile{
		pf("data/comments/2021/05/061.parquet", 452<<20, false),
		pf("data/comments/2021/05/062.parquet", 461<<20, false),
		pf("data/comments/2021/05/063.parquet", 448<<20, false),
		pf("data/comments/2021/05/064.parquet", 455<<20, false),
	}
	summary, desc := commitMessage(files)

	want := "Add comments 2021-05 shards 061-064 (4 files, 1.8 GB)"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
	for _, sub := range []string{
		"comments 2021-05",
		"Added 4 files (1.8 GB):",
		"- data/comments/2021/05/061.parquet (452.0 MB)",
		"- data/comments/2021/05/064.parquet (455.0 MB)",
	} {
		if !contains(desc, sub) {
			t.Fatalf("description missing %q:\n%s", sub, desc)
		}
	}
}

func TestCommitMessageSingleShard(t *testing.T) {
	summary, _ := commitMessage([]preparedFile{pf("data/submissions/2019/12/007.parquet", 3<<30, false)})
	want := "Add submissions 2019-12 shard 007 (1 file, 3.0 GB)"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
}

func TestCommitMessageMixedMonthsDropsScope(t *testing.T) {
	// Two different months share no scope, and non-contiguous shards give no range.
	files := []preparedFile{
		pf("data/comments/2021/05/061.parquet", 1<<20, false),
		pf("data/comments/2021/06/010.parquet", 1<<20, false),
	}
	summary, desc := commitMessage(files)
	want := "Add (2 files, 2.0 MB)"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
	if contains(desc, "2021-05") && contains(desc, "2021-06") {
		// A scope line would name a single month; mixed commits omit it.
		if firstLine(desc) != "Added 2 files (2.0 MB):" {
			t.Fatalf("mixed commit should not carry a scope line, got:\n%s", desc)
		}
	}
}

func TestCommitMessageWithDeletes(t *testing.T) {
	files := []preparedFile{
		pf("data/comments/2021/05/061.parquet", 500<<20, false),
		pf("data/comments/2021/05/060.parquet", 0, true),
	}
	summary, desc := commitMessage(files)
	want := "Update comments 2021-05 shard 061 (1 file, 500.0 MB, 1 removed)"
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
	if !contains(desc, "Removed 1 file:") || !contains(desc, "- data/comments/2021/05/060.parquet") {
		t.Fatalf("description missing removal section:\n%s", desc)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{5 << 20, "5.0 MB"},
		{3 << 30, "3.0 GB"},
		{2 << 40, "2.0 TB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
