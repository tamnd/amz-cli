---
title: "Categories and storefronts"
description: "Walk browse nodes, brand storefronts, seller profiles, and author pages."
weight: 50
---

Four surfaces describe who sells what and where it sits in the tree.

## Categories (browse nodes)

`amz category` resolves a browse node by id or URL into its name, the other
nodes it links, its merchandised shelves, and the ASINs on the landing page:

```bash
amz category 172282                   # by node id
amz category "https://www.amazon.com/b?node=172282"
amz category 172282 --related         # the nodes it links, with their names
amz category 172282 --top -o url      # just the top ASINs as URLs
```

Each `Category` carries `node_id`, `canonical_node`, `name`, `slug`, `related`,
`shelves`, `top_asins`, and `item_count`. Walk outward by feeding the ids in
`related` back in.

There is no parent and no breadcrumb, because a `/b` page publishes neither.
Amazon links a browse page to its children and its siblings with the same markup
and gives no marker for which is which, so the field is called `related` and
carries references rather than pretending to be a tree. Header and footer links
are excluded: eight of the twenty four node links on the Electronics page are
Gift Cards, Sell, Registry and the rest of the site chrome.

The edge upwards comes from a product instead. Each entry in a product's `ranks`
links the node it is ranked in, which is an identifier Amazon stated rather than
a name lifted from a breadcrumb.

## Brand storefronts

`amz brand` reads a brand's storefront from its slug or a `/stores/` URL:

```bash
amz brand anker
amz brand "https://www.amazon.com/stores/Anker/page/..."
amz brand anker --featured -o url     # the featured ASINs
```

A `Brand` carries `slug`, `page_id`, `name`, `description`, `logo_url`,
`banner_url`, `canonical_url`, `featured_asins`, `nav`, and `widgets`. The slug
is the identity and the page id is not: a storefront is seven pages and each one
carries its own uuid, so a record keyed on the page id would give one brand seven
identities.

`nav` is the field a crawl wants. A storefront landing page is a navigation page
rather than a catalogue, and `nav` is the seven sub-pages the products are
actually on.

`follower_count` is declared and comes back empty on every storefront measured.
Amazon renders a Follow button with the word Follow on it and no figure, so the
number is not published, and the envelope says as much rather than the record
reporting a brand nobody follows.

## Seller profiles

`amz seller` reads a third-party seller's profile and feedback breakdown:

```bash
amz seller A1XYZSELLER22
amz seller "https://www.amazon.com/sp?seller=A1XYZSELLER22" -o json
```

A `Seller` carries `seller_id`, `name`, `business_name`, `business_address`,
`about`, `storefront_url`, `shipping_policy`, `return_policy`, `rating`,
`rating_count`, `positive_pct`, `feedback`, `rating_histogram`, and `reviews`.

`rating` and `rating_count` are the twelve month window, which is the one Amazon
puts in the header. `feedback` keeps all four windows, because a seller at 5.0
over 44 ratings in twelve months and 4.8 over 6,365 for its lifetime is two facts
and one number cannot carry them. `business_name` is the legal entity behind the
display name, and the two are usually different: the storefront trading as SIKAI
CASE is Hangzhou Hang Kai Technology Co.,Ltd.

`reviews` is the first page only. The rest come from an AJAX call to
`/sp/ajax/feedback` and are not in the HTML at any depth, so five reviews beside
a rating count of 6,365 is a first page rather than a quiet seller.

## Author pages

`amz author` reads an Author Central page:

```bash
amz author jrr-tolkien
amz author jrr-tolkien --books -o url  # the author's book ASINs
```

An `Author` carries `slug`, `page_id`, `name`, `bio`, `photo_url`, `website`,
`about_url`, `books`, `book_asins`, `total_books`, `sort_options`, and
`languages`.

`books` holds whole product records rather than bare ASINs, because the grid
publishes the title, the price and the rating for each one and dropping them
would cost a request each to get back. `total_books` is what Amazon states, which
is larger than the length of `books` whenever the grid paginates: the measured
page reads 70 and 135. `sort_options` and `languages` are the grid's own filter
vocabulary and are the parameters that page through the rest.

## Compose

Walk a brand's featured items into full records:

```bash
amz brand anker --featured -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | xargs -I{} amz product {} -o jsonl
```
