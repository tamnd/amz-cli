---
title: "Output formats"
description: "Every output format amz can render, plus field projection and templates."
weight: 20
---

Every command streams its records through one renderer, so the output flags work
the same everywhere.

## Choosing a format

`-o` / `--output` takes:

| Format | Use |
| --- | --- |
| `auto` | table on a terminal, JSONL when piped (the default) |
| `table` | aligned columns for reading |
| `json` | a single JSON array |
| `jsonl` | one JSON object per line, the streaming format |
| `csv` | comma-separated, with a header |
| `tsv` | tab-separated, with a header |
| `url` | just the URL of each record, one per line |
| `raw` | the underlying HTML/JSON amz fetched |

```bash
amz product B084DWG2VQ -o json
amz search "usb c cable" -o jsonl
amz bestsellers electronics -o csv > top.csv
```

Because `auto` switches on whether stdout is a terminal, the same command reads
nicely by hand and pipes cleanly in a script, with nothing to remember.

## Projecting fields

`--fields` picks and orders columns by their JSON name. It applies to every
format, so it trims a CSV as readily as a table:

```bash
amz product B084DWG2VQ -o csv --fields asin,price,rating
amz search "usb c cable" --fields asin,title,price -o table
```

`--no-header` drops the header row from table, CSV, and TSV.

## Templates

`--template` renders each record through a Go text/template, for a custom line
format:

```bash
amz search "usb c cable" --template '{{.asin}}  {{.price}}  {{.title}}'
```

## Writing to a file

`-O` / `--out` writes the rendered output to a file instead of stdout (and
forces non-TTY formatting):

```bash
amz reviews B084DWG2VQ -o csv -O reviews.csv
```

## Exit codes

amz uses its exit code to tell apart the ways a command can end, so a script can
branch without parsing output:

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | runtime error |
| 2 | usage error (bad flag, unknown marketplace) |
| 3 | no data (the surface was empty) |
| 4 | partial (some pages fetched, some failed) |
| 5 | blocked (Amazon served the bot wall) |

See [troubleshooting](/reference/troubleshooting/) for what to do with code 5.
