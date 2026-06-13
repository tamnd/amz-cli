# amz

A delightful command line for [Amazon.com](https://www.amazon.com). One binary
that reads every public Amazon surface, products, search, reviews, Q&A, offers,
charts, categories, brands, sellers, authors, and deals, and turns each one into
rich, structured data.

```
amz product B084DWG2VQ -o json
```

```json
{
  "asin": "B084DWG2VQ",
  "title": "Echo Dot (4th Gen) | Smart speaker with Alexa | Charcoal",
  "brand": "Amazon",
  "price": 49.99,
  "currency": "USD",
  "rating": 4.7,
  "ratings_count": 284512,
  "availability": "In Stock",
  "rank": 3
}
```

Full documentation: [amz-cli.tamnd.com](https://amz-cli.tamnd.com).

## Why

Pulling structured data out of Amazon usually means a pile of brittle scrapers,
one per page type, each breaking the next time a selector moves. amz puts all of
it behind one tool with sensible defaults, real output formats, and pipelines
that compose. It reads the public pages on `amazon.com` over plain HTTPS, reads
the JSON-LD Amazon marks up for machines, and falls back to precise HTML
selectors so each record is rich with no missing fields where the page had them.

## Install

```sh
go install github.com/tamnd/amz-cli/cmd/amz@latest
```

Or grab a prebuilt binary from the [releases page](https://github.com/tamnd/amz-cli/releases).
The binary is pure Go with no runtime dependencies. DuckDB is optional and only
needed for the local store and crawl queue.

Build from source:

```sh
git clone https://github.com/tamnd/amz-cli
cd amz-cli
make build      # produces ./bin/amz
```

## Quick start

```sh
amz product B084DWG2VQ                 # one product, fully normalized
amz search "mechanical keyboard"       # catalog result cards
amz reviews B084DWG2VQ --stars 1       # the one-star reviews
amz offers B084DWG2VQ                  # every buying option
amz bestsellers electronics            # the live top-100 chart
amz product B084DWG2VQ -m uk           # any of 16 marketplaces
```

## How it works

Every Amazon page type is a surface, and amz has one command per surface. Each
command builds the canonical URL for the marketplace you picked, fetches it
politely (rotating user agent, rate limit, retry with backoff, on-disk cache),
then parses twice: the embedded JSON-LD first, then the HTML to fill any gaps.
The result is one normalized record. When Amazon serves its bot-check page
instead of content, amz detects it and exits with a distinct code rather than
handing you garbage.

## Commands

| Command | What it does |
| --- | --- |
| `product` | Normalize one or more product detail pages |
| `price` | Print just the current price |
| `related` | Recommendation cards off a detail page |
| `search` | Search the catalog and stream result cards |
| `reviews` | Stream the review corpus |
| `qa` | Question-and-answer pairs |
| `offers` | Every buying option for an ASIN |
| `bestsellers` / `new-releases` / `movers` / `wished` / `gifted` | The five charts |
| `category` | A browse node: name, breadcrumb, children, top ASINs |
| `brand` | A brand storefront |
| `seller` | A third-party seller profile and feedback |
| `author` | An Author Central page |
| `deals` | Today's deals grid |
| `seed` / `crawl` / `db` | Queue and the optional local DuckDB store |
| `open` / `asin` / `info` / `config` / `cache` | Utilities |

Run `amz <command> --help` for the full flag list on any command.

## Output

Every command streams through one renderer. `-o auto` (the default) prints a
table on a terminal and JSONL when piped:

```sh
amz search "usb c cable" -o json      # a JSON array
amz search "usb c cable" -o jsonl     # one object per line
amz bestsellers electronics -o csv    # spreadsheet-ready
amz product B084DWG2VQ -o url         # just the URL
amz product B084DWG2VQ --fields asin,price,rating -o csv
```

## Recipes

Turn a search into full product records:

```sh
amz search "mechanical keyboard" -n 25 -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | xargs -I{} amz product {} -o jsonl > keyboards.jsonl
```

Collect a category's bestsellers into the local store and query it:

```sh
amz bestsellers electronics -n 100 -o url | amz seed --file -
amz crawl
amz db query "select data->>'brand' brand, count(*) n from products group by brand order by n desc"
```

Watch one-star reviews:

```sh
amz reviews B084DWG2VQ --stars 1 -o jsonl | wc -l
```

## Access tiers

amz reads three tiers, selected per run:

- **Public HTML** (default), no setup.
- **Cookied** (`--cookies file`), lends a signed-in session.
- **PA-API** (`--api`), the official Product Advertising API 5.0 with
  credentials, signed locally with SigV4. Same output schema, so scripts do not
  care which tier produced the record.

## Exit codes

`0` ok, `1` runtime error, `2` usage, `3` no data, `4` partial, `5` blocked.

## License

[Apache-2.0](LICENSE).
