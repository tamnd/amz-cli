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
`amz` onto your `PATH`. Every release ships ten archives: Linux on amd64, arm64,
armv7 and 386, and macOS, Windows and FreeBSD on amd64 and arm64. Alongside them
are checksums, an SBOM per archive, and a keyless cosign signature over
`checksums.txt`.

## Linux packages

The releases page also carries `.deb`, `.rpm`, and `.apk` packages:

```sh
# Debian / Ubuntu
sudo dpkg -i amz_*_linux_amd64.deb

# Fedora / RHEL
sudo rpm -i amz_*_linux_amd64.rpm
```

The packages have no dependencies. The binary is pure Go and SQLite is compiled
into it, so there is nothing to install alongside amz for the local store.

## Homebrew and Scoop

When the taps are live:

```sh
brew install --cask tamnd/tap/amz   # macOS
scoop bucket add tamnd https://github.com/tamnd/scoop-bucket && scoop install amz
```

It is a cask and not a formula, so it is macOS only. On Linux use `go install`,
a package, or the archive. Both publish steps self-disable when their token is
absent, which is why "when the taps are live" is a real condition: a release with
no extra secrets still produces every archive and the container image, and each
manager lights up the moment its repository and token exist.

## Docker

```sh
docker run --rm ghcr.io/tamnd/amz product B075F5X8BR
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
