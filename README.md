# amz

[![CI](https://github.com/tamnd/amz-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/tamnd/amz-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/tamnd/amz-cli)](https://github.com/tamnd/amz-cli/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/tamnd/amz-cli.svg)](https://pkg.go.dev/github.com/tamnd/amz-cli)
[![Go Report Card](https://goreportcard.com/badge/github.com/tamnd/amz-cli)](https://goreportcard.com/report/github.com/tamnd/amz-cli)
[![License](https://img.shields.io/github/license/tamnd/amz-cli)](./LICENSE)

A command line for Amazon. `amz` reads every public Amazon surface — products,
search, reviews, Q&A, offers, charts, categories, brands, sellers, authors, and
deals — and turns each one into clean, pipeable records. One pure-Go binary, no
API key required.

[Install](#install) • [Commands](#commands) • [Usage](#usage) • [Access tiers](#access-tiers)

![amz reading Amazon bestsellers as a table and piping through jq](docs/static/demo.gif)

It reads the public pages on `amazon.com` over plain HTTPS, extracts the JSON-LD
Amazon marks up for machines, and falls back to precise HTML selectors so each
record is rich with no missing fields where the page had them. Every request is
paced, retried on transient failures, and cached on disk. When Amazon serves a
bot-check page instead of content, `amz` detects it and exits with a distinct
code rather than handing you garbage. `robots.txt` is fetched and obeyed on
every request.

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
| `amz product <ASIN\|url>...` | one or more product detail pages, fully normalized |
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
| `amz category <node_id\|url>` | a browse node: name, breadcrumb, children, top ASINs |
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
amz db query "select data->>'brand' brand, count(*) n from products group by brand order by n desc"
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

`amz` fetches `robots.txt` from the marketplace it is reading, caches it for 24
hours, and asks it before every request. There is no compiled-in copy, because a
stale copy that says yes is worse than no answer. If the file cannot be fetched,
`amz` reads nothing and exits 8.

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

That last row is why the matcher runs against the query string and not the path:
five browse nodes are refused by a pattern that only ever matches inside a query.

`--no-robots` is the override. It is a flag and only a flag: not a config key,
not an environment variable, and it lasts for one run. It prints a banner, names
every rule it breaks as it breaks it, raises the pace floor from 1s to 5s, and
needs `--yes` before it will do this to a whole crawl queue.

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
