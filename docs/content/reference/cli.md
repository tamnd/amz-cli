---
title: "CLI reference"
description: "Every amz command and its flags, in one place."
weight: 10
---

Run `amz <command> --help` for the authoritative flag list on any command. This
page is the map.

## Global flags

These persistent flags work on every command:

| Flag | Default | Effect |
| --- | --- | --- |
| `-m`, `--marketplace` | `us` | storefront slug |
| `-o`, `--output` | `auto` | `table\|json\|jsonl\|csv\|tsv\|url\|raw` |
| `--color` | `auto` | colorize output: `auto\|always\|never` |
| `--fields` | | comma-separated columns to show |
| `-n`, `--limit` | `0` | cap results (0 = unlimited) |
| `-O`, `--out` | | write output to a file |
| `--no-header` | | omit the table/CSV header |
| `--template` | | Go text/template per row |
| `-j`, `--workers` | `2` | concurrency for multi-page/bulk |
| `--rate` | `3s` | minimum delay between requests |
| `--retries` | `3` | retry attempts on 429/503 |
| `--timeout` | `30s` | per-request timeout |
| `--cookies` | | cookie file for a signed-in session |
| `--api` | | use the official PA-API path |
| `--no-cache` | | bypass the on-disk cache |
| `--refresh` | | ignore the cached copy but repopulate it |
| `--dry-run` | | print the URL(s) that would be fetched, then stop |
| `--raw` | | emit the underlying HTML/JSON |
| `--data-dir` | | root cache/data dir |
| `--config` | | config file |
| `-q`, `--quiet` / `-v`, `--verbose` | | log level |

## Product surfaces

| Command | Purpose | Notable flags |
| --- | --- | --- |
| `product <ASIN\|url>...` | normalize one or more detail pages | `--variants`, `--with-offers`, `--raw`, `--dry-run` |
| `price <ASIN\|url>...` | just the current price | |
| `related <ASIN>` | recommendation cards off a detail page | `--kind` |
| `reviews <ASIN>` | stream the review corpus | `--sort`, `--stars`, `--verified`, `--with-images`, `--page` |
| `qa <ASIN>` | question-and-answer pairs | |
| `offers <ASIN>` | every buying option | `--condition`, `--prime` |

## Search and discovery

| Command | Purpose | Notable flags |
| --- | --- | --- |
| `search <query>` | stream result cards | `--sort`, `--min-price`, `--max-price`, `--min-rating`, `--prime`, `--brand`, `--department`, `--page`, `--enqueue` |
| `deals` | today's deals grid | `--min-discount`, `--department` |

## Charts

All five share the same shape: an optional category positional and `--node`.

| Command | Chart |
| --- | --- |
| `bestsellers [category]` | top sellers |
| `new-releases [category]` | newest releases |
| `movers [category]` | biggest 24h movers |
| `wished [category]` | most wished for |
| `gifted [category]` | most gifted |

## Storefronts and trees

| Command | Purpose | Notable flags |
| --- | --- | --- |
| `category <node\|url>` | a browse node | `--children`, `--top` |
| `brand <slug\|url>` | a brand storefront | `--featured` |
| `seller <id\|url>` | a seller profile and feedback | |
| `author <slug\|url>` | an Author Central page | `--books` |

## Crawl and store

| Command | Purpose | Notable flags |
| --- | --- | --- |
| `seed [ASIN\|url]...` | enqueue work | `--file`, `--entity`, `--priority` |
| `crawl` | drain the queue into the store | `--kinds` |
| `db path\|stats\|query\|vacuum\|reset` | the local DuckDB store | |

## Utilities

| Command | Purpose |
| --- | --- |
| `open <ASIN\|query>` | open the page in a browser (`--reviews`, `--print`) |
| `asin <url>...` | extract the ASIN from any Amazon URL |
| `info` | access tiers, marketplace, config summary |
| `config path\|show\|init` | view and manage configuration |
| `cache info\|clear` | inspect or clear the page cache |
| `completion` | shell completion script |
