---
title: "Search"
description: "Stream catalog result cards with refinements for sort, price, rating, Prime, brand, and department."
weight: 20
---

`amz search` queries the catalog and streams one record per result card, paging
as far as your `--limit` asks.

## A query

```bash
amz search "mechanical keyboard"
amz search "mechanical keyboard" -n 20 -o jsonl
```

Multi-word queries can be quoted or passed as separate arguments; amz joins
them.

## The card

Each result is a `Card`: `position`, `asin`, `title`, `price`, `currency`,
`rating`, `ratings_count`, `image`, `sponsored`, and `url`. Sponsored placements
are flagged, not hidden, so you can keep or drop them yourself:

```bash
amz search "usb c cable" -o jsonl | grep -v '"sponsored":true'
```

## Refinements

| Flag | Effect |
| --- | --- |
| `--sort` | `relevance` (default), `price-asc`, `price-desc`, `review`, `newest` |
| `--min-price`, `--max-price` | price band (whole currency units) |
| `--min-rating` | minimum star rating, 1 to 4 |
| `--prime` | Prime-eligible only |
| `--brand` | filter by brand |
| `--department` | limit to a department or search alias |
| `--page` | first result page to fetch |

```bash
amz search "mechanical keyboard" \
  --min-price 50 --max-price 150 --min-rating 4 --prime --sort review -o table
```

## Straight into the queue

`--enqueue` pushes each result into the crawl queue instead of printing it, so a
search becomes the seed for a bulk product crawl:

```bash
amz search "mechanical keyboard" --enqueue -n 100
amz crawl              # drain the queue into the local store
```

See [crawling at scale](/guides/crawling/) for the queue and store.

## Compose

Turn a search into full product records:

```bash
amz search "mechanical keyboard" -n 25 -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | xargs -I{} amz product {} -o jsonl > keyboards.jsonl
```
