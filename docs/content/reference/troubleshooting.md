---
title: "Troubleshooting"
description: "What each exit code means and what to do about it, the blocked-by-API case, and resuming a stalled publish commit."
weight: 40
---

Most of what trips people up is the shape of the data and the network, not a bug.
The archive is large, the catalog has gaps at its leading edge, and a public API
rate-limits. arctic is honest about each: it returns a stable exit code so a
script can tell an empty result from a block from a real failure.

## The exit codes

| Code | Meaning |
|---|---|
| `0` | OK |
| `1` | Error |
| `2` | Usage error |
| `3` | No data |
| `4` | Partial |
| `5` | Blocked |
| `75` | Commit stalled |

### 0, success

The command did what you asked. For `publish` without `--commit`, that means the
dry run completed; the upload only happens with `--commit`.

### 1, error

Something failed that is not one of the specific cases below: a disk write that
could not complete, a Parquet shard that could not be read, an unexpected error
from a source. The message on stderr says what. Re-running after fixing the cause
(free disk, a reachable network) usually clears it.

### 2, usage error

The command was invoked wrong: an unknown `--output` format, an unknown
`--engine`, `--engine duckdb` on a pure-Go binary, a bad `--kind`, a `--to` month
before `--from`, an unparseable date, or `--commit` with no `HF_TOKEN`. The
message names the offending flag. Fix the invocation and re-run.

### 3, no data

arctic reached the source but there was nothing to act on:

- `query` matched no records. Loosen the filters, or confirm you imported that
  entity with `arctic sub info <name>`.
- `sub info` / `user info` found nothing imported for that entity. Acquire it
  first with `arctic sub` or `arctic user`.
- `pull` or `publish` found no month in the range published in the catalog. The
  recent edge of the catalog lags real time; run `arctic catalog` to see the last
  published month and pull a range that ends on or before it.
- `stats` found an empty index. Import something first.

### 4, partial

A batch finished but some targets in it failed. `pull` reports this when some
months in a range downloaded or converted and others failed, and `process`
reports it when some files converted and others could not. The successful
targets are imported; re-run for the rest, or look at the per-target lines on
stderr to see which failed and why. A month reported as "not yet published" is
skipped, not counted as a failure.

### 5, blocked

A source rate-limited or refused the request. This is almost always the Arctic
Shift API on the `sub --api`, `sub` fallback, or `user` path: the service asks
clients to stay under a polite rate, and a burst gets throttled.

What to do, in order:

1. **Slow down.** Lower `--workers` on the API path so fewer requests go out at
   once:

   ```bash
   arctic user spez --workers 2
   ```

2. **Wait and retry.** A throttle clears after a short pause. The data you
   already pulled is imported, so a re-run continues rather than starting over.
3. **Send a descriptive User-Agent.** If you overrode `--user-agent` with
   something generic, set it back to a descriptive string that identifies your
   client.

A datacenter or shared IP is throttled harder than a home or office connection.
If every request blocks immediately regardless of `--workers`, the network egress
is the cause, and the way through is a different egress or a slower, patient run.

### 75, commit stalled

A `publish --commit` upload stalled partway. Rather than hang, `publish` exits 75
so a supervisor can treat it as "restart me." Run the same command again; it reads
the `stats.csv` ledger in the dataset and resumes from the last committed month
instead of re-uploading what already landed:

```bash
export HF_TOKEN=hf_...
until arctic publish --from 2024-01 --to 2024-12 --commit; do
  [ $? -eq 75 ] || break
done
```

See [publishing](/guides/publishing/) for the full resume loop.

## "needs a binary built with -tags duckdb"

`--engine duckdb` only works on a binary built with the DuckDB engine. The
standard release is pure Go and rejects it with a usage error. Build the cgo
variant if you want DuckDB:

```bash
make build-duckdb
```

Confirm which build you have with `arctic info`: `duckdb_available` is `true` only
on the cgo build.

## "cannot tell comments from submissions by name"

`process` infers the record type from the file name. Give it an `RC_`/`RS_` name
(the dump convention) or a `_comments`/`_submissions` name. A renamed file that
matches neither is skipped with this note; rename it or pass the original dump.

## Disk filled up mid-run

A wide pull writes both the `.zst` files and the Parquet shards under the data
directory at once. Check headroom first with `arctic info` (`disk_free_gb`) and
`arctic catalog --sizes`. Split the two trees onto different disks with
`--raw-dir` and `--work-dir`, and let `publish` clear its local Parquet after each
commit (its default; `--keep` opts out). See
[configuration](/reference/configuration/).

## Where state lives

The downloaded dumps, the per-entity Parquet, the work scratch, and the SQLite
index all sit under the data directory (the XDG default, or `ARCTIC_DATA_DIR` /
`--data-dir`). To see the resolved paths:

```bash
arctic info
```
