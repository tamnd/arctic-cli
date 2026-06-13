---
title: "Configuration"
description: "The data directory, the raw and work trees, the index, the environment, the engine, and the global flags with their defaults."
weight: 20
---

arctic needs almost no configuration. There is no config file; every option is a
flag or an environment variable, and the defaults are chosen so the common case
needs neither. See everything arctic resolved with:

```bash
arctic info
```

It prints the detected hardware, the work budget, the active engine, and the
resolved storage paths.

## The data directory

arctic keeps its state under one tree. It defaults to `$XDG_DATA_HOME/arctic`,
which is `~/.local/share/arctic` on Linux when `XDG_DATA_HOME` is unset. Point it
elsewhere with `--data-dir` or the `ARCTIC_DATA_DIR` environment variable:

```bash
export ARCTIC_DATA_DIR=/mnt/big/arctic
arctic sub golang
```

Under the data directory live the downloaded `.zst` files, the per-entity Parquet
imports, the scratch work directory, and the SQLite index.

## The raw and work directories

Two of those subtrees can be split out to their own paths, which is useful when
your downloads and your conversion scratch want different disks:

| Flag | Default | What lives there |
|---|---|---|
| `--raw-dir` | under the data dir | Downloaded `.zst` dump files |
| `--work-dir` | under the data dir | Conversion scratch and the Parquet shards |

```bash
arctic pull 2024-01..2024-03 --process \
  --raw-dir /mnt/spinning/raw \
  --work-dir /mnt/ssd/work
```

The `work-dir` disk is also where `arctic info` measures `disk_free_gb`, since
that is where the conversion happens.

## The index

Each import is recorded in a small SQLite index under the data directory.
`arctic stats` reads it to summarize what you hold by month, type, or subreddit
without rescanning the Parquet. `arctic info` reports its path as `index_path`.
You do not configure it directly; it follows the data directory.

## The conversion engine

`--engine` chooses how JSONL becomes Parquet:

| Value | Needs | Notes |
|---|---|---|
| `go` | nothing | Pure Go, the default, in every binary |
| `duckdb` | a `-tags duckdb` build with cgo | Faster on a capable host; respects `duckdb_memory_mb` |

A pure-Go binary asked for `--engine duckdb` exits with a usage error. `arctic
info` shows `duckdb_available` so you know which build you have. See
[the hardware budget](/guides/hardware-budget/).

## The API request identity

The Arctic Shift API path sends a default User-Agent. Override it with
`--user-agent` if you are identifying your own client, and set the per-request
timeout with `--timeout`:

```bash
arctic sub golang --api --user-agent "my-archiver/1.0" --timeout 90s
```

## Sizing knobs

| Flag | Default | Meaning |
|---|---|---|
| `-j, --workers` | `0` | Concurrent workers; `0` uses the computed budget |
| `--chunk-lines` | engine default | JSONL lines per Parquet shard |
| `--timeout` | `60s` | Per-request timeout on the API path |

`--workers 0` lets arctic size the parallelism from the detected machine. A
non-zero value overrides the convert and download caps (and the API fetch
concurrency). See [the hardware budget](/guides/hardware-budget/).

## Environment variables

| Variable | Used for |
|---|---|
| `ARCTIC_DATA_DIR` | Root data directory (overrides the XDG default) |
| `HF_TOKEN` | Hugging Face write token for `publish --commit` |

## Global flags

| Flag | Default | Meaning |
|---|---|---|
| `-o, --output` | auto | `table`, `json`, `jsonl`, `csv`, `tsv`, `url`, `raw` |
| `--fields` | all | Comma-separated columns to include |
| `--no-header` | off | Omit the header row in table/csv/tsv |
| `--template` | none | Go text/template applied per record |
| `--color` | auto | `auto`, `always`, or `never` |
| `-n, --limit` | `0` | Maximum records; `0` is no limit |
| `-q, --quiet` | off | Suppress progress on stderr |
| `-j, --workers` | `0` | Concurrent workers; `0` uses the budget |
| `--data-dir` | XDG | Root data directory (env `ARCTIC_DATA_DIR`) |
| `--raw-dir` | under data dir | Where downloaded `.zst` files land |
| `--work-dir` | under data dir | Conversion scratch and shards |
| `--engine` | `go` | Conversion engine: `go` or `duckdb` |
| `--chunk-lines` | engine default | JSONL lines per Parquet shard |
| `--timeout` | `60s` | Per-request timeout for the API path |
| `--user-agent` | arctic default | User-Agent on the API path |

## Output auto-detection

With no `-o`, the default output format adapts to where it is going: an aligned
table when the output is a terminal, JSONL when it is piped. See
[output formats](/reference/output/).

## Exit codes

| Code | Meaning |
|---|---|
| `0` | OK |
| `1` | Error |
| `2` | Usage error |
| `3` | No data (nothing matched, published, or imported) |
| `4` | Partial (some targets in a batch failed) |
| `5` | Blocked (the API rate-limited or refused) |
| `75` | Commit stalled (restart a `publish` run to resume) |
