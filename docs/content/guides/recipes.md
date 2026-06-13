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
amz price B084DWG2VQ -o jsonl \
  | jq -r '[now|todate, .asin, .price, .currency] | @csv' \
  >> price_log.csv
```

Drop that line in a cron job and you have a price history with no database. To
watch a basket, loop a file of ASINs:

```bash
while read asin; do amz price "$asin" -o jsonl; done < watchlist.txt \
  | jq -r '[now|todate, .asin, .price] | @csv' >> basket_log.csv
```

## Find the cheapest offer for an ASIN

The Buy Box is not always the cheapest. List every offer and sort:

```bash
amz offers B084DWG2VQ -o jsonl \
  | jq -s 'sort_by(.price) | .[0] | {price, condition, seller_name, is_buybox}'
```

Or only new, Prime-fulfilled options:

```bash
amz offers B084DWG2VQ --condition new --prime -o jsonl | jq -s 'min_by(.price)'
```

## Enrich a chart into full product records

Charts give you ASINs and a thumbnail of data. Fan each one out into a full
product record:

```bash
amz bestsellers electronics -n 25 -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | xargs -I{} amz product {} -o jsonl > top25.jsonl
```

Now ask questions of the file. Average discount among the top 25:

```bash
jq -s 'map(.savings_pct // 0) | add / length' top25.jsonl
```

The brands that appear most:

```bash
jq -r '.brand' top25.jsonl | sort | uniq -c | sort -rn
```

## Mine the reviews of a product

Pull the corpus and skim sentiment without reading a word:

```bash
echo "1-star: $(amz reviews B084DWG2VQ --stars 1 -o jsonl | wc -l)"
echo "5-star: $(amz reviews B084DWG2VQ --stars 5 -o jsonl | wc -l)"
```

The most-helpful complaints, title and vote count:

```bash
amz reviews B084DWG2VQ --stars 1 --sort helpful -n 20 -o jsonl \
  | jq -r '"\(.helpful_votes)\t\(.title)"'
```

Reviews that mention a keyword:

```bash
amz reviews B084DWG2VQ -o jsonl | jq -r 'select(.text | test("battery"; "i")) | .title'
```

## Compare two products side by side

```bash
for a in B084DWG2VQ B09B8V1LZ3; do amz product "$a" -o jsonl; done \
  | jq -r '[.asin, .price, .rating, .ratings_count, .rank] | @tsv' \
  | column -t
```

## Scan a search for the best-rated value

Search with refinements, then pick the highest-rated card under a price:

```bash
amz search "mechanical keyboard" --min-rating 4 --prime -n 100 -o jsonl \
  | jq -s 'map(select(.price < 120)) | sort_by(-.rating) | .[0:5]'
```

## Walk a brand's catalog

Turn a brand's featured ASINs into full records:

```bash
amz brand anker --featured -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | xargs -I{} amz product {} -o jsonl > anker.jsonl
```

## Build a dataset with the local store

For anything beyond a one-shot, let the queue and DuckDB carry the work. Seed a
category's bestsellers, drain the queue, then query with SQL. See
[crawling at scale](/guides/crawling/) for the full treatment.

```bash
amz bestsellers electronics -n 100 -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | amz seed --file -
amz crawl
amz db query "select data->>'brand' brand, count(*) n,
                     round(avg((data->>'price')::double), 2) avg_price
              from products group by brand order by n desc limit 20"
```

## Cross-marketplace price gap

The same ASIN, priced in two storefronts:

```bash
for m in us uk de; do
  amz price B084DWG2VQ -m "$m" -o jsonl
done | jq -r '[.marketplace // "?", .price, .currency] | @tsv'
```

## Dry-run before a big crawl

See exactly which URLs a run would hit, across marketplaces, without fetching:

```bash
amz product B084DWG2VQ -m jp --dry-run
amz bestsellers electronics -m de --dry-run
```

## Keep iterating for free

Every successful fetch is cached, so once you have pulled a page you can refine
the shape of the output as much as you like without touching the network:

```bash
amz product B084DWG2VQ -o json                       # first run hits the network
amz product B084DWG2VQ --fields asin,price,ranks     # served from cache
amz product B084DWG2VQ --template '{{.title}} is #{{.rank}}'
```
