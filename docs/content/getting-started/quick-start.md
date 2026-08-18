---
title: "Quick start"
description: "From an empty terminal to a fully structured product record, in a handful of commands."
weight: 30
---

This walks the core loop: turn an ASIN into a normalized product, search the
catalog, and read the social proof. Every command here reads live Amazon, one
page at a time, with a 3 second pace between requests.

## 1. Look up a product

```bash
amz product B075F5X8BR
```

A product page becomes one record. On a terminal you get a reading view:

```
Skullcandy Jib Wired Earbuds with 3.5mm AUX Plug - White | 3.5mm Wired Earbuds, In-Ear Headphones, Noise Isolating Fit, In-Line Microphone Call and Track Control, Travel Ready
  B075F5X8BR  ·  amazon.com  ·  read 2026-08-18 12:42

  VND209,378                    was VND261,788, save 20%
  In Stock                      ships from and sold by Amazon.com
  4.4 out of 5                  21,095 ratings

  5 ★ ████████████████████████████████████  73%
  4 ★ ██████                                13%
  3 ★ ███                                    7%
  2 ★ █                                      2%
  1 ★ ██                                     5%
                                counts derived from integer percentages

  Brand      Skullcandy
  Rank       #38 in Electronics · #5 in Earbud & In-Ear Headphones
  Variants   10 of 10 shown  ·  Size, Color
  Category   Electronics › Headphones, Earbuds & Accessories › Headphones & Earbuds › Earbud Headphones
  Bought     10K+ bought in past month

  not read
    other_offers  1 of 2. the all-offers panel is built by javascript and states
                  only its own count on the page
                  run `amz why offers` for the detail
    reviews       13 of 21,095. amazon requires a sign-in for the review corpus, and
                  the detail page carries the histogram and the reviews medley only.
                  the total is the ratings count, which is the largest number the
                  page states
                  run `amz why reviews` for the detail
```

The prices are in dong because amazon.com prices in the currency of whoever is
asking, and the machine that ran this is in Vietnam. `--marketplace` and a
different egress are the two things that change it.

The `not read` block is the part worth reading twice. It is on every record, it
names what the page did not give up and how much of it, and it is why an empty
list here never quietly reads like an answer.

Pipe it and you get JSONL. Ask for JSON to see the shape:

```bash
amz product B075F5X8BR -o json
```

```json
{
  "asin": "B075F5X8BR",
  "parent_asin": "B08X7M7CVW",
  "marketplace": "us",
  "url": "https://www.amazon.com/dp/B075F5X8BR",
  "title": "Skullcandy Jib Wired Earbuds with 3.5mm AUX Plug - White ...",
  "upc": "878615091375",
  "brand": {
    "kind": "brand",
    "name": "Skullcandy",
    "url": "https://www.amazon.com/stores/Skullcandy/page/9F16B940-...",
    "resolved": false
  },
  "offer": {
    "price": { "display": "VND209,378", "value": 209378, "currency": "VND", "via": "corePrice" },
    "list_price": { "display": "VND261,788", "value": 261788, "currency": "VND", "via": "corePriceDisplay_desktop" },
    "savings_pct": 20,
    "availability": "In Stock",
    "in_stock": true,
    "sold_by": { "kind": "seller", "name": "Amazon.com", "resolved": false },
    "ships_from": { "kind": "seller", "name": "Amazon.com", "resolved": false },
    "returns": "FREE 30-day refund/replacement"
  },
  "rating": 4.4,
  "ratings_count": 21095,
  "distribution": { "percent": [73, 13, 7, 2, 5], "derived": true, "total": 21095 },
  "reviews": { "loaded": 13, "total_count": 21095, "complete": false },
  "ranks": [
    { "rank": 38, "category": "Electronics", "overall": true },
    { "rank": 5, "category": "Earbud & In-Ear Headphones" }
  ],
  "envelope": {
    "family": "product",
    "via": { "price": "corePrice", "brand": "bylineInfo", "title": "title" },
    "level": { "price": 1, "brand": 1, "title": 1 },
    "missed": [{ "field": "similar_asins", "why": "..." }]
  }
}
```

That is a trimmed view. A real record carries the feature bullets, the technical
detail table, the variation matrix and its siblings, the breadcrumb as resolvable
nodes, a sample of reviews, `bought_past_month`, and an `extra` map of the
javascript state the page shipped. The [data model](/reference/data-model/) lists
them all.

Three things are worth noticing in that shape. Money is an object, not a float,
so the display string Amazon printed survives beside the parsed value. An entity
like a brand or a seller is an object with a `resolved` flag, so you can tell a
name that was read from a name that was looked up. And `envelope.via` names the
region every field came out of.

You can pass a full URL instead of an ASIN, and amz pulls the ASIN out of it:

```bash
amz product "https://www.amazon.com/dp/B075F5X8BR/ref=sr_1_1"
```

## 2. Search the catalog

```bash
amz search "mechanical keyboard" -n 5
```

Each row is one result card: ASIN, title, price, rating, delivery, and whether it
is Prime. Sponsored placements are dropped by default and the count is reported,
so you always know how many were left out. Refine and choose your output:

```bash
amz search "mechanical keyboard" --price 50-150 --prime -o jsonl
amz search "mechanical keyboard" --sort price-asc -o table
amz search "mechanical keyboard" --brand Logitech --stars 4
```

`--brand`, `--seller`, `--stars` and `--condition` take names and are resolved
against the sidebar of the page that comes back, so a value Amazon did not apply
is an error rather than an unfiltered result set wearing a filter's label. To see
the whole vocabulary a query offers before you commit to one:

```bash
amz refine "mechanical keyboard"
```

Amazon serves at most 306 results over 20 pages for any query, whatever total it
advertises. `amz search --all` partitions the query and unions the cells to get
past that, and `--dry-run` prices the walk first:

```bash
amz search "usb-c hub" --all --dry-run
```

## 3. Read the social proof

```bash
amz reviews B075F5X8BR -n 10
amz reviews B075F5X8BR --stars 5 --verified
amz qa B075F5X8BR
```

The review corpus is behind a sign-in. What is public is the histogram and the
medley of reviews the detail page itself carries, which is around a dozen, and
`amz reviews` says so on stderr every time rather than printing thirteen rows as
though they were the corpus:

```
amz: 13 of 21095. amazon requires a sign-in for the review corpus, and the detail page carries the histogram and the reviews medley only. the total is the ratings count, which is the largest number the page states
amz:   /product-reviews/ is not readable without a session
amz:   /portal/customer-reviews/ is not readable without a session
amz: run `amz why reviews` for the detail
```

`--stars`, `--verified` and `--with-images` filter that medley locally.

## 4. Compare offers and read the charts

```bash
amz offers B075F5X8BR                 # every buying option the page states
amz bestsellers electronics -n 10     # the live top sellers in a category
amz deals --min-discount 30           # today's deals, 30% off or better
```

## 5. Pick a marketplace

Every command takes `-m` to switch storefront:

```bash
amz product B075F5X8BR -m uk          # amazon.co.uk
amz bestsellers -m de                 # the German top 100
```

See what would be fetched without fetching, which is the cheap way to check a
marketplace builds the URL you expect:

```bash
amz product B075F5X8BR -m jp --dry-run
```

## 6. Compose

Output that pipes is the point. Pull the ASINs of the top 25 bestsellers and
fetch a full record for each:

```bash
amz bestsellers electronics -n 25 --fields asin -o csv --no-header \
  | xargs -I{} amz product {} -o jsonl
```

Count the five-star reviews in the sample:

```bash
amz reviews B075F5X8BR --stars 5 -o jsonl | wc -l
```

Ask why a field is missing, with the measurement and the date behind it:

```bash
amz why reviews
```

## Where to next

You have the core loop. From here:

- [Products](/guides/products/) goes deep on the product record and variations.
- [Search](/guides/search/) covers every refinement and the 306 ceiling.
- [Reviews and Q&A](/guides/reviews-and-qa/) covers the social-proof surfaces.
- [Recipes](/guides/recipes/) chains commands into real pipelines.
- [Crawling at scale](/guides/crawling/) covers the queue and the local store.
- The [data model](/reference/data-model/) names every field of every record.
- The [CLI reference](/reference/cli/) lists every command and flag.
