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
| `--flat` | | emit the v0.2.1 flat product record (deprecated, removed in v0.4.0) |
| `--rate` | `3s` | minimum delay between requests (floor 1s, 5s under `--no-robots`) |
| `--retries` | `3` | retry attempts on 429/503 |
| `--timeout` | `30s` | per-request timeout |
| `--api` | | use the official PA-API path |
| `--no-cache` | | bypass the on-disk cache |
| `--refresh` | | ignore the cached copy but repopulate it |
| `--dry-run` | | print the URL(s) that would be fetched, then stop |
| `--raw` | | emit the underlying HTML/JSON |
| `--data-dir` | | root cache/data dir |
| `--config` | | config file |
| `--no-robots` | | ignore robots.txt for this run, and print every rule it breaks |
| `--yes` | | confirm an impolite action without prompting (required by `crawl --no-robots`) |
| `-q`, `--quiet` / `-v`, `--verbose` | | log level |

## Product surfaces

| Command | Purpose | Notable flags |
| --- | --- | --- |
| `product <ASIN\|url>...` | normalize one or more detail pages | `--depth`, `--variants`, `--with-offers`, `--raw`, `--dry-run` |
| `price <ASIN\|url>...` | just the current price | |
| `related <ASIN>` | recommendation cards off a detail page | `--kind` |
| `reviews <ASIN>` | the reviews the detail page carries, with the histogram | `--stars`, `--verified`, `--with-images`, `--deep` |
| `qa <ASIN>` | the answered question count, and any pairs the page carries | |
| `offers <ASIN>` | the buy box, and the count of the offers behind it | `--condition`, `--prime` |
| `variants <ASIN\|url>` | the variation matrix, one row per sibling | |

`--depth` takes `quick`, `meta` (the default), `full`, or `deep`, and decides how
many requests one product record is worth. `deep` prints its bill and asks for
`--yes` above twenty. [Products](/guides/products/) has the table.

## Search and discovery

| Command | Purpose | Notable flags |
| --- | --- | --- |
| `search <query>` | stream result cards | `--all`, `--sort`, `--price`, `--stars`, `--brand`, `--seller`, `--condition`, `-d`, `--refine`, `--page`, `--max-pages`, `--pages`, `--include-sponsored`, `--enqueue` |
| `refine <query>` | the refinement groups and values a query offers | |
| `deals` | today's deals grid | `--min-discount`, `--department` |

`--all` partitions a query and unions the cells to get past the 306 result
ceiling, `--partition` picks the group to split on and `--partition-depth` how
many times a capped cell may be split again. Price it with `--dry-run` first.
[Search](/guides/search/) has the detail.

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
| `category <node\|url>` | a browse node | `--related`, `--top` |
| `tree [node\|url]` | walk the browse node graph outward | `--depth` |
| `brand <name\|slug\|url>` | a brand storefront, resolved from a bare name through a product byline | `--featured` |
| `seller <id\|url>` | a seller profile and feedback | |
| `author <slug\|url>` | an Author Central page | `--books` |

## Crawl and store

| Command | Purpose | Notable flags |
| --- | --- | --- |
| `seed [ASIN\|url]...` | enqueue work | `--file`, `--entity`, `--priority` |
| `crawl` | drain the frontier into the store | `--asin`, `--chart`, `--category`, `--kinds`, `--depth`, `--follow-rails`, `--max-attempts` |
| `db path\|stats\|query\|vacuum\|reset` | the local SQLite store | |
| `query <sql>` | read-only SQL against the store | |
| `find <text>` | full text search over the store, no network | |
| `lookup <uri\|asin>` | one record out of the store, no network | |
| `graph <uri\|asin>` | traverse the crawled graph outward | `--depth` |
| `series <asin>` | price and rank history from the store | |
| `export` | the store as JSONL, turtle or n-triples | `--format` |

## Provenance and drift

These report on the reading rather than on what was read. See
[Extraction](/reference/extraction/) for what the four rungs mean and how the
capture ledger is kept.

| Command | Purpose | Notable flags |
| --- | --- | --- |
| `surfaces` | every Amazon surface `amz` knows, with what was measured about it | |
| `extraction [asin\|url]` | the ladder, or what one page yielded | `--family`, `--fields`, `--unread` |
| `verify` | today's read against the golden captures | `--live`, `--strict` |
| `agent-map <asin\|url>` | Amazon's own interface map, verbatim | |
| `robots` | the live robots.txt and the group `amz` reads under | |
| `robots check <url>...` | ask robots.txt about a URL and print the deciding rule | |

## Utilities

| Command | Purpose |
| --- | --- |
| `open <ASIN\|query>` | open the page in a browser (`--reviews`, `--print`) |
| `asin <ASIN\|ISBN\|url\|amz: URI>...` | read ids out of anything, offline |
| `info` | access tiers, marketplace, config summary |
| `config path\|show\|init` | view and manage configuration |
| `cache info\|clear` | inspect or clear the page cache |
| `doctor` | check the client is honest, the network works and the store is readable |
| `why [topic]` | why something returns less than you expected, with the measurement |
| `serve` | the read commands over HTTP |
| `mcp` | the read commands on stdio as Model Context Protocol |
| `completion` | shell completion script |

`asin` never touches the network. With no `-o` it prints one bare ASIN per line,
so `amz asin "$url" | xargs amz product` keeps working. Name a format and it
gives the whole identity instead: the kind of id, the storefront the input
pointed at, the ISBN-13 when the id is a book, and the `amz:` URI the rest of
the tool files things under.

```console
$ amz asin "https://www.amazon.co.uk/dp/0439023483" -o jsonl
amz: https://www.amazon.co.uk/dp/0439023483 is the uk marketplace, so reading uk and not us
{"asin":"0439023483","input":"https://www.amazon.co.uk/dp/0439023483","isbn10":"0439023483","isbn13":"9780439023481","kind":"isbn10","marketplace":"uk","uri":"amz:uk/product/0439023483"}
```

The ISBN-13 is computed from the ISBN-10 with the check digit recalculated, not
copied, and a ten character string that fails the check digit is reported as a
plain ASIN with no ISBN at all. A made up ISBN in an export is worse than a
missing one, because nothing downstream can tell it is wrong.

The note on stderr is the marketplace rule: a URL that names a storefront beats
`--marketplace`, because somebody who pastes an `amazon.co.uk` link and leaves
the default at `us` meant the link. Passing two URLs for two different
storefronts in one run is a usage error rather than a choice made for you, since
the currency, the number format and the availability strings all come from one
marketplace.
