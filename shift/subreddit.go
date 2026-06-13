package shift

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/tamnd/arctic-cli/arctic"
)

// Progress reports how far a fetch has advanced. Count is the records written so
// far; ThroughEpoch is the created_utc of the most recent record seen, so a
// caller can show "caught up to date X".
type Progress struct {
	Type         string
	Count        int64
	ThroughEpoch int64
}

// ProgressCallback receives a Progress after each page. It may be nil.
type ProgressCallback func(Progress)

// FetchSubreddit pages the API for one subreddit's records of type t with a
// created_utc in [after, before), writes one JSON object per line to out, and
// returns the number of records written. after is clamped up to arctic.MinEpoch;
// a before of zero means "up to now". name carries no r/ prefix.
func (c *Client) FetchSubreddit(ctx context.Context, name string, t arctic.Type, after, before int64, out io.Writer, cb ProgressCallback) (int64, error) {
	return c.fetch(ctx, "subreddit", name, t, after, before, out, cb)
}

// fetch is the shared time-window pager behind FetchSubreddit and FetchUser. It
// walks forward by created_utc: each page is sorted ascending, and the next
// request starts just after the last record seen. An empty page ends the walk.
func (c *Client) fetch(ctx context.Context, kind, name string, t arctic.Type, after, before int64, out io.Writer, cb ProgressCallback) (int64, error) {
	if after < arctic.MinEpoch {
		after = arctic.MinEpoch
	}
	if before <= 0 {
		before = time.Now().Unix()
	}
	if after >= before {
		return 0, nil
	}

	endpoint := endpointFor(t)
	param := entityParam(kind)
	fields := fieldsFor(t)

	var count int64
	currentAfter := after

	for {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		params := url.Values{}
		params.Set(param, name)
		params.Set("limit", "auto")
		params.Set("sort", "asc")
		params.Set("fields", fields)
		params.Set("after", fmt.Sprint(currentAfter))
		params.Set("before", fmt.Sprint(before))

		body, err := c.get(ctx, "/api/"+endpoint+"/search", params)
		if err != nil {
			return count, err
		}

		var result struct {
			Data []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return count, fmt.Errorf("decode %s page: %w", kind, err)
		}

		// An empty page means the window is exhausted. The caller decides whether
		// zero records overall is a no-data condition.
		if len(result.Data) == 0 {
			break
		}

		for _, item := range result.Data {
			if _, err := out.Write(item); err != nil {
				return count, fmt.Errorf("write jsonl: %w", err)
			}
			if _, err := out.Write([]byte{'\n'}); err != nil {
				return count, fmt.Errorf("write jsonl: %w", err)
			}
		}
		count += int64(len(result.Data))

		lastEpoch, err := lastCreatedUTC(result.Data[len(result.Data)-1])
		if err != nil {
			return count, fmt.Errorf("read cursor: %w", err)
		}

		// Step past the last record. When a whole page shares one timestamp the
		// cursor would not move, so nudge it forward by a second to guarantee
		// progress and termination, accepting the rare loss of a same-second tail.
		if lastEpoch <= currentAfter {
			lastEpoch = currentAfter + 1
		}
		currentAfter = lastEpoch

		if cb != nil {
			cb(Progress{Type: string(t), Count: count, ThroughEpoch: lastEpoch})
		}

		// The window is closed once the cursor reaches before.
		if currentAfter >= before {
			break
		}
	}

	return count, nil
}

// lastCreatedUTC pulls created_utc from one record. The API sends it as a number
// that may carry a fractional part, so accept both integer and float forms.
func lastCreatedUTC(item json.RawMessage) (int64, error) {
	var rec struct {
		CreatedUTC json.Number `json:"created_utc"`
	}
	if err := json.Unmarshal(item, &rec); err != nil {
		return 0, err
	}
	if rec.CreatedUTC == "" {
		return 0, fmt.Errorf("record has no created_utc")
	}
	if n, err := rec.CreatedUTC.Int64(); err == nil {
		return n, nil
	}
	f, err := rec.CreatedUTC.Float64()
	if err != nil {
		return 0, fmt.Errorf("parse created_utc %q: %w", rec.CreatedUTC, err)
	}
	return int64(f), nil
}
