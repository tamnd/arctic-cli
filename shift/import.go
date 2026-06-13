package shift

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tidwall/gjson"
)

// Info summarizes what is imported in an entity directory: the record counts by
// type, the number of distinct authors and subreddits, the first and last
// created_utc seen, and the total bytes of the JSONL files. The CLI's
// "sub info" and "user info" read this.
type Info struct {
	Comments    int64 `json:"comments"`
	Submissions int64 `json:"submissions"`
	Authors     int64 `json:"authors"`
	Subreddits  int64 `json:"subreddits"`
	FirstUTC    int64 `json:"first_utc"`
	LastUTC     int64 `json:"last_utc"`
	SizeBytes   int64 `json:"size_bytes"`
}

// scanBufSize gives the line scanner room for the long bodies that show up in
// the dump. The default bufio.Scanner buffer is too small for them.
const scanBufSize = 16 << 20

// infoCacheName is the per-directory cache Inspect writes so a repeat call skips
// re-scanning when the JSONL has not changed.
const infoCacheName = "info.json"

// Inspect scans the comments.jsonl and submissions.jsonl in dir and returns an
// Info. It counts lines, tracks distinct authors and subreddits, and records the
// min and max created_utc across both files. The "[deleted]" placeholder author
// is not counted as a distinct author.
//
// Inspect writes an info.json cache in dir keyed on the JSONL file sizes, and
// reuses it on a later call when those sizes still match.
func Inspect(dir string) (Info, error) {
	comments := filepath.Join(dir, "comments.jsonl")
	submissions := filepath.Join(dir, "submissions.jsonl")

	commentsSize := fileSize(comments)
	submissionsSize := fileSize(submissions)
	totalSize := commentsSize + submissionsSize

	if totalSize == 0 {
		return Info{}, fmt.Errorf("no jsonl files in %s", dir)
	}

	// Reuse the cache when the on-disk sizes still match what it was built for.
	cachePath := filepath.Join(dir, infoCacheName)
	if cached, ok := readInfoCache(cachePath); ok && cached.SizeBytes == totalSize {
		return cached.Info, nil
	}

	authors := make(map[string]struct{})
	subreddits := make(map[string]struct{})

	info := Info{SizeBytes: totalSize}

	for _, src := range []struct {
		path    string
		counter *int64
	}{
		{comments, &info.Comments},
		{submissions, &info.Submissions},
	} {
		if err := scanFile(src.path, src.counter, authors, subreddits, &info); err != nil {
			return Info{}, err
		}
	}

	info.Authors = int64(len(authors))
	info.Subreddits = int64(len(subreddits))

	writeInfoCache(cachePath, info)
	return info, nil
}

// scanFile reads one JSONL file line by line, counting records and folding each
// line's author, subreddit, and created_utc into the running tallies. A missing
// file is fine: it leaves the counts untouched.
func scanFile(path string, count *int64, authors, subreddits map[string]struct{}, info *Info) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), scanBufSize)

	first := info.FirstUTC == 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		*count++

		if a := gjson.GetBytes(line, "author").String(); a != "" && a != "[deleted]" {
			authors[a] = struct{}{}
		}
		if s := gjson.GetBytes(line, "subreddit").String(); s != "" {
			subreddits[s] = struct{}{}
		}

		if r := gjson.GetBytes(line, "created_utc"); r.Exists() {
			utc := r.Int()
			if first || utc < info.FirstUTC {
				info.FirstUTC = utc
				first = false
			}
			if utc > info.LastUTC {
				info.LastUTC = utc
			}
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

// fileSize returns the size of path, or zero when it is missing.
func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// infoCache is the on-disk form of Inspect's result plus the size it was built
// for, so a later call can tell whether the JSONL has changed.
type infoCache struct {
	Info
}

func readInfoCache(path string) (infoCache, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return infoCache{}, false
	}
	var c infoCache
	if json.Unmarshal(data, &c) != nil {
		return infoCache{}, false
	}
	return c, true
}

func writeInfoCache(path string, info Info) {
	data, err := json.Marshal(infoCache{Info: info})
	if err != nil {
		return
	}
	// A cache write failure is not worth failing the inspection over.
	_ = os.WriteFile(path, data, 0o644)
}
