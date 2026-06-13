package shift

import (
	"context"
	"io"

	"github.com/tamnd/arctic-cli/arctic"
)

// FetchUser pages the API for one account's records of type t with a created_utc
// in [after, before), writes one JSON object per line to out, and returns the
// number of records written. Users have no per-subreddit torrent, so the API is
// the only path. after is clamped up to arctic.MinEpoch; a before of zero means
// "up to now". name carries no u/ prefix.
func (c *Client) FetchUser(ctx context.Context, name string, t arctic.Type, after, before int64, out io.Writer, cb ProgressCallback) (int64, error) {
	return c.fetch(ctx, "user", name, t, after, before, out, cb)
}
