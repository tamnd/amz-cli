---
title: "amz"
description: "A delightful command line for Amazon.com. Crawl products, search, reviews, Q&A, offers, charts, categories, brands, sellers, authors, and deals, and turn each one into rich, structured data, all from one binary."
heroTitle: "Amazon.com, from the command line"
heroLead: "amz is a single pure-Go binary that puts every public Amazon surface behind a tool that feels like curl. Look up a product, search the catalog, read the reviews a detail page carries, list the buying options, read the bestseller charts, and walk a category tree, then render it as a table, JSON, JSONL, CSV, or TSV. Every record says which page it came from and what it could not find."
heroPrimaryURL: "/getting-started/quick-start/"
heroPrimaryText: "Get started"
---

Pulling structured data out of Amazon usually means a pile of brittle scrapers,
one per page type, each breaking the next time a selector moves. amz puts all of
it behind one tool with sensible defaults, real output formats, and pipelines
that compose.

```bash
amz product B075F5X8BR                 # one product, fully normalized
amz search "mechanical keyboard" -o jsonl
amz reviews B075F5X8BR --stars 1 -o csv
amz bestsellers electronics            # the live top-100 chart
```

It reads the public pages on `amazon.com` over plain HTTPS, so there is nothing
to sign up for to get started. The binary is pure Go with no runtime
dependencies and nothing to install alongside it: the local store is SQLite
through a pure Go implementation, so there is no external binary that can be
missing.

`robots.txt` is fetched from the marketplace and enforced before every request,
requests are paced at 3s with a floor no flag can lower, and no page is ever
requested behind a login. Where that means a command returns less than you might
hope, it says so on the record rather than filling the gap in.

## What you can do with it

- **Look up products.** `amz product` fetches a detail page and normalizes the
  title, the brand and its storefront, the price and what it was struck from,
  the rating and its histogram, availability, the buy box and who wins it,
  feature bullets, technical detail, images and their variants, the breadcrumb,
  the variation matrix, and every Best Sellers Rank into one record. Each field
  records which of Amazon's own named regions it came out of.
- **Search the catalog.** Stream result cards with the refinement vocabulary
  read off the page itself, real pagination that stops where Amazon says the
  results do, and `--all` to partition a query and get past the 306 result
  ceiling.
- **Read the social proof.** The reviews and the rating histogram the detail
  page carries, and the answered question count with any inline pairs. The full
  corpus is behind a login and `amz` says so instead of pretending otherwise.
- **Compare offers.** The buy box, who sells it, who ships it, and how many
  offers sit behind the panel Amazon builds with javascript.
- **Walk the charts and trees.** Bestsellers, new releases, movers and shakers,
  most wished for, and most gifted, plus category browse nodes, brand
  storefronts, seller profiles, author pages, and today's deals.
- **Keep what you read.** A resumable crawl into a local SQLite store, a graph
  of sixteen predicates over what it recorded, SQL and full text search with no
  network, and an export to JSONL or RDF.
- **Hand it to a model.** `amz serve` puts the read commands behind HTTP and
  `amz mcp` puts the same registry on stdio as Model Context Protocol.

## Where to go next

- New here? Start with the [introduction](/getting-started/introduction/) for
  the mental model, then the [quick start](/getting-started/quick-start/).
- Want to install it? See [installation](/getting-started/installation/).
- Looking for a specific task? The [guides](/guides/) cover products, search,
  reviews and Q&A, offers, charts, crawling at scale, and a page of
  [recipes](/guides/recipes/).
- Need the exact shape of a record? The [data model](/reference/data-model/)
  names every field of every surface.
- Need every flag? The [CLI reference](/reference/cli/) is the full surface.
- Getting less than you expected? [Troubleshooting](/reference/troubleshooting/)
  and `amz why` explain each case, with the measurement and the date behind it.
