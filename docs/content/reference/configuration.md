---
title: "Configuration"
description: "Marketplaces, the polite-fetch defaults, access tiers, paths, and environment variables."
weight: 30
---

amz runs with sensible defaults and no config file. Everything below is
overridable per command with a flag, and the common settings can be pinned in a
config file or environment.

## Marketplaces

`-m` / `--marketplace` selects the storefront. amz knows the major Amazon
marketplaces by short slug:

```
us  uk  de  fr  it  es  ca  jp  in  au  br  mx  nl  se  pl  sg
```

Each slug sets the host, currency, and language for every URL amz builds. An
unknown slug is a usage error (exit 2).

```bash
amz product B084DWG2VQ -m uk
amz info                 # shows the resolved marketplace and access tier
```

## The polite-fetch path

amz is built to read Amazon without hammering it. The defaults:

| Flag | Default | What it does |
| --- | --- | --- |
| `--rate` | 3s | minimum delay between requests |
| `--retries` | 3 | retries on a 429/503 with backoff |
| `--timeout` | 30s | per-request timeout |
| `--workers` | 2 | concurrency for multi-page and bulk work |

Requests carry a rotating browser user agent, and successful pages are cached on
disk so a repeat is free. `--no-cache` bypasses the cache, `--refresh` ignores
the cached copy but repopulates it.

## Access tiers

amz reads three tiers, selected per run:

- **Public HTML**, the default, no setup.
- **Cookied**, `--cookies file`, lends a signed-in session.
- **PA-API**, `--api`, uses the official Product Advertising API 5.0 with
  credentials, signed locally.

## Configuration file

`amz config` manages an optional TOML file:

```bash
amz config path          # where it lives
amz config init          # write a starter file
amz config show          # the resolved configuration (credentials masked)
```

The file lives under the XDG config directory (`~/.config/amz/` on Linux,
`~/Library/Application Support/amz/` style paths on macOS via XDG).

## Environment variables

| Variable | Effect |
| --- | --- |
| `AMZ_DATA_DIR` | root for the local store and database |
| `AMZ_CACHE_DIR` | the on-disk page cache |
| `AMZ_PAAPI_ACCESS_KEY` | PA-API access key |
| `AMZ_PAAPI_SECRET_KEY` | PA-API secret key |
| `AMZ_PAAPI_PARTNER_TAG` | PA-API partner tag |
| `AMZ_BASE_URL` | override the base URL (testing and proxies) |
| `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_CONFIG_HOME` | standard XDG paths |

## Paths

By default amz keeps its data under the XDG data directory and its cache under
the XDG cache directory, with the DuckDB store at `<data>/amz.duckdb`. `amz db
path` and `amz cache info` print the resolved locations.
