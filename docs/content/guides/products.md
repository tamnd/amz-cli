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
amz product B084DWG2VQ
amz product B084DWG2VQ -o json
```

You can pass several at once, or full URLs; amz extracts the ASIN from any
Amazon URL shape:

```bash
amz product B084DWG2VQ B07XJ8C8F5 "https://www.amazon.com/dp/B08N5WRWNW"
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
| `reviews`, `questions` | what the page says exists, not what amz holds |
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
amz product B084DWG2VQ -o json --fields asin,ranks
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
amz product B084DWG2VQ --variants -o jsonl
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
| `quick` | 1 | the 374 KB mobile page: identity, price, rating |
| `meta` | 1 | the full 2.2 MB detail page, rails dropped |
| `full` | 1 | the same page with the recommendation rails kept |
| `deep` | 2 plus one per sibling | full, plus each variation sibling's own page and the seller |

`meta` is the default. It drops the rails it read and records that it did, so a
product with twelve strips on the page never looks like a product with none.
`full` costs no extra request, it just keeps them.

`deep` prints its bill and stops for `--yes` above twenty requests. An apparel
listing with 88 siblings is ninety requests and a minute and a half of polite
crawling for one record, and that is worth agreeing to rather than discovering.

```bash
amz product B084DWG2VQ --depth quick     # cheapest useful read
amz product B084DWG2VQ --depth deep --yes
```

## Offers alongside

`--with-offers` attaches the buying options to the product fetch, so you get the
detail page and the offer list in one go:

```bash
amz product B084DWG2VQ --with-offers -o json
```

## The raw page

When you want the bytes amz parsed, not the record:

```bash
amz product B084DWG2VQ --raw > page.html
```

## Just the price

For price-watching, `price` skips everything else:

```bash
amz price B084DWG2VQ B07XJ8C8F5
amz price B084DWG2VQ -m uk
```

## Recommendation rails

`related` pulls the recommendation cards off a detail page, the "customers also
viewed" and "frequently bought together" rails:

```bash
amz related B084DWG2VQ
amz related B084DWG2VQ --kind also-viewed -o jsonl
```

## Dry run

See the URL without fetching, useful when scripting across marketplaces:

```bash
amz product B084DWG2VQ -m de --dry-run
```
