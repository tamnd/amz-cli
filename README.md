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
| `amz refine <query>` | the refinement groups and values a query offers, with their filter tokens |
| `amz reviews <ASIN>` | the reviews the detail page carries, with the histogram; `--stars`, `--sort` |
| `amz qa <ASIN>` | the answered question count, and the pairs when the page carries them |
| `amz offers <ASIN>` | the buy box winner and the count of the offers behind it |
| `amz variants <ASIN>` | the variation matrix, one row per sibling; `--resolve` |
| `amz bestsellers [category]` | the live top-100 chart |
| `amz new-releases [category]` | newest releases in a category |
| `amz movers [category]` | biggest 24-hour rank movers |
| `amz wished [category]` | most wished-for items |
| `amz gifted [category]` | most gifted items |
| `amz category <node_id\|url>` | a browse node: name, related nodes, shelves, top ASINs |
| `amz tree [node_id\|url]` | the browse node graph outward from a node, one request per node |
| `amz brand <slug\|url>` | a brand storefront |
| `amz seller <id\|url>` | a third-party seller profile and rating breakdown |
| `amz author <slug\|url>` | an Author Central page |
| `amz deals` | today's deals grid |
| `amz seed` | enqueue ASINs or URLs into the crawl queue |
| `amz crawl` | drain a frontier into the local store, with `--dry-run` to price it first |
| `amz graph <uri\|asin>` | walk the edges a crawl recorded, outward from one node |
| `amz export` | the whole store as JSONL, turtle or n-triples |
| `amz query <sql>` | read-only SQL over the local store |
| `amz find <text>` | full text search over everything crawled |
| `amz lookup <uri\|asin>` | one stored record, byte for byte, with no network |
| `amz series <asin>` | the price and rank history this machine has observed |
| `amz db` | where the store is, what is in it, compact it, delete it |
| `amz asin <url>...` | read ids out of URLs, ISBNs and `amz:` URIs, offline |
| `amz open <ASIN\|query>` | open the relevant Amazon page in the browser |
| `amz serve` | the read commands over HTTP, envelope and all |
| `amz mcp` | the same tools over stdio, for a model |
| `amz robots` | the marketplace's live robots.txt and the group `amz` reads under |
| `amz robots check <url>...` | ask robots.txt about a URL and print the rule that decided it |
| `amz surfaces` | every Amazon surface `amz` knows, with what was measured about it |
| `amz extraction [asin\|url]` | how each field is read, and what is on the page that nothing reads |
| `amz verify [--live] [--strict]` | compare pages against what they yielded when they were captured |
| `amz agent-map <asin\|url>` | Amazon's own description of a page, printed verbatim |
| `amz why [topic]` | why a command returns less than you expected, measured and dated |
| `amz doctor` | what this client sends, what the two key surfaces answer, what is in the store |
| `amz info` | show access tier, marketplace, and config summary |
| `amz config` | view and manage configuration and PA-API credentials |
| `amz cache path\|info\|clear` | inspect or clear the on-disk page cache |

Full reference and guides live at [amz-cli.tamnd.com](https://amz-cli.tamnd.com).

## Usage

```bash
amz product B084DWG2VQ                     # one product, fully normalized
amz product B084DWG2VQ --light             # the smallest useful read
amz variants B084DWG2VQ                    # every sibling in the variation family
amz why reviews                            # why there are thirteen and not four thousand
amz doctor                                 # check the client, the network and the store
amz search "mechanical keyboard" -n 20     # catalog search results
amz reviews B084DWG2VQ --stars 1           # the one-star reviews
amz offers B084DWG2VQ                      # the buy box and how many offers sit behind it
amz bestsellers electronics                # the live top-100 chart
amz category 172282                        # the Electronics browse node
amz product B084DWG2VQ -m uk              # any of 16 marketplaces
```

One product on a terminal prints as a card rather than as a row of truncated
cells. The histogram is drawn because Amazon publishes it, and the block at the
bottom is generated from the record's own account of what it could not read:

```
Echo Dot (4th Gen) | Smart speaker with Alexa | Charcoal
  B075F5X8BR  ·  amazon.com  ·  read 2026-08-17 07:15

  $49.99                        was $59.99, save 17%
  In Stock                      ships from and sold by Amazon.com
  4.7 out of 5                  284,512 ratings

  5 ★ ████████████████████████████████████  73%
  4 ★ ███████                               15%
  3 ★ ██                                     6%
  2 ★ █                                      2%
  1 ★ █                                      4%
                                counts derived from integer percentages

  Brand      Amazon
  Rank       #3 in Electronics · #1 in Smart Speakers
  Variants   2 of 2 shown  ·  Color
  Category   Electronics › Smart Home › Speakers

  not read
    other_offers 1 of 22. the all-offers panel is built by javascript and states
                 only its own count on the page
                 run `amz why offers` for the detail
    reviews      13 of 284,512. amazon requires a sign-in for the review corpus
                 run `amz why reviews` for the detail
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
amz crawl --chart bestsellers --category electronics --dry-run   # what it will cost
amz crawl --chart bestsellers --category electronics
amz query "select brand, count(*) n from product group by brand order by n desc"
amz series B084DWG2VQ                                            # every price seen, oldest first
```

The store is SQLite, through a pure Go implementation, so there is nothing to
install alongside the binary and nothing that can be missing. The full record
lives in a `json` column and the typed columns are an index over it, which means
`amz lookup` gives back exactly what was stored rather than a reconstruction.
`price`, `rank` and `chart_entry` are append only, so a later crawl adds to the
history and can never rewrite it.

### The graph

A crawl records more than records. One detail page names a brand, a seller, a
fulfiller, a parent ASIN, six siblings, four browse nodes, three sales ranks,
eight review authors and sixty related products, and all of those come free with
a page that was fetched for its price. They are stored as edges, and `amz graph`
walks them.

```bash
amz graph B084DWG2VQ                          # one hop out, organic only
amz graph B084DWG2VQ --depth 2 --symmetric    # two hops, following variants both ways
amz graph B084DWG2VQ --predicate sold_by      # just the merchant
amz graph B084DWG2VQ --edges -o jsonl         # the claims rather than the nodes
amz graph --predicates                        # the sixteen, and what each one carries
```

The vocabulary is sixteen predicates and it is closed, because an open one means
every consumer has to handle a relationship it has never seen and by the third
one nobody does. An edge is a claim rather than a pair of nodes: it carries the
surface that asserted it and the time that surface was read, since a seller wins
the buy box for an afternoon and a rail is regenerated per request.

Cycles are normal, so the walk is visited-set based and `--depth` defaults to 1.
Paid placements are stored, flagged, and excluded from the walk unless you pass
`--include-sponsored`. Nothing here fetches: `amz graph` reads the store, and a
node with no edges was either never crawled or crawled without `--follow-rails`.

### Export

```bash
amz export                                    # JSONL, header line first
amz export --format turtle --with-text        # RDF, schema.org where it fits
amz export --format ntriples > store.nt
```

JSONL keeps every field, including the ones with no schema.org term, and leads
with a header line carrying the tool, the version and the marketplace so a file
that gets piped, split and reassembled still says where it came from.

Three things are worth knowing before loading the RDF somewhere. Every offer
carries `amzv:retrievedAt`, without exception and without a flag, because a price
without a timestamp is not a fact. `amzv:distributionDerived` is on every rating
histogram, because the bucket counts are reconstructed from the integer
percentages Amazon publishes. An organic recommendation is `schema:isRelatedTo`
and a paid one is `amzv:sponsoredPlacement`, never both and never the same term.

Product descriptions and review text are only included with `--with-text`, for
the reason in the access tiers section below: a local store of prices is your own
measurements, and a local store of Amazon's prose is a copy of Amazon's prose.

### The server

The same read commands, over HTTP for a script and over stdio for a model.

```bash
amz serve                                     # 127.0.0.1:8787
amz serve --tools                             # the registry, without starting anything
curl localhost:8787/v1/tools                  # the 25 tools and their arguments
curl 'localhost:8787/v1/tools/reviews?asin=B084DWG2VQ&stars=5&verified'
curl -X POST localhost:8787/v1/tools/search -d '{"query":"usb-c hub","brand":"Anker","sort":"price-asc"}'
amz mcp                                       # the same tools as Model Context Protocol
```

Neither one reimplements anything. A tool call builds an argv and re-enters the
same command tree the terminal uses, so `amz search` over HTTP and `amz search`
in a shell cannot answer differently, and a parser fix reaches all three front
doors at once. The server answers one call at a time for the same reason
`--workers` is gone: two requests in flight make `--rate` a lie by a factor of
two.

Every response carries the envelope, and `missed` is in it whether or not it has
anything in it. An empty list means the tool looked and there was nothing more.
A missing key would mean the server forgot to say, and a caller cannot tell those
apart afterwards. This is the part that matters over the wire: `reviews` returns
the handful of reviews the detail page carries along with a `missed` entry saying
there are 284,512, so a model reading the result is told what it did not get
rather than left to assume it got everything.

The registry is the allowlist. `crawl`, `seed`, `export`, `config`, `open` and
`cache clear` are not in it, which makes them unreachable rather than refused:
there is no name a caller can send that resolves to one. `--no-robots` is an
argument of no tool, the argv is built from a fixed table, and positionals go
after `--` so no value can turn into a flag. Ignoring robots.txt is a decision a
person makes for one run at their own terminal, not something a tool call
inherits.

`amz serve` binds loopback and refuses a public address without `--yes`, because
an open port on this is somebody else's rate limit and somebody else's IP in
Amazon's logs. It carries no session and wants none.

### Global flags

```
-o, --output       table|json|jsonl|csv|tsv|url|raw   (auto: table on a TTY, jsonl when piped)
    --fields       comma-separated columns to include
    --no-header    omit the header row in table/csv/tsv
    --template     Go text/template applied per record
-n, --limit        max records (0 = unlimited)
-m, --marketplace  marketplace slug: us|uk|de|fr|jp|ca|in|it|es|... (default us)
-q, --quiet        suppress progress output
-v, --verbose      more detail; -vv adds where each field came from
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
price is `null` rather than `0`.

References to other entities carry a URI in the `amz:` space, and whether that
URI names a marketplace depends on the id. Products, browse nodes, charts and
searches are written `amz:us/product/B075F5X8BR`, because the same ASIN is a
different product at a different price in every storefront Amazon runs and
`172282` is Electronics on `.com` and something else on `.de`. Sellers, brands,
authors, reviews and deals are written `amz:seller/A2L77EE7U53NWQ` with no
marketplace, because a merchant id is one company everywhere it trades. What
varies by storefront is the feedback and the storefront page, and those are on
the record, which says which marketplace it was read in.

The store follows the same rule. Its product key is `(marketplace, asin)`, so
crawling `.com` and `.co.uk` gives two rows rather than one row that changes
price depending on which crawl ran last.

```console
$ amz asin "https://www.amazon.co.uk/dp/0439023483" -o jsonl
amz: https://www.amazon.co.uk/dp/0439023483 is the uk marketplace, so reading uk and not us
{"asin":"0439023483","input":"https://www.amazon.co.uk/dp/0439023483","isbn10":"0439023483","isbn13":"9780439023481","kind":"isbn10","marketplace":"uk","uri":"amz:uk/product/0439023483"}
```

That is one command, no network, and it is the fastest way to see what amz
thinks a link is. The `marketplace` is what the link said, not what `-m` was set
to, and a link wins over the flag: somebody who pastes an `amazon.co.uk` URL and
leaves the default at `us` meant the URL, so amz reads the UK and says so on
stderr. A bare ASIN leaves `marketplace` empty, because ten characters belong to
every storefront equally and filling it in would be the default dressed up as a
fact.

`--depth` decides how much of a product page is read:

| Depth | Requests | What you get |
| --- | --- | --- |
| `quick` | 1 | the mobile URL, `/gp/aw/d/`, which returns the same page as `meta` |
| `meta` | 1 | the full 2.2 MB detail page, rails dropped |
| `full` | 1 | the same page with the recommendation rails kept |
| `deep` | 2 + one per sibling | full, plus each variation sibling's own page and the seller |

`meta` is the default. `deep` prints its bill and stops for `--yes` above twenty
requests, because an apparel listing with 88 siblings is ninety requests for one
record.

`quick` was specified as the cheap read and is not one. Amazon gzips both
responses, so the 374 KB it was specified at is what the mobile URL weighs on the
wire while the 2.2 MB it was compared against is what the detail page weighs
after decoding. Measured on 2026-08-17 for B075F5X8BR, `/gp/aw/d/` is 373,945
bytes on the wire and 2,197,291 decoded, against 373,980 and 2,196,553 for
`/dp/`. It is the same page for the same bytes. `quick` is kept because it is the
only thing that reads that surface, and the saving returns if Amazon serves it a
lighter rendering again.

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

`amz verify --live --strict` also runs weekly in CI, on a Monday, and opens an issue when a page yields less than its ledger entry.
Weekly and not nightly, because the ledger is twenty one pages read one at a time at the default pace, and running that every night to catch a change that takes weeks to matter is somebody else's bandwidth spent on our convenience.
That job honours `robots.txt` like everything else here, and `--no-robots` is not on its command line and is not going to be: a scheduled job is the last place a person should be able to hide an override.

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
5  blocked (a CAPTCHA came back; `amz why blocked`)
6  a bot interstitial was still standing after the backoff; `amz why captcha`
7  disallowed by robots.txt (the rule is named in the message); `amz why robots`
8  robots.txt could not be fetched, so nothing was read; `amz why robots`
9  the surface needs a signed-in session, which amz does not have; `amz why policy`
```

Every failure names the URL and the rule or the redirect that produced it, and
the codes above that mean Amazon said no also name the `amz why` topic that
explains what happened and what to do about it.

## Development

```
cmd/amz/    thin main entry point
cli/        cobra commands and output rendering
amz/        HTTP client, parsers, models, marketplace table, and the two servers
pkg/        the importable parts: asin, uri, graph, rdf
docs/       documentation site (Hugo, tago-doks theme)
```

The dependencies point one way. `pkg/*` imports nothing from `amz/`, and `amz/`
imports nothing from `cli/`. That is why `amz serve` and `amz mcp` live in `amz/`
and know nothing about cobra, while the tool registry lives in `cli/` and knows
nothing about HTTP.

```bash
make build   # ./bin/amz
make test    # go test ./...
make vet     # go vet ./...
```

Requires Go 1.26+.

## Releasing

Push a version tag and GitHub Actions runs GoReleaser:

```bash
git tag -a v0.3.0 -m "v0.3.0"
git push --tags
```

Ten targets across five operating systems, a multi-arch container image, deb, rpm
and apk packages, a Homebrew cask, a Scoop manifest, an SBOM per archive and a
keyless cosign signature over `checksums.txt`. The image tag carries no `v`
prefix (`ghcr.io/tamnd/amz:0.3.0`).

What changed in each release, and how to move a script from one to the next, is
in [CHANGELOG.md](./CHANGELOG.md).

## License

Apache-2.0. See [LICENSE](LICENSE).
