# Public-consumer acceptance

`run.sh` exercises the published Go module as an external consumer. It resolves
one exact root revision from `proxy.golang.org`, builds standalone consumers
outside the checkout, and checks the default, namespace-only, and explicit
library contracts on one native runner. It does not rebuild native code, use
Rust, protoc, a private module, or a `replace`/`go.work` file.

The hosted workflow is `.github/workflows/unix-public-consumers.yml`. It runs
when a root `v*` release tag is pushed, or by manual dispatch that names that
tag in `revision`; it never accepts a commit SHA or pseudo-version. The
canonical root module is `github.com/lance-format/lance-go`; the requested tag
must already be publicly resolvable by the Go module proxy, and the exact
`go.lance.org/liblance/<target>` artifact module versions must already be
publicly resolvable. Exact-head CI
covers the equivalent checkout/local-source consumer behavior instead of
resolving a PR head through the proxy.

## Local invocation

Run on the matching native host from the repository checkout:

```sh
ROOT_REVISION=v0.2.0 \
SOURCE_COMMIT=<optional-full-sha> \
TARGET_GOOS=linux TARGET_GOARCH=amd64 \
EXPECTED_UNAME=x86_64 LIBRARY_NAME=liblance_c.so \
EVIDENCE_DIR=/tmp/public-consumer-evidence \
bash scripts/public-consumer/run.sh
```

For pre-publication/local integration only, set `PUBLIC_GOPROXY` and
`PUBLIC_GOSUMDB` plus `ARTIFACT_MODULE_PREFIX=go.lance.org/liblance`; the
production defaults are `proxy.golang.org`/`sum.golang.org`.

`ROOT_REVISION` is passed to Go only through the environment. `SOURCE_COMMIT`
is optional; when empty, the driver uses the checked-out `HEAD`. A supplied SHA
must equal `HEAD`, and a supplied tag must resolve to `HEAD`.

The three hosted matrix entries are `linux/amd64`, `linux/arm64`, and
`darwin/arm64`. Their runners and expected `uname -m` and library names are in
the workflow. Intel macOS is retired for new releases: a `darwin/amd64`
revision has no selector or artifact requirement, so this driver cannot
complete a default consumer for it. Historical Intel tags keep the original
driver revision of their time.

## What is checked

- The proxy-resolved tag version, `Sum`, `GoModSum`, archive, and source tree at
  the requested tag. The root module archive is compared with `git ls-tree`
  content and blob hashes; the tag's full commit is checked against the source.
- The selected artifact module's own `artifact.go` against the source pin, and
  the downloaded payload size and SHA-256 against that metadata. The selected
  artifact is derived from the built binary, not hardcoded.
- Default consumer: no `LANCE_LIB_PATH`, a cold SDK runtime cache, a standalone
  build from the public module using a fresh empty target module and build
  cache, and the literal roundtrip result
  `Created, reopened and scanned 3 rows: [10 20 30]`. The target build must
  download exactly the selected artifact payload archive. Exactly one unpacked
  library is expected, with the published size, SHA-256, and safe modes.
- Namespace-only consumer: the root package is absent from `go list -deps`; the
  namespace package is present and runs `ID`, `ListNamespaces`, and `Close`.
  Its target build also starts from fresh empty caches and must fetch only the
  selected artifact payload archive.
- External consumer: built with `-tags lance_external_lib` from its own fresh
  empty target caches, with no artifact package, embedded payload, or
  extraction, using an absolute `LANCE_LIB_PATH` copied from the verified
  default library. Artifact payload downloads observed in its target stage are
  recorded; external is not a zero-download contract and no zero-archive rule
  is asserted for it.
- The ordinary `go get` path is real, not simulated. It runs in a separate
  install/get cache and may download every artifact module archive because it
  loads packages for all build tags. The driver records that complete set
  and requires it to match the root module graph; those downloads are not
  attributed to the target build.

All Go commands use `CGO_ENABLED=0`, `GOTOOLCHAIN=local`, `GOENV=off`,
`GOWORK=off`, `GOPROXY=https://proxy.golang.org`, and
`GOSUMDB=sum.golang.org`. Each consumer mode has separate install/get and
target-build module and build caches. Root resolution and `go get` run in the
get caches. Immediately before the target build, the target module and build
caches are rechecked empty; the build cannot reuse setup downloads. The
consumer `go.mod` and `go.sum` produced by `go get` are hashed, then checked
unchanged after target build and target dependency work. The driver records
stage-labeled snapshots in `module-cache-inventory.tsv`; it does not run
`go mod tidy`/`go mod verify`, subtract warm setup downloads, copy module
bytes, or prime the target caches.

The runtime cache root is obtained by running a small Go program that calls
`os.UserCacheDir` under `XDG_CACHE_HOME`, because macOS ignores that variable
and uses `$HOME/Library/Caches`. The driver refuses to start if
`<cache-root>/lance-go` already exists. It moves aside only the cache produced
by its own default and namespace runs; it never deletes an arbitrary user
cache.

## Evidence and limits

Evidence is written under `EVIDENCE_DIR` (`$RUNNER_TEMP/public-consumer-evidence`
in CI): command log, summary, cache lifecycle, module-cache inventory, artifact
metadata, JSON reports, runtime logs, and the exact resolved versions. CI
uploads only this compact directory, not module or build caches.

A passing run is evidence for one public-consumer path at one exact revision;
it is not a full runtime regression, a native rebuild, a release, or support
for an unpublished platform. The native source version remains the packaging
claim recorded in artifact metadata; `lance_c_version()` reports only the
binding release, not the source commit.
`go mod tidy`, `go get`, and other module-graph commands may download every
artifact module archive because Go can load packages for all build tags.
This lane records those installation downloads separately and asserts the
selected-archive boundary only for the default and namespace target builds. It
makes no zero-download claim for `go get` or external mode.
