---
title: "Offers and charts"
description: "List every buying option for an ASIN, and read the five bestseller charts and the deals grid."
weight: 40
---

## Offers

`amz offers` returns the buy box and the count of the offers behind it.

```bash
amz offers B075F5X8BR
amz offers B075F5X8BR -o jsonl
```

Each `Offer` carries `price`, `currency`, `condition`, `seller_name`,
`seller_id`, `fulfilled_by`, `delivery`, and `is_buybox`. Narrow by condition:

```bash
amz offers B075F5X8BR --condition used
amz offers B075F5X8BR --prime          # Amazon-fulfilled only
```

### Why this is a count and not a list

The full seller list is not readable. Two separate things stop it, and both are
worth knowing before you go looking for a flag:

`/gp/offer-listing/` is disallowed by `robots.txt`, and amz asks the live file
before every request rather than guessing:

```console
$ amz robots check /gp/offer-listing/B075F5X8BR
disallowed  https://www.amazon.com/gp/offer-listing/B075F5X8BR  Disallow: /gp/offer-listing/  offers s18
```

That path also 301s to the detail page now, so overriding the rule buys a
redirect rather than an offer list. Amazon is saying the offers are on the
product page, and they are: the buy box, the seller, the fulfiller, the condition
and the delivery promise all read cleanly from it.

What is not on it is the other sellers' rows. Those are drawn by JavaScript, and
the endpoint the page's own script calls answers 404 to a direct request in both
of the forms it takes, measured on 2026-08-17:

```
/gp/aod/ajax?asin=<asin>&pc=dp                        404
/gp/aod/ajax/ref=dp_aod_NEW_mbc?asin=<asin>&pc=dp     404
```

What the detail page does state is how many offers there are. So the record says
`other_offers.total_count` with `complete: false` beside the one offer it holds:

```json
"other_offers": { "loaded": 1, "total_count": 2, "complete": false }
```

A count with `complete: false` is a smaller answer than a list, and it is an
honest one. `amz why offers` has the measurement and the date.

## The five charts

Amazon publishes five ranked lists, and amz has a command for each:

| Command | Chart |
| --- | --- |
| `bestsellers` | top sellers |
| `new-releases` | newest releases |
| `movers` | biggest 24-hour rank movers |
| `wished` | most wished for |
| `gifted` | most gifted |

They share a shape. Run one for the whole store, or scope it to a category by
name or browse-node id:

```bash
amz bestsellers                       # the store-wide top 100
amz bestsellers electronics           # by category name
amz bestsellers --node 172282         # by browse-node id
amz new-releases electronics -n 10
amz movers -m uk -o jsonl
```

Each `BestsellerEntry` carries `rank`, `asin`, `title`, `price`, `rating`,
`ratings_count`, the `list_type`, `category` and `node_id` it came from, and an
`envelope` naming the response it was read off.

Ranks come from the page and are never counted. Page one covers ranks 1 to 50
while drawing 30 tiles, so numbering entries as they arrive would label rank 51
as rank 31 and keep doing it for the length of the chart. An entry Amazon listed
without drawing a tile for it comes back with `rank_only` set, its rank and its
ASIN, which is a rank amz can report rather than an item it has to drop.

## Deals

`amz deals` streams today's deals grid:

```bash
amz deals
amz deals --min-discount 30           # 30% off or better
amz deals --department electronics -o jsonl
```

Each `Deal` carries `asin`, `deal_id`, `title`, `deal_price`, `list_price`,
`list_label`, `discount_pct`, `badge` (for example "Lightning Deal"), `ends_soon`,
`shelf`, and `position`.

`list_label` travels with `list_price` because Amazon writes "List", "List Price"
and "Typical" for it, and a typical price is a computed average rather than a
manufacturer's list price. Keeping the number without the label would turn three
different claims into one. `deal_id` is the promotion and it is gone when the
promotion ends, while the ASIN stays.

## Compose

Pull the top 25 bestsellers and enrich each into a full product record:

```bash
amz bestsellers electronics -n 25 --fields asin -o csv --no-header \
  | xargs -I{} amz product {} -o jsonl > top25.jsonl
```
