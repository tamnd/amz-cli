---
title: "Introduction"
description: "What amz is, the Amazon surfaces it reads, and the mental model behind turning a page into a record."
weight: 10
---

amz is a single command-line tool that reads the public pages of Amazon's
storefronts and turns each one into a clean, structured record. Where a browser
shows you a product page, amz gives you the same product as JSON: title, brand,
price, list price and savings, coupons, rating and review counts, availability,
feature bullets, technical specifications, full-resolution images and videos,
breadcrumb, variations, the seller and where it ships from, and every Best
Sellers Rank, all named and typed. The
[data model](/reference/data-model/) names every field of every record.

## The mental model

Every Amazon page type is a **surface**. amz has one command per surface, and
each command does the same three things:

1. **Build the URL** for the surface in the marketplace you picked (`--marketplace`,
   default `us`). You pass the natural identifier, an ASIN, a search query, a
   browse-node id, a seller id, and amz constructs the canonical URL.
2. **Fetch politely.** Requests go out with a rotating browser user agent, a
   minimum delay between them (`--rate`), retry-with-backoff on the rate-limit
   responses, and on-disk caching so a repeated lookup is free.
3. **Parse twice.** amz reads the embedded JSON-LD block first, the data Amazon
   itself marks up for machines, then fills any gaps from the HTML with precise
   selectors. The result is one record with no missing fields where the page had
   them.

## The surfaces

| Surface | Command | Identifier |
| --- | --- | --- |
| Product detail | `product` | ASIN or URL |
| Catalog search | `search` | query |
| Reviews | `reviews` | ASIN |
| Questions & answers | `qa` | ASIN |
| Buying options | `offers` | ASIN |
| Bestseller charts | `bestsellers`, `new-releases`, `movers`, `wished`, `gifted` | category (optional) |
| Browse node | `category` | node id or URL |
| Brand storefront | `brand` | slug or URL |
| Seller profile | `seller` | seller id or URL |
| Author page | `author` | slug or URL |
| Today's deals | `deals` | none |
| Recommendation cards | `related` | ASIN |

## Three ways in

amz reads three tiers of access, and you choose per run:

- **Public HTML** (default). No setup. Reads the same pages a logged-out browser
  sees.
- **Cookied** (`--cookies file`). Lends a signed-in session so you see your
  locale, currency, and any pricing tied to an account.
- **PA-API** (`--api`). Uses Amazon's official Product Advertising API 5.0 when
  you have credentials, signed locally with SigV4. The output schema is the
  same, so a script does not care which tier produced the record.

## Output is the point

Every command streams records through the same renderer, so `-o table` for
reading, `-o json`/`-o jsonl` for piping, `-o csv`/`-o tsv` for a spreadsheet,
`-o url` for just the links. Add `--fields` to project columns and `--template`
for a custom line. The next page, [quick start](/getting-started/quick-start/),
runs the core loop end to end.
