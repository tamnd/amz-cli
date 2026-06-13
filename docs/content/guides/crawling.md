---
title: "Crawling at scale"
description: "Seed a queue, drain it with bounded workers, and store every record in a local DuckDB database."
weight: 60
---

The single-shot commands are enough for most work. When you want to collect a
lot, amz has a queue and an optional local store so a crawl survives restarts
and never loses what it already fetched.

## The store

The store is a DuckDB database amz drives by shelling out to the `duckdb`
binary, never through cgo. It is optional: install `duckdb` and the `db` and
`crawl` commands light up; leave it out and every fetch command still works.

```bash
amz db path                # where the database lives
amz db stats               # row counts per table
amz db query "select asin, data->>'price' price from products order by price desc limit 10"
amz db vacuum              # compact
amz db reset               # delete the file
```

Each surface has its own table (products, reviews, qa, offers, bestsellers,
categories, brands, sellers, authors) plus the queue. Every table keeps a few
key columns typed for fast filtering and the full record in a `data` JSON
column, so any field is reachable with DuckDB's JSON arrow: `data->>'brand'`,
`data->>'rating'`, and so on.

## Seeding the queue

`amz seed` pushes work onto the queue. Give it ASINs and URLs as arguments or a
file:

```bash
amz seed B084DWG2VQ B07XJ8C8F5
amz seed --file asins.txt              # one ASIN/URL per line
cat asins.txt | amz seed --file -      # from stdin
```

Pick what to fetch for each seed with `--entity`, and order the queue with
`--priority`:

```bash
amz seed --file asins.txt --entity reviews --priority 10
```

`search --enqueue` is the other way in: it seeds the queue with every result of
a search.

```bash
amz search "mechanical keyboard" --enqueue -n 200
```

## Draining the queue

`amz crawl` pulls items off the queue and writes the resulting records into the
store, with bounded concurrency from the global `--workers`:

```bash
amz crawl                  # drain everything
amz crawl --kinds product,reviews   # only these entity kinds
amz crawl -j 4             # four workers
```

A crawl is polite by construction: it shares the rate limiter and retry/backoff
with every other command. When a page hits the bot wall, that item goes back to
the queue with a short backoff instead of failing the run, so the crawl rides
out a temporary block and keeps its place.

## A full pipeline

Collect a category's bestsellers, fetch every product, and read the result back
with SQL:

```bash
amz bestsellers electronics -n 100 -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | amz seed --file -
amz crawl
amz db query "select data->>'brand' brand, count(*) n,
                     avg((data->>'price')::double) p
              from products group by brand order by n desc limit 20"
```
