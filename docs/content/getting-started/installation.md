---
title: "Installation"
description: "Install amz with go install, a prebuilt binary, a package manager, or Docker."
weight: 20
---

amz is a single static binary. Pick whichever route fits your machine; they all
land the same `amz` on your `PATH`.

## go install

```sh
go install github.com/tamnd/amz-cli/cmd/amz@latest
```

This builds from source and drops `amz` in `$(go env GOPATH)/bin`. Requires Go
1.26 or newer.

## Prebuilt binary

Grab an archive for your OS and architecture from the
[releases page](https://github.com/tamnd/amz-cli/releases), unpack it, and move
`amz` onto your `PATH`. Every release ships archives for Linux, macOS, Windows,
and FreeBSD on amd64 and arm64, plus checksums, SBOMs, and a cosign signature.

## Linux packages

The releases page also carries `.deb`, `.rpm`, and `.apk` packages:

```sh
# Debian / Ubuntu
sudo dpkg -i amz_*_linux_amd64.deb

# Fedora / RHEL
sudo rpm -i amz_*_linux_amd64.rpm
```

The package suggests `duckdb` as an optional dependency for the local store; amz
runs fine without it.

## Homebrew and Scoop

When the taps are live:

```sh
brew install tamnd/tap/amz          # macOS / Linux
scoop bucket add tamnd https://github.com/tamnd/scoop-bucket && scoop install amz
```

## Docker

```sh
docker run --rm ghcr.io/tamnd/amz product B084DWG2VQ
```

Mount a volume at `/data` to keep the cache and local store between runs:

```sh
docker run --rm -v ~/data/amz:/data ghcr.io/tamnd/amz search "usb c cable" -o jsonl
```

## Build from source

```sh
git clone https://github.com/tamnd/amz-cli
cd amz-cli
make build      # produces ./bin/amz
```

## Verify

```sh
amz --version
```

Next, the [quick start](/getting-started/quick-start/) runs the core loop.
