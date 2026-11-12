# Releasing native bundles

This is the maintainer sequence for a preview release that includes a compiled
`lance-c` bundle. It documents the existing build, ABI, generator, and loader
contracts; it does not add release infrastructure. It is not authorization to
tag or publish.

Four Unix targets now have published artifact modules and active selectors:
`linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64`. The
`linux/amd64` bundle has prior native, default, external, and race evidence.
The three companion modules were adopted for validation; their hosted native
cold public-consumer acceptance is pending, so publication is not a companion
support claim. Windows remains `BLOCKED`. Real AWS S3 refresh passed in both
direct-accessor and operator-owned namespace modes at `59a427e` through PR #37;
this does not claim commercial namespace-service or production-account support.
Release notes may claim only the evidence recorded for the claimed target; they
must not claim that issue #1 is complete.

The Unix workflow proposed in PR #40 builds and tests four native runners and
uploads staging modules as CI evidence. A successful staging run does not
publish a production module or activate a selector. Use the current workflow
and support status in [artifacts/README.md](artifacts/README.md); record results
for the exact release candidate rather than copying an earlier status table.

## Release units and order

```text
root module          $ROOT_MODULE                         tag v...
linux/amd64 module   $ROOT_MODULE/artifacts/linux-amd64   tag artifacts/linux-amd64/v...
selector             internal/bundled/lance_bundle_linux_amd64.go
root requirement     $ROOT_MODULE/artifacts/linux-amd64   $ARTIFACT_VERSION
```

Repeat the artifact/selector/requirement rows for each published Unix target.
The current companion versions and payload facts are recorded in
[artifacts/README.md](artifacts/README.md). Publish in this order: verify the
native source and header, build and validate the library, generate and verify
the production artifact module, publish that module and resolve its exact
version through the public proxy and checksum database, then activate the
selector and root requirement, test, and only after an authorized readiness
decision create the root tag and release.

Never activate an `artifacts/staging/...` selector. Staging modules are
validation uploads, not published targets.

Run the examples in one Bash shell from the repository root. The examples use
GNU `sha256sum`; on macOS install that tool or use `shasum -a 256`. Values
marked as required must come from the release record.

```bash
set -euo pipefail
unset GOFLAGS GORACE
REPO_ROOT=$(pwd)
```

## 1. Choose and verify the native input

`internal/ffi/upstream.go` is the pin for the native repository, commit, and
header hash. `upstreamRelease` is informational; it does not prove that a
release archive, tag, and commit contain the same header. The pinned commit and
the `v0.1.9` archive have different headers and ABI; do not substitute
`v0.1.9` for the pin.

Version selection is a maintainer decision tracked by issue #1. Check the
actual native release, tag, and commit alignment before choosing a version. Do
not select the latest tag automatically, and do not substitute a release
archive for the pinned commit.

```sh
: "${NATIVE_SRC:?path to the lance-c checkout}"
: "${NATIVE_COMMIT:?full 40-hex native commit}"
: "${HEADER_SHA256:?value from internal/ffi/upstream.go}"

git -C "$NATIVE_SRC" fetch origin "$NATIVE_COMMIT"
if [ -n "${NATIVE_TAG:-}" ]; then
  git -C "$NATIVE_SRC" fetch origin "refs/tags/$NATIVE_TAG:refs/tags/$NATIVE_TAG"
  test "$(git -C "$NATIVE_SRC" rev-parse --verify "$NATIVE_TAG^{commit}")" = "$NATIVE_COMMIT"
fi
test "$(git -C "$NATIVE_SRC" show "$NATIVE_COMMIT:include/lance/lance.h" | sha256sum | awk '{print $1}')" = "$HEADER_SHA256"
```

If a claimed tag does not peel to the commit, or the header hash differs, stop.
A preview may use an exact unreleased native commit. In that case leave
`NATIVE_TAG` unset and record the absence of a matching release tag explicitly;
do not label the binary with an older native release. Record the native
repository, commit, optional tag, and header hash.

Root and artifact versions are independent module versions. The root module
path has no `/vN` suffix, so root tags must remain `v0` or `v1`. A companion
module tag includes the module path suffix. The following is syntax only, not a
chosen release version:

```bash
REPO_ROOT=$(pwd)
ROOT_VERSION=v0.1.0-preview.1       # example
TARGET=linux/amd64                  # example
SLUG=${TARGET//\//-}
ARTIFACT_VERSION=v0.1.0-preview.1   # example
# root tag:            v0.1.0-preview.1
# companion tag:       artifacts/linux-amd64/v0.1.0-preview.1
```

A companion module tag is always `<module-path-suffix>/<version>`. For a
module published as `$ROOT_MODULE/artifacts/$SLUG`, that suffix is
`artifacts/$SLUG`; the root tag has no prefix.

A pseudo-version is resolved from a pushed commit, not created as a tag. Use
the exact `Version` returned by `go mod download -json`; do not construct one
from a timestamp or copy an old example. Read the currently selected companion
module version from `go.mod`.

```sh
ROOT_MODULE=$(sed -n 's/^module[[:space:]]*//p' go.mod)
ARTIFACT_MODULE="$ROOT_MODULE/artifacts/$SLUG"
```

The root module and the generated artifact module use Go 1.23.0. The artifact
module declares no requirements and contains only `embed` data.

## 2. Build, check, and record native acceptance

For Linux and macOS, build from the pinned commit into an output directory
outside the checkout:

```sh
: "${NATIVE_OUT:?new output directory}"
scripts/build-lance-c.sh --out "$NATIVE_OUT"
NATIVE_OUT=$(cd "$NATIVE_OUT" && pwd -P)
NATIVE_LIB=$(find "$NATIVE_OUT" -type f \( -name liblance_c.so -o -name liblance_c.dylib \) -print -quit)
NATIVE_HEADER="$NATIVE_OUT/include/lance/lance.h"
```

`build-lance-c.sh` checks out `upstreamCommit`, verifies the source and
installed header hashes, builds, and installs. It does not strip the library.
Keep the original build for diagnostics. Prepare a separate stripped packaging
copy using the target toolchain, verify that its exported bindings and runtime
dependencies are unchanged, and run a consumer against that copy. Set
`NATIVE_LIB` to this verified packaging copy. Record both hashes and prove that
the payload decompresses byte-for-byte to the packaging input. The generator
packages the library it is given; it does not strip it.

Normalize the selected packaging copy before changing directories for consumers:

```bash
test -f "$NATIVE_LIB"
NATIVE_LIB=$(cd "$(dirname "$NATIVE_LIB")" && printf '%s/%s\n' "$(pwd -P)" "$(basename "$NATIVE_LIB")")
```

Windows needs its own native DLL build and runtime evidence. The Unix build and
ELF/Mach-O checker below do not validate PE imports, architecture, or execution.

Check the bindings against the installed header:

```sh
LANCE_C_HEADER="$NATIVE_HEADER" go run ./scripts/check-abi-symbols.go
```

When the PR #40 checker is available on the release branch, run:

```sh
scripts/check-native-library.sh \
  --library "$NATIVE_LIB" --header "$NATIVE_HEADER" --target "$TARGET"
```

It checks the pinned header hash, ELF or Mach-O object format and
architecture, RPATH/RUNPATH, direct dependencies, the libc or macOS deployment
floor, and exported binding symbols. Until then, those checks are manual and
must be recorded before accepting a platform.

Record acceptance per claimed platform. The table records the state to capture
for a release, not a claim that a future release already has every cell. Keep
these states separate; one does not imply another:

| Target | Native ABI | Default runtime | External runtime | Race/checkptr | Cross-compile | Public archive |
| --- | --- | --- | --- | --- | --- | --- |
| `linux/amd64` | record | record | record | record | record | record |
| `linux/arm64` | record | record | record | record | record | record |
| `darwin/amd64` | record | record | record | record | record | record |
| `darwin/arm64` | record | record | record | record | record | record |
| `windows/amd64` | record | record | record | record | record | record |
| `windows/arm64` | record | record | record | record or toolchain limitation | record | record |

Fill each cell with its tested object, result, and evidence, or an explicit
pending/unsupported state. This is an evidence template, not a support claim.
Native88 exports 876 `lance_*` symbols; the Go candidate `9912e80` binds 603 of
them. Record both counts separately. The ABI checker validates the bound subset;
it does not establish runtime coverage of every native export.

The `linux/amd64` library was built on Ubuntu 24.04.5 with glibc 2.39 and
requires libc, libm, libgcc_s, and the dynamic loader. Older glibc, musl,
Windows, and other architectures have no compatible bundle claim. The real S3
acceptance above is scoped to Linux amd64 and the paired native88 library.
Source review is a separate state: it can justify
understanding of the native change, but it is not ABI or runtime acceptance.

A race run must use a supplied library; record whether it used the default
selector or the external tag. For the external contract, run
`CGO_ENABLED=1 go test -race -gcflags=all=-d=checkptr=1 -tags lance_external_lib ./...`
with the matching `LANCE_LIB_PATH`. Native Rust internals are outside Go's race
detector.
The default lane must run with `LANCE_LIB_PATH` unset, a fresh
`XDG_CACHE_HOME`, and the expected uncompressed hash. See
[README Testing](README.md#testing) for the existing commands rather than
duplicating the full matrix here.

## 3. Generate and verify the production artifact module

Generate the production module, not a staging module, outside the checkout:

```sh
: "${ARTIFACT_OUT:?new output directory outside the checkout}"
scripts/sync-lance-c.sh \
  --library "$NATIVE_LIB" \
  --header "$NATIVE_HEADER" \
  --target "$TARGET" \
  --module-path "$ARTIFACT_MODULE" \
  --out "$ARTIFACT_OUT"
```

The output contains a data-only `module/` (with `go.mod`, `artifact.go`, and the
compressed payload) and a `selector/` file for `internal/bundled`.

The Unix workflow uses `--module-path "$ROOT_MODULE/artifacts/staging/$SLUG"`
and uploads the result with seven-day retention. That module is evidence, not a
published target. If a staging module was produced, discard its selector and
regenerate `artifacts/$SLUG` from the verified library and header with the
production module path.

Inspect the generated module before publishing:

```sh
cat "$ARTIFACT_OUT/module/go.mod"
sed -n '1,40p' "$ARTIFACT_OUT/module/artifact.go"
PAYLOAD=$(find "$ARTIFACT_OUT/module" -type f -name '*.zst' -print -quit)
wc -c "$PAYLOAD"
sha256sum "$PAYLOAD"
```

Check that the module path, target, library name, compressed and uncompressed
sizes and hashes, native commit, and header hash match the build record. The
commit and header hash are packaging claims: `lance-c` has no runtime version
symbol, so the loader cannot prove them from the shared library.

The PR #40 verifier checks the payload hash and size, decompresses it,
compares it byte-for-byte with the library, and checks the uncompressed hash,
pin, and module path. It also checks that `Payload []byte` embeds that exact
archive and that the module contains only the allowed data files:

```sh
go run ./scripts/verify-lance-c-artifact \
  -module "$ARTIFACT_OUT/module" \
  -library "$NATIVE_LIB" \
  -target "$TARGET" \
  -module-path "$ARTIFACT_MODULE"
```

If the verifier is not present on the release branch, preserve equivalent
module-layout, embed, size, hash, decompression, and byte-comparison evidence
from the packaging run.

The tracked payload is the compressed archive. The current `linux/amd64`
payload is 85,642,384 bytes (81.7 MiB) compressed and 322,169,936 bytes
(307.2 MiB) uncompressed; the raw library is never committed. GitHub rejects
ordinary Git blobs over 100 MiB, and warns above 50 MiB. Keep each tracked
compressed payload below that hard limit. A future target whose compressed
payload exceeds it needs a separate storage decision; do not silently switch
formats or move the payload outside the module contract.

The tracked payload under `artifacts/$SLUG/` must stay byte-identical to the
published module archive. Preserve and record both sizes and both hashes.

## 4. Publish the artifact and resolve its exact version

Commit the generated `artifacts/$SLUG` module on the task branch and push the
branch. Record that full SHA as `ARTIFACT_COMMIT`. Publish the companion module
with a nested tag, or resolve a pseudo-version from that pushed commit. For the
tagged path:

```sh
: "${ARTIFACT_VERSION:?version chosen for this release}"
: "${SLUG:?target slug}"
: "${ARTIFACT_COMMIT:?verified pushed artifact commit}"
git tag -a "artifacts/$SLUG/$ARTIFACT_VERSION" -m "artifacts/$SLUG $ARTIFACT_VERSION" "$ARTIFACT_COMMIT"
git push origin "refs/tags/artifacts/$SLUG/$ARTIFACT_VERSION"
```

The tag and release commands in this guide are examples. Run them only as the
authorized publication step, with the expanded values checked first. For a
pseudo-version, skip the nested tag and resolve `$ARTIFACT_MODULE@$ARTIFACT_COMMIT`
from the pushed commit, then use the returned `Version` as `ARTIFACT_VERSION`
in the archive path and root requirement. The companion module tag must resolve
to `ARTIFACT_COMMIT`; verify the remote tag after pushing it.

Resolve the exact version through the public proxy and checksum database before
touching the selector or root requirement:

```sh
PROBE_WORK=$(mktemp -d)
mkdir -p "$PROBE_WORK/probe" && cd "$PROBE_WORK/probe"
go mod init release-probe
env -u GOPRIVATE -u GONOPROXY -u GONOSUMDB \
  GOENV=off GOWORK=off GOMODCACHE="$PROBE_WORK/modcache" \
  GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org \
  go mod download -json "$ARTIFACT_MODULE@$ARTIFACT_VERSION" | tee "$PROBE_WORK/artifact.json"
cd "$REPO_ROOT"
```

Record `Version`, `Sum`, `GoModSum`, and `Zip` from that output. A missing
checksum entry or a version that only resolves from a local cache is a stop
condition. Download the public archive and compare its payload with the tracked
payload; the zip entry is under the module and version prefix:

```sh
PAYLOAD=$(find "$ARTIFACT_OUT/module" -type f -name '*.zst' -print -quit)
ZIP=$(sed -n 's/.*"Zip": "\([^"]*\)".*/\1/p' "$PROBE_WORK/artifact.json")
PAYLOAD_NAME=$(basename "$PAYLOAD")
test -n "$ZIP"
unzip -p "$ZIP" "$ARTIFACT_MODULE@$ARTIFACT_VERSION/$PAYLOAD_NAME" > "$PROBE_WORK/published.zst"
cmp "$PROBE_WORK/published.zst" "artifacts/$SLUG/$PAYLOAD_NAME"
sha256sum "$PROBE_WORK/published.zst"
```

The compressed payload hash must match both the module metadata and the
tracked file.

## 5. Activate the selector and root requirement

Only after the public module and checksum entry are confirmed, copy the
production selector into `internal/bundled`, add the exact requirement, and
update `go.sum`:

```bash
cd "$REPO_ROOT"
SELECTOR="lance_bundle_$(printf '%s' "$TARGET" | tr / _).go"
cp "$ARTIFACT_OUT/selector/$SELECTOR" internal/bundled/
GOWORK=off go mod edit -require="$ARTIFACT_MODULE@$ARTIFACT_VERSION"
GOWORK=off go mod tidy
```

Check the selector build constraint is `!lance_external_lib && <goos> &&
<goarch>`, its import path is the production module, and no staging path
remains:

```sh
if grep -R "artifacts/staging" internal/bundled go.mod go.sum; then
  echo "staging path remains; use the production module" >&2
  exit 1
fi
```

The command should produce no matches. Also confirm there is no `replace`
directive for the root or artifact module, and that `go.sum` contains the
recorded `Sum` and `GoModSum`. Update `artifacts/README.md` with the same
target, payload sizes and hashes, native commit, header hash, and published
requirement.

Commit and push the activation commit before running the consumer. The public
proxy cannot fetch an unpublished commit. Set `ROOT_COMMIT` to the full pushed
activation commit, then verify that the consumer resolves exactly that object.

Test with fresh module, build, and runtime caches, `GOWORK=off`, and no
`replace` directive. Default consumers must not set `LANCE_LIB_PATH`:

```sh
CONSUMER_WORK=$(mktemp -d)
CONSUMER_MODCACHE="$CONSUMER_WORK/consumer-modcache"
CONSUMER="$CONSUMER_WORK/consumer"
mkdir -p "$CONSUMER" && cd "$CONSUMER"
go mod init release-consumer
env -u GOPRIVATE -u GONOPROXY -u GONOSUMDB \
  GOENV=off GOWORK=off GOMODCACHE="$CONSUMER_MODCACHE" \
  GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org \
  go get "$ROOT_MODULE@$ROOT_COMMIT"
env -u LANCE_LIB_PATH CGO_ENABLED=0 GOWORK=off \
  GOMODCACHE="$CONSUMER_MODCACHE" GOCACHE="$CONSUMER_WORK/buildcache" \
  XDG_CACHE_HOME="$CONSUMER_WORK/runtime" \
  go run "$ROOT_MODULE/examples/roundtrip"
```

Each invocation allocates a new `CONSUMER_WORK`, including the post-tag check.
Use `@$ROOT_VERSION` instead of `@$ROOT_COMMIT` for that check. Also
build a consumer that imports only `$ROOT_MODULE/namespace`: the standalone
namespace package must pull in the selector and select the bundle when
`LANCE_LIB_PATH` is unset. Run one namespace operation, not just a compile.

External consumers use `-tags lance_external_lib` and a matching
`LANCE_LIB_PATH`:

```sh
LANCE_LIB_PATH="$NATIVE_LIB" CGO_ENABLED=0 GOWORK=off \
  GOMODCACHE="$CONSUMER_MODCACHE" GOCACHE="$CONSUMER_WORK/buildcache" \
  XDG_CACHE_HOME="$CONSUMER_WORK/runtime-external" \
  go build -tags lance_external_lib -o "$CONSUMER_WORK/consumer-external" \
  "$ROOT_MODULE/examples/roundtrip"
LANCE_LIB_PATH="$NATIVE_LIB" XDG_CACHE_HOME="$CONSUMER_WORK/runtime-external" \
  "$CONSUMER_WORK/consumer-external"
go version -m "$CONSUMER_WORK/consumer-external"
```

The external tag excludes the selector, `bundle.go`, `cache.go`,
`lock_unix.go`, the decompression path, and the artifact package from the
compiled dependency graph and embedding. It does not remove the artifact from
`go.mod` or the module graph; `go get` and module-graph commands may still
download the archive. This is not a zero-download contract. Confirm the built
binary and its runtime do not use the bundle: `go version -m` must not list the
artifact module, and no extraction directory should appear.

### Public-consumer acceptance

The maintained public-consumer lane is
`.github/workflows/unix-public-consumers.yml` with the driver in
`scripts/public-consumer/run.sh`. It runs on Linux and macOS amd64 and arm64
runners, resolves one exact root revision through `proxy.golang.org` and
`sum.golang.org`, and builds standalone default, namespace-only, and external
consumers outside the checkout. It verifies the root archive and source tree,
the artifact selected by the built binary, payload and extracted-library
bytes, cache modes, and the roundtrip result. It does not rebuild native code.

A pull request uses the exact PR head commit; manual dispatch takes a full
commit SHA or checked-out tag. Run it locally on the matching native host:

```sh
ROOT_REVISION=<full-sha-or-tag> \
TARGET_GOOS=linux TARGET_GOARCH=amd64 \
EXPECTED_UNAME=x86_64 LIBRARY_NAME=liblance_c.so \
bash scripts/public-consumer/run.sh
```

`ROOT_REVISION` is passed through the environment, never interpolated into
shell code. The driver refuses an existing `<os.UserCacheDir>/lance-go` cache,
uses separate module/build caches for each consumer mode, and moves aside only
the SDK cache it produced. It records ordinary target build downloads rather
than assuming that only one artifact archive can ever be fetched. The evidence
directory contains compact logs and JSON manifests, not module or build caches.
See [scripts/public-consumer/README.md](scripts/public-consumer/README.md) for
the full checks and limits. This lane is public-distribution acceptance, not a
full runtime regression.

## 6. Evidence, notes, and authorized release

CI uploads native and race evidence with seven-day retention, and the Unix
workflow uploads staged modules with the same retention. Those files are not
the release record. Before tagging, preserve durable evidence in the existing
report location; after creating the release, attach the evidence files. Record
content hashes in the release notes. Preserve an immutable record of those
hashes and exact objects; expiring CI artifacts are not sufficient. Do not
invent a separate release database.

The evidence record must identify the exact objects:

```text
root:             commit, tag, module path
artifact:         commit, tag or pseudo-version, module path, go.sum entries
native:           repository, commit, tag or explicit absence, header SHA-256
library:          bytes, SHA-256, packaging source
payload:          compressed and uncompressed bytes and SHA-256
public archive:   proxy zip comparison and checksum-database result
public consumer:  exact root/artifact versions, selected payload and extracted library hashes, result
platform:         target, ABI/runtime/race/default/external results, floor and dependencies
gaps:             untested targets and storage paths
```

The current `linux/amd64` bundle, native `ac2634c553b450c7787577ffe964fc2b283b8334`,
header `c19789e9e003930c6065181b5cd9f6f7b464e08a5dc4daca8b21ad82d5160f5b`,
payload `e933fc867b2a9ee404d01959aaef8dc8378ddf0b582d7bb2367ed3dbfe46c5b5`,
and uncompressed library
`e5571d6da16e81a0434f2509ade5262785b63df0b37ff7155537642841dd9378` are
current pin examples, not a released preview. The published companion versions,
payload sizes, and hashes are listed in [artifacts/README.md](artifacts/README.md);
their cold public-consumer acceptance remains pending.

Prepare release notes from that record. Include the artifact version, payload
sizes and hashes, evidence location, supported bundle target and runtime floor,
the default and external behavior, and what remains pending. Do not claim
Windows or other Unix runtime support or completion of issue #1 without the
corresponding accepted evidence. Describe S3 acceptance with its tested source,
native library, and self-hosted namespace boundary.

This document is not release authorization or acceptance. A maintainer decides
readiness and authorizes the preview tag only after the evidence above exists
for every claimed platform, the exact artifact version is publicly resolvable,
and the working tree is clean. Do not tag while native alignment or public
archive verification is unresolved.

```sh
cd "$REPO_ROOT"
: "${ROOT_VERSION:?authorized root version}"
: "${ROOT_COMMIT:?verified release commit}"
test -z "$(git status --porcelain)"
test "$(git rev-parse HEAD)" = "$ROOT_COMMIT"
git tag -a "$ROOT_VERSION" -m "release $ROOT_VERSION" "$ROOT_COMMIT"
git push origin "refs/tags/$ROOT_VERSION"
```

Read back the remote tag and verify that it peels to `ROOT_COMMIT`.
Create the release for that pushed root tag, mark it as a pre-release when
appropriate, and attach the evidence. Do not move or reuse a published version
to hide a mismatch; document the failure and fix forward.

After publication, verify the fresh public path in a new module cache with
`GOWORK=off` and no `replace`: fetch the tagged root and the exact artifact
version from the public proxy, run the default example, run the external
example with the matching library, and re-check the archive payload bytes and
checksum-database entry. Run the entire section 5 consumer block again, beginning with a new
`CONSUMER_WORK=$(mktemp -d)`, and use `@$ROOT_VERSION`. Do not reuse the
pre-tag module, build or runtime cache.
Record those post-publication results with the release.

## 7. Reusing existing evidence

The roundtrip example, `compat/`, and `benchmarks/soak` results may be cited only
when the relevant code, dependencies, and configuration are unchanged since the
run, and only for the exact commits they exercised. A docs-only delta does not
require mechanically rerunning a 30-minute soak or four foreign SDK builds.
State the reused evidence, its commit, and its coverage limits instead.

Cross-compilation is not runtime acceptance. Source review is not binary
acceptance. A public archive check is not a native runtime check. Keep those
states separate in the release record.
