# Contributing

This repository ships the Go SDK and its prebuilt `lance-c` native bundles.
Default builds and the bundled test paths use `CGO_ENABLED=0` and need no C
toolchain, Rust toolchain or system Lance installation. The
[development reference](docs/development.md) covers the loader, build tags,
native ABI checks and the full test matrix; [RELEASING.md](RELEASING.md) covers
artifact publication, public-consumer acceptance and release handoff.

## Where to look

| Topic | Start here |
| --- | --- |
| Install, quickstart and supported targets | [README.md](README.md) |
| API usage and runnable examples | [docs/api-guide.md](docs/api-guide.md) |
| Build, test, ABI, loader and Go version policy | [docs/development.md](docs/development.md) |
| Native module design, scope and validation lanes | [docs/native-packages.md](docs/native-packages.md) |
| Release, artifacts, public consumers and docs handoff | [RELEASING.md](RELEASING.md) |
| Public-consumer drivers | [Unix](scripts/public-consumer/README.md), [Windows](scripts/public-consumer/WINDOWS.md) |

## Build and test

Use a Go toolchain that satisfies the `go` directive in [`go.mod`](go.mod). The
baseline checks are:

```sh
gofmt -l .
go mod tidy -diff
go vet ./...
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build -tags lance_external_lib ./...
CGO_ENABLED=0 go test -tags lance_external_lib ./...
```

The unit suite does not require a preinstalled native library. On `linux/amd64`
the default build supplies the published bundle; native tests on other targets
and the external-tag suite need a matching library, normally through
`LANCE_LIB_PATH`. The race lane intentionally uses `CGO_ENABLED=1` so Go's race
detector can run against that library. See
[Testing](docs/development.md#testing) for the default, external-library, race
and RTree test commands.

## Compatibility checks

Go API compatibility is compared with the latest stable tag by
[`scripts/check-api-compat.sh`](scripts/check-api-compat.sh). The script needs a
full clone with tags; pass a tag as its first argument to choose another base.
An incompatible change passes only through the reviewed `apidiff/<base-tag>.txt`
allowlist and migration notes for an approved minor release. See
[API compatibility review](RELEASING.md#api-compatibility-review).

The native ABI is pinned in
[`internal/ffi/upstream.go`](internal/ffi/upstream.go). The native workflows run
the library and header checks; a published or source-built library can be
checked locally with:

```sh
./scripts/check-native-library.sh --library /path/to/liblance_c.so \
  --header /path/to/include/lance/lance.h --target linux/amd64
LANCE_C_HEADER=/path/to/include/lance \
  go -C internal/tools run ./check-abi-symbols
```

`check-native-library.sh` accepts Linux amd64/arm64 and macOS arm64 targets.
`check-abi-symbols` accepts a legacy single header or the split `include/lance`
directory; the composite identity defaults to the pin when it records one. See
[Pinned ABI and verification](docs/development.md#pinned-abi-and-verification).
A pin change requires the full companion release sequence in
[RELEASING.md](RELEASING.md).

## Consumer verification

The loader performs no network I/O. Go module resolution downloads the selected
artifact, so a build from a warm module cache and the resulting binary need no
network. The acceptance drivers start from cold caches; a warm-cache run does
not replace them.

On `linux/amd64`, the exact-head quickstart check builds a consumer module
outside the checkout and covers the bundled library plus the read-only
`LANCE_LIB_PATH` path:

```sh
LANCE_GO_QUICKSTART_SOURCE_DIR="$PWD" ./scripts/verify-quickstart.sh
```

An external consumer builds with `-tags lance_external_lib`, sets an absolute
`LANCE_LIB_PATH`, and must not link, embed or extract the artifact package. Its
library must come from the same SDK release. Public acceptance runs from an
exact published tag through the maintained
[Unix](scripts/public-consumer/README.md) or
[Windows](scripts/public-consumer/WINDOWS.md) driver; those guides define the
checks and cache requirements.

## Pull requests and release handoff

Keep a pull request focused and describe the behavior change, the commands you
ran, and any platform or native coverage you could not exercise. CI and the
workflow files are the source of truth for required checks.

Version-specific release notes and `CHANGELOG.md` entries are written during
release handoff. A change that needs a new native chain, a reviewed API
migration or a release must follow [RELEASING.md](RELEASING.md) end to end. Do
not create release tags or publish artifact modules from a pull request.
