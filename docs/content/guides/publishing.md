---
title: "Publishing"
description: "Convert a month range and upload the Parquet shards to a Hugging Face dataset, with a dry run by default, a token from the environment, and a resumable commit."
weight: 50
---

`publish` runs the bulk pipeline over a month range and uploads the resulting
Parquet shards to a [Hugging Face](https://huggingface.co) dataset. It is the
bulk pull plus an upload, wrapped so a long unattended run can rehearse, resume,
and leave a ledger of what it committed.

## The token

The upload reads the token from the `HF_TOKEN` environment variable. Export a
write token from your Hugging Face account before a real commit:

```bash
export HF_TOKEN=hf_...
```

A `--commit` run with no `HF_TOKEN` set exits with a usage error rather than
starting work it cannot finish.

## Dry run by default

Without `--commit`, `publish` does everything except the upload: it processes the
months into Parquet and reports what it would push. This is the way to rehearse a
run and confirm the range and the row counts before you spend the bandwidth:

```bash
arctic publish --from 2024-01 --to 2024-03
```

Add `--commit` to actually upload:

```bash
arctic publish --from 2024-01 --to 2024-03 --commit
```

## The month range and types

`--from` and `--to` bound the range and default to the catalog start and end, the
same as `pull`. `--type` narrows to `comments`, `submissions`, or `both`:

```bash
arctic publish --from 2024-01 --to 2024-06 --type submissions --commit
```

## The dataset repo

By default `publish` uploads to arctic's default dataset repo. Point it at your
own with `--repo`, and create it as private with `--private`:

```bash
arctic publish --from 2024-01 --to 2024-03 --repo your-name/reddit-archive --commit
arctic publish --from 2024-01 --to 2024-03 --repo your-name/reddit-archive --private --commit
```

## The stats.csv ledger and resuming

A publish run records what it committed in a `stats.csv` ledger in the dataset.
That ledger is the record of which months have landed, so a re-run knows where to
pick up.

If a commit stalls, `publish` exits with code 75 rather than hanging or pretending
it finished. A supervisor (a shell loop, a systemd unit, a CI job) can treat code
75 as "restart me" and run the same command again; the next run reads the
`stats.csv` ledger and resumes from the last committed month instead of starting
over. A simple supervisor loop:

```bash
export HF_TOKEN=hf_...
until arctic publish --from 2024-01 --to 2024-12 --commit; do
  [ $? -eq 75 ] || break    # only retry on a stalled commit
  echo "commit stalled, resuming"
done
```

## Keeping the local Parquet

After a successful commit, `publish` clears the local Parquet it produced for the
run by default, since the canonical copy now lives in the dataset. Pass `--keep`
to leave the local shards in place, which is what you want if you also intend to
query them locally:

```bash
arctic publish --from 2024-01 --to 2024-03 --commit --keep
```

## Sizing the run

A publish run carries the same conversion cost as a bulk pull, so it is bounded by
your hardware. arctic sizes the parallelism from the detected machine; override
the caps with `--workers` and pick the engine with `--engine go` or
`--engine duckdb`. See [the hardware budget](/guides/hardware-budget/).

## What the exit codes mean here

| Code | Meaning for `publish` |
|---|---|
| `0` | The range processed and (with `--commit`) uploaded |
| `2` | Usage error, including `--commit` without `HF_TOKEN` |
| `3` | No data: no month in the range was published in the catalog |
| `5` | Blocked: a source rate-limited or refused |
| `75` | A commit stalled; restart to resume from the ledger |

See [troubleshooting](/reference/troubleshooting/) for the full table.
