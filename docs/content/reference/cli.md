---
title: "CLI"
description: "Every command and subcommand, with the flags that matter and one example each."
weight: 10
---

```
arctic <command> [args] [flags]
```

Run `arctic <command> --help` for the full flag list on any command. This page is
the map. The global flags at the end apply to every command.

## Commands

| Command | What it does |
|---|---|
| `pull` | Download monthly bulk dumps from the torrent catalog |
| `sub` | A community's history, torrent first then API |
| `sub info` | What is imported locally for one subreddit |
| `user` | An account's history through the Arctic Shift API |
| `user info` | What is imported locally for one account |
| `process` | Convert decompressed JSONL dumps into Parquet |
| `query` | Filter imported records from one entity |
| `catalog` | List the months the bulk torrent catalog covers |
| `stats` | Summarize the local index of imports |
| `publish` | Convert a month range and upload to Hugging Face |
| `info` | Report detected hardware, the work budget, and storage paths |
| `version` | Print version, commit, and build date |

## pull

```
arctic pull [months...] [flags]
```

Downloads the monthly `RC_`/`RS_` dumps from the public torrent catalog. Name
months as arguments: a single month, a list, or a `2024-01..2024-03` range. With
no arguments it uses `--from` and `--to`. With `--process` each completed file is
converted to Parquet as it lands.

| Flag | Default | Meaning |
|---|---|---|
| `--from` | catalog start | First month (`YYYY-MM`) when no months are named |
| `--to` | catalog end | Last month (`YYYY-MM`) when no months are named |
| `--type` | `both` | `comments`, `submissions`, or `both` |
| `--process` | off | Convert each completed file to Parquet |

```bash
arctic pull 2024-01..2024-03 --type comments --process
```

## sub

```
arctic sub <subreddit> [flags]
```

Pulls a community's full history. Torrent-first: it downloads the per-subreddit
bundle file when the community is in it, and falls back to the Arctic Shift API
otherwise. `--api` forces the API path. The argument accepts `golang`, `r/golang`,
or `/r/golang`.

| Flag | Default | Meaning |
|---|---|---|
| `--api` | off | Force the Arctic Shift API path |
| `--kind` | `both` | `comments`, `submissions`, or `both` |
| `--after` | none | Earliest date (`YYYY`, `YYYY-MM`, or `YYYY-MM-DD`) |
| `--before` | none | Latest date |
| `--no-import` | off | Download only, skip the Parquet import |

```bash
arctic sub golang --kind submissions --after 2020-01-01
```

### sub info

```
arctic sub info <name>
```

Reports what is imported locally for one subreddit: shard count, row count, byte
size, and the date span, per type. Exits 3 if nothing is imported.

```bash
arctic sub info golang
```

## user

```
arctic user <username> [flags]
```

Pulls one account's full history through the Arctic Shift API (there is no
per-account torrent). Takes the same `--kind`, `--after`, `--before`, and
`--no-import` as `sub`. The argument accepts `spez`, `u/spez`, or `/user/spez`.

```bash
arctic user spez --kind comments --after 2023-01-01
```

### user info

```
arctic user info <name>
```

Reports what is imported locally for one account, the same shape as `sub info`.

```bash
arctic user info spez
```

## process

```
arctic process <file.zst>... [flags]
```

Converts one or more decompressed JSONL dumps into Parquet. It infers the record
type from the file name: an `RC_` or `_comments` name is comments, an `RS_` or
`_submissions` name is submissions. A file it cannot classify is skipped with a
note.

| Flag | Default | Meaning |
|---|---|---|
| `--out` | alongside the input | Output directory for the Parquet shards |

```bash
arctic process RC_2024-01.zst RS_2024-01.zst --out ./parquet/2024-01
```

## query

```
arctic query <subreddit|user> [flags]
```

Scans the Parquet shards of an entity you have already imported and filters them.
A `u/` prefix or `--user` reads the argument as an account; everything else is a
subreddit.

| Flag | Default | Meaning |
|---|---|---|
| `--author` | none | Filter by author |
| `--contains` | none | Case-insensitive substring in body/title/selftext |
| `--min-score` | `0` | Minimum score |
| `--after` | none | Earliest date (`YYYY`, `YYYY-MM`, or `YYYY-MM-DD`) |
| `--before` | none | Latest date |
| `--kind` | `both` | `comments`, `submissions`, or `both` |
| `--user` | off | Read the argument as a username |

```bash
arctic query golang --contains generics --min-score 100 -n 20
```

## catalog

```
arctic catalog [flags]
```

Lists the months the bulk torrent catalog covers, with a flag for whether each
month is in the single large bundle. With `--sizes` it fetches the per-file byte
sizes from the catalog over the network.

| Flag | Default | Meaning |
|---|---|---|
| `--sizes` | off | Fetch per-file sizes from the catalog |

```bash
arctic catalog --sizes
```

## stats

```
arctic stats [flags]
```

Summarizes the local index of imported shards without rescanning the Parquet.

| Flag | Default | Meaning |
|---|---|---|
| `--by` | `month` | Group by `month`, `type`, or `subreddit` |

```bash
arctic stats --by type
```

## publish

```
arctic publish [flags]
```

Processes a month range into Parquet and uploads it to a Hugging Face dataset.
Reads the token from `HF_TOKEN`. Without `--commit` it runs the full pipeline but
skips the upload. A stalled commit exits 75 so a supervisor can restart and resume
from the `stats.csv` ledger.

| Flag | Default | Meaning |
|---|---|---|
| `--from` | catalog start | First month (`YYYY-MM`) |
| `--to` | catalog end | Last month (`YYYY-MM`) |
| `--type` | `both` | `comments`, `submissions`, or `both` |
| `--repo` | default dataset | Hugging Face dataset repo |
| `--commit` | off | Upload (default is a dry run) |
| `--private` | off | Create the dataset repo as private |
| `--keep` | off | Keep local Parquet after a successful commit |

```bash
arctic publish --from 2024-01 --to 2024-03 --commit
```

## info

```
arctic info
```

Reports the detected hardware, the work budget arctic derives from it, the active
engine and whether DuckDB is available, and the resolved storage paths.

```bash
arctic info
```

## version

```
arctic version
```

Prints the version, commit, and build date.

```bash
arctic version
```

## Global flags

These apply to every command. See [configuration](/reference/configuration/) for
the defaults and the environment.

| Flag | Meaning |
|---|---|
| `-o, --output` | `table`, `json`, `jsonl`, `csv`, `tsv`, `url`, `raw` (auto = table on a TTY, jsonl piped) |
| `--fields` | Comma-separated columns to include |
| `--no-header` | Omit the header row in table/csv/tsv |
| `--template` | Go text/template applied per record |
| `--color` | `auto`, `always`, or `never` |
| `-n, --limit` | Maximum records (`0` means no limit) |
| `-q, --quiet` | Suppress progress on stderr |
| `-j, --workers` | Concurrent workers (`0` uses the hardware budget) |
| `--data-dir` | Root for per-entity imports and the index (env `ARCTIC_DATA_DIR`) |
| `--raw-dir` | Where downloaded `.zst` files land |
| `--work-dir` | Scratch for conversion |
| `--engine` | Conversion engine: `go` or `duckdb` |
| `--chunk-lines` | JSONL lines per Parquet shard |
| `--timeout` | Per-request timeout for the API path |
| `--user-agent` | Override the API request User-Agent |
