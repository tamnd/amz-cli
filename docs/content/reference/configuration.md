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
amz product B075F5X8BR -m uk
amz info                 # shows the resolved marketplace and access tier
```

## The polite-fetch path

amz is built to read Amazon without hammering it. The defaults:

| Flag | Default | What it does |
| --- | --- | --- |
| `--rate` | 3s | minimum delay between requests |
| `--retries` | 3 | retries on a 429/503 with backoff |
| `--timeout` | 30s | per-request timeout |

Requests carry `amz-cli/<version> (+https://github.com/tamnd/amz-cli)` and two
other headers, never a browser string, and successful pages are cached on
disk so a repeat is free. `--no-cache` bypasses the cache, `--refresh` ignores
the cached copy but repopulates it.

## Access tiers

amz reads three tiers, selected per run:

- **Public HTML**, the default, no setup.
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
the XDG cache directory, with the SQLite store at `<data>/amz.db`. `amz db path`
and `amz cache info` print the resolved locations, and `--data-dir` moves the
whole lot, which is the easy way to keep one store per project.

## Keys that do not exist

`--no-robots` is a flag and only a flag.
There is no `no_robots` config key and no `AMZ_NO_ROBOTS` environment variable, and `amz` has tests asserting that neither ever appears.
A stop signal you can turn off in a file you forgot about is not a stop signal.

The same is true of the pace floor.
`--rate` can raise it and nothing lowers it.
