---
title: "Bulk pulls"
description: "Pull a span of monthly bulk dumps from the torrent catalog, convert them to Parquet as they land, and summarize what you hold."
weight: 10
---

The bulk path is for a span of time across all of Reddit. `arctic pull`
downloads the monthly dumps from the public torrent catalog, and with one flag
converts each completed file to Parquet. `arctic stats` then summarizes what you
hold. This is the path behind `arctic publish` too.

## What the catalog covers

The catalog is the set of months the bulk torrent covers. The older months sit in
a single large bundle; the recent months are one torrent per month. List the full
range:

```bash
arctic catalog
```

Each row carries the month and whether it is in the bundle. Add `--sizes` to
fetch the per-file byte sizes from the catalog, which is worth doing before a
large pull so you know what you are about to download:

```bash
arctic catalog --sizes -o table
```

## Pull a month range

Name months as arguments. A single month, a list, or a `..` range all work:

```bash
arctic pull 2024-01                      # one month
arctic pull 2024-01 2024-02 2024-03      # a list
arctic pull 2024-01..2024-03             # a range
```

Or set the bounds with `--from` and `--to`, which default to the catalog start
and end, so bare `arctic pull --from 2024-01 --to 2024-03` is the same range:

```bash
arctic pull --from 2024-01 --to 2024-03
```

Each month pulls both record types by default. Narrow it with `--type`:

```bash
arctic pull 2024-01..2024-03 --type comments
arctic pull 2024-01..2024-03 --type submissions
```

`--type` takes `comments`, `submissions`, or `both` (the default). A month that
the catalog has not published yet is reported as "not yet published" and skipped
rather than failing the whole run.

## Convert as you pull

By default `pull` only downloads the `.zst` files. Add `--process` to convert
each completed file to Parquet right after it lands, so the download and the
conversion overlap instead of running as two separate passes:

```bash
arctic pull 2024-01..2024-03 --process
```

Each converted file reports its shard count, row count, and how many lines were
skipped as unparseable. The shards go under the work directory, and the import is
recorded in the local index.

If you already have decompressed dumps on disk and only want the conversion step,
`process` runs it directly on one or more files:

```bash
arctic process RC_2024-01.zst RS_2024-01.zst
arctic process RC_2024-01.zst --out ./parquet/2024-01/comments
```

`process` infers comments from submissions by the file name: an `RC_` or
`_comments` name is comments, an `RS_` or `_submissions` name is submissions. A
file it cannot classify by name is skipped with a note.

## Summarize what you hold

Once months are imported, `stats` reads the local index and summarizes it without
rescanning the Parquet:

```bash
arctic stats --by month
arctic stats --by type
arctic stats --by subreddit
```

`--by` takes `month`, `type`, or `subreddit` (default `month`). It is the fast
way to confirm a pull landed and to see the row counts you accumulated:

```bash
arctic stats --by month -o csv > coverage.csv
```

## A whole bulk session

Putting it together, taking a quarter of the archive and confirming it looks
like this:

```bash
arctic catalog --sizes                   # size the download first
arctic pull 2024-01..2024-03 --process   # download and convert
arctic stats --by month                  # confirm the row counts
```

The downloaded `.zst` files land in the raw directory, the Parquet shards in the
work directory, and the index alongside, all under the data directory. Point that
elsewhere with `--data-dir` or `ARCTIC_DATA_DIR`, or split the raw and work trees
with `--raw-dir` and `--work-dir`. See
[configuration](/reference/configuration/).

## Sizing the run

A bulk pull is bounded by your CPU, memory, and disk. arctic derives how many
downloads and conversions to run at once from the detected hardware; see
[the hardware budget](/guides/hardware-budget/). Override the convert and
download caps with `--workers` when you want to set the parallelism by hand, and
pick the conversion engine with `--engine go` or `--engine duckdb`.
