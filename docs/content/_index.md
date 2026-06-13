---
title: "amz"
description: "A delightful command line for Amazon.com. Crawl products, search, reviews, Q&A, offers, charts, categories, brands, sellers, authors, and deals, and turn each one into rich, structured data, all from one binary."
heroTitle: "Amazon.com, from the command line"
heroLead: "amz is a single pure-Go binary that puts every public Amazon surface behind a tool that feels like curl. Look up a product, search the catalog, stream the review corpus, list the buying options, read the bestseller charts, and walk a category tree, then render it as a table, JSON, JSONL, CSV, or TSV."
heroPrimaryURL: "/getting-started/quick-start/"
heroPrimaryText: "Get started"
---

Pulling structured data out of Amazon usually means a pile of brittle scrapers,
one per page type, each breaking the next time a selector moves. amz puts all of
it behind one tool with sensible defaults, real output formats, and pipelines
that compose.

```bash
amz product B084DWG2VQ                 # one product, fully normalized
amz search "mechanical keyboard" -o jsonl
amz reviews B084DWG2VQ --stars 1 -o csv
amz bestsellers electronics           # the live top-100 chart
```

It reads the public pages on `amazon.com` over plain HTTPS, so there is nothing
to sign up for to get started. The binary is pure Go with no runtime
dependencies. DuckDB is optional and only used for the local store and crawl
queue; without it, amz still fetches and prints everything.

## What you can do with it

- **Look up products.** `amz product` fetches a detail page and normalizes
  title, brand, price, rating, availability, feature bullets, technical
  specifications, images, breadcrumb, variations, and sales rank into one
  record, reading both the JSON-LD block and the HTML.
- **Search the catalog.** Stream result cards with refinements for sort, price
  range, rating, Prime, brand, and department, page after page.
- **Read the social proof.** Stream the full review corpus with star, verified,
  and image filters, and pull the classic question-and-answer pairs.
- **Compare offers.** List every buying option for an ASIN: seller, condition,
  price, and shipping.
- **Walk the charts and trees.** Bestsellers, new releases, movers and shakers,
  most wished for, and most gifted, plus category browse nodes, brand
  storefronts, seller profiles, author pages, and today's deals.

## Where to go next

- New here? Start with the [introduction](/getting-started/introduction/) for
  the mental model, then the [quick start](/getting-started/quick-start/).
- Want to install it? See [installation](/getting-started/installation/).
- Looking for a specific task? The [guides](/guides/) cover products, search,
  reviews and Q&A, offers, charts, and crawling at scale.
- Need every flag? The [CLI reference](/reference/cli/) is the full surface.
