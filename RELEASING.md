# Releasing native bundles

This is the maintainer sequence for a release that includes a compiled
`lance-c` bundle. It documents the existing build, ABI, generator, and loader
contracts; it does not add release infrastructure. It is not authorization to
tag or publish.

The native packages are published as ordinary Go modules at
`go.lance.org/liblance/<target>@<artifact-version>`, not as Git tags, from an
official `lance-c` release. `internal/ffi/upstream.go` is the native source pin;
the companion module versions required in `go.mod` are a separate, SDK-selected
immutable origin distribution, and a source pin bump does not name them. All
five active selectors and root requirements use those static module versions.
The five release archives and checksums were verified, each module payload
decompresses byte-for-byte to the official archive library, and every module
resolves through the public proxy and checksum database. Accepted exact-version
evidence belongs to the release record. The source pin and the selected
companion identities are in `internal/ffi/upstream.go`, `go.mod`, the generated
`artifact.go` and the identity manifests;
[docs/native-packages.md](docs/native-packages.md) describes the module design
and validation.

Intel macOS is retired and has no module or selector. Each new tag still
requires its own post-tag runs. Real AWS S3 refresh passed in both
direct-accessor and operator-owned namespace modes at `59a427e` through PR #37.
That is historical evidence against the `native88` library, not against the
source pin; it does not claim commercial namespace-service or
production-account support. Release notes may claim only the evidence recorded
for the claimed target. They must not claim completion of issue #1; the broader
RTree/raw v2.2 official-core work remains outside this release.

The maintained Unix workflow, `.github/workflows/unix-native.yml`, builds and
tests three native runners and generates unpublished candidate modules for
their real static module paths. The workflow uploads those candidates as review
evidence; it does not publish a production module, create a Git tag, or activate
a selector. A build/test success is not acceptance: candidate verification must
also pass. The separate public-consumer workflow verifies published module
identity and cold runtime behavior. Static module publication is performed by
`.github/workflows/publish-go-module-origin.yml`; use
[scripts/go-module-origin/README.md](scripts/go-module-origin/README.md) for
the exact identity and publication procedure. That workflow and its identity
are pinned to the reviewed publication selected in its `ORIGIN_*` inputs. Before
dispatch, confirm `ORIGIN_VERSION`, `ORIGIN_SOURCE_COMMIT`,
`ORIGIN_SOURCE_RELEASE`, `ORIGIN_MANIFEST_SHA256`, the run IDs and the matching
`scripts/go-module-origin/<version>.identity.json`. `v0.2.0-preview.1` remains
the first recorded example. A later native version requires new pinned candidate
runs, a new reviewed identity file and updated workflow inputs; record results
for the exact release candidate rather than copying an earlier status table.

## Release units and order

```text
root module          $ROOT_MODULE                         tag <root-tag>
linux/amd64 module   go.lance.org/liblance/linux-amd64      <artifact-version>
selector             internal/bundled/lance_bundle_linux_amd64.go
root requirement     go.lance.org/liblance/linux-amd64      <artifact-version>
```

Repeat the artifact/selector/requirement rows for each published target.
Artifact modules are immutable static module versions, not Git tags. Publish in
this order: verify the native source and header, build and validate the library,
generate and verify all artifact modules, publish the static modules and resolve
their exact versions through the public proxy and checksum database, then
activate the selectors and root requirements, test, and only after an authorized
readiness decision create the root tag and release.

A candidate module produced by the Unix workflow is unpublished evidence.
Never point a selector or root requirement at a candidate commit or candidate
module; wait until the exact version is published and resolves through the
public proxy and checksum database.

Run the examples in one Bash shell from the repository root. The examples use
GNU `sha256sum`; on macOS install that tool or use `shasum -a 256`. Values
marked as required must come from the release record.

```bash
set -euo pipefail
unset GOFLAGS GORACE
REPO_ROOT=$(pwd)
```

## Documentation handoff

Version-specific consumer documentation belongs to the same release handoff as
the root version and native input, but its durable home is the immutable GitHub
release notes and the tag-pinned `CHANGELOG.md`. Evergreen docs must not repeat
the current release's tag, native chain, sizes or hashes. Do not derive the
release notes from an unversioned `main` checkout or publish them before the
exact modules exist.

- README and [`docs/api-guide.md`](docs/api-guide.md): evergreen install and
  quickstart (`@latest`), the unversioned pkg.go.dev reference, and links to the
  release notes and changelog. Do not add a release tag, native release
  sentence, exact native mapping or library hash to the README or guide; those
  live in the release notes, changelog, `go.mod`, `internal/ffi/upstream.go`
  and the generated artifact metadata.
- [`docs/development.md`](docs/development.md): the "Building liblance_c" Rust
  requirement and matching native build guards, the "Pinned ABI" reproducible
  command, and the "Testing" section's RTree command when those contracts
  change.
- [`docs/native-packages.md`](docs/native-packages.md): module architecture,
  target and runtime scope, formats and validation lanes. Point to the release
  notes, changelog, `go.mod`, `internal/ffi/upstream.go`, generated
  `artifact.go` and identity manifests for checkout-specific values; do not
  duplicate their versions or hashes.
- `scripts/verify-quickstart.sh` version, module and version pins, published
  module archive size, and expected library size and SHA-256. Keep the module
  archive size separate from the compressed native payload. These are
  functional test pins, not user-facing documentation.
- Go version declarations when the supported minimum changes: `go.mod`, the Go
  matrices in `.github/workflows/ci.yml` and `windows.yml`, the artifact module
  generators, the public-consumer toolchain assertions, and the version support
  text in README and `docs/development.md`. State the minimum and the tested Go
  versions in the release notes and migration notes.
- `apidiff/<base-tag>.txt` and the new release's migration notes when the
  release carries reviewed API changes. Confirm the allowlist matches the
  exported API and the notes cover every caller-visible migration.

The release handoff owns the beta/preview version wording; a docs-only change
does not weaken the runtime acceptance requirements below.

### API compatibility review

When a minor release carries reviewed Go API changes, `apidiff/<base-tag>.txt`
records the incompatible entries against the previous stable tag. The review
material is the release's migration notes and the relevant API guide sections.
Before the stable decision:

```sh
: "${BASE_TAG:?set to the previous stable tag}"
./scripts/check-api-compat.sh "$BASE_TAG"
```

Confirm every entry still matches the exported API and every caller-visible
migration is documented. The allowlist is scoped to its base tag; the new stable
tag becomes the next comparison base. A patch line must not add an allowlist.
CI runs this gate on pull requests and manual dispatch, not on main pushes,
because approved deltas predate the new tag.

## 1. Choose and verify the native input

`internal/ffi/upstream.go` is the pin for the native repository, commit, and
header hash. `upstreamRelease` is informational; it does not prove that a
release archive, tag, and commit contain the same header. The recorded
`v0.2.0-preview.1` publication used the annotated `v0.2.1-beta.3` tag, peeled to
`a800fd5f28636bfff34778e3ad6663f3cf099a38`, with canonical header
`6bab6899ce240b93cf47a1c93f7fb4c8bb2b20ba0a82e3c4a0ec04e7bfc915ad`. For a new
pin, the constants in `internal/ffi/upstream.go` are authoritative; the values
above remain the historical v0.2.0-preview.1 example. Do not substitute another
release archive for the pin you are verifying. The selected `Cargo.toml`
declares `rust-version = "1.95.0"`; `scripts/build-lance-c.sh`, `ci.yml`, and
`unix-native.yml` reject older Rust toolchains before building.

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
path has no `/vN` suffix, so root tags must remain `v0` or `v1`. Artifact modules
are static module versions at `go.lance.org/liblance/<target>`; they do not use
Git tags. The following is syntax only, not a chosen release version:

```bash
REPO_ROOT=$(pwd)
ROOT_VERSION=vX.Y.Z                  # placeholder root tag
TARGET=linux/amd64                  # example
SLUG=${TARGET//\//-}
ARTIFACT_MODULE="go.lance.org/liblance/$SLUG"
ARTIFACT_VERSION=vX.Y.Z-preview.N   # placeholder static module version
```

A Go-only release may reuse the existing immutable artifact module versions
and checksums when the native bytes are unchanged. Any native byte
change, including a new `lance-c` pin or a rebuilt library, requires new
artifact module versions; never replace the bytes behind a published static
module version.

```sh
ROOT_MODULE=$(sed -n 's/^module[[:space:]]*//p' go.mod)
```

The root module, the nested `internal/tools` module and the generated artifact
module use the Go floor declared in `go.mod`. The artifact module declares no
requirements and contains only `embed` data. Already published artifact modules
keep the directive they were published with.

## 2. Build, check, and record native acceptance

The `go -C internal/tools` commands in this section run the tool from the
`internal/tools` module directory. Pass absolute paths for `-out`, `-pin`,
`-root`, `-module`, `-library`, `-header`, `-header-manifest` and
`LANCE_C_HEADER`; a relative value resolves under `internal/tools`, not the
repository root.

When the pin is a published `lance-c` release, acquire the exact published
bytes instead of rebuilding:

```sh
: "${TARGET:?target as GOOS/GOARCH, for example linux/amd64}"
: "${RELEASE_OUT:?absolute new output directory}"
go -C internal/tools run ./fetch-lance-c-release -target "$TARGET" -out "$RELEASE_OUT"
NATIVE_LIB=$(find "$RELEASE_OUT" -type f \( -name liblance_c.so -o -name liblance_c.dylib \) -print -quit)
NATIVE_HEADER="$RELEASE_OUT/include/lance/lance.h"
NATIVE_ABI_HEADER="$(./scripts/abi-checker-input.sh "$RELEASE_OUT/acquisition.txt")"
```

The tool verifies the published per-asset checksum, the pinned header identity,
and the target machine. A legacy archive ships only `include/lance/lance.h` and
is checked against `upstreamHeaderSHA256`. For a split archive, the tool extracts
every public `include/lance/*.h` (excluding `lance.hpp`) and requires an
independent composite identity through `-expected-header-identity`,
`-expected-header-manifest`, or the optional `upstreamHeaderIdentitySHA256` pin
constant. It also compares the library with `UncompressedSHA256` in the artifact
module that this target's generated selector imports, resolved through the Go
proxy and checksum database. After a source pin bump the selected companion modules still
describe the previous commit, so that independent check is inactive until new
modules are published and adopted; candidate packaging proceeds from the
acquired bytes.
Release archives are already stripped, so pass the library directly to the
generator in step 3. Build from source only when the pin has no matching release
archive.

For a source build on Linux or macOS, build from the pinned commit into an
output directory outside the checkout:

```sh
: "${NATIVE_OUT:?new output directory}"
scripts/build-lance-c.sh --out "$NATIVE_OUT"
NATIVE_OUT=$(cd "$NATIVE_OUT" && pwd -P)
NATIVE_LIB=$(find "$NATIVE_OUT" -type f \( -name liblance_c.so -o -name liblance_c.dylib \) -print -quit)
NATIVE_HEADER="$NATIVE_OUT/include/lance/lance.h"
```

`build-lance-c.sh` checks out `upstreamCommit`, verifies the source and
installed header hashes, builds, and installs. It does not strip the library;
the official release archives are already stripped and need no local strip.
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

Check the bindings against the installed header. `scripts/abi-checker-input.sh`
selects the recorded file for a legacy archive or the recorded directory for a
split archive; the legacy file is still what `check-native-library.sh` and the
generator consume:

```sh
LANCE_C_HEADER="$NATIVE_ABI_HEADER" go -C internal/tools run ./check-abi-symbols
# split install: also pass the identity recorded by the acquisition tool
go -C internal/tools run ./check-abi-symbols \
  -header "$NATIVE_ABI_HEADER" \
  -header-identity "$(sed -n 's/^header_identity_sha256=//p' "$RELEASE_OUT/acquisition.txt")"
```

Run the maintained native-library checker:

```sh
scripts/check-native-library.sh \
  --library "$NATIVE_LIB" --header "$NATIVE_HEADER" --target "$TARGET"
```

It statically checks the pinned header hash, ELF or Mach-O object format and
architecture, dependency paths, direct dependencies, the libc or macOS
deployment floor, and every symbol bound by the Go package. It does not load
the library or establish runtime coverage. These checks are not optional and
must be recorded before accepting a platform; do not substitute ad hoc manual
inspection. If the checker is missing from the release branch, stop and fix the
branch first.

Record acceptance per claimed platform. The table records the state to capture
for a release, not a claim that a future release already has every cell. Keep
these states separate; one does not imply another:

| Target | Native ABI | Default runtime | External runtime | Race/checkptr | Cross-compile | Public archive |
| --- | --- | --- | --- | --- | --- | --- |
| `linux/amd64` | record | record | record | record | record | record |
| `linux/arm64` | record | record | record | record | record | record |
| `darwin/arm64` | record | record | record | record | record | record |
| `windows/amd64` | record | record | record | record | record | record |
| `windows/arm64` | record | record | record | record or toolchain limitation | record | record |

Fill each cell with its tested object, result, and evidence, or an explicit
pending/unsupported state. This is an evidence template, not a support claim.
The pinned `a800fd5f` native build exports 890 `lance_*` symbols; the Go
bindings bind 608 of them. Record both counts separately. The ABI checker
validates the bound subset; it does not establish runtime coverage of every
native export.

The official `linux/amd64` and `linux/arm64` release packages are generic
glibc 2.28 builds. The macOS package uses deployment target 11.0 and is tested
on macOS 15.x hosts. The other supported targets use their own bundles; their
accepted hosts and recorded payload facts are listed in
[docs/native-packages.md](docs/native-packages.md). Older glibc/macOS floors and
musl
compatibility have not been established beyond the recorded package floors.
The real S3 acceptance above is scoped to Linux amd64 and the historical
`native88` library at source `59a427e`; it is not evidence for the
`ef465b43` native payload. Source review is a separate state: it can justify
understanding of the native change, but it is not ABI or runtime acceptance.

A race run must use a supplied library; record whether it used the default
selector or the external tag. For the external contract, run
`CGO_ENABLED=1 go test -race -gcflags=all=-d=checkptr=1 -tags lance_external_lib ./...`
with the matching `LANCE_LIB_PATH`. Native Rust internals are outside Go's race
detector.
On Linux, the default lane must run with `LANCE_LIB_PATH` unset, a fresh
`XDG_CACHE_HOME`, and the expected uncompressed hash. macOS ignores
`XDG_CACHE_HOME`; use the public-consumer driver below for a cold macOS runtime
cache. See [`docs/development.md#testing`](docs/development.md#testing) for the existing command matrix
rather than duplicating it here.

## 3. Generate and verify the static artifact modules

Generate the release artifact modules from the recorded libraries and headers,
outside the checkout. The generator remains `scripts/sync-lance-c.sh`; the
static-origin packager consumes its `module/` output for each target:

```sh
: "${ARTIFACT_OUT:?absolute new output directory outside the checkout}"
scripts/sync-lance-c.sh \
  --library "$NATIVE_LIB" \
  --header "$NATIVE_HEADER" \
  --target "$TARGET" \
  --module-path "$ARTIFACT_MODULE" \
  --out "$ARTIFACT_OUT"
```

The output contains a data-only `module/` (with `go.mod`, `artifact.go`, and the
compressed payload) and a `selector/` file for `internal/bundled`.

For a candidate set, the static-origin generator packages all five modules and
writes the standard proxy files, discovery HTML, and identity manifest. Set the
roles explicitly; `SOURCE_COMMIT` is the SDK commit the candidates were built
from, not the upstream native commit, and `SOURCE_RELEASE` is the upstream
`lance-c` release:

```sh
: "${ARTIFACT_VERSION:?new companion module version}"
: "${SDK_SOURCE_COMMIT:?SDK commit the candidates were built from}"
: "${NATIVE_SOURCE_RELEASE:?upstream lance-c release}"
: "${RELEASE_TIME:?origin .info time in RFC3339}"
: "${CANDIDATES:?normalized candidates directory}"
: "${ORIGIN_OUT:?origin output directory}"

scripts/go-module-origin/stage-candidates.sh "$DOWNLOADS" "$CANDIDATES"
python3 scripts/go-module-origin/verify-candidate-identity.py \
  "scripts/go-module-origin/${ARTIFACT_VERSION}.identity.json" "$CANDIDATES"
VERSION="$ARTIFACT_VERSION" \
SOURCE_COMMIT="$SDK_SOURCE_COMMIT" \
SOURCE_RELEASE="$NATIVE_SOURCE_RELEASE" \
RELEASE_TIME="$RELEASE_TIME" \
CANDIDATES_DIR="$CANDIDATES" \
OUT_DIR="$ORIGIN_OUT" \
scripts/build-go-module-origin.sh
ORIGIN_DIR="$ORIGIN_OUT" scripts/test-go-module-origin.sh
```

`scripts/build-go-module-origin.sh` defaults to the first recorded
`v0.2.0-preview.1` publication. The repository records each publication in
`scripts/go-module-origin/<artifact-version>.identity.json`; the publish
workflow inputs pin the active one. `verify-candidate-identity.py` validates the
hosted candidate bytes against the matching identity file (`SOURCE_COMMIT`
records the candidate source commit, not the generator). The recorded
`v0.2.0-preview.1` manifest was produced at `9f3ba53` (go 1.23.0); reproducing it
from current main produces a different manifest, and the active publication's
manifest is the one pinned in the workflow. A later native version needs a
generator whose Go floor is the reviewed one, a new identity file from its
hosted candidate runs, updated publish-workflow inputs, and a matching update
to `test-go-module-origin.sh` (fixed to the active version and hashes). The
current workflow pins the active publication and is not a generic publisher.

The generated module path, target, library name, compressed and uncompressed
sizes and hashes, native commit, and header hash must match the build record.
The commit and header hash are packaging claims: `lance_c_version()` reports
only the binding release, so the loader cannot prove them from the shared
library.

The maintained verifier checks the payload hash and size, decompresses it,
compares it byte-for-byte with the library, and checks the uncompressed hash,
pin, and module path. It also checks that `Payload []byte` embeds that exact
archive and that the module contains only the allowed data files:

```sh
go -C internal/tools run ./verify-lance-c-artifact \
  -module "$ARTIFACT_OUT/module" \
  -library "$NATIVE_LIB" \
  -target "$TARGET" \
  -module-path "$ARTIFACT_MODULE"
```

If the verifier is missing from the release branch, stop and fix the branch;
do not substitute an ad hoc layout check or accept the candidate without this
verification.

The recorded `v0.2.0-preview.1` `linux/amd64` payload is 88,580,128 bytes
compressed and 345,989,752
bytes uncompressed; exact hashes are recorded in the generated `artifact.go` and
the release notes. The published module ZIP is
87,897,705 bytes and `scripts/verify-quickstart.sh` pins it separately. The raw
library is never committed. GitHub rejects ordinary Git blobs over 100 MiB, and
warns above 50 MiB; static module publications do not add Git blobs, but the
same payload size limits still apply to the origin. Do not silently switch
formats or move the payload outside the module contract.

## 4. Publish the static modules and resolve their exact versions

Publish through the CI workflow
`.github/workflows/publish-go-module-origin.yml` on authorized `main`, with the
reviewed workflow-selected identity and `publish=true`. Before dispatch,
confirm the workflow's `ORIGIN_*` values and run IDs match the reviewed identity
and candidate runs; do not assume the first-release values. The staging job
downloads the pinned same-repo candidate artifacts, verifies the identity
manifest, regenerates
the origin, checks the exact manifest SHA-256, and uploads the staging tree. The
protected `go-module-origin` publish job receives that exact tree and publishes
only manifest-validated bytes. The local publisher procedure is documented in
[scripts/go-module-origin/README.md](scripts/go-module-origin/README.md).

Artifact module versions are immutable. Publish `.info`, `.mod`, and `.zip`
first, then the merged `@v/list`, and discovery last. Never create a Git tag for
an artifact module and never move a published static version. The three
pre-existing darwin-arm64 version objects are preserved by create-only logic.

Resolve the exact versions through the public proxy and checksum database before
touching the selectors or root requirements:

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
condition. Download the public archive and compare its payload with the generated
module payload; the zip entry is under the module and version prefix:

```sh
PAYLOAD=$(find "$ARTIFACT_OUT/module" -type f -name '*.zst' -print -quit)
ZIP=$(sed -n 's/.*"Zip": "\([^"]*\)".*/\1/p' "$PROBE_WORK/artifact.json")
PAYLOAD_NAME=$(basename "$PAYLOAD")
test -n "$ZIP"
unzip -p "$ZIP" "$ARTIFACT_MODULE@$ARTIFACT_VERSION/$PAYLOAD_NAME" > "$PROBE_WORK/published.zst"
cmp "$PROBE_WORK/published.zst" "$PAYLOAD"
sha256sum "$PROBE_WORK/published.zst"
```

The compressed payload hash must match both the module metadata and the
generated file verified before publication.

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
<goarch>`, its import path is the production module, and no legacy
staging path remains:

```sh
if grep -R "artifacts/" internal/bundled go.mod go.sum; then
  echo "legacy in-tree artifact path remains; use the published artifact module" >&2
  exit 1
fi
```

The command should produce no matches. Also confirm there is no `replace`
directive for the root or artifact module, and that `go.sum` contains the
recorded `Sum` and `GoModSum`. Do not duplicate the target, payload sizes and
hashes, native commit, header hash and published requirement in
`docs/native-packages.md`; the generated `artifact.go`,
`internal/ffi/upstream.go` and the release notes carry them.

Commit and push the activation commit, then verify that exact head with the
checkout/local-source mode below. Before the root tag exists, do not try to
resolve the activation commit through the public proxy: public module
acceptance uses release tags only. After the tag exists, the same recipe can
use `ROOT_VERSION`.

A manual consumer recipe is only an illustrative install smoke test. It is not
release acceptance and does not establish cold target or runtime caches:

```sh
SMOKE_WORK=$(mktemp -d)
SMOKE="$SMOKE_WORK/consumer"
mkdir -p "$SMOKE" && cd "$SMOKE"
go mod init release-smoke
env -u GOPRIVATE -u GONOPROXY -u GONOSUMDB \
  GOENV=off GOWORK=off GOMODCACHE="$SMOKE_WORK/modcache" \
  GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org \
  go get "$ROOT_MODULE@$ROOT_VERSION"
env -u LANCE_LIB_PATH CGO_ENABLED=0 GOWORK=off \
  GOMODCACHE="$SMOKE_WORK/modcache" GOCACHE="$SMOKE_WORK/buildcache" \
  GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org \
  go run "$ROOT_MODULE/examples/roundtrip"
```

This smoke test shares its module cache between `go get` and the target run.
The target uses a separate build cache that `go get` does not use. On macOS it
uses the normal `os.UserCacheDir` cache, so a pre-existing cache can satisfy the run.
It only checks that the install commands complete and the default example runs;
it is not a cold or cache-identity check. Default, namespace-only, and external
acceptance must use the maintained driver below with fresh per-mode caches.

External consumers use `-tags lance_external_lib` and a matching
`LANCE_LIB_PATH`. The external tag excludes the selector, `bundle.go`,
`cache.go`, `lock_unix.go`, the decompression path, and the artifact package
from the compiled dependency graph and embedding. It does not remove the
artifact modules from `go.mod` or the module graph; `go get` and module-graph
commands may still download every companion archive. This is not a
zero-download contract. Confirm the built binary and its runtime do not use the
bundle: `go version -m` must not list the artifact module, and no extraction
directory should appear. The public-consumer driver exercises this contract
from fresh target caches.

### Public-consumer acceptance

Required public-distribution acceptance uses both maintained lanes:

- `.github/workflows/unix-public-consumers.yml`, with
  `scripts/public-consumer/run.sh`, covers Linux amd64 and arm64 plus macOS
  arm64.
- `.github/workflows/windows-public-consumers.yml`, with
  `scripts/public-consumer/run-windows.ps1`, covers Windows amd64 and arm64.
  See [the Windows driver contract](scripts/public-consumer/WINDOWS.md) for
  host requirements, DLL identity checks and Windows cache properties.

Both lanes resolve one exact **official release tag** through
`proxy.golang.org` and `sum.golang.org`, and build standalone default,
namespace-only, and external consumers outside the checkout. They verify the
root archive and source tree, the artifact selected by the built binary, payload
and extracted-library bytes, platform cache properties, and the roundtrip
result. They do not rebuild native code. The manual smoke test above is not a
substitute for these drivers.

The workflows run when a root `v*` release tag is pushed, or by manual dispatch
that names and checks out that tag. They never accept a commit SHA or
pseudo-version: a tag is the published install unit. Exact-head CI keeps the
equivalent coverage without public resolution by building the quickstart from
the checkout, checking the read-only deployment path, and running the bundled
RTree and raw Blob v2 regressions on linux/amd64; the native workflows cover
the rest of the runtime matrix. Maintainers can run the same exact-head check
locally:

```sh
LANCE_GO_QUICKSTART_SOURCE_DIR="$PWD" ./scripts/verify-quickstart.sh
```

Run the Unix driver locally on the matching native host against a tag:

```sh
cd "$REPO_ROOT"
ROOT_REVISION="$ROOT_VERSION" \
TARGET_GOOS=linux TARGET_GOARCH=amd64 \
EXPECTED_UNAME=x86_64 LIBRARY_NAME=liblance_c.so \
EVIDENCE_DIR=/tmp/public-consumer-evidence \
bash scripts/public-consumer/run.sh
```

`ROOT_REVISION` is passed through the environment, never interpolated into
shell code. Set `SOURCE_COMMIT` explicitly when the evidence should name the
checked-out source commit; it must equal the local checkout HEAD. `EVIDENCE_DIR`
selects the output directory. The driver refuses an existing
`<os.UserCacheDir>/lance-go` cache, uses separate install/get and target-build
module and build caches for each consumer mode, and moves aside only the SDK
cache it produced. It records the complete
`go get` download set against the root module graph, then requires each default
and namespace target build to fetch exactly the selected artifact payload
archive from fresh empty caches. External target downloads are recorded without
a zero-download claim. The evidence directory contains compact logs and JSON
manifests, not module or build caches.
See [scripts/public-consumer/README.md](scripts/public-consumer/README.md) for
the full checks and limits. This lane is acceptance for the exact revision and
host, not a full runtime regression.

## 6. Evidence, notes, and authorized release

The Unix workflow uploads native, race, and unpublished candidate-module
evidence with seven-day retention. Those files are not the release record.
Before tagging, preserve durable evidence in the existing report location;
after creating the release, attach the evidence files. Record content hashes in
the release notes. Preserve an immutable record of those
hashes and exact objects; expiring CI artifacts are not sufficient. Do not
invent a separate release database.

The evidence record must identify the exact objects:

```text
root:             commit, tag, module path
go:               module minimum and tested Go versions
api diff:         base tag, reviewed entry count, review location
artifact:         static module path and version, go.sum entries, proxy/sumdb identity
native:           repository, commit, tag or explicit absence, header SHA-256
library:          bytes, SHA-256, packaging source
payload:          compressed and uncompressed bytes and SHA-256
public archive:   proxy zip comparison and checksum-database result
public consumer:  exact root/artifact versions, selected payload and extracted library hashes, result
platform:         target, ABI/runtime/race/default/external results, floor and dependencies
gaps:             untested targets and storage paths
```

The recorded `v0.2.0-preview.1` `linux/amd64` package, native
`a800fd5f28636bfff34778e3ad6663f3cf099a38`, header
`6bab6899ce240b93cf47a1c93f7fb4c8bb2b20ba0a82e3c4a0ec04e7bfc915ad`, payload
`48c83c5ded9f376b3aecfd6bb6b1994de6a1d7790031df9134d8749405aa9231`, and
uncompressed library
`011c3bcc207ff5dd4ee0376d63338d9d05a76083ef45c616f7047978c4c59ece` are
recorded in `internal/ffi/upstream.go`, `go.mod`, the generated `artifact.go`,
the identity manifest and the release notes. Published versions, payload sizes,
hashes and accepted exact-version consumer runs are listed there and in the
release evidence. The authorized root tag requires the gates above.

Include the artifact version, payload sizes and hashes, evidence location,
supported bundle target and runtime floor, the default and external behavior,
and what remains pending. Do not claim Windows or other Unix runtime support or
completion of issue #1 without the corresponding accepted evidence. Describe S3
acceptance with its tested source, native library, and self-hosted namespace
boundary.

This document is not release authorization or acceptance. A maintainer decides
readiness and authorizes the release tag only after the evidence above exists
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

After publication, the tag push starts both maintained public-consumer
workflows. They can also be dispatched with the same tag, for example to
re-run a failed lane. Set both the workflow ref and its revision input to that
tag so the checked-out driver and publicly resolved root identify the release:

```sh
: "${ROOT_VERSION:?authorized published root version}"
gh workflow run unix-public-consumers.yml --repo lance-format/lance-go \
  --ref "$ROOT_VERSION" -f "revision=$ROOT_VERSION"
gh workflow run windows-public-consumers.yml --repo lance-format/lance-go \
  --ref "$ROOT_VERSION" -f "revision=$ROOT_VERSION"
```

Both workflows create fresh evidence, module, build and runtime caches; do not
reuse pre-tag caches. Require all five targets and all 15 default,
namespace-only and external consumer modes, including cleanup, to pass. They
must fetch the tagged root and exact artifact versions through the public
proxy and verify archive bytes, checksum-database identity, extraction and
runtime behavior. Preserve both runs and their checksummed evidence with the
release; an accepted pseudo-version does not satisfy this post-tag gate.

## 7. Reusing existing evidence

The roundtrip example, `internal/compat/`, and `internal/benchmarks/soak`
results may be cited only when the relevant code, dependencies, and
configuration are unchanged since the run, and only for the exact commits
they exercised. A docs-only delta does not require mechanically rerunning a 30-minute soak or four foreign SDK builds.
State the reused evidence, its commit, and its coverage limits instead.

Cross-compilation is not runtime acceptance. Source review is not binary
acceptance. A public archive check is not a native runtime check. Keep those
states separate in the release record.

## 8. Documentation checks

The broad wording review is manual: a grep cannot tell a legitimate machine pin
or historical migration note from a stale current-release claim. The automated
steps below fail on a fixed `@vX` install command and a leftover `Unreleased`
heading; every other grep hit is reviewed and either fixed or justified.
Prefer linking to the `go` directive in `go.mod` over copying the minimum into
user-facing prose.

Pre-tag:

```sh
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
for path in README.md CHANGELOG.md docs RELEASING.md scripts/*/README.md \
  examples/README.md internal/benchmarks/soak/README.md internal/compat/README.md; do
  test -e "$path" || { echo "missing documentation path: $path" >&2; exit 1; }
done

# Manual review: inspect every hit. Machine pins, historical migration notes and
# this checklist may keep a version; install/quickstart references must not.
grep -RnE 'v[0-9]+\.[0-9]+\.[0-9]+|current stable|latest release|next (minor|release)|Unreleased' \
  README.md CHANGELOG.md docs RELEASING.md scripts/*/README.md examples/README.md \
  internal/benchmarks/soak/README.md internal/compat/README.md || true

# Automated: the current release section must not still be marked Unreleased.
if grep -n 'Unreleased' CHANGELOG.md; then
  echo "changelog still marks the current release Unreleased" >&2
  exit 1
fi
# Automated: documented install and quickstart must use @latest.
if grep -RnE 'go (get|run) .*@v[0-9]' README.md docs/api-guide.md examples/README.md; then
  echo "documented install/quickstart uses a fixed release tag" >&2
  exit 1
fi
# Automated: the local-source consumer path must still pass before the tag exists.
CGO_ENABLED=0 LANCE_GO_QUICKSTART_SOURCE_DIR="$PWD" ./scripts/verify-quickstart.sh
```

Post-publication. The evergreen smoke and the exact release evidence are
separate: `@latest` may legitimately resolve to a later stable or a maintenance
patch and must not be asserted equal to `ROOT_VERSION`.

```sh
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
: "${ROOT_VERSION:?set to the published release tag}"

# Evergreen smoke: the documented install and quickstart work against @latest.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
(
  cd "$work"
  go mod init docs-check >/dev/null
  go get github.com/lance-format/lance-go@latest
  CGO_ENABLED=0 go run github.com/lance-format/lance-go/examples/roundtrip@latest
)
echo "latest resolved to: $(cd "$work" && go list -m -f '{{.Version}}' github.com/lance-format/lance-go@latest)"

# Exact release evidence: always resolve and run the authorized tag itself.
(cd "$work" && go list -m -f '{{.Version}}' "github.com/lance-format/lance-go@$ROOT_VERSION")
(cd "$work" && CGO_ENABLED=0 go run "github.com/lance-format/lance-go/examples/roundtrip@$ROOT_VERSION")

# The unversioned package reference and release destinations resolve.
curl -fsSI https://pkg.go.dev/github.com/lance-format/lance-go >/dev/null
curl -fsSI https://github.com/lance-format/lance-go/releases >/dev/null
curl -fsSI "https://github.com/lance-format/lance-go/blob/$ROOT_VERSION/CHANGELOG.md" >/dev/null
```

Record the manual grep results, the resolved `@latest` version, the exact-tag
resolution and roundtrip output, and the checked URLs in the release evidence.
A fixed `@vX.Y.Z` install tag or an `Unreleased` heading fails the automated
checks; stale current/next wording or a copied Go minimum is a manual-review
finding to fix or justify. `@latest` differing from `ROOT_VERSION` is recorded,
not a failure.
