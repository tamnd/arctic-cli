package shift

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/tamnd/arctic-cli/arctic"
)

// defaultWorkers is the fetch concurrency when the caller passes a non-positive
// worker count. Eight keeps a long range moving without leaning too hard on the
// API.
const defaultWorkers = 8

// FetchRange fetches an entity's records of type t over [after, before) by
// splitting the span into one-month buckets and pulling them concurrently, then
// writing the buckets to out in chronological order. kind is "subreddit" or
// "user". It returns the total records written.
//
// Each bucket lands in its own temp file first so concurrent workers never
// interleave their lines on out, then the files concatenate in order. The result
// is the same byte stream a single sequential FetchSubreddit or FetchUser would
// produce, gathered faster.
func (c *Client) FetchRange(ctx context.Context, kind, name string, t arctic.Type, after, before int64, workers int, out io.Writer, cb ProgressCallback) (int64, error) {
	if after < arctic.MinEpoch {
		after = arctic.MinEpoch
	}
	if before <= 0 {
		before = time.Now().Unix()
	}
	if after >= before {
		return 0, nil
	}

	buckets := monthBuckets(after, before)
	if len(buckets) == 0 {
		return 0, nil
	}

	if workers <= 0 {
		workers = defaultWorkers
	}
	if workers > len(buckets) {
		workers = len(buckets)
	}

	tmpDir, err := os.MkdirTemp("", "arctic-shift-")
	if err != nil {
		return 0, fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// counts[i] is the records written for bucket i; files[i] is its temp path.
	files := make([]string, len(buckets))
	counts := make([]int64, len(buckets))

	// total tracks records across buckets for the progress callback. Buckets
	// finish out of order, so ThroughEpoch carries the finishing bucket's window
	// end rather than a strict high-water mark.
	var total atomic.Int64

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)

	for i, b := range buckets {
		g.Go(func() error {
			path := fmt.Sprintf("%s/%04d.jsonl", tmpDir, i)
			files[i] = path

			f, err := os.Create(path)
			if err != nil {
				return fmt.Errorf("create bucket %s: %w", b.label, err)
			}

			n, err := c.fetch(gctx, kind, name, t, b.after, b.before, f, nil)
			if err != nil {
				_ = f.Close()
				return fmt.Errorf("bucket %s: %w", b.label, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close bucket %s: %w", b.label, err)
			}
			counts[i] = n

			if cb != nil {
				cb(Progress{
					Type:         string(t),
					Count:        total.Add(n),
					ThroughEpoch: b.before,
				})
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return 0, err
	}

	// Concatenate in bucket order so out reads oldest to newest.
	var written int64
	for i := range buckets {
		if files[i] == "" {
			continue
		}
		f, err := os.Open(files[i])
		if err != nil {
			return written, fmt.Errorf("reopen bucket: %w", err)
		}
		if _, err := io.Copy(out, f); err != nil {
			_ = f.Close()
			return written, fmt.Errorf("concat bucket: %w", err)
		}
		_ = f.Close()
		written += counts[i]
	}

	return written, nil
}

// bucket is one calendar-month slice of a fetch range.
type bucket struct {
	label  string
	after  int64
	before int64
}

// monthBuckets splits [after, before) into calendar-month windows clamped to the
// span. Each window is [start-of-month, start-of-next-month) intersected with the
// range, so the union covers the span exactly with no overlap.
func monthBuckets(after, before int64) []bucket {
	start := time.Unix(after, 0).UTC()
	end := time.Unix(before, 0).UTC()

	var buckets []bucket
	for y := start.Year(); y <= end.Year(); y++ {
		first := time.January
		last := time.December
		if y == start.Year() {
			first = start.Month()
		}
		if y == end.Year() {
			last = end.Month()
		}
		for m := first; m <= last; m++ {
			ba := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).Unix()
			bb := time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC).Unix()
			if ba < after {
				ba = after
			}
			if bb > before {
				bb = before
			}
			if ba >= bb {
				continue
			}
			buckets = append(buckets, bucket{
				label:  fmt.Sprintf("%04d-%02d", y, int(m)),
				after:  ba,
				before: bb,
			})
		}
	}
	return buckets
}
