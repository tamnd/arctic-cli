package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/parquet-go/parquet-go"
	"github.com/tamnd/arctic-cli/arctic"
)

// filter is the small, indexable set of conditions query and info apply to a
// shard scan. A zero value matches everything.
type filter struct {
	author    string
	subreddit string
	after     int64
	before    int64
	minScore  int64
	contains  string
	limit     int
}

func (f filter) matchComment(c arctic.Comment) bool {
	if f.author != "" && !strings.EqualFold(c.Author, f.author) {
		return false
	}
	if f.subreddit != "" && !strings.EqualFold(c.Subreddit, f.subreddit) {
		return false
	}
	if f.after > 0 && c.CreatedUTC < f.after {
		return false
	}
	if f.before > 0 && c.CreatedUTC > f.before {
		return false
	}
	if f.minScore != 0 && c.Score < f.minScore {
		return false
	}
	if f.contains != "" && !strings.Contains(strings.ToLower(c.Body), strings.ToLower(f.contains)) {
		return false
	}
	return true
}

func (f filter) matchSubmission(s arctic.Submission) bool {
	if f.author != "" && !strings.EqualFold(s.Author, f.author) {
		return false
	}
	if f.subreddit != "" && !strings.EqualFold(s.Subreddit, f.subreddit) {
		return false
	}
	if f.after > 0 && s.CreatedUTC < f.after {
		return false
	}
	if f.before > 0 && s.CreatedUTC > f.before {
		return false
	}
	if f.minScore != 0 && s.Score < f.minScore {
		return false
	}
	if f.contains != "" {
		hay := strings.ToLower(s.Title + " " + s.Selftext)
		if !strings.Contains(hay, strings.ToLower(f.contains)) {
			return false
		}
	}
	return true
}

// shardDir is where ProcessFile writes a type's shards under an entity import.
func shardDir(cfg arctic.Config, kind, name string, t arctic.Type) string {
	return filepath.Join(cfg.EntityDir(kind, name), string(t))
}

// recordShards adds every Parquet shard in dir to the local index so stats can
// roll them up without re-reading the data. entity is the subreddit or user name
// for per-entity imports and empty for monthly dumps; year and month are set for
// monthly dumps and zero otherwise. Row counts come from the Parquet footer and
// byte sizes from the file, so it never reads the row data.
func recordShards(cfg arctic.Config, t arctic.Type, year, month int, entity, dir string) error {
	paths, err := listShards(dir)
	if err != nil || len(paths) == 0 {
		return err
	}
	idx, err := arctic.OpenIndex(cfg.IndexPath())
	if err != nil {
		return err
	}
	defer idx.Close()
	for _, path := range paths {
		records, size, ferr := shardFooter(path)
		if ferr != nil {
			return ferr
		}
		if rerr := idx.RecordShard(t, year, month, entity, path, records, size); rerr != nil {
			return rerr
		}
	}
	return nil
}

// shardFooter reads one shard's row count and byte size without loading the rows.
func shardFooter(path string) (records, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, 0, err
	}
	pf, err := parquet.OpenFile(f, fi.Size())
	if err != nil {
		return 0, 0, err
	}
	return pf.NumRows(), fi.Size(), nil
}

// listShards returns the .parquet files in dir, in stable order.
func listShards(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.parquet"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// scanComments reads the comment shards in dir, applies f, and returns matches.
// It loads one shard at a time so a large import does not need to fit in memory
// all at once, and stops as soon as the limit is met.
func scanComments(dir string, f filter) ([]arctic.Comment, error) {
	shards, err := listShards(dir)
	if err != nil {
		return nil, err
	}
	var out []arctic.Comment
	for _, path := range shards {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		rows, err := parquet.Read[arctic.Comment](bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, c := range rows {
			if !f.matchComment(c) {
				continue
			}
			out = append(out, c)
			if f.limit > 0 && len(out) >= f.limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// scanSubmissions mirrors scanComments for submission shards.
func scanSubmissions(dir string, f filter) ([]arctic.Submission, error) {
	shards, err := listShards(dir)
	if err != nil {
		return nil, err
	}
	var out []arctic.Submission
	for _, path := range shards {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		rows, err := parquet.Read[arctic.Submission](bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, s := range rows {
			if !f.matchSubmission(s) {
				continue
			}
			out = append(out, s)
			if f.limit > 0 && len(out) >= f.limit {
				return out, nil
			}
		}
	}
	return out, nil
}
