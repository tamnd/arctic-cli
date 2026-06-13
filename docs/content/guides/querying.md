---
title: "Querying"
description: "Read imported records back out of the Parquet shards with filters on author, score, date, type, and a substring match."
weight: 30
---

`query` reads records back out of the Parquet shards of an entity you have
already pulled or fetched. It scans the shards for a subreddit or account,
applies the filters you pass, and renders the matching records through the same
formatter every command uses.

```bash
arctic query golang
```

With no filters it renders every imported record for r/golang. The interesting
part is the filters.

## Subreddit or user

The argument is a subreddit by default. To read an account instead, prefix it
with `u/` or pass `--user`:

```bash
arctic query golang              # the subreddit r/golang
arctic query spez --user         # the account u/spez
arctic query u/spez              # the same account, by prefix
```

Both forms accept the loose prefixes (`r/golang`, `/u/spez`) and normalize them.

## Filters

Every filter narrows the scan. Combine as many as you want; a record must match
all of them.

### By author

```bash
arctic query golang --author rob_pike
```

`--author` matches the record's author exactly, normalized the same way the
entity arguments are, so `u/rob_pike` and `rob_pike` both work.

### By substring

`--contains` does a case-insensitive substring match against the body of a
comment and the title and selftext of a submission:

```bash
arctic query golang --contains "context deadline"
arctic query golang --contains generics --kind submissions
```

### By score

`--min-score` keeps only records at or above a score:

```bash
arctic query golang --min-score 100
arctic query golang --author rob_pike --min-score 500
```

### By date

`--after` and `--before` bound the record's creation time. Each takes `YYYY`,
`YYYY-MM`, or `YYYY-MM-DD`:

```bash
arctic query golang --after 2024-01-01
arctic query golang --after 2020 --before 2021
arctic query golang --after 2024-01 --before 2024-04 --contains generics
```

### By type

`--kind` chooses which record types to scan: `comments`, `submissions`, or
`both` (the default):

```bash
arctic query golang --kind submissions
arctic query golang --kind comments --author rob_pike
```

When you ask for a single type, the output columns match that type. When you ask
for both and the result has both, comments render first and submissions after.

## Limiting the result

`-n` (the global `--limit`) caps the number of records returned; `0`, the
default, means no limit:

```bash
arctic query golang --contains generics -n 20
```

## How the scan works

`query` reads the Parquet shards for the entity and type under the data
directory and filters them as it goes. It reads the columnar shards directly, so
it does not depend on the SQLite index, and it covers everything you imported for
that entity regardless of which acquisition path or month it came from, because
every shard shares the same fixed schema. A query that matches nothing exits with
code 3 (no data), so a script can tell an empty result from an error.

## Composing

Because the output adapts to the destination, a query pipes cleanly. Pull one
field out of the matches:

```bash
arctic query golang --contains generics -o jsonl | jq -r .author | sort | uniq -c
```

Keep a few columns for a quick look:

```bash
arctic query golang --kind submissions --min-score 1000 --fields author,score,title
```

Apply a template per record:

```bash
arctic query golang --contains generics --template '{{.score}}	{{.title}}'
```

See [output formats](/guides/output-formats/) for the full set of renderings and
[the troubleshooting reference](/reference/troubleshooting/) for what the exit
codes mean.
