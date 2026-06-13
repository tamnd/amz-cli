---
title: "Categories and storefronts"
description: "Walk browse nodes, brand storefronts, seller profiles, and author pages."
weight: 50
---

Four surfaces describe who sells what and where it sits in the tree.

## Categories (browse nodes)

`amz category` resolves a browse node by id or URL into its name, breadcrumb,
child nodes, and the ASINs on the landing page:

```bash
amz category 172282                   # by node id
amz category "https://www.amazon.com/b?node=172282"
amz category 172282 --children        # just the child node ids
amz category 172282 --top -o url      # just the top ASINs as URLs
```

Each `Category` carries `node_id`, `name`, `parent_node_id`, `breadcrumb`,
`child_node_ids`, and `top_asins`. Walk a tree by feeding child ids back in.

## Brand storefronts

`amz brand` reads a brand's storefront from its slug or a `/stores/` URL:

```bash
amz brand anker
amz brand "https://www.amazon.com/stores/Anker/page/..."
amz brand anker --featured -o url     # the featured ASINs
```

A `Brand` carries `name`, `description`, `logo_url`, `banner_url`,
`follower_count`, and `featured_asins`.

## Seller profiles

`amz seller` reads a third-party seller's profile and feedback breakdown:

```bash
amz seller A1XYZSELLER22
amz seller "https://www.amazon.com/sp?seller=A1XYZSELLER22" -o json
```

A `Seller` carries `name`, `rating`, `rating_count`, and the
`positive_pct`/`neutral_pct`/`negative_pct` feedback split.

## Author pages

`amz author` reads an Author Central page:

```bash
amz author jrr-tolkien
amz author jrr-tolkien --books -o url  # the author's book ASINs
```

An `Author` carries `name`, `bio`, `photo_url`, `website`, `follower_count`, and
`book_asins`.

## Compose

Walk a brand's featured items into full records:

```bash
amz brand anker --featured -o url \
  | sed 's#.*/dp/##; s#/.*##' \
  | xargs -I{} amz product {} -o jsonl
```
