# amz

[![CI](https://github.com/tamnd/amz-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/tamnd/amz-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/tamnd/amz-cli)](https://github.com/tamnd/amz-cli/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/tamnd/amz-cli.svg)](https://pkg.go.dev/github.com/tamnd/amz-cli)
[![Go Report Card](https://goreportcard.com/badge/github.com/tamnd/amz-cli)](https://goreportcard.com/report/github.com/tamnd/amz-cli)
[![License](https://img.shields.io/github/license/tamnd/amz-cli)](./LICENSE)

A command line for Amazon.
`amz` reads every public Amazon surface (products, search, reviews, Q&A, offers, charts, categories, brands, sellers, authors, deals) and turns each one into clean, pipeable records.
One pure-Go binary, no API key required.

[Install](#install) • [Commands](#commands) • [Usage](#usage) • [Access tiers](#access-tiers)

![amz reading Amazon bestsellers as a table and piping through jq](docs/static/demo.gif)

It reads the public pages on `amazon.com` over plain HTTPS and normalizes each one into a record with named fields.
Every request is paced, retried on transient failures, and cached on disk.
`robots.txt` is fetched from the marketplace and obeyed before every request.
When Amazon serves a bot-check page instead of content, `amz` detects it and exits with a distinct code rather than handing you garbage.

`amz` is an independent tool. It is not affiliated with or endorsed by Amazon.

## Install

```bash
go install github.com/tamnd/amz-cli/cmd/amz@latest
```

Or grab a prebuilt binary from the [releases](https://github.com/tamnd/amz-cli/releases),
or run the container image:

```bash
docker run --rm ghcr.io/tamnd/amz:latest bestsellers electronics -n 10
```

Shell completion is built in: `amz completion bash|zsh|fish|powershell`.

## Commands

| Command | Reads |
| --- | --- |
| `amz product <ASIN\|url>...` | one or more product detail pages, fully normalized; `--depth` |
| `amz price <ASIN\|url>...` | current price only |
| `amz related <ASIN>` | recommendation cards from a product page |
| `amz search <query>` | catalog search result cards |
| `amz reviews <ASIN>` | the full review corpus; `--stars`, `--sort` |
| `amz qa <ASIN>` | customer question-and-answer pairs |
| `amz offers <ASIN>` | every buying option (seller, condition, price) |
| `amz bestsellers [category]` | the live top-100 chart |
| `amz new-releases [category]` | newest releases in a category |
| `amz movers [category]` | biggest 24-hour rank movers |
| `amz wished [category]` | most wished-for items |
| `amz gifted [category]` | most gifted items |
| `amz category <node_id\|url>` | a browse node: name, related nodes, shelves, top ASINs |
| `amz brand <slug\|url>` | a brand storefront |
| `amz seller <id\|url>` | a third-party seller profile and rating breakdown |
| `amz author <slug\|url>` | an Author Central page |
| `amz deals` | today's deals grid |
| `amz seed` | enqueue ASINs or URLs into the crawl queue |
| `amz crawl` | drain the crawl queue into the local store |
| `amz db query <sql>` | query the optional local DuckDB store |
| `amz asin <url>...` | extract the ASIN from any Amazon URL |
| `amz open <ASIN\|query>` | open the relevant Amazon page in the browser |
| `amz robots` | the marketplace's live robots.txt and the group `amz` reads under |
| `amz robots check <url>...` | ask robots.txt about a URL and print the rule that decided it |
| `amz surfaces` | every Amazon surface `amz` knows, with what was measured about it |
| `amz extraction [asin\|url]` | how each field is read, and what is on the page that nothing reads |
| `amz verify [--live] [--strict]` | compare pages against what they yielded when they were captured |
| `amz agent-map <asin\|url>` | Amazon's own description of a page, printed verbatim |
| `amz info` | show access tier, marketplace, and config summary |
| `amz config` | view and manage configuration and PA-API credentials |
| `amz cache path\|info\|clear` | inspect or clear the on-disk page cache |

Full reference and guides live at [amz-cli.tamnd.com](https://amz-cli.tamnd.com).

## Usage

```bash
amz product B084DWG2VQ                     # one product, fully normalized
amz search "mechanical keyboard" -n 20     # catalog search results
amz reviews B084DWG2VQ --stars 1           # the one-star reviews
amz offers B084DWG2VQ                      # every buying option
amz bestsellers electronics                # the live top-100 chart
amz category 172282                        # the Electronics browse node
amz product B084DWG2VQ -m uk              # any of 16 marketplaces
```

Records come out as a table (the default on a terminal), JSON, JSONL, CSV, TSV,
url, or raw:

```bash
amz bestsellers electronics --fields rank,title,price,rating -o table
amz bestsellers electronics -n 20 --fields asin,title,price -o csv
amz bestsellers electronics -n 10 -o url
amz product B084DWG2VQ -o json
amz reviews B084DWG2VQ -o jsonl | jq 'select(.stars <= 2)'
```

Turn a search into full product records:

```bash
amz search "mechanical keyboard" -n 25 -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | xargs -I{} amz product {} -o jsonl > keyboards.jsonl
```

Collect a category's bestsellers and query the local store:

```bash
amz bestsellers electronics -n 100 -o url | amz seed --file -
amz crawl
amz db query "select data->'brand'->>'name' brand, count(*) n from products group by brand order by n desc"
```

### Global flags

```
-o, --output       table|json|jsonl|csv|tsv|url|raw   (auto: table on a TTY, jsonl when piped)
    --fields       comma-separated columns to include
    --no-header    omit the header row in table/csv/tsv
    --template     Go text/template applied per record
-n, --limit        max records (0 = unlimited)
-m, --marketplace  marketplace slug: us|uk|de|fr|jp|ca|in|it|es|... (default us)
-q, --quiet        suppress progress output
    --color        auto|always|never
    --rate         min spacing between requests (default 3s, floor 1s)
    --timeout      per-request timeout (default 30s)
    --retries      retry attempts on 429/503 (default 3)
    --no-cache     bypass the on-disk cache
    --dry-run      print the URL(s) that would be fetched, then stop
```

## What a record says about itself

Every record carries an `envelope`, which is the part of the record that talks
about the rest of the record: which responses went into it, which surfaces those
were, when they were retrieved, which region or payload answered each field, and
what was looked for and not found.

```console
$ amz product B075F5X8BR -o jsonl | jq -r '.envelope.via.price'
corePrice

$ amz product B075F5X8BR -o jsonl | jq -r '.envelope.missed[] | .field + ": " + .why'
similar_asins: product region "similarities" or "sims-consolidated-2_feature_div" not present on this page
reviews: amazon requires a sign-in for the review corpus, and the detail page carries the rating and the histogram only
rails: the page carries 2 recommendation strips and this depth drops them
```

That second line is the one that matters. If a field is missing and nothing in
`missed` names it, `amz` read the place that field lives and there was nothing
there. Absence is an answer rather than a gap, and a product with no reviews is
distinguishable from a product whose reviews `amz` was not allowed to read.

A `missed` entry with `have` and `total` is the case people get wrong: the field
is present and incomplete. Eight reviews on a page that states 4,812 comes back
as `have: 8, total: 4812`, so counting the array and publishing that as the
total is visibly the wrong move rather than an invisible one.

Prices are objects, not numbers. Each carries the string Amazon printed beside
the parse of it, so a wrong parse is recoverable instead of lost, and a missing
price is `null` rather than `0`. References to other entities carry a
marketplace-scoped URI, `amz:us/product/B075F5X8BR`, because the same ASIN is a
different product at a different price in every marketplace Amazon runs.

`--depth` decides how much of a product page is read:

| Depth | Requests | What you get |
| --- | --- | --- |
| `quick` | 1 | the 374 KB mobile page: identity, price, rating |
| `meta` | 1 | the full 2.2 MB detail page, rails dropped |
| `full` | 1 | the same page with the recommendation rails kept |
| `deep` | 2 + one per sibling | full, plus each variation sibling's own page and the seller |

`meta` is the default. `deep` prints its bill and stops for `--yes` above twenty
requests, because an apparel listing with 88 siblings is ninety requests for one
record.

`--flat` emits the older single-level record with prices as bare numbers. It is
there so a pipeline written against v0.2.1 keeps running while it is updated, it
is deprecated, and it goes away in v0.4.0.

## Who amz says it is

Every request goes out as:

```
User-Agent: amz-cli/<version> (+https://github.com/tamnd/amz-cli)
Accept: text/html
Accept-Encoding: gzip
```

Three headers, one identity, no rotation and no disguise. `amz` reads one page
at a time and never faster than one request per second.

This is not decoration. Through v0.2.1 `amz` rotated five browser user agents
and sent the header set that goes with them, and that combination is what earned
it a CAPTCHA on every page. Measured over four ASINs on 2026-08-17: the browser
identity failed 4 of 4, this one was served 4 of 4.

`amz` also carries no borrowed session. `--cookies` is gone along with the code
that loaded it. Nothing `amz` reads needs a login, and the surfaces that do are
reported rather than reached.

## robots.txt

`amz` fetches `robots.txt` from the marketplace it is reading, caches it for 24 hours, and asks it before every request.
There is no compiled-in copy, because a stale copy that says yes is worse than no answer.
If the file cannot be fetched, `amz` reads nothing and exits 8.

```console
$ amz robots
host:      www.amazon.com
fetched:   2026-08-17 15:20:07 (cached 24h0m0s)
agent:     amz-cli/0.3.0 (+https://github.com/tamnd/amz-cli)
group:     "*" of 100
rules:     118 disallow, 17 allow

$ amz robots check /dp/B075F5X8BR /gp/offer-listing/B075F5X8BR '/b?node=7454917011'
allowed     .../dp/B075F5X8BR                  (no rule matches)             product s1
disallowed  .../gp/offer-listing/B075F5X8BR    Disallow: /gp/offer-listing/  offers s18
disallowed  .../b?node=7454917011              Disallow: /b?*node=7454917011 category s4
```

That last row is why the matcher runs against the query string and not the path.
Five browse nodes are refused by a pattern that only ever matches inside a query, and a path-only matcher gets all five wrong.

`--no-robots` is the override, and it is a flag and only a flag: not a config key, not an environment variable, and it lasts for one run.
It prints a banner, names every rule it breaks as it breaks it, raises the pace floor from 1s to 5s, and needs `--yes` before it will do this to a whole crawl queue.

## Where every field comes from

A scraper is a claim about somebody else's HTML, and the useful question is not whether it returned something but how it knew.
`amz extraction` answers that. Every field is declared at one of four rungs, and the rung is written down rather than inferred, so a field that quietly changes source has to change its declared rung to compile.

```
$ amz extraction
FAMILY   REGION  PAYLOAD  ATTR  SELECTOR  TOTAL
product  25      0        1     1         27
search   17      0        4     0         21
chart    0       1        4     4         9
browse   3       1        6     11        21
store    2       7        5     0         14
seller   14      0        0     0         14
```

Rung 1 is a region Amazon named itself, `data-feature-name="bylineInfo"`, and it is the only rung that survives a restyling.
Rung 2 is a JavaScript payload the page ships, rung 3 is a data attribute, and rung 4 is a bare CSS selector, which is a guess that happens to be right today.
The report lists all sixteen rung 4 fields by name with the date each was added, because a selector that has survived a year of Amazon's restyling is a different risk from one written last week, and the date is the only evidence either way.

Point it at a page and it reports what that page actually yielded.

```
$ amz extraction B075F5X8BR
product  product  https://www.amazon.com/dp/B075F5X8BR  2359626 bytes
26 fields set, 2 missed, 261 regions Amazon named that nothing reads

not on this page:
  similar_asins  product region "similarities" or "sims-consolidated-2_feature_div" not present on this page
  reviews        amazon requires a sign-in for the review corpus, and the detail page carries the rating and the histogram only (on /product-reviews/ and /portal/customer-reviews/)
```

A miss is a field the registry declared and the page did not carry, and the sentence beside it is the parser saying what it looked for.
That is the difference between "no price" and "no price because the buy box is not on this page", and only the second one tells a caller what to do.
`--fields` prints every field that filled and where it came from, and `--unread` lists the regions Amazon named that no field reads, which is the worklist for the next version rather than a silence.

## Drift

Twenty one pages are checked into this repository as gzipped captures, one per page family plus the body Amazon serves with a 200 and no product on it.
Each one records what the parser made of it on the day it was taken, and `amz verify` compares that against what the same page yields now.

```
$ amz verify --live
CAPTURE         STATUS  DETAIL
product_simple  moved   261 unread regions, was 260
seller_rated    same    14 fields, 5 records
```

Fewer fields than the ledger is a regression and fails under `--strict`.
More unread regions is Amazon adding a section, which is worth knowing and is not a failure, because a tool that cried failure every time a marketing widget appeared would be ignored inside a month.
Without `--live` it reads only pages already in the cache, so a curious run costs Amazon nothing.

The ledger found two real defects on its first pass: four of Amazon's own canonical URL forms resolved to no known surface, and chart pages were recording fifty entries beside an envelope claiming nothing had been read.

`amz agent-map` prints Amazon's own interface map for a page, exactly as served.
It is recorded and never trusted. It is a statement by the site about the site, useful for finding a region worth reading and worthless as evidence that the region holds what it says.

## Access tiers

`amz` reads two tiers, selected per run:

**Public HTML** (the default) reads what a logged-out browser sees. No setup.

**PA-API** (`--api`) calls the official Amazon Product Advertising API 5.0,
signed locally with SigV4. Needs credentials (`amz config set-api`). Returns
the same output schema as the other tiers, so scripts work unchanged.

## Exit codes

```
0  success, at least one record
1  error
2  usage error
3  no results
4  partial results
5  blocked (bot-check or CAPTCHA; try --rate, --marketplace, or --api)
7  disallowed by robots.txt (the rule is named in the message)
8  robots.txt could not be fetched, so nothing was read
```

## Development

```
cmd/amz/    thin main entry point
cli/        cobra commands and output rendering
amz/        HTTP client, parsers, models, and marketplace table
docs/       documentation site (Hugo, tago-doks theme)
```

```bash
make build   # ./bin/amz
make test    # go test ./...
make vet     # go vet ./...
```

Requires Go 1.26+.

## Releasing

Push a version tag and GitHub Actions runs GoReleaser:

```bash
git tag -a v0.2.0 -m "v0.2.0"
git push --tags
```

The image tag carries no `v` prefix (`ghcr.io/tamnd/amz:0.2.0`).

## License

Apache-2.0. See [LICENSE](LICENSE).
