---
title: "Introduction"
description: "What amz is, the Amazon surfaces it reads, and the mental model behind turning a page into a record."
weight: 10
---

amz is a single command-line tool that reads the public pages of Amazon's
storefronts and turns each one into a clean, structured record. Where a browser
shows you a product page, amz gives you the same product as JSON: title, brand,
price, list price and savings, coupons, rating and its histogram, availability,
feature bullets, technical detail, full-resolution images and videos,
breadcrumb, variations, who sells it and where it ships from, and every Best
Sellers Rank, all named and typed. The
[data model](/reference/data-model/) names every field of every record.

## The mental model

Every Amazon page type is a **surface**. amz has one command per surface, and
each command does the same four things:

1. **Build the URL** for the surface in the marketplace you picked (`--marketplace`,
   default `us`). You pass the natural identifier, an ASIN, a search query, a
   browse-node id, a seller id, and amz constructs the canonical URL.
2. **Ask robots.txt.** The marketplace's live `robots.txt` is fetched, parsed and
   enforced before the request goes out, cached for 24 hours with the time it was
   fetched. Nothing about it is hardcoded, and `amz robots check <url>` prints
   the rule that decided any URL.
3. **Fetch politely.** Requests go out as `amz-cli/<version>`, naming the tool
   and its repo, with three headers and no disguise, one page at a time, a
   minimum delay between them (`--rate`, floor 1s), retry-with-backoff on the
   rate-limit responses, and on-disk caching so a repeated lookup is free.
   There is no session, no cookie jar and no concurrency.
4. **Read the page the way the page labels itself.** Fields come off Amazon's own
   `data-feature-name`, `data-component-type`, `data-csa-c-item-id` and
   `data-testid` regions and the payloads it ships, in that order, and every
   field records which rung of that ladder produced it. `amz extraction` prints
   the ladder, and what is on the page that nothing reads yet.

## Every record says where it came from

A record carries an `envelope` beside its fields. It names the surfaces the
record was read from and when, the region each field came out of (`via`), and
what the page did not give up (`missed`), with a count and an `amz why <topic>`
to run.

That last part is the design. Amazon puts the full review corpus, the all-offers
panel and a brand's product grid behind a login or behind javascript, and amz
carries no session and runs no javascript. So those commands return what the
public page states and say what is missing, rather than returning an empty list
that reads like an answer.

## The surfaces

| Surface | Command | Identifier |
| --- | --- | --- |
| Product detail | `product` | ASIN or URL |
| Catalog search | `search`, `refine` | query |
| Reviews | `reviews` | ASIN |
| Questions & answers | `qa` | ASIN |
| Buying options | `offers` | ASIN |
| Variation family | `variants` | ASIN |
| Bestseller charts | `bestsellers`, `new-releases`, `movers`, `wished`, `gifted` | category (optional) |
| Browse node | `category`, `tree` | node id or URL |
| Brand storefront | `brand` | name, slug or URL |
| Seller profile | `seller` | seller id or URL |
| Author page | `author` | slug or URL |
| Today's deals | `deals` | none |
| Recommendation cards | `related` | ASIN |

`amz surfaces` prints the same list as the tool knows it, with the path each one
lives at, what `robots.txt` says about it, and the date it was last measured.

## Two ways in

amz reads two tiers of access, and you choose per run:

- **Public HTML** (default). No setup. Reads the same pages a logged-out browser
  sees.
- **PA-API** (`--api`). Uses Amazon's official Product Advertising API 5.0 when
  you have credentials, signed locally with SigV4. The output schema is the
  same, so a script does not care which tier produced the record.

## Output is the point

Every command streams records through the same renderer, so `-o table` for
reading, `-o json`/`-o jsonl` for piping, `-o csv`/`-o tsv` for a spreadsheet,
`-o url` for just the links. Add `--fields` to project columns and `--template`
for a custom line. The next page, [quick start](/getting-started/quick-start/),
runs the core loop end to end.
