---
title: "Data model"
description: "Every record amz emits, field by field, plus the envelope that says where each value came from and what was looked for and not found."
weight: 15
---

amz turns each Amazon surface into one typed record. Every command emits a
stream of one record type, and the JSON name of a field is stable across
formats, so `--fields`, `--template`, and DuckDB's `data->>'name'` all reach the
same names you see here.

Two rules hold everywhere in this document.

The first is about absence. If a field is missing from a record and no entry in
`envelope.missed` names it, amz looked at the place that field lives and there
was nothing there. Absence is an answer, not a gap.

The second is about zero. A rating of `0` and a product nobody has rated are
different facts, so the fields that can be legitimately zero are pointers and
come back as `null` rather than as `0`. Read them as null-or-value and you will
never confuse a free product with an unpriced one.

## The envelope

Every record carries an `envelope`. It is the part of the record that talks
about the rest of the record.

| Field | Type | Meaning |
| --- | --- | --- |
| `family` | string | which parser read this page: `product`, `browse`, `store`, `seller`, `search`, `chart` |
| `marketplace` | string | the marketplace slug the read was scoped to |
| `sources` | []Source | every HTTP response that went into this record |
| `surfaces` | []string | the surface ids those responses belong to, `s1`, `s2`, `s11` |
| `depth` | string | which of `quick`, `meta`, `full`, `deep` produced the record |
| `via` | map | field name to the region, payload or selector that answered |
| `level` | map | field name to the rung of the extraction ladder it came from |
| `levels` | map | the same counted, which is what the ladder report reads |
| `missed` | []Miss | what was looked for and not found, and why |
| `disagree` | []Disagreement | fields two sources answered differently |
| `unread` | []string | named regions on the page nothing read |
| `robots` | RobotsNote | present only when a robots.txt rule was involved |
| `extra` | map | payloads the page shipped that no field reads yet, verbatim |
| `retrieved_at` | timestamp | the newest source's time |

A `Source` is one response: `url`, `surface`, `status`, `bytes`, `cached`, and
`retrieved_at`. The time is the response's, so a record built from a page that
was cached yesterday is dated yesterday and not now.

A `Miss` is `field`, `why`, and then whichever of `have`, `total`, `surfaces`
and `fix` apply. Together they encode four different states, and telling them
apart is the whole point:

| The record says | It means |
| --- | --- |
| field present | Amazon published it and amz read it |
| field absent, nothing in `missed` | amz read the region and it was empty |
| field absent, `missed` entry with `surfaces` | the data lives on a page this fetch did not read |
| field absent, `missed` entry naming regions | the regions amz expects are not on this page, which usually means Amazon moved them |

A `missed` entry with `have` and `total` is the fifth case and the one most
likely to be misread: the field is present and incomplete. Eight reviews on a
page that states 4,812 is `have: 8, total: 4812`, and a consumer who counts the
array and publishes that as the total has produced a wrong number.

`robots` is present only when robots.txt had something to say. It carries
`allowed`, the `rule` that matched, and `override`, which is true when the run
used `--no-robots`. A record fetched under an override says so in the record and
not only in the terminal it was fetched from.

## Shared types

These appear inside more than one record.

**Money** is `display`, `value`, `currency`, and `via`. `display` is the string
exactly as the page wrote it, `VND208,927`, so a wrong parse is recoverable
rather than lost. `value` is a float for the consumers that want a number and it
is lossy by construction. amz itself does arithmetic on an exact rational that
is never serialized, which is why sums it reports and sums you compute from
`value` can differ in the last place. A missing price is `null`, never `0`.

**Ref** points at another entity: `kind`, `id`, `name`, `uri`, `url`,
`resolved`. The `uri` is marketplace scoped, `amz:us/product/B075F5X8BR`,
because the same ASIN is a different product at a different price in every
marketplace Amazon runs. `resolved` is false when amz has a name and no
identifier, which is the normal state of a review author: the profile link goes
to `/gp/profile/`, robots.txt disallows it, so the name is kept and the record
admits it cannot resolve it.

**Conn** is a partial collection: `loaded`, `total_count`, `complete`, `url`.
`complete` is never omitted from the JSON, because a connection that is not
complete and a connection whose flag was dropped would otherwise look identical.

**Date** is `raw` and `parsed`. A date amz could not parse keeps its `raw` line
and has no `parsed`, rather than becoming a zero timestamp that reads as January
of year 1.

## Product

`amz product` returns one `Product` per ASIN.

| Field | Type | Meaning |
| --- | --- | --- |
| `asin` | string | the item |
| `parent_asin` | string | the variation parent, when this is a child |
| `marketplace` | string | the marketplace slug |
| `url` | string | the canonical detail page |
| `title` | string | product title |
| `isbn10`, `isbn13`, `upc`, `ean`, `model_number` | string | the identifiers the detail table carries |
| `brand` | Ref | the brand, resolved to a store id when the byline links one |
| `manufacturer` | string | the manufacturer line from the detail table |
| `byline` | string | the byline as written, before the "Visit the Store" wrapper is stripped |
| `authors` | []Ref | books |
| `offer` | Offer | the buy box |
| `other_offers` | Conn | how many other buying options the page claims |
| `rating` | number | average star rating |
| `ratings_count` | number | how many people rated it |
| `reviews_count` | number | how many wrote a review |
| `distribution` | Distribution | the five bucket star histogram |
| `reviews`, `questions` | Conn | what the page says exists, not what amz holds |
| `breadcrumb` | []Ref | browse nodes, root first |
| `ranks` | []Rank | every Best Sellers Rank line |
| `bullets` | []string | the "About this item" list |
| `description` | string | the description paragraph |
| `details` | map | the technical details table |
| `variation` | Variation | the twister |
| `images` | []Image | one entry per distinct photo, with its variants |
| `image_urls` | []string | the same photos as plain full resolution URLs |
| `videos` | []Video | inline product videos |
| `bought_past_month` | string | the "N+ bought in past month" line as written |
| `bought_past_month_n` | number | the parse of that line |
| `rails` | []Rail | the recommendation strips, at `--depth full` and above |
| `similar_asins` | []string | the "similar items" ASINs |
| `extra` | map | every `a-state` blob the page shipped, verbatim |
| `envelope` | Envelope | where all of the above came from |

**Offer** is the buy box: `price`, `list_price`, `savings`, `savings_pct`,
`per_unit` and `per_unit_label`, `coupon`, `subscribe`, `business_price`,
`condition`, `availability`, `in_stock`, `sold_by`, `ships_from`, `prime`,
`delivery`, `returns`. `savings` and `savings_pct` are derived from the two
prices rather than read off the page, because Amazon prints its own saving line
and the two disagree often enough that the derived pair is the honest one.
`availability` is the line as written and `in_stock` is amz's reading of it, and
both are kept because the reading is a guess about phrasing and the string is
not.

**Rank** is `rank`, `category`, `overall`, and `node`. `node` is the browse node
the rank line links to, which is the strongest category edge amz has, because it
is an identifier Amazon stated rather than a name lifted from a breadcrumb.
`overall` marks the department level rank, the one people mean by "sales rank",
and it is flagged rather than assumed to be the first line.

**Distribution** is the star histogram: `percent` indexed one star through five,
`count`, `total`, and `derived`. Amazon publishes integer percentages and not
counts, so the counts are percent times total over one hundred. `derived` is
true on every record today and is stated anyway, so that the day Amazon starts
publishing counts is a day this field can announce.

**Variation** is `parent_asin`, `dimensions`, `current`, `siblings`,
`total_count`, `complete`. A sibling carries its ASIN and its dimension values
always, and its price, image and availability only after `--depth deep`, which
fetches each sibling's own page. The parent page states none of those three, so
filling them from the parent would quote the selected variant's price against
every colour in the family.

## Depth

`amz product --depth` decides how much is read.

| Depth | Requests | What you get |
| --- | --- | --- |
| `quick` | 1 | the mobile URL, `/gp/aw/d/`, which returns the same page as `meta` |
| `meta` | 1 | the full 2.2 MB detail page, rails dropped |
| `full` | 1 | the same page with the recommendation rails kept |
| `deep` | 2 + one per sibling | full, plus each variation sibling's own page and the seller page |

`meta` is the default. It drops the rails it read and records that it did, so a
product with twelve strips on the page never looks like a product with none.
`deep` prints its cost and stops for `--yes` above twenty requests, because an
apparel listing with 88 siblings is ninety requests and a minute and a half of
polite crawling for one record.

`quick` was specified as the cheap read and is not one. Amazon gzips both
responses, so the 374 KB it was specified at is what the mobile URL weighs on the
wire while the 2.2 MB it was compared against is what the detail page weighs
after decoding. Measured on 2026-08-17 for B075F5X8BR, `/gp/aw/d/` is 373,945
bytes on the wire and 2,197,291 decoded, against 373,980 and 2,196,553 for
`/dp/`. It is the same page for the same bytes. `quick` is kept because it is the
only thing that reads that surface, and the saving returns if Amazon serves it a
lighter rendering again.

## Card

`amz search` and `amz related` return a stream of `Card`, and the chart and rail
records embed the same type.

| Field | Type | Meaning |
| --- | --- | --- |
| `position` | number | position in the stream |
| `rank` | number | rank on the source page, when it carries one |
| `asin` | string | the item |
| `title` | string | product title |
| `price`, `list_price` | Money | the tile's prices |
| `rating` | number | average star rating |
| `ratings_count` | number | number of ratings |
| `image` | string | the thumbnail, canonicalized to full resolution |
| `badge` | string | "Amazon's Choice", "Best Seller", and similar |
| `prime` | bool | Prime eligibility |
| `bought_past_month` | string | the "N+ bought in past month" line |
| `delivery` | string | the delivery promise on the tile |
| `sponsored` | bool | whether the card is a paid placement |
| `kind` | string | the source rail |
| `envelope` | Envelope | which page and response this row came off |

`sponsored` is never omitted from the JSON. An advertisement and an organic
result are different data, and a consumer who cannot tell them apart is holding
a corrupted dataset without knowing it.

## Review

`amz reviews` returns one `Review` per review: `review_id`, `asin`, `author` as
a Ref, `reviewer_name`, `rating`, `title`, `text`, `date` as a Date, `country`,
`verified_purchase`, `helpful_votes`, `images`, `variant_attrs`, `url`.

Amazon requires a sign-in for the review corpus. A product record therefore
carries the rating, the count and the histogram, and no reviews, and it says so
with a `missed` entry naming both review surfaces. That entry is what tells a
product with no reviews apart from a product whose reviews amz was not allowed
to read.

## QA

`amz qa` returns one `QA` per pair: `qa_id`, `asin`, `question`, `question_by`,
`answer`, `answer_by`, `helpful_votes`, `url`. Amazon is retiring this section
across many categories, and a product without one returns a distinct error
rather than an empty list.

## Offer listing

`amz offers` returns one `OfferListing` per buying option: `asin`, `price`,
`currency`, `shipping`, `condition`, `seller_name`, `seller_id`,
`seller_rating`, `fulfilled_by`, `delivery`, `is_buybox`, `url`.

## BestsellerEntry

The five chart commands all return `BestsellerEntry`: `list_type`, `category`,
`node_id`, `rank`, `asin`, `title`, `price`, `rating`, `ratings_count`, `image`,
`url`, and `envelope`.

Ranks come from the page and are never counted. Page one covers ranks 1 to 50
while drawing 30 tiles, so numbering entries as they arrive would label rank 51
as rank 31 and keep doing it for the length of the chart. An entry Amazon listed
without drawing a tile for it carries `rank_only`, its rank and its ASIN, and
nothing else, which is a rank amz can report rather than an item it must drop.

## Category

`amz category` returns one `Category` per browse node.

| Field | Type | Meaning |
| --- | --- | --- |
| `node_id` | string | the node that was asked for |
| `canonical_node` | string | the node Amazon says it served, which is not always the same one |
| `name` | string | the node name, from the canonical slug |
| `slug` | string | the readable segment of the canonical URL |
| `related` | []Ref | the other browse nodes the page links, with their names |
| `shelves` | []CategoryShelf | the merchandised carousels, each with its heading |
| `top_asins` | []string | every ASIN across those shelves, deduplicated |
| `item_count` | number | the tile count before deduplication |
| `url`, `canonical_url` | string | where it was read from and where Amazon says it lives |

There is no parent and no breadcrumb here, because a `/b` page publishes
neither. `related` is named for what it is: Amazon links a browse page to its
children and its siblings and gives no marker for which is which, so these are
related nodes and not a tree. Links in the site header and footer are excluded,
which matters more than it sounds like: eight of the twenty four node links on
the Electronics page are Gift Cards, Sell, Registry and the rest of the chrome.

The edge upwards comes from a product's rank line instead, which does link its
node.

## Brand, Seller, Author

`amz brand` returns `slug`, `page_id`, `name`, `description`, `logo_url`,
`banner_url`, `canonical_url`, `featured_asins`, `nav`, `widgets`. A storefront
is a navigation page rather than a catalogue, so `nav` is the field that matters
for a crawl: it is the seven sub-pages the products are actually on.

`amz seller` returns `seller_id`, `name`, `business_name`, `business_address`,
`about`, `logo_url`, `storefront_url`, `shipping_policy`, `return_policy`,
`rating`, `rating_count`, `positive_pct`, `feedback`, `rating_histogram`,
`reviews`. The four feedback windows are all kept, because a seller at 5.0 over
30 days and 4.6 over twelve months is a fact that one number cannot carry.

`amz author` returns `slug`, `page_id`, `name`, `bio`, `photo_url`, `website`,
`about_url`, `books`, `book_asins`, `total_books`, `sort_options`, `languages`.
`total_books` is what Amazon states, which is larger than `len(books)` whenever
the grid paginates, and `sort_options` and `languages` are the parameters that
page through the rest.

Each of these records, and `Product` and `Category`, can point at itself as a
`Ref`, which is what the claim graph and the RDF export walk.

## Deal

`amz deals` returns one `Deal` per grid entry: `asin`, `deal_id`, `title`,
`deal_price`, `list_price`, `list_label`, `discount_pct`, `badge`, `ends_soon`,
`shelf`, `position`, `url`, `image`.

`list_label` is kept beside `list_price` because Amazon writes "List:", "List
Price:" and "Typical:" for it, and a typical price is a computed average rather
than a manufacturer's list price. Recording the number without the label would
turn three different claims into one.

## The flat shape

`--flat` emits the v0.2.1 record: one level, the old column names, and prices as
bare numbers. It is there so a pipeline written against the old shape keeps
running while it is updated, it is deprecated, and it will be removed one version
after the release that introduced it.

The one thing it does not drop is the envelope, which travels whole. Everything
else in the flat record is a projection and the envelope is not, so a flat record
still names its sources and its misses.

```bash
amz product B084DWG2VQ --flat
```

## Reaching a field

Because every field has one stable name, the same name works in every tool:

```bash
# project columns in any format
amz product B084DWG2VQ -o csv --fields asin,price,savings_pct,rank

# a custom line with a template
amz search "usb c cable" --template '{{.asin}} {{.price}} {{.title}}'

# the envelope is part of the record, so provenance queries are ordinary queries
amz product B084DWG2VQ -o jsonl | jq -r '.envelope.via.price'
amz product B084DWG2VQ -o jsonl | jq -r '.envelope.missed[] | .field + ": " + .why'

# a typed column out of the local store's JSON
amz db query "select asin, (data->'offer'->'price'->>'value')::double price from products order by price desc limit 10"
```
