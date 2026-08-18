# Changelog

## v0.3.0

The version that made the tool honest.

v0.2.1 sent rotating browser user agents, loaded cookies from a file, ignored `robots.txt`, and pointed four commands at pages that redirect to a login.
It got blocked, and it deserved to.
This release deletes all of that, points every command at a surface that answers a logged out reader, and makes every record say where it came from and what it could not find.

### Breaking changes

No command is removed. Three change what they read, one needs a flag, and the record shape changes.

| v0.2.1 | v0.3.0 |
| --- | --- |
| `amz reviews <asin>` reads `/product-reviews/`, which redirects to a login | reads the reviews and the histogram embedded in the detail page, and says in `missed` that the corpus needs a login. `--deep` still tries the old path and needs `--no-robots` |
| `amz qa <asin>` reads `/ask/questions/`, which redirects to a login | reads the answered question count and any inline Q&A from the `ask` feature region, and says the rest needs a login |
| `amz offers <asin>` reads `/gp/offer-listing/`, which is disallowed and redirects | reads every offer from the detail page's buy box and offer display regions. Needs no flag |
| `amz product` returns 38 fields | returns everything the 288 feature regions carry |
| `--cookies <file>` | removed. The code that sent a `Cookie` header is deleted |
| rotating browser user agents | one honest user agent. No flag restores the old behaviour |
| flat records | records carry an envelope, so every JSON key moves down one level unless `--flat` |
| `amz db query` needs a `duckdb` binary | pure Go SQLite, no external binary |
| exit 5 for everything unusual | 5 captcha, 6 challenge, 7 needs `--no-robots`, 8 robots unfetchable, 9 needs a login |

The one behaviour change that will surprise people is that `amz reviews` returns a handful of reviews instead of failing, so the error has to be a good one when the user wanted all of them.

```console
$ amz reviews B075F5X8BR
# 13 reviews on stdout, then on stderr:
amz: 13 of 21095. amazon requires a sign-in for the review corpus, and the detail page carries the histogram and the reviews medley only. the total is the ratings count, which is the largest number the page states
amz:   /product-reviews/ is not readable without a session
amz:   /portal/customer-reviews/ is not readable without a session
amz: run `amz why reviews` for the detail
```

Thirteen reviews and an honest sentence beats zero reviews and wrong advice.

`--flat` gives v0.2.1 shaped output for one version and prints a deprecation note to stderr.

### Migrating from v0.2.1

**`--cookies <file>` is gone and there is no replacement.**
Passing it is `unknown flag: --cookies` and an exit 2.
There is no friendly deprecation shim for this one, deliberately: `TestNoCookieHeader` fails the build on `--cookies` appearing in any Go source file in this repository, along with `"Cookie"`, `loadCookies` and `cookies.txt`, and a flag that exists only to print a nicer message is still a flag named `--cookies`.
The code that read a cookie file and set a `Cookie` header is deleted, and that test keeps it deleted.
A session is what turns a public reader into an account doing something, and `amz` does not carry one.
If you were using cookies to reach the review corpus or the all offers panel, read `amz why reviews` and `amz why offers`: both of those now return what the public page carries, and say in `missed` what is behind the login and why.

**`--workers <n>` is gone.**
The client holds one connection with `MaxConnsPerHost: 1` and paces requests at 3s by default with a 1s floor that no flag, env var or config key can lower.
Scripts that passed `--workers 8` to go faster were the reason the tool got blocked.
Use `--rate` if you want to go slower.

**The product record is nested, and `jq` filters need updating.**
`asin`, `title`, `url` and `marketplace` are where they were.
Everything that was a loose scalar with a prefix in its name is now the object that prefix was standing in for.

| v0.2.1 | v0.3.0 |
| --- | --- |
| `.price` (float), `.currency` | `.offer.price.value`, `.offer.price.currency`, and `.offer.price.display` for the string the page printed |
| `.list_price`, `.savings`, `.savings_pct`, `.coupon` | the same names under `.offer` |
| `.availability`, `.in_stock`, `.condition` | the same names under `.offer` |
| `.seller_name`, `.seller_id`, `.sold_by`, `.ships_from` | `.offer.sold_by` and `.offer.ships_from`, each a `Ref` with a name, an id and a URI |
| `.brand` (string), `.brand_id`, `.brand_url` | `.brand.name`, `.brand.id`, `.brand.url`, and `.brand.uri` |
| `.bullet_points`, `.specs` | `.bullets`, `.details` |
| `.images` (strings) | `.image_urls` for the strings, `.images` for the objects with their variants |
| `.category_path`, `.browse_node_ids` | `.breadcrumb`, one `Ref` per crumb with its node id and URI |
| `.rank`, `.rank_category` | `.ranks`, because a product holds several and v0.2.1 kept one |
| `.variant_asins`, `.color_to_asin` | `.variation`, which carries the dimensions as well as the siblings |
| `.reviews_count`, `.answered_qs` | `.reviews.total_count`, `.questions.total_count`, each beside a `loaded` and a `complete` |

A price is an object because a float alone cannot say what the page actually printed, and every `Money` keeps the display string next to the parse so a wrong parse is recoverable rather than lost.
A missing price is `null` and not `0`, which is the other half of the same idea: most of the nullable fields above are pointers now, so absent and zero stop looking alike.

The new top level key is `envelope`, and it is worth reading before you reach for `--flat`.
It carries the surfaces the record came from, the clock they were read at, `via` naming the region each field came out of, and `missed` naming what the page did not give up.

`--flat` emits the v0.2.1 shape for this one version and prints a deprecation note to stderr.
It is removed in v0.4.0, it carries the envelope even so, and it drops the rails, the delivery promises, the rating histogram, the variation dimensions and the per unit price, because the old shape has no slot for any of them.

**Four commands return less than they used to, on purpose.**
`reviews` returns the reviews the detail page embeds instead of failing on a login redirect.
`qa` returns the answered question count and any inline pairs instead of failing on a login redirect.
`offers` returns the buy box and the offers the detail page carries instead of failing on a disallowed path, and no longer needs a flag.
`variants` returns the twister matrix, which names every sibling but prices only the ones the page prices, and `--resolve` fetches the rest one at a time.
In all four cases the shortfall is in `missed` with a count, the surfaces that were tried, and an `amz why <topic>` to run.
A number that is smaller and true beats a number that is larger and invented.

**Exit codes split.**
v0.2.1 returned 5 for everything unusual.
Now 5 is a CAPTCHA, 6 is an interstitial that is worth retrying later, 7 is a read that would need `--no-robots`, 8 is a `robots.txt` that could not be fetched, and 9 is a page that wants a login.
3 is still no data and 4 is still partial.
A script that tested `$? -eq 5` and retried now retries a CAPTCHA that will not clear, so test for 6 instead.
The full table is in the README under Exit codes, and `amz why blocked`, `amz why captcha` and `amz why robots` each explain one of them.

### Added

`amz serve` puts the 25 read commands behind HTTP and `amz mcp` puts the same registry on stdio as Model Context Protocol.
Neither reimplements a command: a tool call builds an argv from a fixed table and re-enters the same command tree a terminal uses.
The registry is the allowlist, so `crawl`, `seed`, `export`, `config`, `open` and `cache clear` are unreachable rather than refused, and `--no-robots` is an argument of no tool.
Every response carries the envelope, and `missed` is serialized whether or not it has anything in it.

`amz robots` prints the marketplace's live `robots.txt` and the group `amz` reads under, and `amz robots check <url>` prints the rule that decided a URL.
`amz surfaces` lists every Amazon surface the tool knows, with what was measured about it.
`amz extraction` prints how each field is read and what is on the page that nothing reads.
`amz verify [--live] [--strict]` compares pages against what they yielded when they were captured, and runs weekly in CI.
`amz agent-map` prints Amazon's own description of a page verbatim.
`amz why [topic]` explains why a command returns less than you expected, measured and dated.
`amz doctor` prints what this client sends, what the two key surfaces answer, and what is in the store.

`amz refine <query>` lists the refinement groups and values a query offers, with their filter tokens.
`amz search` gained the refinement vocabulary read off the page, real pagination that stops on the advertised last page, and `--all` to partition a query and get past the 306 result ceiling.
`amz tree` walks the browse node graph outward from a node.
`amz graph` walks the edges a crawl recorded, over a closed vocabulary of sixteen predicates.
`amz series` prints the price and rank history this machine has observed.
`amz query`, `amz find` and `amz lookup` read the local store with no network at all.

### Changed

Requests are built in one place.
`amz/headers.go` is the only file in the repository that sets a request header, it sets three, and the user agent is `amz-cli/<version> (+https://github.com/tamnd/amz-cli)` built from the version rather than written as a literal.

`robots.txt` is fetched, parsed and enforced at request time and cached for 24 hours with the time it was fetched.
Longest match wins, `Allow` beats `Disallow` at equal length, `*` and `$` are matched against the path and the query because Amazon has `/b?*node=<id>` rules, and named agent groups are honoured.
`amz` will not rename itself to fall back to `*`.
`--no-robots` prints every rule it breaks, needs `--yes` with `crawl`, raises the pace floor to 5s, is not sticky, and cannot be set by config or by env.

Extraction reads the page the way the page labels itself.
Fields come off `data-feature-name`, `data-component-type`, `data-csa-c-item-id`, `data-testid` and the payloads Amazon ships, in that order, and every field records which rung of that ladder produced it.
The number of fields still read by a bare CSS selector is printed by `amz extraction` and asserted to be non-increasing.

The store is SQLite through a pure Go implementation, so there is no external binary on `PATH` to install and no cgo in the build.
A crawl is resumable: killed and restarted, it loses nothing and duplicates nothing.

Sellers, brands and authors are global rather than marketplace scoped, because a merchant id is the same id in every store.
This changes the `uri` field on every seller, brand and author reference, which is why it landed inside v0.3.0 rather than after it.

### Fixed

`amz brand <name>` works with a bare name.
Amazon puts a storefront at `/stores/<name>/page/<uuid>` and nothing derives that uuid from the name, so `/stores/anker` is a 404 and always was.
A name that does not already point at a page is now resolved the way a person resolves it, by searching the name and following the byline link on the first result that is actually that brand, and a slug that already carries the uuid still costs one request.

The brand is read off `premiumBylineInfo`, which is where Amazon puts it for a premium brand while leaving `bylineInfo` on the page and empty.
Every premium listing on the site, which is most of the ones anybody searches for by name, returned a null brand before this.
A byline that renders the brand as a logo and then as a link no longer stops at the logo, since the first anchor in that region has no text in it.

`api-services-support@amazon.com` is no longer treated as a block marker above 5 KB, so a real soft 404 classifies as not found instead of as blocked.
Gzip is decoded correctly now that `Accept-Encoding` is set by hand, which turns off Go's transparent handling.
The rating histogram is marked `derived`, because Amazon publishes integer percentages and the bucket counts are reconstructed from them.
Money is exact, on `*big.Rat`, and never a float.

## v0.2.1

Earlier releases are on the [releases page](https://github.com/tamnd/amz-cli/releases).
