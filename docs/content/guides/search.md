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

Each result is a `Card`: `position`, `rank`, `asin`, `title`, `price`,
`list_price`, `rating`, `ratings_count`, `image`, `badge`, `prime`,
`bought_past_month`, `delivery`, `sponsored`, `kind`, `url`, and an `envelope`
naming the response the row was read off. `list_price` carries the struck through
price when the card is discounted, `badge` holds the "Amazon's Choice" and "Best
Seller" style tags, and `prime` flags Prime eligibility.

Prices are objects: each keeps the string Amazon printed beside the parse of it,
so `price.value` is the number and `price.display` is what the page said.

Sponsored placements are dropped by default and counted, so a run tells you what
it left out:

```
amz: 3 sponsored placements left out. pass --include-sponsored to keep them
```

`--include-sponsored` keeps them, and they arrive flagged rather than mixed in.
The `sponsored` field is never omitted from the JSON, because an advertisement
and an organic result are different data and a consumer that cannot tell them
apart is holding a corrupted dataset without knowing it.

## Refinements

| Flag | Effect |
| --- | --- |
| `--sort` | `featured` (default), `price-asc`, `price-desc`, `review`, `newest`, `bestselling`, or a raw Amazon sort value |
| `--price` | a band in major units: `50-150`, `50-`, `-150` |
| `--stars` | minimum star rating, resolved against the sidebar |
| `--brand` | brand name or id |
| `--seller` | seller name or merchant id |
| `--condition` | `New`, `Used` or `Renewed` |
| `-d`, `--department` | limit to a department, as listed by `--list-departments` |
| `--refine` | any group at all, repeatable: `--refine p_123=213704,111070` |
| `--page`, `--max-pages` | where the walk starts and where it stops |

```bash
amz search "mechanical keyboard" \
  --price 50-150 --stars 4 --sort review -o table
```

`--prime` is gone. It is still a registered flag so that passing it says why
rather than being an unknown flag, and what it says is an exit 2:

```console
$ amz search "mechanical keyboard" --prime
amz: --prime is gone in v0.3.0. no capture taken on 2026-08-17 offered the p_85 group at all, and the id v0.2.1 sent (2470955011) would have been dropped in silence.
run `amz refine <query>` to see whether this query offers a prime filter, then pass it with --refine
```

The id v0.2.1 sent is per marketplace, and a query that does not offer the group
gets an unfiltered page rather than an error, which is the failure mode the whole
refinement layer exists to prevent. `amz offers --prime` is a different thing and
still works, because that one filters records amz already holds.

### The vocabulary is read, not compiled in

Only six refinement codes mean the same thing on every search, and amz knows only
those six without looking. Everything else is per query: one search for keyboards
on 2026-08-17 offered thirty-three groups, and `p_n_g-1003532609111` is "Key
Count" on that search and does not exist on a search for shoes.

So the sidebar is the source. `amz refine` prints the whole vocabulary a query
offers, with the code, the human label, the scope, and every value with its id:

```bash
amz refine "mechanical keyboard"
```

```json
{"code":"p_123","label":"Brands","scope":"global","values":[{"id":"213704","label":"Logitech"},{"id":"220854","label":"Razer"}]}
{"code":"p_n_condition-type","label":"Condition","scope":"named","values":[{"id":"2224371011","label":"New"}]}
```

`--brand Logitech` and `--brand 213704` both work, and `-v` prints the id a name
resolved to, so a script can pin the id and skip the lookup next time.

### A refinement Amazon dropped is an error

Amazon does not reject an `rh` group it does not honour. It drops the term and
serves the unfiltered result set with a 200 and a full grid. So amz checks the
sidebar of the page that came back and fails when what it asked for is not marked
as applied:

```
amazon did not apply the refinement: sent p_n_cpf_labels:121136630011 and the page came back without it applied, so these results are not filtered by it
```

The alternative is handing back ten thousand unfiltered rows labelled as a
filtered search, with nothing downstream able to tell.

## Getting past 306

Amazon serves at most 306 results over 20 pages for any query, whatever total it
advertises. That is not a pagination problem and no amount of paging opens it.
`amz search` says so when it hits the wall:

```
amz: amazon serves at most 306 results per search over 20 pages, whatever the corpus says
amz: run `amz why search-depth`, narrow with --refine, or partition with --all to reach the rest
```

`--all` is the way through. It partitions the query on a refinement group, runs
one search per value, and unions the cells on ASIN, so each cell gets its own 306
and a cell that still hits the ceiling is split again.

Price it before you run it:

```bash
amz search "usb-c hub" --all --dry-run
```

Measured on 2026-08-18, `amz search "usb-c hub" --all` partitioned on `p_123`
(Brands) into 68 cells and returned 1,508 unique results, against the 306 a plain
search can reach.

A partitioned run is honest about its own holes, and there are always some:

```
amz: partitioned on p_123 (Brands), 68 cells, 1508 unique results, 298 repeat sightings
amz: 19 cells still hit the ceiling and were not split further, so this union is incomplete: ...
amz: 4 cells were offered in the sidebar and then served unfiltered, so they read nothing: ...
```

The first list is where the union is known to be short, and `--partition-depth`
raises how many times a capped cell may be split again. The second is Amazon
offering a filter in its own sidebar and then ignoring it, which happens on
values like "Any Feature" under Sustainability Features. One such cell does not
end the run; it is named and the other sixty-seven are still read.

Every card carries the cells it was found in, so a result that turns up under
three brands is one record that says so rather than three records that look like
three products.

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
amz search "mechanical keyboard" -n 25 --fields asin -o csv --no-header \
  | xargs -I{} amz product {} -o jsonl > keyboards.jsonl
```

Count the pages and the ceiling without keeping the cards, which is the cheap way
to size a query before committing to it:

```bash
amz search "mechanical keyboard" --pages -o json
```
