# Native packages

The SDK ships its native library as five immutable data-only Go modules, one per
supported target. This page describes the module design, target and runtime
scope, and validation lanes. The checkout-specific versions, bytes and hashes
live in the sources of truth linked below; this page does not duplicate them.
Publication mechanics live in
[RELEASING.md](../RELEASING.md) and
[scripts/go-module-origin/README.md](../scripts/go-module-origin/README.md).

## Modules

Each module contains `artifact.go`, `go.mod`, `LICENSE`, and one zstd-compressed
payload. `artifact.go` declares the target, library name, payload and library
sizes and SHA-256 values, the native repository and commit, and the canonical
header hash; the payload decompresses byte-for-byte to the shared library in the
matching official `lance-c` release archive.

The modules are served as ordinary Go proxy objects from a static origin, not
published as Git tags, and the root module requires each of them. A published
version is immutable: never replace the bytes behind a version, and never point
a selector or requirement at a candidate that is not yet public.

| Target | Module | Library |
| --- | --- | --- |
| `linux/amd64` | `go.lance.org/liblance/linux-amd64` | `liblance_c.so` |
| `linux/arm64` | `go.lance.org/liblance/linux-arm64` | `liblance_c.so` |
| `darwin/arm64` | `go.lance.org/liblance/darwin-arm64` | `liblance_c.dylib` |
| `windows/amd64` | `go.lance.org/liblance/windows-amd64` | `lance_c.dll` |
| `windows/arm64` | `go.lance.org/liblance/windows-arm64` | `lance_c.dll` |

## Where the values live

- Companion module versions required by this checkout: [`go.mod`](../go.mod).
- Native repository, release, commit and canonical header hash:
  [`internal/ffi/upstream.go`](../internal/ffi/upstream.go).
- Payload and library sizes, SHA-256 values, commit and header: the generated
  `artifact.go` in each companion module.
- Publication identity (version/time, source commit, workflow runs, candidate
  hashes, module h1 values, manifest SHA-256):
  `scripts/go-module-origin/<artifact-version>.identity.json`.
- Published chain and evidence: the immutable GitHub release notes and the
  tag-pinned `CHANGELOG.md`.

## Payload identity

The public C header identity covers the flat `*.h` files directly under
`include/lance`. The C++ `lance.hpp`, the `detail/` implementation tree and
other non-`.h` files are outside it; a split release still has one composite
identity from the sorted header manifest.

The payload sizes and hashes are the values `artifact.go` declares and the loader
verifies. They are not maintained as a second copy in this page.

`lance-c` reports its binding release through `lance_c_version()`, but that
string does not identify the Git commit, the header or the built bytes. The
commit and header hash in `artifact.go` are packaging claims made by the release
pipeline; the Go package does not bind or check them at runtime. What is
verifiable is that the payload decompresses to those bytes, that the module
version resolves through the public proxy and checksum database, and that the
bytes match the official release archive:

- `fetch-lance-c-release` in the `internal/tools` module acquires the archive
  for the pinned release, verifies the published per-asset checksum and the
  canonical header set, and then compares the extracted library with the resolved
  artifact module that this target's generated selector imports. A legacy
  archive keeps the single-file `upstreamHeaderSHA256` check; a split archive
  extracts every public `include/lance/*.h` (excluding `lance.hpp`) and verifies
  the sorted manifest against an independent composite identity. The record
  keeps `header=` for file consumers and adds `abi_header=` for the ABI
  checker; `scripts/abi-checker-input.sh` selects the directory for split
  layouts and the validated file for legacy layouts. A release archive and a
  replacement checksum cannot change the bytes CI tests without failing that
  comparison. The check reports itself as inactive while a new pin has no
  republished module yet.
- `scripts/check-native-library.sh` validates the acquired library against the
  pinned header, object format and architecture, dependency closure, libc or
  macOS deployment floor, and every symbol the Go bindings bind.
- `check-abi-symbols` in the `internal/tools` module verifies that each bound
  symbol is declared by the header with compatible arity and pointer/integer
  classes. It accepts a single legacy header or a verified `include/lance`
  directory; the directory form checks the manifest and parses the union,
  including type-only headers.

The ABI check reports the bound subset of `lance_*` exports declared by the
pinned header; the exact counts are generated from the pin at verification time
and are not maintained here. The ABI check does not establish runtime coverage
of every export.

## Platform and runtime scope

| Target | Native object | Runtime floor | Accepted hosts |
| --- | --- | --- | --- |
| `linux/amd64`, `linux/arm64` | 64-bit ELF shared object | glibc 2.28 | Ubuntu 24.04 |
| `darwin/arm64` | thin 64-bit arm64 dylib | macOS 11.0 deployment target | macOS 15.x |
| `windows/amd64`, `windows/arm64` | 64-bit PE DLL, OS-only import closure | Windows Server 2022 / Windows 11 | hosted Windows runners |

Older glibc and macOS releases, musl, non-Apple-silicon macOS and other
architectures are not supported. The Windows DLLs must not require a
redistributable CRT: the release build uses the static CRT, and the acceptance
lane rejects `vcruntime*`, `msvcp<digit>*`, `concrt*`, `mfc*`, `vcomp*` and
`ucrtbased*` modules while accepting the operating system's `msvcrt.dll`,
`msvcp_win.dll` and `ucrtbase.dll`.

## Validation lanes

Exact-head CI and the native workflows accept the bytes a release will ship;
the public-consumer lanes accept the published modules.

- `.github/workflows/ci.yml` runs the Go suite against the bundled
  `linux/amd64` module with no `LANCE_LIB_PATH`, requires the native test to pass
  rather than skip, checks the cache entry's SHA-256, runs the RTree and raw
  Blob v2 regressions from the checkout, and reruns the suite against the
  acquired release library with the race detector. A cross-build matrix compiles
  Linux amd64/arm64 and macOS arm64 but does not run them.
- `.github/workflows/unix-native.yml` acquires the official archive for
  `linux/amd64`, `linux/arm64` and `darwin/arm64` on native runners, runs the
  header/ABI/export/dependency checks, and runs the default, external and
  race/checkptr test passes plus an external roundtrip against those exact
  bytes. It then stages an unpublished candidate module and verifies its
  payload, and asserts that the checkout is unchanged, so the lane cannot
  activate a selector.
- `.github/workflows/windows-native.yml` does the same for `windows/amd64` and
  `windows/arm64`: PE identity, header and export agreement, the actual
  dependency closure, a native C consumer, the full SDK suite on the default and
  external tags, and a `CGO 1` race lane on amd64. Windows arm64 has no
  supported Go race build, which is recorded as unsupported rather than skipped.
- `.github/workflows/unix-public-consumers.yml` and
  `.github/workflows/windows-public-consumers.yml` resolve one exact official
  release tag through the public proxy and checksum database, then build and run
  default, namespace-only and external consumers on all five targets with cold
  module, build and loader caches. They verify the root archive against the
  checked-out tree, the selected module and payload, the extracted library
  identity and the roundtrip output, and run the default-bundle RTree and raw
  Blob v2 regressions. They run on a root `v*` tag or by dispatch naming that
  tag; a SHA or pseudo-version is rejected.
- `.github/workflows/go-module-origin-checks.yml` runs the origin generator unit
  tests, shell and Python syntax checks, and a mock publisher test. It does not
  reach the origin.

A build or test success is not acceptance, cross-compilation is not runtime
coverage, a native runtime check is not a public archive check, and a candidate
module is not a published one. Record each state separately for the exact
release candidate.

## Loading and cache

The loader verifies the payload, extracts the library to
`<cache-root>/lance-go/<uncompressed-sha256>/<libraryName>`, verifies the
extracted bytes, and loads them under a cross-process lock. `LANCE_LIB_PATH`
bypasses the bundle and the cache. See
[the loader description](development.md#how-loading-works) for the full
resolution order, build tags and cache properties.
