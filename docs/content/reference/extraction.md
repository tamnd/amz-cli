---
title: "Extraction"
description: "The four rungs every field is read from, the regions nothing reads yet, and the capture ledger that catches Amazon moving."
weight: 25
---

A scraper is a claim about somebody else's HTML. The useful question is not
whether it returned something but how it knew, and that is what this part of
`amz` exists to answer.

## The four rungs

Every field in the registry is declared at one of four levels. The level is
written down in the declaration rather than inferred from the code, so a field
that quietly changes source has to change its declared rung to compile.

| Rung | Source | What it means |
| --- | --- | --- |
| 1 | a region Amazon named | `data-feature-name="bylineInfo"`, `cel_widget_id`, `data-component-type`. Amazon's own label for the block. |
| 2 | a JavaScript payload | a `var config = {...}` or `A.state(...)` object the page ships. Machine shaped and stable, but a shape Amazon owns. |
| 3 | a data attribute | `data-asin`, `data-offset`, `data-index-offset`. Structural, and Amazon rarely renames these. |
| 4 | a bare CSS selector | `.dcl-product-price-new`. A guess that happens to be right today. |

Rung 1 is the only rung that survives a restyling, because a name Amazon chose
for a block is a name Amazon has to keep using for its own code to work. Rung 4
is debt: it works now and says nothing about tomorrow.

```
$ amz extraction
FAMILY   REGION  PAYLOAD  ATTR  SELECTOR  TOTAL
product  25      0        1     1         27
search   17      0        4     0         21
chart    0       1        4     4         9
browse   3       1        6     11        21
store    2       7        5     0         14
seller   14      0        0     0         14
```

The report then lists every rung 4 field by name, oldest first, with the date it
was added. A selector that has survived a year of Amazon's restyling is a
different risk from one written last week, and the date is the only evidence
either way.

Two families sit low on the ladder for reasons worth knowing. Charts predate
`data-*` entirely, so the tile title is read from the only anchor a chart tile
has, which is its link to `/dp/`. The deals grid is a React surface whose class
names are the closest thing it ships to a contract.

## What one page yielded

Give the command a page and it reports the read rather than the registry.

```
$ amz extraction B075F5X8BR
product  product  https://www.amazon.com/dp/B075F5X8BR  2355440 bytes
29 fields set, 3 missed, 259 regions Amazon named that nothing reads

not on this page:
  similar_asins  product region "similarities" or "sims-consolidated-2_feature_div" not present on this page
  reviews        amazon requires a sign-in for the review corpus, and the detail page carries the histogram and the reviews medley only. the total is the ratings count, which is the largest number the page states (have 13 of 21095, on /product-reviews/ and /portal/customer-reviews/, amz why reviews)
  other_offers   the all-offers panel is built by javascript and states only its own count on the page (have 1 of 2, on /gp/aod/ajax and /gp/offer-listing/, amz why offers)
```

A miss is a field the registry declared and the page did not carry, and the
sentence beside it is the parser saying what it looked for. That is the
difference between "no price" and "no price because the buy box is not on this
page", and only the second one tells a caller what to do.

| Flag | Effect |
| --- | --- |
| `--fields` | every field that filled, with the region, payload, attribute or selector it came from |
| `--unread` | the named regions on the page that no field reads |
| `--family` | limit the ladder report to one family |

`--unread` is the worklist. The detail page measured above carries 288 distinct
`data-feature-name` regions and amz reads 29 of them. The other 259 are not a
silence, they are the next version's work, and printing them is how the size of
the gap stays honest.

Add `-o json` on a page report to get the same numbers as a record, which is
what CI reads.

## The four states of a missing field

`amz extraction` reports this for a page. Every record reports it for itself, in
`envelope.missed`, and the distinction is the same one:

| The record says | It means |
| --- | --- |
| the field is present | Amazon published it and amz read it |
| absent, and nothing in `missed` names it | amz read the region and it was empty |
| absent, with a `missed` entry carrying `surfaces` | the data lives on a page this fetch did not read |
| absent, with a `missed` entry naming regions | the regions amz expects are not on this page, which usually means Amazon moved them |

That third state is the one people assume is the second. A product record has no
reviews because Amazon requires a sign-in for the corpus, and the entry naming
`/product-reviews/` and `/portal/customer-reviews/` is what separates that from a
product nobody has reviewed.

There is a fifth case that is not absence at all: a `missed` entry with `have`
and `total` means the field is present and incomplete. Eight reviews on a page
that states 4,812 reads `have: 8, total: 4812`, and counting the array is then
visibly the wrong way to get a total rather than an invisible one.

Every cap amz applies reports itself this way. A browse node that links more than
fifty related nodes keeps fifty and files a `missed` entry with the real count,
because a silent truncation reads exactly like a complete answer.

## The capture ledger

Twenty one pages live in this repository as gzipped captures, covering all six
families plus the body Amazon serves with a 200 status and no product on it.
Every one records what the parser made of it on the day it was taken: bytes,
fields set, fields missed, unread regions and records found.

`amz verify` fetches those same pages and compares.

```
$ amz verify --live
CAPTURE         STATUS  DETAIL
product_simple  moved   261 unread regions, was 260
seller_rated    same    14 fields, 5 records

20 checked, 1 drifted, 0 worse than the ledger, 1 skipped
```

The skipped one is the soft 404. Its right answer is a refusal rather than a
record, so it has no field counts to compare and re-fetching it would spend a
request to learn that a page that did not exist still does not.

Fewer fields, more misses or fewer records is `worse` and fails under
`--strict`. A change in unread regions is `moved`, reported and never a failure,
because Amazon adding a section is Amazon adding a section and a tool that cried
failure every time a marketing widget appeared would be ignored inside a month.

Without `--live` it reads only pages already in your cache. `amz verify` is a
command people run out of curiosity, and a curiosity that fetches twenty one
pages from a site that did not ask to be measured is not a polite default.

The captures were taken with no cookie jar, so none of them carries an account,
a cart or a personalized layout. The 746 per-request identifiers Amazon prints
into its own ref tags were replaced with zeros before the files were stored.

The ledger paid for itself on its first run. Four of Amazon's own canonical URL
forms, `/electronics-store/b`, `/computer-pc-hardware-accessories-add-ons/b`,
`/Best-Sellers-Electronics/zgbs/electronics` and `/events/wintersale`, resolved
to no known surface at all. Chart pages were reporting fifty entries beside an
envelope claiming nothing had been read from the page. Both are fixed. A third
suspect was exonerated: the movers grid comes back empty because Amazon serves
it with `data-offset="0"` and no tiles for the client to fill, so an empty
result there is the correct answer rather than a parser failure.

## Amazon's own account of a page

Some pages ship an interface map: Amazon's own list of what the regions are and
what they are for.

```
$ amz agent-map B075F5X8BR
```

It is recorded and never trusted. It is a statement by the site about the site,
useful for finding a region worth reading and worthless as evidence that the
region holds what it says. Every field in `amz` is measured against the HTML
instead, which is why the ladder exists at all.
