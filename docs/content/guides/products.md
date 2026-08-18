---
title: "Products"
description: "Turn an ASIN or URL into a fully normalized product record, with variations, specs, and rank."
weight: 10
---

`amz product` is the workhorse. It fetches a detail page and normalizes it into
one record that carries everything the page exposes.

There is no structured blob to read first. amazon.com publishes no JSON-LD, no
`__NEXT_DATA__` and no Apollo cache, so every field comes from a region Amazon
named, a JavaScript payload it ships, an attribute, or a selector, and the record
says which of those answered.

## One product

```bash
amz product B075F5X8BR
amz product B075F5X8BR -o json
```

You can pass several at once, or full URLs; amz extracts the ASIN from any
Amazon URL shape:

```bash
amz product B075F5X8BR B07XJ8C8F5 "https://www.amazon.com/dp/B08N5WRWNW"
```

## The fields

A product record names every field the page had:

| Field | Meaning |
| --- | --- |
| `asin`, `parent_asin` | the item, and its variation parent when present |
| `title`, `byline`, `manufacturer` | identity as the page words it |
| `brand`, `authors` | references to the storefronts behind those names |
| `isbn10`, `isbn13`, `upc`, `ean`, `model_number` | the identifiers the detail table carries |
| `offer` | the buy box: price, list price, savings, coupon, availability, seller, delivery, returns |
| `other_offers` | how many other buying options the page claims |
| `rating`, `ratings_count`, `reviews_count` | social proof |
| `distribution` | the five bucket star histogram |
| `reviews`, `questions` | how many exist, not what amz holds: `loaded`, `total_count`, `complete` |
| `review_sample`, `qa_sample` | the reviews and pairs amz does hold, which is what the detail page embedded |
| `bought_past_month`, `bought_past_month_n` | the "N+ bought in past month" line, and the parse of it |
| `description`, `bullets` | marketing copy |
| `details` | the technical details table, as key and value pairs |
| `images`, `image_urls`, `videos` | one entry per distinct photo with its variants, the same photos as plain URLs, and the inline videos |
| `breadcrumb`, `ranks` | browse node references, and every Best Sellers Rank line |
| `variation` | the twister: dimensions, the current selection, and the siblings |
| `rails`, `similar_asins` | the recommendation strips and the similar items |
| `extra` | every payload the page shipped that no field reads yet, verbatim |
| `envelope` | which responses this came from, what answered each field, and what was looked for and not found |

Fields the page did not carry are omitted, and that omission is load bearing: if
a field is missing and nothing in `envelope.missed` names it, amz read the place
that field lives and there was nothing there.

Prices are objects rather than numbers. Each keeps the string Amazon printed
beside the parse of it, so a wrong parse is recoverable, and a product with no
price serializes as `null` rather than as `0`.

### The buy box

`offer` holds `price`, `list_price`, `savings`, `savings_pct`, `per_unit` and
`per_unit_label`, `coupon`, `subscribe`, `business_price`, `condition`,
`availability`, `in_stock`, `sold_by`, `ships_from`, `prime`, `delivery`, and
`returns`.

`savings` and `savings_pct` are computed from the two prices rather than read off
the page. Amazon prints its own saving line and the two disagree often enough
that the derived pair is the honest one. `availability` is the line as written
and `in_stock` is amz's reading of it, and both are kept because the reading is a
guess about phrasing and the string is not.

### Images and videos

Amazon serves the same photo at dozens of sizes and from several CDN hosts. amz
strips the size modifier from every image URL and pins one canonical host, so
`images` holds one full-resolution URL per distinct photo, with the thumbnails,
tracking pixels, and sprites removed. The same canonicalization runs on every
surface that carries an image (search cards, reviews, brand logos, author
photos), so an image URL means the same thing everywhere.

### Ranks

A product is usually ranked once overall and again in one or more subcategories.
Each entry in `ranks` carries `rank`, `category`, `node`, and `overall`:

```bash
amz product B075F5X8BR -o json --fields asin,ranks
```

`overall` marks the department level rank, the one people mean by "sales rank",
and it is flagged rather than assumed to be the first line. `node` is the browse
node the rank line links, which is the strongest category edge amz has, because
it is an identifier Amazon stated rather than a name lifted from a breadcrumb.

## Variations

`variation` carries the twister: the parent ASIN, the dimensions Amazon offers,
the current selection, and the siblings. Add `--variants` to print the sibling
ASINs as their own rows:

```bash
amz product B075F5X8BR --variants -o jsonl
```

A sibling always has its ASIN and its dimension values. It gets a price, an image
and an availability only at `--depth deep`, which fetches each sibling's own
page, because the parent page states none of those three and filling them from
the parent would quote the selected variant's price against every colour in the
family.

## Depth

`--depth` decides how much is read:

| Depth | Requests | What you get |
| --- | --- | --- |
| `quick` | 1 | the mobile URL, `/gp/aw/d/`, which returns the same page as `meta` |
| `meta` | 1 | the full 2.2 MB detail page, rails dropped |
| `full` | 1 | the same page with the recommendation rails kept |
| `deep` | 2 plus one per sibling | full, plus each variation sibling's own page and the seller |

`meta` is the default. It drops the rails it read and records that it did, so a
product with twelve strips on the page never looks like a product with none.
`full` costs no extra request, it just keeps them.

`quick` was specified as the cheap read and is not one. Amazon gzips both
responses, so the 374 KB it was specified at is what the mobile URL weighs on the
wire while the 2.2 MB it was compared against is what the detail page weighs
after decoding. Measured on 2026-08-17 for B075F5X8BR, `/gp/aw/d/` is 373,945
bytes on the wire and 2,197,291 decoded, against 373,980 and 2,196,553 for
`/dp/`. It is the same page for the same bytes. `quick` is kept because it is the
only thing that reads that surface, and the saving returns if Amazon serves it a
lighter rendering again.

`deep` prints its bill and stops for `--yes` above twenty requests. An apparel
listing with 88 siblings is ninety requests and a minute and a half of polite
crawling for one record, and that is worth agreeing to rather than discovering.

```bash
amz product B075F5X8BR --depth quick     # cheapest useful read
amz product B075F5X8BR --depth deep --yes
```

## The buy box alongside

`--with-offers` emits the buy box as its own offer row beside the product, so a
pipeline that wants both shapes gets them from one fetch:

```bash
amz product B075F5X8BR --with-offers -o json
```

It is the buy box and not a list of every seller. The other sellers' rows are
drawn by JavaScript and the endpoint behind them answers 404 to a direct
request, so what the page states is a count, and that count is in
`other_offers`. See [offers](/guides/offers-and-charts/) for the measurement.

## The raw page

When you want the bytes amz parsed, not the record:

```bash
amz product B075F5X8BR --raw > page.html
```

## Just the price

For price-watching, `price` skips everything else:

```bash
amz price B075F5X8BR B07XJ8C8F5
amz price B075F5X8BR -m uk
```

## Recommendation rails

`related` pulls the recommendation cards off a detail page, the "customers also
viewed" and "frequently bought together" rails:

```bash
amz related B075F5X8BR
amz related B075F5X8BR --kind also-viewed -o jsonl
```

## Dry run

See the URL without fetching, useful when scripting across marketplaces:

```bash
amz product B075F5X8BR -m de --dry-run
```
