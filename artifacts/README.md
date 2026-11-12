# Packaged lance-c artifacts

This directory contains the published `linux/amd64` artifact module and
documents the packaging interface between the loader and release artifacts.
The tracked `artifacts/linux-amd64/liblance_c.so.zst` is the production
compressed payload; no raw shared library, static archive, or build evidence is
tracked.

The Unix native workflow separately produces unpublished production-path
candidates from the accepted native library. Candidate modules are workflow
artifacts only: they are not tracked here, not required by the root module,
and not activated by a selector. See "Native Unix validation workflow".

## Active linux/amd64 bundle

The root module requires the published data-only module:

```text
require github.com/lance-format/lance-go/artifacts/linux-amd64 v0.0.0-20261111213734-a779f4a15524
```

Its sources and payload are also tracked under `artifacts/linux-amd64/`. Because
that directory is a nested module, the root module ignores it and resolves the
requirement through the Go module proxy; the published module is authoritative
for builds. The tracked payload must stay byte-identical to the published
archive.

| Property | Value |
| --- | --- |
| Target | `linux/amd64` |
| Library | `liblance_c.so` |
| Compressed size | 85642384 bytes |
| Compressed SHA-256 | `892be9602cf09f791cc62ed7e05002d5f4bd377113a8e0b414a03a90fd069927` |
| Uncompressed size | 322169936 bytes |
| Uncompressed SHA-256 | `20b2b63598fbca3f83146061f8ef4d2f616effb359af9ae4a01fd9afe9dad1d8` |
| Upstream commit | `88c07cf8ccafebf579e769416251be370b45180e` |
| Header SHA-256 | `c23406d5c683133eb647cfeb150fa05182ffc8ba7da283b3902dbb4bf0926c6c` |

`internal/bundled/lance_bundle_linux_amd64.go` is the generated selector.
Its build constraint is `!lance_external_lib && linux && amd64`. The `init`
function registers the descriptor, so a default `linux/amd64` import of the
root package or the standalone namespace package uses the bundle after
`LANCE_LIB_PATH` is absent and before the system loader. `LANCE_LIB_PATH` still takes precedence when set. Once a bundle
is registered, verification, extraction, and `dlopen` errors are returned with
the bundle module in the error; the loader does not fall back to a system copy.

The compressed payload is data. Importing the artifact module does not link,
load, or execute native code; the loader decompresses and maps it on first use.

## Platform and runtime scope

`linux/amd64` is the only published target bundle. The packaged library was
built on Ubuntu 24.04.5 with glibc 2.39 and requires libc, libm, libgcc_s and
the dynamic loader. Do not assume it works on older glibc, musl, Windows, or
other architectures; no bundle or runtime support is claimed for them.

The cross-build matrix in `ci.yml` compiles other targets but does not run them
and does not publish artifacts. The Unix native workflow produces unpublished
candidates for the four native Unix targets; they do not change the published
target set. Each additional published target needs its own accepted payload,
selector, runtime validation, and platform documentation.

The bundle is fetched through the normal Go module proxy when a default
`linux/amd64` build compiles the artifact package. This is not a zero-download
installation. Commands that load all modules, including `go mod tidy`, may
download archives that the final build does not embed.

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
pinned commit. The active bundle was packaged from `88c07cf8ccafebf579e769416251be370b45180e`;
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
github.com/lance-format/lance-go/artifacts/linux-amd64 v0.0.0-20261111213734-a779f4a15524 h1:/qgBNBo3mxbjIzEf/xRot8I5fHXWDMkRxw8BKvPiNug=
github.com/lance-format/lance-go/artifacts/linux-amd64 v0.0.0-20261111213734-a779f4a15524/go.mod h1:hmgfCi9X6+yByjEXjF26x8U4yNmD+NK5tFKAPQ2JXtI=
```

`lance_external_lib` builds compile out `bundle.go`, `cache.go`,
`lock_unix.go`, `lock_windows.go`, and the zstd dependency path. They still use
the platform loader, `golang.org/x/sys/windows`, and `LANCE_LIB_PATH` support
where the platform needs them.

## Verification status and remaining gates

The native build and 15 C/C++ consumer runs at the pinned commit succeeded on
NieR, covering ordinary index progress, commit locks, native write options,
custom namespaces, fragment creation, raw files, and index-name escaping.
Fragment mutation consumers were not run. The library exports all 876 declared
symbols. Packaging round-tripped the
compressed payload to the stripped native input. The public proxy archive
was downloaded and its payload compared byte-for-byte with the tracked module.
The runtime baseline is Ubuntu 24.04.5 with glibc 2.39.

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

In the earlier consumer checks, `go get` downloaded the artifact archive before
the target build in both modes. External mode is not a zero-download contract.
Only one artifact target is published, so these checks do not establish archive
selection across a multi-platform release. Broader glibc/musl coverage, macOS
runtime support, and real Windows DLL build, packaging and cold-cache loading
remain pending.

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
retention. These are unpublished candidates, not a published target or an
accepted default bundle. The workflow publishes the validated install cache
only after the stripped candidate is staged and verified and all consumers of
the cache staging path finish; the cache continues to hold the unstripped
accepted library, and candidates are never cached. The workflow asserts that
the checkout is unchanged, so it cannot activate a selector.

A successful run establishes native build, test, strip, and candidate
verification results for that commit only. Public proxy and sumdb
verification, cold default/external/namespace consumers of a published module,
selector activation, release/tag creation, and acceptance of the candidate
bundles remain pending and are owned by the release process. macOS runtime
support, a deployment floor, and completion of the platform roadmap are not
claimed.
