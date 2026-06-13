---
title: "The hardware budget"
description: "How arctic sizes its work to the machine, what arctic info reports, and the choice between the Go and DuckDB conversion engines."
weight: 60
---

Converting the bulk archive is bounded by CPU, memory, and disk. A small laptop
and a large server should not run the same number of parallel conversions, so
arctic detects the machine it is on and derives a work budget from it rather than
assuming a fixed level of parallelism.

## See the budget

```bash
arctic info
```

That reports the detected hardware and the budget arctic computed from it. The
hardware fields:

| Field | Meaning |
|---|---|
| `os`, `hostname` | The host arctic detected |
| `cpus` | Logical CPU count |
| `ram_total_gb` | Total system memory |
| `ram_available_gb` | Memory available right now |
| `disk_free_gb` | Free space on the work directory's disk |

The budget fields derived from them:

| Field | Meaning |
|---|---|
| `max_downloads` | How many dumps to download at once |
| `max_process` | How many files to convert at once |
| `max_convert_workers` | Worker threads inside one conversion |
| `duckdb_memory_mb` | Memory ceiling handed to the DuckDB engine |
| `sequential` | Whether the host is small enough to run strictly one step at a time |

And the storage and engine fields:

| Field | Meaning |
|---|---|
| `engine` | The active conversion engine (`go` or `duckdb`) |
| `duckdb_available` | Whether this binary was built with the DuckDB engine |
| `data_dir`, `raw_dir`, `work_dir`, `index_path` | The resolved storage paths |

## The sequential fallback

On a small host, running downloads and conversions in parallel competes for the
same scarce memory and disk and ends up slower than doing one thing at a time. So
the budget carries a `sequential` flag: when the detected memory or core count is
low, arctic runs strictly one step after another. You do not set this; it follows
from the hardware. A larger host runs the downloads and conversions concurrently
up to the `max_*` caps.

## Overriding the caps

`--workers` overrides the convert and download caps when you want to set the
parallelism by hand, for example to leave headroom for other work or to push a
capable machine harder:

```bash
arctic pull 2024-01..2024-03 --process --workers 4
arctic info --workers 4          # see the budget with the override applied
```

On the API path, `--workers` sets the fetch concurrency the same way. With
`--workers 0` (the default) arctic uses the computed budget.

## The two conversion engines

arctic ships two engines for the JSONL-to-Parquet step.

The **Go engine** (`--engine go`, the default) is pure Go and built into every
binary. It needs nothing installed alongside arctic and runs everywhere the
binary does. This is the right default and the only engine in the standard
release build.

The **DuckDB engine** (`--engine duckdb`) does the same conversion through
DuckDB's `read_json` and `COPY ... TO`. It is faster on a capable host and
respects the `duckdb_memory_mb` budget so it does not exhaust memory on a large
file. It needs a binary built with `-tags duckdb` and a cgo toolchain:

```bash
make build-duckdb
./bin/arctic pull 2024-01..2024-03 --process --engine duckdb
```

A pure-Go binary asked for `--engine duckdb` exits with a usage error telling you
to build with `-tags duckdb`, and `arctic info` shows `duckdb_available: false`,
so you always know which build you are running. Both engines write the same fixed
schema, so the choice is purely about speed and toolchain, and shards from one are
indistinguishable from the other at query time.

## Disk reality

The dumps are large: hundreds of gigabytes uncompressed for a wide range, and the
`.zst` files plus the Parquet shards both live under the data directory while a
run is in flight. `arctic info` reports `disk_free_gb` so you can check before a
big pull, and `arctic catalog --sizes` reports the per-file download sizes. A
`publish --commit` run clears its local Parquet after each successful commit by
default to keep the footprint down; pass `--keep` to hold onto it. See
[configuration](/reference/configuration/) for splitting the raw and work trees
onto different disks with `--raw-dir` and `--work-dir`.
