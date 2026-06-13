package arctic

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	parquet "github.com/parquet-go/parquet-go"
)

// writeZst compresses lines into a .zst file at path, matching the dump shape:
// one JSON object per line.
func writeZst(t *testing.T, path string, lines [][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	enc, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		_, _ = enc.Write(l)
		_, _ = enc.Write([]byte{'\n'})
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessFileComments(t *testing.T) {
	tmp := t.TempDir()
	zst := filepath.Join(tmp, "RC_2011-03.zst")

	var lines [][]byte
	good := 0
	for i := 0; i < 1200; i++ {
		row := map[string]any{
			"id":          "c" + itoa(i),
			"author":      "user_" + itoa(i%50),
			"subreddit":   "golang",
			"body":        "comment number " + itoa(i),
			"score":       i % 100,
			"created_utc": 1300000000 + i,
			"link_id":     "t3_x" + itoa(i%10),
			"parent_id":   "t1_y" + itoa(i%20),
		}
		b, _ := json.Marshal(row)
		lines = append(lines, b)
		good++
	}
	// Throw in malformed lines: empty, non-JSON, and a truncated object. These
	// must be counted and skipped, never abort the file.
	lines = append(lines, []byte(""))
	lines = append(lines, []byte("not json at all"))
	lines = append(lines, []byte(`{"id":"broken"`))
	writeZst(t, zst, lines)

	cfg := DefaultConfig()
	cfg.ChunkLines = 500 // force multiple shards
	out := filepath.Join(tmp, "out")

	res, err := ProcessFile(context.Background(), cfg, zst, TypeComments, out, nil)
	if err != nil {
		t.Fatalf("ProcessFile: %v", err)
	}
	if res.Records != int64(good) {
		t.Errorf("Records = %d, want %d", res.Records, good)
	}
	if res.SkippedLines < 3 {
		t.Errorf("SkippedLines = %d, want >= 3", res.SkippedLines)
	}
	if res.Shards != 3 {
		t.Errorf("Shards = %d, want 3 (1200 / 500)", res.Shards)
	}

	// Read the shards back and confirm the schema and derived columns survive.
	var total int
	for n := 0; n < res.Shards; n++ {
		shardPath := filepath.Join(out, padShard(n)+".parquet")
		rows := readComments(t, shardPath)
		total += len(rows)
		for _, c := range rows {
			if c.Subreddit != "golang" {
				t.Fatalf("subreddit = %q", c.Subreddit)
			}
			if c.BodyLength != int32(len([]rune(c.Body))) {
				t.Fatalf("body_length %d != rune len of %q", c.BodyLength, c.Body)
			}
			if c.CreatedAt.Unix() != c.CreatedUTC {
				t.Fatalf("created_at %v != created_utc %d", c.CreatedAt, c.CreatedUTC)
			}
		}
	}
	if total != good {
		t.Errorf("read back %d rows, want %d", total, good)
	}
}

func TestProcessFileSubmissions(t *testing.T) {
	tmp := t.TempDir()
	zst := filepath.Join(tmp, "RS_2011-03.zst")
	var lines [][]byte
	const n = 300
	for i := 0; i < n; i++ {
		row := map[string]any{
			"id":           "s" + itoa(i),
			"author":       "poster_" + itoa(i%10),
			"subreddit":    "test",
			"title":        "post title " + itoa(i),
			"selftext":     "body " + itoa(i),
			"score":        i,
			"created_utc":  1300000000 + i,
			"num_comments": i % 5,
			"url":          "https://example.com/" + itoa(i),
			"over_18":      i%7 == 0,
		}
		b, _ := json.Marshal(row)
		lines = append(lines, b)
	}
	writeZst(t, zst, lines)

	cfg := DefaultConfig()
	cfg.ChunkLines = 1000
	out := filepath.Join(tmp, "out")
	res, err := ProcessFile(context.Background(), cfg, zst, TypeSubmissions, out, nil)
	if err != nil {
		t.Fatalf("ProcessFile: %v", err)
	}
	if res.Records != n || res.Shards != 1 {
		t.Fatalf("Records=%d Shards=%d, want %d and 1", res.Records, res.Shards, n)
	}
	rows := readSubmissions(t, filepath.Join(out, padShard(0)+".parquet"))
	if len(rows) != n {
		t.Fatalf("read back %d, want %d", len(rows), n)
	}
	for _, s := range rows {
		if s.TitleLength != int32(len([]rune(s.Title))) {
			t.Fatalf("title_length mismatch on %q", s.Title)
		}
	}
}

func readComments(t *testing.T, path string) []Comment {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parquet.Read[Comment](bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func readSubmissions(t *testing.T, path string) []Submission {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parquet.Read[Submission](bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
