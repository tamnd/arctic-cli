---
title: "Subreddits and users"
description: "Acquire one community or one account: the torrent-first sub path, the API user path, the date and type filters, and the info subcommands."
weight: 20
---

For a single subreddit or a single account, the full-history bulk torrents are
the wrong tool: you would download terabytes to keep a sliver. The entity path
acquires just one community or one person and imports it to the same Parquet
store the bulk path uses.

## A community: arctic sub

```bash
arctic sub golang
```

`sub` pulls a community's full history. It is torrent-first: arctic checks the
per-subreddit torrent bundle, and if the community is in it, downloads that file
directly. If the community is not in the bundle, or the catalog check fails, it
falls back to streaming the records from the Arctic Shift API, page by page, at
the rate the service asks for. It prints which path it took on stderr, so you can
see whether r/golang came from a torrent or the API.

The argument is forgiving about prefixes: `golang`, `r/golang`, and `/r/golang`
all resolve to the same community.

### Force the API

To skip the torrent check and go straight to the API, pass `--api`. This is the
path to use for a small or new community that is unlikely to be in the bundle, or
when you want the API's exact date bounds rather than a whole torrent file:

```bash
arctic sub golang --api --after 2024-01-01
```

### Type and date filters

Narrow what you pull with `--kind` and a date window. `--kind` takes `comments`,
`submissions`, or `both` (the default). `--after` and `--before` take a date as
`YYYY`, `YYYY-MM`, or `YYYY-MM-DD`:

```bash
arctic sub golang --kind submissions
arctic sub golang --after 2020-01-01 --before 2021-01-01
arctic sub golang --kind comments --after 2024
```

The date bounds apply on the API path, which serves an exact range; the torrent
path brings the whole community file and you filter at query time.

### Download without importing

`--no-import` downloads the data but skips the Parquet conversion, which is
useful when you want the raw `.zst` or JSONL to feed something else:

```bash
arctic sub golang --no-import
```

## An account: arctic user

```bash
arctic user spez
```

`user` pulls one account's full history. There is no per-account torrent, so this
always goes through the Arctic Shift API. It takes the same `--kind`, `--after`,
`--before`, and `--no-import` flags as `sub`, and the same forgiving argument:
`spez`, `u/spez`, and `/user/spez` all resolve to the same account.

```bash
arctic user spez --kind comments --after 2023-01-01
```

## What you hold: the info subcommands

Both commands carry an `info` subcommand that reports what is imported locally for
one entity:

```bash
arctic sub info golang
arctic user info spez
```

Each prints one row per record type with the shard count, row count, byte size,
and the first and last dates covered. The counts come from the Parquet footers,
so it is a metadata read and does not scan the row data. If nothing is imported
for that entity, it exits with code 3 (no data).

## Query what you acquired

Once a community or account is imported, read it back with `query`. A `u/` prefix
or `--user` tells `query` to read the argument as an account; everything else is
read as a subreddit:

```bash
arctic query golang --contains generics
arctic query spez --user --kind comments -n 50
```

See the [querying guide](/guides/querying/) for every filter.

## Where it all lands

Per-entity imports live under the data directory, keyed by kind and name, with
the index recording each one. Point the data directory elsewhere with
`--data-dir` or `ARCTIC_DATA_DIR`. The API path sends a default User-Agent you
can override with `--user-agent`, and honors `--timeout` for each request. See
[configuration](/reference/configuration/).
