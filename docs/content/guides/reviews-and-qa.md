---
title: "Reviews and Q&A"
description: "Stream the full review corpus with star, verified, and image filters, and pull question-and-answer pairs."
weight: 30
---

Two surfaces carry the social proof: the review corpus and the classic
question-and-answer pairs. Both take an ASIN.

## Reviews

```bash
amz reviews B084DWG2VQ
amz reviews B084DWG2VQ -n 50 -o jsonl
```

`reviews` pages through the corpus, emitting one record per review until it hits
your `--limit`.

### The review record

Each `Review` carries `review_id`, `reviewer_name`, `reviewer_id`, `rating`,
`title`, `text`, `date`, `country`, `verified_purchase`, `helpful_votes`,
`images`, and `variant_attrs` (the format strip, parsed into key/value pairs
such as `colour` and `size`). When the page has no stable id, amz derives a
stable `review_id` so the same review hashes the same across runs.

### Filters

| Flag | Effect |
| --- | --- |
| `--sort` | `recent` (default) or `helpful` |
| `--stars` | only N-star reviews, 1 to 5 |
| `--verified` | verified purchases only |
| `--with-images` | reviews that include photos |
| `--page` | first review page to fetch |

```bash
amz reviews B084DWG2VQ --stars 1 --verified -o csv > one_star.csv
amz reviews B084DWG2VQ --sort helpful --with-images -n 20
```

### Just the URL

To open the review pages yourself, render the URL:

```bash
amz reviews B084DWG2VQ -o url
```

## Questions and answers

```bash
amz qa B084DWG2VQ
amz qa B084DWG2VQ -o jsonl
```

Each `QA` record carries `qa_id`, `question`, `question_by`, `answer`,
`answer_by`, and `helpful_votes`. When a product has no Q&A section, amz exits
with the no-data code (3) rather than printing an empty table, so a script can
tell "no questions" from "fetch failed".

## Compose

A quick sentiment skim, count one-star versus five-star:

```bash
echo "1-star: $(amz reviews B084DWG2VQ --stars 1 -o jsonl | wc -l)"
echo "5-star: $(amz reviews B084DWG2VQ --stars 5 -o jsonl | wc -l)"
```
