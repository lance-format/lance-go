# Packaged lance-c artifacts

This directory contains the published `linux/amd64` artifact module and
documents the packaging interface between the loader and release artifacts.
The tracked `artifacts/linux-amd64/liblance_c.so.zst` is the production
compressed payload; no raw shared library, static archive, or build evidence is
tracked. Windows companion candidates are generated outside the checkout by the
native evidence lane and are not tracked or published.

## Active linux/amd64 bundle

The root module requires the published data-only module:

```text
require github.com/lance-format/lance-go/artifacts/linux-amd64 v0.0.0-20261111235843-ac5955a095d0
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
| Compressed size | 85696976 bytes |
| Compressed SHA-256 | `e933fc867b2a9ee404d01959aaef8dc8378ddf0b582d7bb2367ed3dbfe46c5b5` |
| Uncompressed size | 322099824 bytes |
| Uncompressed SHA-256 | `e5571d6da16e81a0434f2509ade5262785b63df0b37ff7155537642841dd9378` |
| Upstream commit | `ac2634c553b450c7787577ffe964fc2b283b8334` |
| Header SHA-256 | `c19789e9e003930c6065181b5cd9f6f7b464e08a5dc4daca8b21ad82d5160f5b` |

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

`linux/amd64` is the only published target bundle. The packaged library was
built on Ubuntu 24.04.5 with glibc 2.39 and requires libc, libm, libgcc_s and
the dynamic loader. Do not assume it works on older glibc, musl, Windows, or
other architectures; no bundle or runtime support is claimed for them.

The CI cross-build matrix compiles other targets but does not run them and does
not supply their artifacts. Each additional target needs its own payload,
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
remains Ubuntu 24.04.5 with glibc 2.39. Full Go SDK, race/checkptr, fresh-bundle
and public-consumer acceptance for this paired update is pending.

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
runtime support, Windows module publication, selector activation, and hosted
Windows cold-cache loading remain pending. Windows native execution evidence and
an unpublished candidate are not a supported Windows distribution.
