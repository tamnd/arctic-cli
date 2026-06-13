// Package shift is the client for the Arctic Shift backfill API at
// https://arctic-shift.photon-reddit.com. It fetches a single subreddit's or a
// single user's full public Reddit history as JSONL over HTTP, paginated by the
// created_utc timestamp.
//
// The API is the only path for individual users (the bulk torrents are keyed by
// subreddit, not by author) and the only path for data newer than the last
// published monthly dump. It serves records back to the 2005-01-01 epoch; this
// package clamps any earlier request up to that floor.
//
// This package speaks HTTP and writes JSONL. It does no torrent work and no
// Parquet conversion; the arctic package and the CLI orchestrate those around
// the JSONL this package produces. The Inspect helper reads back a written
// entity directory to report counts and the date span without re-parsing into
// the full schema.
package shift
