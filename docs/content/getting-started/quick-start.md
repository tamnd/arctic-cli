---
title: "Quick start"
description: "From an empty data directory to a real community imported, queried, and summarized, in a handful of commands."
weight: 30
---

This walks the core loop: see what the catalog covers, acquire one community,
query the records back, and inspect what you hold. None of it needs an account or
a token. The first run downloads and converts data, so give it a moment and a few
gigabytes of free disk.

## 1. See what the catalog covers

```bash
arctic catalog
```

Each row is a month the bulk torrent catalog covers, with a flag for whether that
month is in the single large bundle or its own per-month torrent. Add `--sizes`
to fetch the per-file byte sizes from the catalog before you commit to a download:

```bash
arctic catalog --sizes
```

## 2. Acquire one community

```bash
arctic sub golang
```

This pulls r/golang's full history. arctic first checks the per-subreddit torrent
bundle; if golang is in it, it downloads that file, and if not it streams the
records from the Arctic Shift API. Either way it converts the result to Parquet
and records the import in the local index. Narrow it to one record type or a date
window:

```bash
arctic sub golang --kind submissions --after 2020-01-01
```

## 3. Query what you imported

```bash
arctic query golang --contains generics -n 20
```

`query` scans the Parquet shards of an entity you already imported. Filter by
author, score, and date, and search a substring of the body, title, or selftext:

```bash
arctic query golang --author rob_pike --min-score 100
arctic query golang --kind submissions --after 2024-01-01 --contains "go 1.22"
```

## 4. Inspect one entity

```bash
arctic sub info golang
```

That reports what is imported locally for r/golang: the shard count, row count,
byte size, and the date span, per type, read straight from the Parquet footers.
There is a matching `arctic user info <name>` for accounts.

## 5. See the budget and paths

```bash
arctic info
```

That prints the detected hardware, the work budget arctic derives from it
(`MaxDownloads`, `MaxProcess`, `MaxConvertWorkers`, `DuckDBMemoryMB`), the active
engine, and the resolved storage paths.

## A bulk pull

For a span of time across all of Reddit rather than one community, pull a month
range and convert it as it lands:

```bash
arctic pull 2024-01..2024-03 --process
arctic stats --by month
```

`stats` then summarizes the local index: rows per month here, or `--by type` and
`--by subreddit` for the other cuts.

## Where to next

You have the core loop. From here:

- [Bulk pulls](/guides/bulk-pulls/) covers `pull`, month ranges, `--process`, and
  `stats`.
- [Subreddits and users](/guides/subreddits-and-users/) goes deep on the `sub`
  and `user` paths and their `info` subcommands.
- [Querying](/guides/querying/) covers every `query` filter and how the scan
  works.
- [Output formats](/guides/output-formats/) covers the table, JSON, CSV, and
  template rendering.
- [Publishing](/guides/publishing/) covers `publish` and Hugging Face.
- [The hardware budget](/guides/hardware-budget/) covers `info` and the engines.
- The [CLI reference](/reference/cli/) lists every command and flag.
