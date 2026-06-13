---
title: "Offers and charts"
description: "List every buying option for an ASIN, and read the five bestseller charts and the deals grid."
weight: 40
---

## Offers

`amz offers` lists every buying option on the offer-listing page: the Buy Box
plus all the competing sellers and conditions.

```bash
amz offers B084DWG2VQ
amz offers B084DWG2VQ -o jsonl
```

Each `Offer` carries `price`, `currency`, `shipping`, `condition`,
`seller_name`, `seller_id`, `seller_rating`, `fulfilled_by`, `delivery`, and
`is_buybox`. Narrow by condition:

```bash
amz offers B084DWG2VQ --condition used
amz offers B084DWG2VQ --prime          # Prime / FBA-fulfilled only
```

Find the cheapest option:

```bash
amz offers B084DWG2VQ -o jsonl \
  | sort -t: -k2 -n            # or pipe through jq for a clean min
```

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
`ratings_count`, and the `list_type`/`category`/`node_id` it came from.

## Deals

`amz deals` streams today's deals grid:

```bash
amz deals
amz deals --min-discount 30           # 30% off or better
amz deals --department electronics -o jsonl
```

Each `Deal` carries `deal_price`, `list_price`, `discount_pct`, and `badge`
(for example "Lightning Deal").

## Compose

Pull the top 25 bestsellers and enrich each into a full product record:

```bash
amz bestsellers electronics -n 25 -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | xargs -I{} amz product {} -o jsonl > top25.jsonl
```
