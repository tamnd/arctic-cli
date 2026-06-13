---
title: "Introduction"
description: "The two paths into the archive, the Parquet conversion with a fixed schema, the local index, and the hardware budget arctic sizes its work against."
weight: 10
---

The whole public history of [Reddit](https://www.reddit.com) comments and
submissions has been archived as monthly zstd-compressed JSONL dumps and seeded
on [Academic Torrents](https://academictorrents.com). It is a large corpus:
hundreds of files, terabytes uncompressed, decoder windows that want two
gigabytes of memory, and a JSON shape that has drifted over twenty years. arctic
turns that pile into something you can query.

arctic is a single binary. You point it at a month range or a community, and it
fetches the parts you ask for, converts each dump into columnar Parquet with a
stable schema, keeps a local index of what you hold, and reads it back with
filters and a choice of output formats.

## Two paths into the archive

Two acquisition paths feed the same Parquet store.

The **bulk path** downloads the monthly dumps from the public torrent catalog:
`RC_YYYY-MM.zst` for comments and `RS_YYYY-MM.zst` for submissions. The catalog
is a single large bundle for the older months and one torrent per month for the
recent ones; `arctic catalog` lists the full range it covers. Each file is
verified, decompressed with a two-gigabyte zstd decoder window, and read line by
line. This is the path for a span of time across all of Reddit, and it is what
`arctic pull` and `arctic publish` run.

The **entity path** serves one subreddit or one account. For a community that is
in the per-subreddit bundle, arctic pulls its file directly; otherwise it streams
the records from the [Arctic Shift](https://arctic-shift.photon-reddit.com)
backfill API, page by page, clamped to the polite rate the service asks for.
Either way the result is the same JSONL the bulk dumps carry. This is the path
for one community or one person, and it is what `arctic sub` and `arctic user`
run. Because of the fallback, `arctic sub golang` works whether or not golang is
in a torrent, and `arctic user spez` always goes through the API since there is
no per-account torrent.

## Conversion to Parquet

Conversion reads the JSONL and writes Parquet in shards, 500,000 rows each by
default, compressed with zstd. The schema is fixed: twelve columns for comments
and fourteen for submissions, the same shape regardless of which path or which
month the data came from, so a query reads uniformly across everything you
imported. Lines that do not parse are counted and skipped rather than aborting a
multi-gigabyte file, because a twenty-year archive carries some malformed
records and one bad line should not cost you a whole month.

The default conversion engine is pure Go and needs nothing installed alongside
the binary. An optional DuckDB engine does the same conversion through
`read_json` and `COPY ... TO`; it is faster on a capable host but needs a binary
built with `-tags duckdb` and a cgo toolchain. Pick it per run with
`--engine duckdb`.

## The local index

Every import records what it produced in a small local SQLite index. That lets
`arctic stats` summarize what you hold by month, by type, or by subreddit
without rescanning the Parquet, and lets `arctic sub info` and `arctic user info`
report the shard count, row count, byte size, and date span of one entity from
the Parquet footers. The index is metadata about your imports; the records
themselves stay in the Parquet shards under the data directory.

## The hardware budget

A conversion this large is bounded by CPU, memory, and disk, so arctic sizes its
own work to the machine instead of assuming a fixed level of parallelism.
`arctic info` reports the detected CPU count, total and available memory, and
free disk, and the budget it derives from them: how many downloads and
conversions to run at once (`MaxDownloads`, `MaxProcess`, `MaxConvertWorkers`),
how much memory to give DuckDB (`DuckDBMemoryMB`), and whether to fall back to a
strictly sequential run on a small host. The `--workers` flag overrides the
convert and download caps when you want to set them by hand. See the
[hardware budget guide](/guides/hardware-budget/) for the details.

## Independent and public-data only

arctic is an independent, open-source tool. It is not affiliated with, endorsed
by, or sponsored by Reddit, Inc. It moves only public archive data: the monthly
dumps seeded on Academic Torrents and the records served by the Arctic Shift
backfill API. It does not log in, hold credentials, or touch anything behind an
account.

Next: [install it](/getting-started/installation/), then take the
[quick start](/getting-started/quick-start/).
