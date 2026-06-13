---
title: "arctic"
description: "A command line for the bulk Reddit archive. Pull the monthly torrent dumps and the Arctic Shift backfill API, convert them to Parquet with a stable schema, keep a local index, and query it back, all from one binary."
heroTitle: "The Reddit archive, from the command line"
heroLead: "arctic is a single binary that turns the bulk Reddit archive into queryable Parquet. It pulls the monthly torrent dumps and the Arctic Shift backfill API, decompresses the zstd JSONL, writes columnar Parquet with a fixed schema, keeps a local index of what you hold, and can publish the shards to a Hugging Face dataset."
heroPrimaryURL: "/getting-started/quick-start/"
heroPrimaryText: "Get started"
---

The whole public history of Reddit comments and submissions has been archived as
monthly zstd-compressed JSONL dumps and seeded on
[Academic Torrents](https://academictorrents.com). It is a large, awkward thing
to work with: hundreds of files, terabytes uncompressed, and a JSON shape that
has drifted over twenty years. arctic turns that pile into something you can
query.

```bash
arctic catalog                                 # which months the catalog covers
arctic sub golang                              # one community, torrent or API
arctic query golang --contains generics -n 20  # read back what you imported
arctic stats --by month                        # what you hold, summarized
```

The default binary is pure Go with no runtime dependencies. It fetches the parts
you ask for, converts each dump into columnar Parquet with a stable schema, keeps
a local SQLite index of what you hold, and reads it back with filters and a
choice of output formats. An optional DuckDB conversion engine is available
behind a build tag when you want its speed and have a cgo toolchain.

## What you can do with it

- **Pull the bulk dumps.** `arctic pull` downloads the monthly
  `RC_YYYY-MM.zst` (comments) and `RS_YYYY-MM.zst` (submissions) files from the
  public torrent catalog, a span at a time, and with `--process` converts each
  one to Parquet as it lands.
- **Acquire a single community or account.** `arctic sub golang` reaches for the
  per-subreddit torrent bundle when a community is in it and otherwise streams
  the same records from the Arctic Shift API. `arctic user spez` pulls one
  account through the API.
- **Convert to Parquet.** Every dump becomes columnar Parquet in shards, zstd
  compressed, with a fixed twelve-column schema for comments and fourteen for
  submissions. Bad lines are counted and skipped, not fatal.
- **Query what you hold.** `arctic query` scans the Parquet shards of an entity
  with filters on author, score, date, and a substring match.
- **Summarize the index.** `arctic stats` reports what you hold by month, by
  type, or by subreddit without rescanning the Parquet.
- **Publish to Hugging Face.** `arctic publish` runs the bulk pipeline over a
  month range and uploads the shards to a Hugging Face dataset, with a dry run by
  default and a resumable commit.
- **Size the work to the machine.** `arctic info` reports the detected hardware
  and the work budget it derives from it.

## Independent and public-data only

arctic is an independent, open-source tool. It is not affiliated with, endorsed
by, or sponsored by Reddit, Inc. It moves only public archive data: the monthly
dumps seeded on Academic Torrents and the records served by the
[Arctic Shift](https://arctic-shift.photon-reddit.com) backfill API. It does not
log in, hold credentials, or touch anything behind an account.

## Where to go next

- New here? Start with the [introduction](/getting-started/introduction/) for the
  two acquisition paths and the mental model, then the
  [quick start](/getting-started/quick-start/).
- Want to install it? See [installation](/getting-started/installation/).
- Looking for a specific task? The [guides](/guides/) cover bulk pulls,
  subreddits and users, querying, output formats, publishing, and the hardware
  budget.
- Need every flag? The [CLI reference](/reference/cli/) is the full surface.
