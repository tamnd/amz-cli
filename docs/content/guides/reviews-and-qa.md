---
title: "Reviews and Q&A"
description: "Read the reviews and the histogram a detail page carries, filter them, and pull the question and answer pairs, with an honest account of what is behind a sign-in."
weight: 30
---

Two surfaces carry the social proof: the reviews and the classic question and
answer pairs. Both take an ASIN, and both are partly behind a login, which is the
first thing to understand about them.

## What is public

Amazon requires a sign-in for the review corpus. `/product-reviews/` and
`/portal/customer-reviews/` both redirect to `/ap/signin`, and amz carries no
session and wants none, so there is no version of this tool that pages through
twenty thousand reviews.

What is public is on the detail page: the rating, the ratings count, the five
bucket star histogram, and a medley of about a dozen reviews Amazon chooses. That
is what `amz reviews` returns, and it says so on stderr on every run rather than
printing thirteen rows as though they were the corpus:

```
amz: 13 of 21095. amazon requires a sign-in for the review corpus, and the detail page carries the histogram and the reviews medley only. the total is the ratings count, which is the largest number the page states
amz:   /product-reviews/ is not readable without a session
amz:   /portal/customer-reviews/ is not readable without a session
amz: run `amz why reviews` for the detail
```

The same fact is on the record, in `reviews.loaded`, `reviews.total_count` and
`reviews.complete`, and in `envelope.missed`. A product nobody has reviewed and a
product whose reviews amz was not allowed to read are different things and they
serialize differently.

`--deep` attempts the standalone corpus anyway. It will fail, and it exists so
that the failure is something you can reproduce rather than something you have to
take on trust.

## Reviews

```bash
amz reviews B075F5X8BR
amz reviews B075F5X8BR -n 50 -o jsonl
```

### The review record

Each `Review` carries `review_id`, `author`, `reviewer_name`, `rating`, `title`,
`text`, `date`, `country`, `verified_purchase`, `helpful_votes`, `images`, and
`variant_attrs` (the format strip, parsed into key and value pairs such as
`colour` and `size`). When the page has no stable id, amz derives a stable
`review_id` so the same review hashes the same across runs.

`author` is a reference and it is almost always unresolved. The profile link goes
to `/gp/profile/`, which robots.txt disallows, so amz has a display name and no
identifier to file it under, and the record says that rather than dropping the
name or inventing an id.

`date` is the line Amazon wrote plus the parse of it when one succeeded. A date
amz could not read keeps its `raw` and has no `parsed`, because a failed parse
that became a zero time would be January of year 1 and everything downstream
would treat it as a real date.

### The histogram

`distribution` is the five bucket star breakdown, and it carries `derived` for a
reason. Amazon prints the buckets as whole percentages and not as counts, so the
counts amz reports are the percentages multiplied by the total and they do not
sum to the total exactly. `derived: true` is the record saying which of those two
numbers was read and which was calculated.

### Filters

These apply to the medley, locally, after it is read. They narrow what you are
looking at, and none of them reaches a review that was not on the page.

| Flag | Effect |
| --- | --- |
| `--stars` | only N-star reviews, 1 to 5 |
| `--verified` | verified purchases only |
| `--with-images` | reviews that include photos |

```bash
amz reviews B075F5X8BR --stars 1 --verified -o csv > one_star.csv
amz reviews B075F5X8BR --with-images -n 20
```

`--sort` orders what came back, and it is local too. It takes `recent` or
`helpful`, and it reorders the medley rather than asking Amazon for a different
one, because the surface that takes a sort parameter is the corpus and the corpus
needs a session. A review whose date amz could not parse sorts last under
`recent` rather than to the epoch, so an unparsed date does not masquerade as the
oldest review on the page.

```bash
amz reviews B075F5X8BR --sort recent
amz reviews B075F5X8BR --sort helpful -o jsonl | jq -r '"\(.helpful_votes)\t\(.title)"'
```

The field is `rating`, not `stars`. `--stars` is the flag that filters on it, and
`jq 'select(.stars <= 2)'` matches every review rather than none, because `null`
compares less than a number in jq.

### Just the URL

To open the review pages yourself, render the URL:

```bash
amz reviews B075F5X8BR -o url
```

## Questions and answers

```bash
amz qa B075F5X8BR
amz qa B075F5X8BR -o jsonl
```

Each `QA` record carries `qa_id`, `question`, `question_by`, `answer`,
`answer_by`, and `helpful_votes`.

The same login wall applies. The ask region on the detail page states how many
questions have been answered and carries the pairs themselves only on some
products, and the standalone page redirects to a sign-in whose `assoc_handle` is
`amzn_ask_us`, which is the Q&A service asking rather than a generic redirect.
Most products now have no ask region at all:

```
amz: this product has no ask region at all, which is now the usual case
no question and answer pairs on the page
```

That exits 3, the no-data code, rather than printing an empty table, so a script
can tell "no questions" from "fetch failed". The answered count, when there is
one, is also on the product record under `questions`.

## Compose

A quick sentiment skim of the medley, one-star against five-star:

```bash
echo "1-star: $(amz reviews B075F5X8BR --stars 1 -o jsonl | wc -l)"
echo "5-star: $(amz reviews B075F5X8BR --stars 5 -o jsonl | wc -l)"
```

The histogram is the better instrument for the whole corpus, since it covers all
21,095 ratings rather than the thirteen reviews Amazon chose to show:

```bash
amz product B075F5X8BR -o json | jq '.[0].distribution'
```
