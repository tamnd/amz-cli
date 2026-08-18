---
title: "Crawling at scale"
description: "Seed a frontier, drain it one page at a time, and keep every record in a local SQLite store you can query, search and export."
weight: 60
---

The single-shot commands are enough for most work. When you want to collect a
lot, amz has a frontier and a local store, so a crawl survives restarts and never
loses what it already fetched.

## The store

The store is a SQLite database, and SQLite is compiled into the binary as pure
Go. There is nothing to install alongside amz and no external process to shell
out to, so `db`, `crawl`, `query`, `find`, `graph`, `series`, `lookup` and
`export` work on a fresh machine with nothing on it but this binary.

```bash
amz db path                # where the database lives
amz db stats               # row counts per table
amz db vacuum              # compact
amz db reset               # delete the file
```

The default path is under your XDG data dir, and `--data-dir` moves the whole
thing, which is the easy way to keep one store per project.

### The tables

`amz db stats` lists them, and they are the shape of the data rather than the
shape of the pages:

```
product        price          rank           chart_entry
edge           review         qa             offer
category       brand          seller         author
queue
```

`product` keeps the columns worth filtering on typed out (`asin`, `title`,
`brand`, `rating`, `ratings_count`, `availability`) plus the whole record in a
`json` column, so nothing is lost to the schema. It also keeps `ops`, the surface
and depth that produced the row, which is what lets a light read merge with a
deep one without the light read looking like a deletion.

`price` and `rank` are observation tables, one row per reading with an
`observed_at`, which is what makes `amz series` a history rather than a snapshot.
`edge` is the graph: a subject, a predicate, an object, and the region it was
read from.

### Querying it

`amz query` runs read-only SQL and prints JSON rows. SQLite's JSON functions
reach anything the typed columns do not:

```bash
amz query "select asin, title, rating from product order by ratings_count desc limit 10"

amz query "select json_extract(json, '$.brand.name') brand,
                  count(*) n,
                  avg(json_extract(json, '$.offer.price.value')) p
           from product group by brand order by n desc limit 20"
```

The provenance is queryable the same way, which is the point of putting the
envelope in the record rather than in a log:

```bash
amz query "select json_extract(json, '$.envelope.via.price') region, count(*)
           from product group by region"
```

`amz find` is full text search over the same store with no network at all, and
`amz lookup` reads one record back by ASIN or `amz:` URI.

## Seeding the frontier

`amz seed` pushes work onto the queue. Give it ASINs and URLs as arguments or a
file:

```bash
amz seed B075F5X8BR B07XJ8C8F5
amz seed --file asins.txt              # one ASIN/URL per line
cat asins.txt | amz seed --file -      # from stdin
```

Pick what to fetch for each seed with `--entity`, and order the queue with
`--priority`:

```bash
amz seed --file asins.txt --entity reviews --priority 10
```

`search --enqueue` is the other way in: it seeds the frontier with every result
of a search instead of printing them.

```bash
amz search "mechanical keyboard" --enqueue -n 200
```

`amz crawl` can also seed itself, which is usually simpler than piping:

```bash
amz crawl --chart bestsellers --category electronics --limit 100
amz crawl --asin B075F5X8BR --asin B07XJ8C8F5
```

## Draining it

`amz crawl` pulls items off the queue and writes the resulting records into the
store, one page at a time:

```bash
amz crawl                            # drain everything
amz crawl --kinds product,reviews    # only these entity kinds
amz crawl --depth full               # read more per page, as `amz product --depth`
amz crawl --dry-run                  # print the plan and read nothing
```

A crawl is polite by construction: it shares the rate limiter, the robots check
and the retry backoff with every other command, and there is no worker pool to
turn up. When a page hits the bot wall the item goes back to the queue with a
backoff instead of failing the run, so a crawl rides out a temporary block and
keeps its place. `--max-attempts` parks an item that keeps failing, so one bad
URL cannot stop a crawl from finishing.

`--follow-rails` adds the recommendation strips on each detail page to the
frontier. It is the only free source of new ASINs in the tool, since a related
product costs nothing extra to notice on a page already being read. Sponsored
cards are left out unless you pass `--include-sponsored`.

Note that this grows the frontier while it drains it, which is what you want for
discovery and is not what you want if you expected the run to end. Price it with
`--dry-run` first.

## Getting it back out

```bash
amz export -o jsonl > store.jsonl     # every record, one per line
amz export --format turtle > store.ttl
amz graph B075F5X8BR --depth 2        # walk outward from one node
amz series B075F5X8BR                 # price and rank over time
```

## A full pipeline

Collect a category's bestsellers, fetch a full record for each, follow the rails
one hop, and read the result back with SQL:

```bash
amz crawl --chart bestsellers --category electronics --limit 100 \
          --depth full --follow-rails

amz query "select json_extract(json, '$.brand.name') brand,
                  count(*) n,
                  round(avg(json_extract(json, '$.offer.price.value'))) avg_price
           from product
           group by brand having n > 2 order by n desc limit 20"
```
