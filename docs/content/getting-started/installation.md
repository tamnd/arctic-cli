---
title: "Installation"
description: "Install arctic from a release, with go install, from a package manager, as a container, or from source with the optional DuckDB engine."
weight: 20
---

## Prebuilt binaries

Every [release](https://github.com/tamnd/arctic-cli/releases) carries archives
for Linux, macOS, Windows, and FreeBSD on amd64 and arm64, plus deb, rpm, and apk
packages for Linux. Download, unpack, put `arctic` on your `PATH`, done. The
released binary is the pure-Go build, so there is nothing to install alongside
it.

## With Go

```bash
go install github.com/tamnd/arctic-cli/cmd/arctic@latest
```

That puts `arctic` in `$(go env GOPATH)/bin`, which is `~/go/bin` unless you
moved it. Make sure that directory is on your `PATH`.

## Homebrew

```bash
brew install --cask tamnd/tap/arctic
```

## Scoop

```bash
scoop install arctic
```

## Container image

The multi-arch image is on GHCR:

```bash
docker run --rm ghcr.io/tamnd/arctic catalog
```

Mount a volume if you want the downloaded dumps and the index to survive the
container, since arctic writes a fair amount of state under its data directory:

```bash
docker run --rm -v "$PWD/data:/data" -e ARCTIC_DATA_DIR=/data \
  ghcr.io/tamnd/arctic sub golang
```

## From source

```bash
git clone https://github.com/tamnd/arctic-cli
cd arctic-cli
make build              # produces ./bin/arctic (pure Go)
./bin/arctic version
```

The default build is pure Go with `CGO_ENABLED=0` and needs nothing linked
against it. For the optional DuckDB conversion engine, build with the tag:

```bash
make build-duckdb       # cgo build with the DuckDB engine
```

That build adds the `duckdb` engine to `--engine`. The pure-Go binary rejects
`--engine duckdb` with a usage error telling you to build with `-tags duckdb`, so
you always know which one you are running.

## Requirements

- **Go 1.26 or later** to build the pure-Go binary. The released binary has no Go
  requirement.
- **A cgo toolchain** only if you build with `-tags duckdb`. The default build
  needs none.

## Checking the install

```bash
arctic version
```

prints the version, commit, and build date. Then see the detected hardware,
the work budget, and where arctic will keep its state:

```bash
arctic info
```

When that prints sensible paths and a budget, you are ready for the
[quick start](/getting-started/quick-start/).
