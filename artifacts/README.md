# Packaged lance-c artifacts

This directory contains the published `linux/amd64` artifact module and
documents the packaging interface between the loader and release artifacts.
The tracked `artifacts/linux-amd64/liblance_c.so.zst` is the production
compressed payload; no raw shared library, static archive, or build evidence is
tracked.

## Active linux/amd64 bundle

The root module requires the published data-only module:

```text
require github.com/lance-format/lance-go/artifacts/linux-amd64 v0.0.0-20261111143555-4bc42e026349
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
| Compressed size | 81695913 bytes |
| Compressed SHA-256 | `33c4ee3249ee73ddf6a07855153abeb42801134f993309718e40bfacdabcd6af` |
| Uncompressed size | 308280480 bytes |
| Uncompressed SHA-256 | `dfe3b1135725daeb876998befce18b58117074ffef302850a53f21e8376b7363` |
| Upstream commit | `07cd7d0cd06d65c5c643972267cc7981decef067` |
| Header SHA-256 | `12981740fff3c240c7a5f3d46d8b6cd85e64a848d2bf421b1846286b774fbc4b` |

`lance_bundle_linux_amd64.go` in the root package is the generated selector.
Its build constraint is `!lance_external_lib && linux && amd64`. The `init`
function registers the descriptor, so a default `linux/amd64` import of the
root package uses the bundle after `LANCE_LIB_PATH` is absent and before the
system loader. `LANCE_LIB_PATH` still takes precedence when set. Once a bundle
is registered, verification, extraction, and `dlopen` errors are returned with
the bundle module in the error; the loader does not fall back to a system copy.

The compressed payload is data. Importing the artifact module does not link,
load, or execute native code; the loader decompresses and maps it on first use.

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

`selector/` contains a file for the root package. Its build constraint is
`!lance_external_lib && <goos> && <goarch>`. It imports the artifact module
and registers the descriptor with `internal/loader` in an `init` function. Copy
the selector into the root package after publishing the artifact module and
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
pinned commit. The active bundle was packaged from `07cd7d0cd06d65c5c643972267cc7981decef067`;
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
github.com/lance-format/lance-go/artifacts/linux-amd64 v0.0.0-20261111143555-4bc42e026349 h1:ONS0APX2hx2QnE1sApwS/XJ2Dqk2ix+VR1WvECRcNow=
github.com/lance-format/lance-go/artifacts/linux-amd64 v0.0.0-20261111143555-4bc42e026349/go.mod h1:hmgfCi9X6+yByjEXjF26x8U4yNmD+NK5tFKAPQ2JXtI=
```

`lance_external_lib` builds compile out `bundle.go`, `cache.go`,
`lock_unix.go`, `lock_windows.go`, and the zstd dependency path. They still use
the platform loader, `golang.org/x/sys/windows`, and `LANCE_LIB_PATH` support
where the platform needs them.

## Verification status and remaining gates

The native build and C++ smoke test at the pinned commit succeeded on NieR.
The library exports all 613 declared symbols. Packaging round-tripped the
compressed payload to the stripped native input. The public proxy archive
was downloaded and its payload compared byte-for-byte with the tracked module.
The runtime baseline is Ubuntu 24.04.5 with glibc 2.39.

The CI default Go lane exercises the real public path: it runs
`TestCreateReopenMetadataAndScan` with `LANCE_LIB_PATH` unset and a fresh
`XDG_CACHE_HOME`, requires a pass rather than a skip, and verifies the unpacked
cache entry's SHA-256. Loader and generator unit tests cover cache reuse and
corruption, cross-process locking, environment precedence, descriptor
validation, and synthetic target headers.

Independent default/external consumers were previously accepted with the ab5
artifact. Those results establish the distribution contract, not acceptance
of this updated native payload. The matching transaction and public bundled
consumer gates must run against this revision before delivery.

In the earlier consumer checks, `go get` downloaded the artifact archive before
the target build in both modes. External mode is not a zero-download contract.
Only one artifact target is published, so these checks do not establish archive
selection across a multi-platform release. Broader glibc/musl coverage, macOS
runtime support, and real Windows DLL build, packaging and cold-cache loading
remain pending.
