---
title: "Recipes"
description: "End-to-end pipelines that combine amz commands into real work: price tracking, review mining, competitive scans, and market research."
weight: 70
---

amz is built to compose. Every command streams the same record types through
the same renderer, so the output of one is the input of the next. These recipes
chain them into the work people actually do with Amazon data. They use `jq` for
JSON wrangling, but plain `sed`/`awk` versions are shown where they are simpler.

## Track a price over time

Append a timestamped price row to a CSV on every run, then watch the file:

```bash
amz price B075F5X8BR -o jsonl \
  | jq -r '[now|todate, .asin, .price.value, .price.currency] | @csv' \
  >> price_log.csv
```

Drop that line in a cron job and you have a price history with no database. To
watch a basket, loop a file of ASINs:

```bash
while read asin; do amz price "$asin" -o jsonl; done < watchlist.txt \
  | jq -r '[now|todate, .asin, .price.value] | @csv' >> basket_log.csv
```

## Find the cheapest offer for an ASIN

The Buy Box is not always the cheapest. List every offer and sort:

```bash
amz offers B075F5X8BR -o jsonl \
  | jq -s 'sort_by(.price) | .[0] | {price, condition, seller_name, is_buybox}'
```

Or only new, Prime-fulfilled options:

```bash
amz offers B075F5X8BR --condition new --prime -o jsonl | jq -s 'min_by(.price)'
```

## Enrich a chart into full product records

Charts give you ASINs and a thumbnail of data. Fan each one out into a full
product record:

```bash
amz bestsellers electronics -n 25 --fields asin -o csv --no-header \
  | xargs -I{} amz product {} -o jsonl > top25.jsonl
```

Now ask questions of the file. Average discount among the top 25:

```bash
jq -s 'map(.offer.savings_pct // 0) | add / length' top25.jsonl
```

The brands that appear most:

```bash
jq -r '.brand.name // ""' top25.jsonl | sort | uniq -c | sort -rn
```

## Mine the reviews of a product

The corpus is behind a sign-in and what is public is the histogram plus a medley
of about a dozen reviews. For the whole rating distribution, read the histogram:

```bash
amz product B075F5X8BR -o json | jq '.[0].distribution'
```

For the medley, sorted by how many people found it helpful:

```bash
amz reviews B075F5X8BR -o jsonl \
  | jq -rs 'sort_by(-.helpful_votes) | .[] | "\(.helpful_votes)\t\(.title)"'
```

Reviews that mention a keyword:

```bash
amz reviews B075F5X8BR -o jsonl | jq -r 'select(.text | test("battery"; "i")) | .title'
```

## Compare two products side by side

```bash
for a in B075F5X8BR B09B8V1LZ3; do amz product "$a" -o jsonl; done \
  | jq -r '[.asin, .offer.price.value, .rating, .ratings_count, (.ranks[] | select(.overall) | .rank)] | @tsv' \
  | column -t
```

## Scan a search for the best-rated value

Search with refinements, then pick the highest-rated card under a price:

```bash
amz search "mechanical keyboard" --stars 4 --prime -n 100 -o jsonl \
  | jq -s 'map(select(.price.value < 120)) | sort_by(-.rating) | .[0:5]'
```

## Get more than 306 results for a query

A plain search tops out at 306 results however many Amazon claims. `--all`
partitions the query on a refinement group and unions the cells on ASIN. Price it
first, because it is one search per cell:

```bash
amz search "usb-c hub" --all --dry-run
amz search "usb-c hub" --all -o jsonl > hubs.jsonl
```

Measured on 2026-08-18 that partitioned into 68 cells and returned 1,508 unique
results. The stderr summary names the cells that still hit the ceiling and the
ones Amazon served unfiltered, so you know where the union is short:

```bash
amz search "usb-c hub" --all -o jsonl 2> hubs.log > hubs.jsonl
```

## Walk a brand's catalog

Turn a brand's featured ASINs into full records:

```bash
amz brand anker --featured --fields asin -o csv --no-header \
  | xargs -I{} amz product {} -o jsonl > anker.jsonl
```

## Build a dataset with the local store

For anything beyond a one-shot, let the frontier and the local store carry the
work. Seed a category's bestsellers, drain the queue, then query with SQL. See
[crawling at scale](/guides/crawling/) for the full treatment.

```bash
amz crawl --chart bestsellers --category electronics --limit 100 --depth full
amz query "select json_extract(json, '$.brand.name') brand,
                  count(*) n,
                  round(avg(json_extract(json, '$.offer.price.value'))) avg_price
           from product group by brand order by n desc limit 20"
```

## Cross-marketplace price gap

The same ASIN, priced in two storefronts:

```bash
for m in us uk de; do
  amz price B075F5X8BR -m "$m" -o jsonl
done | jq -r '[.marketplace, .price.value, .price.currency] | @tsv'
```

## Dry-run before a big crawl

See exactly which URLs a run would hit, across marketplaces, without fetching:

```bash
amz product B075F5X8BR -m jp --dry-run
amz bestsellers electronics -m de --dry-run
```

## Keep iterating for free

Every successful fetch is cached, so once you have pulled a page you can refine
the shape of the output as much as you like without touching the network:

```bash
amz product B075F5X8BR -o json                       # first run hits the network
amz product B075F5X8BR --fields asin,price,ranks     # served from cache
amz product B075F5X8BR --template '{{.title}} is #{{.rank}}'
```
