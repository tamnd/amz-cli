---
title: "Quick start"
description: "From an empty terminal to a fully structured product record, in a handful of commands."
weight: 30
---

This walks the core loop: turn an ASIN into a normalized product, search the
catalog, and read the social proof. Every command here hits live Amazon and
finishes in a second or two.

## 1. Look up a product

```bash
amz product B084DWG2VQ
```

A product page becomes one record. On a terminal you get an aligned table; pipe
it and you get JSONL. Ask for JSON to see the full shape:

```bash
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
  "rating_count": 284512,
  "availability": "In Stock",
  "sales_rank": 3
}
```

You can pass a full URL instead of an ASIN, and amz pulls the ASIN out of it:

```bash
amz product "https://www.amazon.com/dp/B084DWG2VQ/ref=sr_1_1"
```

## 2. Search the catalog

```bash
amz search "mechanical keyboard" -n 5
```

Each row is one result card: ASIN, title, price, rating, and whether it is
Prime. Refine and choose your output:

```bash
amz search "mechanical keyboard" --min-price 50 --max-price 150 --prime -o jsonl
amz search "mechanical keyboard" --sort price-asc -o table
```

## 3. Read the reviews and questions

```bash
amz reviews B084DWG2VQ -n 10
amz reviews B084DWG2VQ --stars 1 --verified -o csv
amz qa B084DWG2VQ
```

`reviews` streams the corpus page by page with star, verified, and image
filters; `qa` pulls the classic question-and-answer pairs.

## 4. Compare offers and read the charts

```bash
amz offers B084DWG2VQ                 # every buying option for an ASIN
amz bestsellers electronics -n 10     # the live top sellers in a category
amz deals --min-discount 30           # today's deals, 30% off or better
```

## 5. Pick a marketplace

Every command takes `-m` to switch storefront:

```bash
amz product B084DWG2VQ -m uk          # amazon.co.uk, prices in GBP
amz bestsellers -m de                 # the German top 100
```

See what would be fetched without fetching, handy across marketplaces:

```bash
amz product B084DWG2VQ -m jp --dry-run
```

## 6. Compose

Output that pipes is the point. Pull the ASINs of the top 25 bestsellers and
fetch a full record for each:

```bash
amz bestsellers electronics -n 25 -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | xargs -I{} amz product {} -o jsonl
```

Count one-star reviews:

```bash
amz reviews B084DWG2VQ --stars 1 -o jsonl | wc -l
```

## Where to next

You have the core loop. From here:

- [Products](/guides/products/) goes deep on the product record and variations.
- [Search](/guides/search/) covers every refinement.
- [Reviews and Q&A](/guides/reviews-and-qa/) covers the social-proof surfaces.
- [Crawling at scale](/guides/crawling/) covers the queue and the local store.
- The [CLI reference](/reference/cli/) lists every command and flag.
