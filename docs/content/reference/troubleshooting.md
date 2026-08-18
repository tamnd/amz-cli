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

## "interstitial challenge" (exit 6)

This is the bot challenge rather than the CAPTCHA, and the two are separate exit
codes because they call for different things. A CAPTCHA is a decision and an
interstitial is a rate, so 6 is worth retrying later and 5 is not.

amz backs off and retries on its own before it gives up, and it says so while it
waits:

```
amz: interstitial challenge on https://www.amazon.com/s?k=usb-c+hub, waiting 1m0s
amz: interstitial challenge on https://www.amazon.com/s?k=usb-c+hub, waiting 2m0s
```

Exit 6 means it was still there after the backoff ran out. Wait, then raise
`--rate`. It usually means the machine has asked amazon.com for a lot today,
which a long `--all` run will do.

## "sign in required" (exit 9)

The surface is behind a login. amz carries no credentials and wants none, so this
is a stop rather than a step: the page is not public, and there is nothing about
it this tool is entitled to read. There is no flag for it and there is not going
to be one.

`amz why reviews` and `amz why offers` cover the two you are most likely to hit,
with the measurement and the date behind each.

## "disallowed by robots.txt" (exit 7)

`amz` asked the marketplace's live `robots.txt` and it said no.
The message names the rule, the pattern it matched, and the group it came from, so you can check it yourself:

```console
$ amz robots check /gp/offer-listing/B075F5X8BR
disallowed  https://www.amazon.com/gp/offer-listing/B075F5X8BR  Disallow: /gp/offer-listing/  offers s18
```

This is the site's answer, not a bug in `amz`.
`--no-robots` overrides it for one run, prints a banner, names every rule it breaks, and raises the pace floor to 5s.
`amz crawl --no-robots` additionally requires `--yes`.

Note that `/gp/offer-listing/` also redirects to the detail page, so overriding the rule buys you a redirect rather than an offer list.
The buy box on `/dp/` is where that data lives now.

## "robots.txt could not be fetched" (exit 8)

`amz` could not read `robots.txt`, so it read nothing at all.
A crawler that treats a failed fetch as permission is the worst kind of crawler there is, so there is no fallback copy and no "assume allowed" path.

Retry, check the network, or check that `--marketplace` names a real host.

## "no results" (exit 3)

Code 3 means the fetch succeeded but the surface was genuinely empty, for
example a product with no Q&A, or a search with no hits. It is distinct from a
runtime error (1) or a block (5), so a script can branch on it. Double-check the
identifier and any filters (`--stars`, `--price`) that might exclude
everything.

## Rate limits and retries

A 429 or 503 is retried automatically with backoff (`--retries`, default 3).
Persistent 429s mean you are going too fast: raise `--rate`. The cache helps
here too: a repeated lookup never re-hits the network, so iterate on
`--fields`, `--template`, and `-o` against a cached page freely.

## Stale data

amz caches successful pages. To force a fresh fetch:

```bash
amz product B075F5X8BR --refresh    # ignore the cache, repopulate it
amz product B075F5X8BR --no-cache   # bypass the cache entirely
amz cache clear                     # drop the whole cache
```

## The local store needs nothing

SQLite is compiled into the binary as pure Go. There is no `duckdb` to install,
no cgo, and no external process, so `db`, `crawl`, `query`, `find` and `export`
work on a machine with nothing on it but this binary. `amz doctor` checks that
the store is readable along with everything else.

## Empty or odd fields

There is no structured blob to fall back on. amazon.com publishes no JSON-LD, no
`__NEXT_DATA__` and no Apollo cache, so every field comes off a region Amazon
named, a JavaScript payload it ships, an attribute, or a selector, and the record
says which one answered.

So the first thing to look at is the record itself. `envelope.via` names the
region each field came from, `envelope.missed` names the fields that were looked
for and not found and why, and `envelope.unread` lists the regions on the page
that nothing reads yet.

```bash
amz product B075F5X8BR -o json | jq '.[0].envelope.missed'
amz product B075F5X8BR -vv                  # prints where each field came from
amz extraction B075F5X8BR                   # the ladder, and what is unread
amz why <topic>                             # the measurement behind a gap
```

If a field is missing and nothing in `envelope.missed` names it, amz read the
place that field lives and there was nothing there. That is a different answer
from "amz did not look", and telling them apart is the whole point of the
envelope.

Beyond that, the page genuinely differs by marketplace and by where the request
came from. Try a different `-m`. Prices in particular come back in the currency
of whoever is asking: a machine egressing from Vietnam gets dong on amazon.com,
and there is no flag for that because the record says what the page said. Pass
`--raw` to inspect the exact bytes amz parsed.
