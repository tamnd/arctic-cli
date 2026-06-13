package shift

import (
	"os"
	"path/filepath"
	"testing"
)

func writeJSONL(t *testing.T, path string, lines []string) {
	t.Helper()
	data := ""
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInspect(t *testing.T) {
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "comments.jsonl"), []string{
		`{"id":"a","author":"alice","subreddit":"golang","created_utc":1104537700}`,
		`{"id":"b","author":"bob","subreddit":"golang","created_utc":1104537900}`,
		`{"id":"c","author":"[deleted]","subreddit":"rust","created_utc":1104537800}`,
	})
	writeJSONL(t, filepath.Join(dir, "submissions.jsonl"), []string{
		`{"id":"d","author":"alice","subreddit":"rust","created_utc":1104537600}`,
	})

	info, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}

	if info.Comments != 3 {
		t.Errorf("comments = %d, want 3", info.Comments)
	}
	if info.Submissions != 1 {
		t.Errorf("submissions = %d, want 1", info.Submissions)
	}
	// alice and bob; "[deleted]" is not counted.
	if info.Authors != 2 {
		t.Errorf("authors = %d, want 2", info.Authors)
	}
	// golang and rust.
	if info.Subreddits != 2 {
		t.Errorf("subreddits = %d, want 2", info.Subreddits)
	}
	if info.FirstUTC != 1104537600 {
		t.Errorf("first = %d, want 1104537600", info.FirstUTC)
	}
	if info.LastUTC != 1104537900 {
		t.Errorf("last = %d, want 1104537900", info.LastUTC)
	}
	if info.SizeBytes == 0 {
		t.Error("size = 0, want nonzero")
	}

	// The cache should land and a repeat call should match.
	if _, err := os.Stat(filepath.Join(dir, infoCacheName)); err != nil {
		t.Errorf("info cache not written: %v", err)
	}
	again, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again != info {
		t.Errorf("cached inspect differs: %+v vs %+v", again, info)
	}
}

func TestInspectCommentsOnly(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, filepath.Join(dir, "comments.jsonl"), []string{
		`{"id":"a","author":"alice","subreddit":"golang","created_utc":1200000000}`,
	})

	info, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Comments != 1 || info.Submissions != 0 {
		t.Errorf("got comments=%d submissions=%d, want 1 and 0", info.Comments, info.Submissions)
	}
	if info.FirstUTC != 1200000000 || info.LastUTC != 1200000000 {
		t.Errorf("span = [%d,%d], want both 1200000000", info.FirstUTC, info.LastUTC)
	}
}

func TestInspectEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := Inspect(dir); err == nil {
		t.Fatal("expected error for empty dir")
	}
}
