---
title: "Output formats"
description: "Every output format, how to narrow columns, how to template records, and the auto-detection that picks a format for you."
weight: 30
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
| `url` | Bare URL per row |
| `raw` | The unformatted bytes |

The formatter is shared by every command that emits records: `query`, `stats`,
`catalog`, `process`, `sub info`, `user info`, `info`, and `version`.

## Narrowing columns

Keep only the fields you want:

```bash
arctic query golang --fields author,score,body
arctic stats --by month --fields month,records
```

The field names are the record's keys, which match the JSON output, so
`arctic query golang -o json -n 1` shows you every available name.

`--no-header` drops the header row in `table`, `csv`, and `tsv` output, which is
handy when a downstream tool expects bare rows.

## Templating records

For full control over each line, apply a Go `text/template`. The fields are the
record's keys:

```bash
arctic query golang --template '{{.author}}: {{.body}}'
arctic query golang --kind submissions --template '{{.score}}	{{.title}}'
```

## Why auto-detection helps

Because the default adapts to the destination, the same command reads well by
hand and parses cleanly in a pipe:

```bash
arctic query golang                  # a table, because this is a terminal
arctic query golang | jq -r .author  # JSONL, because this is a pipe
```

You only reach for `-o` when you want something other than that default.

## Color

`--color` is `auto` by default: arctic colors table output on a terminal and
drops color when piped. Force it with `--color always` or turn it off with
`--color never`.

## Progress and stdout

Acquisition and conversion print progress to stderr, while the records render on
stdout. That keeps a pipe clean and lets you silence the progress with `-q`
(`--quiet`) without losing the data.
