---
title: "Data model"
description: "Every record amz emits, field by field, so you know exactly what each command returns and how to reach it."
weight: 15
---

amz turns each Amazon surface into one typed record. Every command emits a
stream of one record type, and the JSON name of a field is stable across
formats, so `--fields`, `--template`, and DuckDB's `data->>'name'` all use the
same names you see here.

A field is present only when the page carried it. An omitted field always means
Amazon did not show it for your marketplace and session, never that amz dropped
it. The two always-present anchors on every record are `url` (the canonical page
the record came from) and, on fetched records, `fetched_at` (an RFC 3339 UTC
timestamp).

## Product

`product` returns one `Product` per ASIN. It is the richest record amz builds,
read from the JSON-LD block first and completed from the HTML.

| Field | Type | Meaning |
| --- | --- | --- |
| `asin` | string | the item |
| `parent_asin` | string | the variation parent, when the page is a child |
| `title` | string | product title |
| `brand` | string | brand name |
| `brand_id` | string | the brand's browse-node id, when linked |
| `price` | number | current price |
| `currency` | string | ISO currency code (`USD`, `GBP`, ...) |
| `list_price` | number | the struck-through list price, when discounted |
| `savings` | number | `list_price` minus `price` |
| `savings_pct` | number | the discount as a whole percent |
| `coupon` | string | the clip-coupon line, when one is offered |
| `rating` | number | average star rating, 0 to 5 |
| `ratings_count` | number | number of ratings |
| `reviews_count` | number | number of written reviews |
| `answered_qs` | number | answered questions on the page |
| `bought_past_month` | string | the "N+ bought in past month" line |
| `availability` | string | the stock line as shown |
| `in_stock` | bool | whether `availability` means buyable |
| `description` | string | the product description paragraph |
| `bullet_points` | []string | the "About this item" feature bullets |
| `specs` | map | the technical-details table as key/value pairs |
| `images` | []string | full-resolution image URLs, one per distinct photo |
| `videos` | []string | inline product video URLs |
| `category_path` | []string | the breadcrumb, root to leaf |
| `browse_node_ids` | []string | the node ids behind the breadcrumb |
| `seller_id` | string | the merchant's seller id |
| `seller_name` | string | the merchant's display name |
| `sold_by` | string | the "Sold by" line from the buy box |
| `ships_from` | string | the "Ships from" line from the buy box |
| `fulfilled_by` | string | the fulfiller (often Amazon) |
| `variant_asins` | []string | the other ASINs in the variation family |
| `similar_asins` | []string | "similar items" ASINs on the page |
| `rank` | number | the overall Best Sellers Rank |
| `rank_category` | string | the category that overall rank is in |
| `ranks` | []ProductRank | every Best Sellers Rank, overall and per subcategory |
| `marketplace` | string | the marketplace slug the record came from |

`ProductRank` is one rank line: `rank` (number) and `category` (string). A
product is usually ranked once overall and again in one or more subcategories;
`rank`/`rank_category` keep the overall rank flat for quick filtering, while
`ranks` holds them all.

### Images and videos

Amazon serves the same photo at dozens of sizes and from several CDN hosts. amz
strips the size modifier from every image URL and pins one canonical host, so
`images` holds one full-resolution URL per distinct photo, with thumbnails,
tracking pixels, and sprites removed. The same canonicalization runs on every
record that carries an image, so an image URL means the same thing everywhere.

## Card

`search` and `related` return a stream of `Card`, a lightweight catalog hit.

| Field | Type | Meaning |
| --- | --- | --- |
| `position` | number | 1-based position in the stream |
| `rank` | number | rank on the source page, when it carries one |
| `asin` | string | the item |
| `title` | string | product title |
| `price` | number | current price |
| `list_price` | number | the struck-through price, when discounted |
| `currency` | string | ISO currency code |
| `rating` | number | average star rating |
| `ratings_count` | number | number of ratings |
| `image` | string | the card thumbnail, canonicalized to full resolution |
| `badge` | string | "Amazon's Choice", "Best Seller", and similar tags |
| `prime` | bool | Prime eligibility |
| `bought_past_month` | string | the "N+ bought in past month" line |
| `sponsored` | bool | whether the card is a paid placement |
| `kind` | string | the source rail (`search`, `related`, `also-viewed`, ...) |

## Review

`reviews` returns one `Review` per review.

| Field | Type | Meaning |
| --- | --- | --- |
| `review_id` | string | stable id, derived when the page has none |
| `asin` | string | the reviewed item |
| `reviewer_id` | string | the reviewer's profile id |
| `reviewer_name` | string | display name |
| `rating` | number | star rating, 1 to 5 |
| `title` | string | review title |
| `text` | string | review body |
| `date` | string | the review date as shown |
| `country` | string | the reviewer's country |
| `verified_purchase` | bool | the verified-purchase badge |
| `helpful_votes` | number | "N people found this helpful" |
| `images` | []string | reviewer photos, full resolution |
| `variant_attrs` | map | the format strip parsed to key/value (`colour`, `size`) |

## QA

`qa` returns one `QA` per question-and-answer pair.

| Field | Type | Meaning |
| --- | --- | --- |
| `qa_id` | string | stable id for the pair |
| `asin` | string | the item |
| `question` | string | the question text |
| `question_by` | string | who asked |
| `answer` | string | the top answer |
| `answer_by` | string | who answered |
| `helpful_votes` | number | votes on the answer |

## Offer

`offers` returns one `Offer` per buying option.

| Field | Type | Meaning |
| --- | --- | --- |
| `asin` | string | the item |
| `price` | number | the offer price |
| `currency` | string | ISO currency code |
| `shipping` | string | the shipping line |
| `condition` | string | `New`, `Used - Like New`, and so on |
| `seller_name` | string | the offering seller |
| `seller_id` | string | the seller's id |
| `seller_rating` | string | the seller's feedback summary |
| `fulfilled_by` | string | the fulfiller |
| `delivery` | string | the delivery promise |
| `is_buybox` | bool | whether this is the featured Buy Box offer |

## BestsellerEntry

The five chart commands (`bestsellers`, `new-releases`, `movers`, `wished`,
`gifted`) all return `BestsellerEntry`.

| Field | Type | Meaning |
| --- | --- | --- |
| `list_type` | string | which chart (`bestsellers`, `most-gifted`, ...) |
| `category` | string | the category scope, when set |
| `node_id` | string | the browse-node scope, when set |
| `rank` | number | rank in the chart |
| `asin` | string | the item |
| `title` | string | product title |
| `price` | number | current price |
| `currency` | string | ISO currency code |
| `rating` | number | average star rating |
| `ratings_count` | number | number of ratings |

## Category

`category` returns one `Category` per browse node.

| Field | Type | Meaning |
| --- | --- | --- |
| `node_id` | string | the browse-node id |
| `name` | string | the node name |
| `parent_node_id` | string | the parent node, when known |
| `breadcrumb` | []string | the path from the root |
| `child_node_ids` | []string | the immediate children |
| `top_asins` | []string | the ASINs on the landing page |

## Brand

`brand` returns one `Brand` per storefront.

| Field | Type | Meaning |
| --- | --- | --- |
| `slug` | string | the storefront slug |
| `name` | string | brand name |
| `description` | string | the storefront description |
| `logo_url` | string | the brand logo, full resolution |
| `banner_url` | string | the storefront banner, full resolution |
| `follower_count` | number | followers, when shown |
| `featured_asins` | []string | the ASINs the storefront features |

## Seller

`seller` returns one `Seller` per third-party profile.

| Field | Type | Meaning |
| --- | --- | --- |
| `seller_id` | string | the seller's id |
| `name` | string | the seller's display name |
| `rating` | string | the headline rating line |
| `rating_count` | number | number of ratings |
| `positive_pct` | number | percent positive feedback |
| `neutral_pct` | number | percent neutral feedback |
| `negative_pct` | number | percent negative feedback |

## Author

`author` returns one `Author` per Author Central page.

| Field | Type | Meaning |
| --- | --- | --- |
| `slug` | string | the author slug |
| `name` | string | author name |
| `bio` | string | the biography |
| `photo_url` | string | the author photo, full resolution |
| `website` | string | the linked website |
| `book_asins` | []string | the author's book ASINs |
| `follower_count` | number | followers, when shown |

## Deal

`deals` returns one `Deal` per deals-grid entry.

| Field | Type | Meaning |
| --- | --- | --- |
| `asin` | string | the item |
| `title` | string | product title |
| `deal_price` | number | the deal price |
| `list_price` | number | the pre-deal price |
| `discount_pct` | number | the discount as a whole percent |
| `badge` | string | the deal badge ("Lightning Deal", ...) |
| `currency` | string | ISO currency code |

## Reaching a field

Because every field has one stable name, the same name works in every tool:

```bash
# project columns in any format
amz product B084DWG2VQ -o csv --fields asin,price,savings_pct,rank

# a custom line with a template
amz search "usb c cable" --template '{{.asin}} {{.price}} {{.title}}'

# a typed column out of the local store's JSON
amz db query "select asin, (data->>'price')::double price from products order by price desc limit 10"
```
