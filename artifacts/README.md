# Packaged lance-c artifacts

This directory contains the published Unix artifact modules and documents the
packaging interface between the loader and release artifacts. The tracked
`artifacts/<target>/liblance_c.so.zst` or `liblance_c.dylib.zst` files are the
production compressed payloads; no raw shared library, static archive, or build
evidence is tracked. Windows companion candidates are generated outside the
checkout by the native evidence lane and are not tracked or published.

The Unix native workflow separately produces staging candidates from the
accepted native library. Staging candidates are workflow artifacts only; after
review and publication, accepted payloads are tracked here, required by the
root module, and paired with generated selectors. See "Native Unix validation
workflow" and "Public-consumer acceptance".

## Active Unix bundles

The root module requires four published data-only modules:

```text
github.com/lance-format/lance-go/artifacts/linux-amd64   v0.0.0-20261111235843-ac5955a095d0
github.com/lance-format/lance-go/artifacts/linux-arm64   v0.0.0-20261112064406-43fcbbee5161
github.com/lance-format/lance-go/artifacts/darwin-amd64  v0.0.0-20261112064406-43fcbbee5161
github.com/lance-format/lance-go/artifacts/darwin-arm64  v0.0.0-20261112064406-43fcbbee5161
```

Their sources and payloads are also tracked under `artifacts/<target>/`.
Because those directories are nested modules, the root module ignores them and
resolves each requirement through the Go module proxy; the published modules
are authoritative for builds. The tracked payloads must stay byte-identical to
the published archives.

`linux/amd64` has prior native, default, external, and race evidence. The three
companion modules were published and their selectors adopted for validation;
their hosted native cold public-consumer acceptance is pending. Publication is
not a companion runtime support claim.

### linux/amd64 payload

The published `linux/amd64` module is
`github.com/lance-format/lance-go/artifacts/linux-amd64` at
`v0.0.0-20261111235843-ac5955a095d0`.

| Property | Value |
| --- | --- |
| Target | `linux/amd64` |
| Library | `liblance_c.so` |
| Compressed size | 85696976 bytes |
| Compressed SHA-256 | `e933fc867b2a9ee404d01959aaef8dc8378ddf0b582d7bb2367ed3dbfe46c5b5` |
| Uncompressed size | 322099824 bytes |
| Uncompressed SHA-256 | `e5571d6da16e81a0434f2509ade5262785b63df0b37ff7155537642841dd9378` |
| Upstream commit | `ac2634c553b450c7787577ffe964fc2b283b8334` |
| Header SHA-256 | `c19789e9e003930c6065181b5cd9f6f7b464e08a5dc4daca8b21ad82d5160f5b` |

### Published Unix companion payloads

All three companions use `UpstreamCommit`
`ac2634c553b450c7787577ffe964fc2b283b8334` and `HeaderSHA256`
`c19789e9e003930c6065181b5cd9f6f7b464e08a5dc4daca8b21ad82d5160f5b`.

| Target | Module version | Compressed size | Compressed SHA-256 | Uncompressed size | Uncompressed SHA-256 |
| --- | --- | --- | --- | --- | --- |
| `linux/arm64` | `v0.0.0-20261112064406-43fcbbee5161` | 82574957 | `4bb00ab7cc52a6f693b0fd79daf2b94bf5afa1e17ffc65a6b634747609eb1fdb` | 286825888 | `c03b4d69f4597d71d99971606f250e2b11bb69f50a01492cdf82c02c8413d879` |
| `darwin/amd64` | `v0.0.0-20261112064406-43fcbbee5161` | 81328057 | `c44cccfe51d70d955e9ee1887f20994bb7a5ac8deca39e0f80f781fa779b0805` | 293866120 | `e377468f7d0f5afd13a6ab4d18f9cf17dc544a38a68da04fb1a5557839260bc7` |
| `darwin/arm64` | `v0.0.0-20261112064406-43fcbbee5161` | 76784146 | `80e2e23985ec20955ce8ab67e6267a206de16af9dfab7b32296157455f7b395d` | 272228496 | `f214c238b890bd02427ecc260231f4f886997a396e050c951978f81270e25200` |

The generated selectors are
`internal/bundled/lance_bundle_linux_arm64.go`,
`internal/bundled/lance_bundle_darwin_amd64.go`, and
`internal/bundled/lance_bundle_darwin_arm64.go`. Their build constraints are
`!lance_external_lib && <goos> && <goarch>`.

`internal/bundled/lance_bundle_linux_amd64.go` is the generated selector.
Its build constraint is `!lance_external_lib && linux && amd64`. The `init`
function registers the descriptor, so a default `linux/amd64` import of the
root package or the standalone namespace package uses the bundle after
`LANCE_LIB_PATH` is absent and before the system loader. `LANCE_LIB_PATH` still takes precedence when set. Once a bundle
is registered, verification, extraction, and `dlopen` errors are returned with
the bundle module in the error; the loader does not fall back to a system copy.

The compressed payload is data. Importing the artifact module does not link,
load, or execute native code; the loader decompresses and maps it on first use.

## Windows production companion candidates

No Windows target is published. `internal/bundled` has no Windows selector, the
root `go.mod` has no Windows artifact requirement, and no Windows module is
available through the proxy. The Windows native lane may produce an unpublished
candidate at `github.com/lance-format/lance-go/artifacts/windows-<arch>` only after
the DLL, header, public ABI, dependency staging, native C consumer, and selected
Go external-library tests all pass.

The candidate uses the exact staged DLL bytes. No Unix-style strip is applied,
and dependency bytes or install names are not modified. The read-only captured
Windows DLL reviewed for this work, from native source evidence rather than the
current pin, has a zero COFF symbol table pointer and count, no `.debug`
sections, and a `.pdata` unwind section. `.pdata` is runtime unwind
metadata, not debug data, so it remains in the packaged bytes. The PDB and MSVC
import library are native build evidence; they do not belong in a data-only
module.

The generator writes `module/artifact.go`, `module/go.mod`,
`module/lance_c.dll.zst`, and a `selector/lance_bundle_windows_<arch>.go` file
with build constraint `!lance_external_lib && windows && <arch>`. The selector is
not copied into `internal/bundled`. The shared verifier checks target, module
path, upstream repository/commit, header hash, payload size and hash, decoded
size and hash, byte equality, embed layout, and file set. The exact staged DLL
is hashed before and after generation. A compressed payload larger than the
ordinary 100 MiB GitHub blob limit blocks the candidate; this repository does
not introduce chunking for it.

Dependency eligibility is classified by module name, not by the directory where
a hosted runner happens to find the file. The single-DLL companion requires a
reviewed OS-only dependency closure. The captured native DLL imports
`VCRUNTIME140.dll`, which is a separately deployed VC Runtime; its presence in
the hosted `System32` is not OS-only availability. Redistributable and
unclassified dependencies therefore block candidate generation explicitly
after the native evidence, and the workflow records `BLOCKED` with actionable
provenance. Upstream
[#215](https://github.com/lance-format/lance-c/issues/215) tracks the supported
Windows deployment contract; [#214](https://github.com/lance-format/lance-c/issues/214)
tracks dependency exports and is distinct. No System32 files are copied into the
module, no installer prerequisite or loader sidecar is added, and no license
assumption is made.

If the closure is OS-only, the candidate is verified and then exercised by
`go run -tags lance_external_lib ./examples/roundtrip` with `LANCE_LIB_PATH`
pointing at the exact staged DLL. The required output is
`Created, reopened and scanned 3 rows: [10 20 30]`. This is a supplied-library
check, not a cold default, cold external, or public consumer, and it does not
establish zero-config Windows distribution. The candidate manifest records
`candidate_status=verified-unpublished`, `selector_activated=false`,
`module_requirement_added=false`, `publication_attempted=false`, and
`cold_default_consumer=not_run`, `cold_external_consumer=not_run`, and
`public_consumer=not_run`. Candidate and native evidence are uploaded as
separate architecture-scoped CI artifacts. Windows publication, selector
activation, hosted cold-cache loading, and supported distribution remain
pending.

## Platform and runtime scope

Four Unix targets have published bundles. The `linux/amd64` library was built
on Ubuntu 24.04.5 with glibc 2.39 and requires libc, libm, libgcc_s and the
dynamic loader. The companion payloads use the same pin and header, but their
native ABI floors and cold runtime behavior are not established here. Do not
assume any payload works on older glibc, musl, Windows, or other architectures;
no such runtime support is claimed.

The cross-build matrix in `ci.yml` compiles other targets but does not run them
and does not publish artifacts. The Unix native workflow rebuilds candidate
libraries; publication and selector adoption are separate release steps.
Windows remains `BLOCKED` and has no published module or selector.

A bundle is fetched through the normal Go module proxy when a default build for
a published Unix target compiles its artifact package. This is not a
zero-download installation. Commands that load all modules, including `go get`
and `go mod tidy`, may download every companion archive because Go can load
packages for all build tags. The public-consumer lane records that installation
set separately, then measures each default and namespace target build from
fresh empty caches.

## Public-consumer acceptance

`scripts/public-consumer/run.sh` resolves an exact root revision from the
public proxy and builds standalone default, namespace-only, and external
consumers on one native runner. It verifies the root archive and source tree,
the artifact selected by the built binary, payload and extracted-library bytes,
safe cache modes, and the roundtrip output. It records the complete `go get`
download set against the root module graph, then requires each default and
namespace target build to fetch only the selected artifact payload archive from
fresh empty caches. External target downloads are recorded without a
zero-download claim. The four hosted matrix entries are Linux and macOS on
amd64 and arm64. The lane is public-distribution acceptance, not a
native rebuild or a full runtime regression. See
[scripts/public-consumer/README.md](../scripts/public-consumer/README.md).

## External build contract

`-tags lance_external_lib` excludes the generated selector, `bundle.go`,
`cache.go`, `lock_unix.go`, the decompression dependency path, and the artifact
package from compilation and embedding. The artifact module remains in
`go.mod` and the module graph, and module-graph commands may still download its
archive. External builds use only `LANCE_LIB_PATH` and system discovery.

## Layout produced by the generator

```text
<out>/
  module/
    go.mod
    artifact.go        # embeds the payload and declares metadata
    liblance_c.so.zst  # or liblance_c.dylib.zst / lance_c.dll.zst
  selector/
    lance_bundle_<goos>_<goarch>.go
```

`module/` is a data-only Go module. Its `artifact.go` imports only `embed`; it
does not link, load, or execute native code. The generated `go.mod` targets Go
1.23 and declares no requirements.

The generator packages the library it is given; it does not strip or otherwise
modify that library. For a production candidate, strip a copy first as
described below and pass the stripped copy to the generator.

`selector/` contains a file for `internal/bundled`. Its build constraint is
`!lance_external_lib && <goos> && <goarch>`. It imports the artifact module
and registers the descriptor with `internal/loader` in an `init` function. Copy
the selector into `internal/bundled` after publishing the artifact module and
adding the module requirement. That activation step belongs to the release
process, not to this generator.

## Generating an artifact

Build lance-c from the exact commit pinned in `internal/ffi/upstream.go`, then
package that build. Do not substitute a release archive with a different
header: the pinned commit and the `v0.1.9` archive have different headers.

```sh
scripts/sync-lance-c.sh \
  --library /path/to/liblance_c.so \
  --header /path/to/include/lance/lance.h \
  --target linux/amd64 \
  --out /tmp/lance-go-artifact-linux-amd64
```

The script reads `upstreamRepo`, `upstreamCommit`, and
`upstreamHeaderSHA256` from `internal/ffi/upstream.go`. It verifies the header
hash, target architecture, library format and basename, then refuses to
overwrite an existing output directory. Supported targets are Linux, macOS
and Windows on amd64 and arm64. Linux requires a 64-bit ELF shared object,
macOS a thin 64-bit dylib, and Windows a 64-bit PE DLL with the matching
machine type.

The commit and header hash written into the generated module are
packaging-time provenance claims. lance-c exposes no runtime version symbol,
so the loader cannot prove from the shared library which commit built it. The
packaging host and the release process must supply a library built from the
pinned commit. The active bundle was packaged from `ac2634c553b450c7787577ffe964fc2b283b8334`;
its `artifact.go` records the values in the table above.

### Preparing a stripped production candidate

Do not pass the install-cache library directly to the generator for a
production candidate. Copy, strip, and verify the copy first:

```sh
out="$RUNNER_TEMP/production/linux-amd64"
scripts/strip-native-library.sh \
  --library "$LANCE_LIBRARY" \
  --header "$LANCE_HEADER" \
  --target linux/amd64 \
  --out "$out"
scripts/check-native-library.sh \
  --library "$out/library/liblance_c.so" \
  --header "$LANCE_HEADER" \
  --target linux/amd64
scripts/sync-lance-c.sh \
  --library "$out/library/liblance_c.so" \
  --header "$LANCE_HEADER" \
  --target linux/amd64 \
  --module-path github.com/lance-format/lance-go/artifacts/linux-amd64 \
  --out "$RUNNER_TEMP/candidate/linux-amd64"
```

The helper copies the exact accepted library into `<out>/library`, strips only
that copy, and fails unless the source library's size and SHA-256 are unchanged.
Linux uses `strip --strip-unneeded`; macOS uses `strip -x`. It records the raw
and stripped hashes and sizes, the strip tool path, hash, version, and argv,
and the source/header/target provenance. It does not rewrite dependencies,
install names, or RPATH. Re-running `check-native-library.sh` on the copy
revalidates the architecture, dependency contract, deployment floor, and every
bound export. The stripped copy is then passed to `sync-lance-c.sh`; the
candidate is not written into the checkout or the install cache.

## Runtime cache

In a default build with a registered bundle, `loader.Load` verifies the
compressed payload and materializes the library under:

```text
<os.UserCacheDir>/lance-go/<uncompressed-sha256>/<libraryName>
```

The cache directory and entry directory are private (`0700`) on Unix, where
the library is also read-only (`0500`). On Windows the cache lives under the
per-user `LocalAppData` directory; Go cannot express Unix permission bits
there, so the cached DLL remains writable and access is inherited from that
per-user directory. A `.lock` file coordinates extraction across processes:
`flock` on Unix and `LockFileEx` on Windows. Existing bytes are checked for
exact size and SHA-256 before the platform loader opens them. A corrupt
regular entry is removed and replaced under the lock. Extraction writes a
temporary file in the same directory, verifies the decompressed size and
hash, syncs it, and publishes it with an atomic rename. Failed extraction
never publishes a loadable file.

`LANCE_LIB_PATH` takes precedence over a registered bundle. Once a bundle is
selected, cache, extraction, and platform-loader errors are returned with the
module and target; the loader does not silently fall back to a system copy.

## Dependencies for root integration

The loader's default build adds two direct dependencies:

```text
require (
    github.com/klauspost/compress v1.18.0
    golang.org/x/sys v0.35.0
)
```

Matching `go.sum` entries:

```text
github.com/klauspost/compress v1.18.0 h1:c/Cqfb0r+Yi+JtIEq73FWXVkRonBlf0CRNYc8Zttxdo=
github.com/klauspost/compress v1.18.0/go.mod h1:2Pp+KzxcywXVXMr50+X0Q/Lsb43OQHYWRCY2AiWywWQ=
golang.org/x/sys v0.35.0 h1:vz1N37gP5bs89s7He8XuIYXpyY0+QlsKmzipCbUtyxI=
golang.org/x/sys v0.35.0/go.mod h1:BJP2sWEmIv4KK5OTEluFJCKSidICx8ciO85XgH3Ak8k=
```

`klauspost/compress` `v1.18.0` is the newest non-retracted release compatible
with the module's Go 1.23.0 requirement. `v1.18.1` is retracted; `v1.19.x`
requires Go 1.24. `golang.org/x/sys` supplies the Windows loader and file-lock
bindings. The generated artifact module itself has no requirements, and the
generator uses only that same compression package plus the standard library.

The root requirement for the artifact module adds these `go.sum` entries:

```text
github.com/lance-format/lance-go/artifacts/linux-amd64 v0.0.0-20261111235843-ac5955a095d0 h1:tOJ/owlrb9wrZ1Yr18N3yXakU0+JCuSNutOu3sREkJ4=
github.com/lance-format/lance-go/artifacts/linux-amd64 v0.0.0-20261111235843-ac5955a095d0/go.mod h1:hmgfCi9X6+yByjEXjF26x8U4yNmD+NK5tFKAPQ2JXtI=
```

`lance_external_lib` builds compile out `bundle.go`, `cache.go`,
`lock_unix.go`, `lock_windows.go`, and the zstd dependency path. They still use
the platform loader, `golang.org/x/sys/windows`, and `LANCE_LIB_PATH` support
where the platform needs them.

## Verification status and remaining gates

The pinned ac2634c native build exports exactly all 878 declared C symbols.
The full committed C11 and C++17 fragment mutation consumers passed on NieR,
including both bulk-offset APIs, invalid-entry errors and independent result
ownership. Their ten independent fixtures were produced using the previously
accepted native88 library and audited after the producer exited. Other native88
consumer results remain historical evidence; they were not all rerun at ac2634c.

Packaging round-tripped the compressed payload to the stripped native input.
The exact published module was resolved through the public Go proxy and sumdb;
its payload matched the tracked module byte-for-byte. The runtime baseline
remains Ubuntu 24.04.5 with glibc 2.39. The linux/amd64 paired update has
accepted default, external, race/checkptr, strip, and roundtrip evidence. The
companion payloads use the same pin and header; their hosted native cold
public-consumer acceptance is pending.

The CI default Go lane exercises the real public path: it runs
`TestCreateReopenMetadataAndScan` with `LANCE_LIB_PATH` unset and a fresh
`XDG_CACHE_HOME`, requires a pass rather than a skip, and verifies the unpacked
cache entry's SHA-256. Loader and generator unit tests cover cache reuse and
corruption, cross-process locking, environment precedence, descriptor
validation, and synthetic target headers.

Independent default/external consumers were previously accepted with the
07cd7d0 artifact. Those results establish the distribution contract, not acceptance
of this updated native payload. The integrated Go native/race/checkptr, fresh-bundle and public consumer gates
must run against this revision before delivery.

In the earlier consumer checks, `go get` downloaded every companion artifact
archive before the target build; `go get` loads packages for all build tags, so
that is installation behavior, not target selection. External mode is not a
zero-download contract. Those checks used one target, so they do not establish
archive selection across a multi-platform release. The public-consumer lane
records installation and each fresh-cache target build separately; default and
namespace target builds must fetch only the selected archive, while external
target downloads are recorded without that assertion. Broad multi-platform
runtime acceptance still requires hosted results. Broader glibc/musl coverage, Linux arm64 and
macOS cold runtime acceptance for the published companions, Windows module
publication and selector activation, and hosted Windows cold-cache loading
remain pending.
Windows native execution evidence and an unpublished candidate are not a
supported Windows distribution.

## Native Unix validation workflow

`.github/workflows/unix-native.yml` builds and tests the pinned lance-c source
on native runners for `linux/amd64`, `linux/arm64`, `darwin/amd64`, and
`darwin/arm64`. The runners are `ubuntu-24.04`, `ubuntu-24.04-arm`,
`macos-15-intel`, and `macos-15`. The workflow runs on pull requests and `main`
pushes that touch native inputs, and on manual dispatch.

Each job restores or builds the exact pinned source through
`scripts/build-lance-c.sh`, validates the install cache with
`scripts/lance-c-cache.sh`, and runs `scripts/check-native-library.sh` against
the installed library and header. That check verifies the pinned header hash,
object format and architecture, direct dependencies, the libc or macOS
deployment floor, and that every symbol in the Go binding table is exported. It
reports observed export and dependency counts; it does not assert that the
library exports only `lance_` symbols.

The workflow then runs `scripts/check-abi-symbols.go` and three test passes
against the supplied library: the default build, an external build
(`-tags lance_external_lib`) with `CGO_ENABLED=0`, and an external build with
the race detector. A skipped native test fails the job.

After the three test runs, the job copies the accepted install-cache library
into `$RUNNER_TEMP/production/<slug>/library`, outside the checkout and cache,
and runs `scripts/strip-native-library.sh` on that copy. The helper records the raw
and stripped sizes and SHA-256s, the strip tool path, hash, version, and argv,
and fails if the source library's bytes or hash change. Linux uses
`strip --strip-unneeded`; macOS uses `strip -x`. It does not rewrite
dependencies, install names, or RPATH. `scripts/check-native-library.sh` then
revalidates the stripped copy against the matching header, including the
architecture, dependency contract, deployment floor, and every bound export.

The job runs `examples/roundtrip` with `CGO_ENABLED=0`,
`-tags lance_external_lib`, and `LANCE_LIB_PATH` set explicitly to the stripped
copy. The external tag excludes the generated selector and bundle path, and
the explicit path prevents system discovery from being used. The step requires
the exact three-row output, not just exit status. The raw library still receives
the default, external, and race test passes above.

The job then passes the stripped copy to `scripts/sync-lance-c.sh` with the
production module path `github.com/lance-format/lance-go/artifacts/<goos>-<goarch>`.
The generated selector keeps the
`!lance_external_lib && <goos> && <goarch>` constraint and imports that
production path, but the workflow does not copy it into `internal/bundled`,
change `go.mod`/`go.sum`, or write into the checkout.
`scripts/verify-lance-c-artifact` checks the compressed payload hash and size,
decompresses it, compares it byte for byte with the stripped copy, and checks
the uncompressed hash, size, pin, module path, exact embed target, and file
allowlist. Darwin export inspection must produce a symbol table; unavailable
export evidence fails verification.

The candidate module and evidence are uploaded as
`unix-native-candidate-<slug>` and `unix-native-evidence-<slug>` with seven-day
retention. These workflow candidates remain unpublished. The tracked companion
modules were published after review and their selectors are active; their cold
public-consumer acceptance remains pending. The workflow publishes the validated
install cache only after the
stripped candidate is staged and verified and all consumers of the cache
staging path finish; the cache continues to hold the unstripped
accepted library, and candidates are never cached. The workflow asserts that
the checkout is unchanged, so it cannot activate a selector.

A successful run establishes native build, test, strip, and candidate
verification results for that commit only. Public proxy and sumdb
verification, cold default/external/namespace consumers of a published module,
selector activation, release/tag creation, and acceptance of the candidate
bundles remain pending and are owned by the release process. macOS runtime
support, a deployment floor, and completion of the platform roadmap are not
claimed.
