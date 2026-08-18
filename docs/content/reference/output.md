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
amz product B075F5X8BR -o json
amz search "usb c cable" -o jsonl
amz bestsellers electronics -o csv > top.csv
```

Because `auto` switches on whether stdout is a terminal, the same command reads
nicely by hand and pipes cleanly in a script, with nothing to remember.

## Projecting fields

`--fields` picks and orders columns by their JSON name. It applies to every
format, so it trims a CSV as readily as a table:

```bash
amz product B075F5X8BR -o csv --fields asin,price,rating
amz search "usb c cable" --fields asin,title,price -o table
```

A name that is not one of the record's table columns is looked up in the record
itself, so any field is reachable even when the table does not promote it:

```bash
amz product B075F5X8BR -o csv --fields asin,ranks,bullets
```

`--no-header` drops the header row from table, CSV, and TSV.

## Templates

`--template` renders each record through a Go text/template, for a custom line
format:

```bash
amz search "usb c cable" --template '{{.asin}}  {{.price}}  {{.title}}'
```

A template renders text, so a field that is both a table column and a structured
value renders as the column. `{{.price}}` gives `12.99` rather than the price
object it is in the record, which is still there in `-o json` for the callers
that want the currency and the string Amazon printed.

## Provenance is part of the record

The `envelope` is an ordinary field, so nothing special is needed to read it:

```bash
amz product B075F5X8BR -o jsonl | jq -r '.envelope.via.price'
amz product B075F5X8BR -o jsonl | jq -r '.envelope.missed[] | .field + ": " + .why'
amz search "usb c cable" -o jsonl | jq -r '.envelope.sources[].url'
```

Rows that come many to a page, search cards and chart entries, carry the sources
of the page they were read from, so a single line out of a stream can still say
where it came from.

## The older flat shape

`--flat` emits the v0.2.1 product record: one level, the old column names, and
prices as bare numbers. It exists so a pipeline written against that shape keeps
running while it is updated, it is deprecated, and it goes away in v0.4.0.

The envelope travels with it. Provenance is not a projection, and a caller who
has not migrated yet still has a record that can say where it came from and what
was looked for and not found.

```bash
amz product B075F5X8BR --flat -o jsonl
```

## Writing to a file

`-O` / `--out` writes the rendered output to a file instead of stdout (and
forces non-TTY formatting):

```bash
amz reviews B075F5X8BR -o csv -O reviews.csv
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
| 5 | blocked (Amazon served the CAPTCHA) |
| 6 | interstitial (the bot challenge, still there after the backoff gave up) |
| 7 | disallowed by robots.txt, and the rule that decided it is named in the message |
| 8 | robots.txt could not be fetched, so nothing was read |
| 9 | the surface is behind a sign-in |

5 and 6 are separate on purpose, because the two call for different things. A
CAPTCHA is a decision and an interstitial is a rate, so 6 is worth retrying later
and 5 is not. 9 is a stop rather than a step: the page is not public, amz carries
no credentials and wants none, and there is nothing about it this tool is
entitled to read.

See [troubleshooting](/reference/troubleshooting/) for what to do with each.
