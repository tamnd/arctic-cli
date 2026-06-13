---
title: "Output formats"
description: "Render records as a table, JSON, JSONL, CSV, or TSV, narrow the columns, template each row, and script against the exit codes."
weight: 40
---

Every command that emits records renders through the same formatter. Pick a
format with `--output` (or `-o`), or let arctic choose: a table when writing to a
terminal, JSONL when piped.

## Formats

```bash
arctic query golang -o table   # aligned columns for reading
arctic query golang -o jsonl   # one JSON object per line, for piping
arctic query golang -o json    # a single JSON array
arctic query golang -o csv     # spreadsheet friendly
arctic query golang -o tsv     # tab-separated
```

| Format | Best for |
|---|---|
| `table` | Reading on a terminal |
| `jsonl` | Piping into another tool, one object at a time |
| `json` | Loading a whole result as an array |
| `csv` / `tsv` | Spreadsheets and quick column math |

The same formatter renders every command, so `query`, `stats`, `catalog`,
`process`, `sub info`, `info`, and `version` all take `-o` and the field and
template flags below.

## Auto-detection

With no `-o`, the default adapts to where the output is going: an aligned table
when the output is a terminal, JSONL when it is piped. That keeps interactive use
readable and scripted use parseable without you setting `--output` either time:

```bash
arctic query golang                  # a table, because this is a terminal
arctic query golang | jq -r .author  # JSONL, because this is a pipe
```

You only reach for `-o` when you want something other than that default.

## Narrowing columns

Keep only the fields you want with `--fields`:

```bash
arctic query golang --fields author,score,body
arctic query golang --kind submissions --fields author,score,title
arctic stats --by month --fields month,records
```

The field names are the record's keys, which match the JSON output, so a quick
`arctic query golang -o json -n 1` shows you every name available.

`--no-header` drops the header row in `table`, `csv`, and `tsv` output, which is
handy when a downstream tool expects bare rows:

```bash
arctic query golang --fields author,score -o csv --no-header
```

## Templating rows

For full control over each line, apply a Go `text/template`. The fields are the
record's keys:

```bash
arctic query golang --template '{{.author}}: {{.body}}'
arctic query golang --kind submissions --template '{{.score}}	{{.title}}'
```

## Color

`--color` is `auto` by default: arctic colors table output on a terminal and
drops color when piped. Force it with `--color always` or turn it off with
`--color never`.

## Progress on stderr

Acquisition and conversion print progress to stderr, separate from the record
output on stdout, so a pipe stays clean. Silence the progress with `-q`
(`--quiet`):

```bash
arctic sub golang -q
```

## Exit codes for scripting

arctic returns a stable exit code so a script can branch on the outcome:

| Code | Meaning |
|---|---|
| `0` | OK |
| `1` | Error |
| `2` | Usage error |
| `3` | No data (nothing matched, nothing published, nothing imported) |
| `4` | Partial (some targets in a batch failed) |
| `5` | Blocked (the API rate-limited or refused) |
| `75` | Commit stalled (restart a `publish` run to resume) |

For example, treat an empty result differently from a real failure:

```bash
arctic query golang --contains generics -o json > out.json
case $? in
  0) echo "got matches" ;;
  3) echo "nothing matched" ;;
  *) echo "failed" ;;
esac
```

See [troubleshooting](/reference/troubleshooting/) for what to do about each
non-zero code.
