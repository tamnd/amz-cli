---
title: "Products"
description: "Turn an ASIN or URL into a fully normalized product record, with variations, specs, and rank."
weight: 10
---

`amz product` is the workhorse. It fetches a detail page and normalizes it into
one record that carries everything the page exposes, reading the JSON-LD block
first and filling gaps from the HTML.

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
| `title`, `brand`, `brand_id` | identity |
| `price`, `currency`, `list_price` | current and struck-through price |
| `savings`, `savings_pct` | the discount off the list price, in money and percent |
| `coupon` | the clip-coupon line when one is offered |
| `rating`, `ratings_count`, `reviews_count`, `answered_qs` | social proof |
| `bought_past_month` | the "N+ bought in past month" line |
| `availability`, `in_stock` | the stock line as shown, and whether it means buyable |
| `description`, `bullet_points` | marketing copy |
| `specs` | the technical-details table, as key/value pairs |
| `images` | full-resolution image URLs, one per distinct photo |
| `videos` | inline product video URLs |
| `category_path`, `browse_node_ids` | the breadcrumb and its node ids |
| `seller_id`, `seller_name`, `sold_by`, `fulfilled_by`, `ships_from` | the merchant and fulfillment |
| `variant_asins`, `similar_asins` | other choices on the page |
| `rank`, `rank_category` | the overall Best Sellers Rank |
| `ranks` | every Best Sellers Rank, overall and per subcategory |

Fields the page did not carry are omitted, so an empty value always means
Amazon did not show it.

### Images and videos

Amazon serves the same photo at dozens of sizes and from several CDN hosts. amz
strips the size modifier from every image URL and pins one canonical host, so
`images` holds one full-resolution URL per distinct photo, with the thumbnails,
tracking pixels, and sprites removed. The same canonicalization runs on every
surface that carries an image (search cards, reviews, brand logos, author
photos), so an image URL means the same thing everywhere.

### Ranks

A product is usually ranked once overall and again in one or more subcategories.
`rank` and `rank_category` keep the overall rank flat for quick filtering, while
`ranks` lists them all:

```bash
amz product B084DWG2VQ -o json --fields asin,ranks
```

## Variations

Add `--variants` to expand the variation family into a record per child ASIN:

```bash
amz product B084DWG2VQ --variants -o jsonl
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
