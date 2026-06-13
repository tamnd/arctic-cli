# arctic

A command line for the bulk [Reddit](https://www.reddit.com) archive. One binary
that pulls the monthly dumps from the public torrent catalog and the
[Arctic Shift](https://arctic-shift.photon-reddit.com) backfill API, decompresses
the zstd JSONL, writes Parquet, keeps a local index, and can publish the shards
to a [Hugging Face](https://huggingface.co) dataset. Reads as a table, JSON,
JSONL, CSV, or TSV.

```
arctic sub golang --kind submissions -o table -n 5
```

```
ID      AUTHOR        TITLE                                       SCORE  CREATED_AT
1abc23  rob_pike      Go 1.0 is released                          1842   2012-03-28
2def45  bradfitz      What are you working on this week?          1190   2014-01-06
3ghi67  dgryski       A deep dive into the scheduler              980    2016-09-12
4jkl89  peterbourgon  Generics, two years on                      640    2024-02-15
5mno01  klauspost     Show: a pure-Go zstd benchmark              512    2019-07-22
```

Full documentation: [arctic-cli.tamnd.com](https://arctic-cli.tamnd.com).

> arctic is an independent, open-source tool. It is not affiliated with,
> endorsed by, or sponsored by Reddit, Inc. It moves only public archive data.

## Why

The whole public history of Reddit comments and submissions has been archived as
monthly zstd-compressed JSONL dumps and seeded on
[Academic Torrents](https://academictorrents.com). It is a large, awkward thing
to work with: hundreds of files, terabytes uncompressed, decoder windows that
need two gigabytes of memory, and a JSON shape that has drifted over twenty
years. arctic turns that pile into something you can query: it fetches the parts
you ask for, converts each dump into columnar Parquet with a stable schema, keeps
a local index of what you hold, and reads it back with filters and a choice of
output formats.

For a single subreddit or a single account, the full-history torrents are the
wrong tool. arctic reaches for the per-subreddit bundle when a community is in
it, and otherwise streams the same records from the Arctic Shift backfill API,
so `arctic sub golang` works whether or not golang is in a torrent.

The default binary is pure Go with no runtime dependencies. An optional DuckDB
conversion engine is available behind a build tag for when you want its speed and
have a cgo toolchain.

## Install

```sh
go install github.com/tamnd/arctic-cli/cmd/arctic@latest
```

Or grab a prebuilt binary from the [releases page](https://github.com/tamnd/arctic-cli/releases),
install a Linux package (`deb`, `rpm`, `apk`), or pull the container image:

```sh
docker run --rm ghcr.io/tamnd/arctic catalog
```

Homebrew and Scoop:

```sh
brew install --cask tamnd/tap/arctic
scoop install arctic
```

Build from source:

```sh
git clone https://github.com/tamnd/arctic-cli
cd arctic-cli
make build              # produces ./bin/arctic (pure Go)
make build-duckdb       # optional cgo build with the DuckDB engine
```

## Quick start

```sh
arctic catalog                                 # which months the torrent covers
arctic sub golang                              # one community, torrent or API
arctic user spez                               # one account, via the API
arctic query golang --contains generics -n 20  # read back what you imported
arctic sub info golang                         # what is held locally for r/golang
arctic info                                    # detected hardware and work budget
```

Pull a span of monthly bulk dumps and convert them as they land:

```sh
arctic pull 2024-01..2024-03 --process
arctic stats --by month
```

## How it works

Two paths feed the same Parquet store.

The bulk path downloads the monthly `RC_YYYY-MM.zst` (comments) and
`RS_YYYY-MM.zst` (submissions) files from the public torrent catalog. The
catalog is a single large bundle for the older months and one torrent per month
for the recent ones; `arctic catalog` lists the full range. Each file is verified,
decompressed with a two-gigabyte zstd decoder window, and read line by line.

The entity path serves one subreddit or one account. For a community that is in
the per-subreddit bundle, arctic pulls its file directly; otherwise it streams
the records from the Arctic Shift API, page by page, clamped to the polite rate
the service asks for. Either way the result is the same JSONL the bulk dumps
carry.

Conversion reads the JSONL and writes Parquet in shards (default 500,000 rows
each), compressed with zstd, with a fixed twelve-column schema for comments and
fourteen for submissions. Bad lines are counted and skipped rather than
aborting a multi-gigabyte file. The default engine is pure Go; the optional
DuckDB engine does the same conversion through `read_json` and `COPY ... TO`.

Every import records what it produced in a small local SQLite index, so `arctic
stats` can summarize what you hold by month, by type, or by subreddit without
rescanning the Parquet.

arctic sizes its own work to the machine. `arctic info` reports the detected CPU
count, memory, and free disk, and the budget it derives from them: how many
downloads and conversions to run at once, how much memory to give DuckDB, and
whether to fall back to a strictly sequential run on a small host.

## Commands

| Command | What it does |
| --- | --- |
| `pull [months...]` | Download monthly bulk dumps (`--from`, `--to`, `--type`, `--process`) |
| `sub <subreddit>` | A community's history, torrent first then API (`--api`, `--kind`, `--after`, `--before`) |
| `sub info <name>` | What is imported locally for one subreddit |
| `user <username>` | An account's history via the Arctic Shift API |
| `user info <name>` | What is imported locally for one account |
| `process <file.zst>...` | Convert decompressed JSONL dumps into Parquet (`--out`) |
| `query <subreddit\|user>` | Filter imported records (`--author`, `--contains`, `--min-score`, `--after`, `--before`, `--kind`) |
| `catalog` | List the months the bulk torrent catalog covers (`--sizes`) |
| `stats` | Summarize the local index (`--by month\|type\|subreddit`) |
| `publish` | Convert a month range and upload to Hugging Face (`--from`, `--to`, `--commit`) |
| `info` | Report detected hardware, the work budget, and storage paths |
| `version` | Print version, commit, and build date |

## Output

Output is a table on a terminal and JSONL when piped, so it drops straight into
a pipeline. Pick any format explicitly with `-o`:

```sh
arctic query golang -o json                          # pretty JSON array
arctic query golang -o jsonl                         # one JSON object per line
arctic stats --by month -o csv                       # CSV with a header row
arctic query golang --fields author,score,body -o tsv
arctic query golang --template '{{.author}}: {{.body}}'
```

Choose columns with `--fields`, drop the header with `--no-header`, and apply a
Go `text/template` per record with `--template`.

## Publishing

`publish` runs the bulk pipeline over a month range and uploads the Parquet
shards to a Hugging Face dataset. Without `--commit` it does everything except
the upload, which is the way to rehearse a run. The token comes from `HF_TOKEN`.
A stalled commit exits with code 75 so a supervisor can restart the command and
resume from the last committed month, which is recorded in a `stats.csv` ledger
in the dataset.

```sh
export HF_TOKEN=hf_...
arctic publish --from 2024-01 --to 2024-03            # dry run
arctic publish --from 2024-01 --to 2024-03 --commit   # upload
```

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | error |
| 2 | usage error |
| 3 | no data (not published, empty result) |
| 4 | partial (some targets in a batch failed) |
| 5 | blocked (the API rate-limited or refused; slow down or lower `--workers`) |
| 75 | commit stalled (restart to resume; for supervised publish runs) |

## Configuration

State lives under `$XDG_DATA_HOME/arctic` (or `~/.local/share/arctic`),
overridable with `--data-dir` or `ARCTIC_DATA_DIR`. Downloaded `.zst` files, the
per-entity Parquet imports, the scratch work directory, and the SQLite index all
sit there. Networking and sizing knobs (`--workers`, `--engine`, `--chunk-lines`,
`--timeout`, `--user-agent`) are global flags on every command. Run `arctic
info` to see the resolved paths and the work budget, and `arctic <command>
--help` for the full surface.

## Library

The acquisition, conversion, indexing, and publishing live in the `arctic`
package, and the Arctic Shift client in `shift`, so you can build the archive
from your own program without the CLI:

```go
import "github.com/tamnd/arctic-cli/arctic"

cfg := arctic.DefaultConfig()
m, _ := arctic.ParseMonth("2024-01")
path, err := arctic.DownloadMonth(ctx, cfg, m, arctic.TypeComments, nil)
res, err := arctic.ProcessFile(ctx, cfg, path, arctic.TypeComments, "out", nil)
```

## Development

```sh
make build         # build ./bin/arctic (pure Go)
make build-duckdb  # build with the optional DuckDB engine (needs cgo)
make test          # go test ./...
make vet           # go vet ./...
make fmt           # gofmt -s -w .
```

## License

[Apache-2.0](LICENSE). Copyright 2026 Duc-Tam Nguyen.
