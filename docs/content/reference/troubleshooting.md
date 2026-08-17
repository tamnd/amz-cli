---
title: "Troubleshooting"
description: "The bot wall, empty results, rate limits, and the local store."
weight: 40
---

## "blocked" (exit 5)

Amazon sometimes serves a bot-check page instead of the content, especially for
product detail pages from data-center IPs. amz detects that page and exits with
code 5 rather than handing you a record parsed from a CAPTCHA. This is expected,
not a bug in amz.

When it happens:

- **Slow down.** Raise `--rate` (try `--rate 6s`). A
  steadier, slower stream is far less likely to trip the wall.
- **Use the official API.** With PA-API credentials, `--api` avoids the HTML
  path entirely for the surfaces it covers.
- **Switch network.** A residential IP is blocked far less often than a
  data-center one.

amz's crawl loop already handles transient blocks for you: a blocked item goes
back on the queue with a backoff instead of failing the whole run.

## "disallowed by robots.txt" (exit 7)

`amz` asked the marketplace's live `robots.txt` and it said no. The message names
the rule, the pattern it matched, and the group it came from, so you can check it
yourself:

```console
$ amz robots check /gp/offer-listing/B075F5X8BR
disallowed  https://www.amazon.com/gp/offer-listing/B075F5X8BR  Disallow: /gp/offer-listing/  offers s18
```

This is the site's answer, not a bug in `amz`. `--no-robots` overrides it for one
run, prints a banner, names every rule it breaks, and slows the pace floor to 5s.
`amz crawl --no-robots` additionally requires `--yes`.

Note that `/gp/offer-listing/` also redirects to the detail page, so overriding
the rule buys you a redirect rather than an offer list. The buy box on `/dp/` is
where that data lives now.

## "robots.txt could not be fetched" (exit 8)

`amz` could not read `robots.txt`, so it read nothing at all. A crawler that
treats a failed fetch as permission is the worst kind of crawler there is, so
there is no fallback copy and no "assume allowed" path.

Retry, check the network, or check that `--marketplace` names a real host.

## "no results" (exit 3)

Code 3 means the fetch succeeded but the surface was genuinely empty, for
example a product with no Q&A, or a search with no hits. It is distinct from a
runtime error (1) or a block (5), so a script can branch on it. Double-check the
identifier and any filters (`--stars`, `--min-price`) that might exclude
everything.

## Rate limits and retries

A 429 or 503 is retried automatically with backoff (`--retries`, default 3).
Persistent 429s mean you are going too fast: raise `--rate`. The cache helps
here too: a repeated lookup never re-hits the network, so iterate on
`--fields`, `--template`, and `-o` against a cached page freely.

## Stale data

amz caches successful pages. To force a fresh fetch:

```bash
amz product B084DWG2VQ --refresh    # ignore the cache, repopulate it
amz product B084DWG2VQ --no-cache   # bypass the cache entirely
amz cache clear                     # drop the whole cache
```

## The local store needs DuckDB

The `db` and `crawl` commands shell out to the `duckdb` binary. If it is not on
your `PATH`, install it (`brew install duckdb`, or your distro's package) and
re-run. Every fetch command works without it.

## Empty or odd fields

amz reads the JSON-LD block first and fills gaps from the HTML. If a field you
expect is missing, the page likely did not show it for your marketplace or
session. Try a different `-m`, or a cookied session for locale-specific pricing.
Pass `--raw` to inspect the exact bytes amz parsed.
