# Native packages

The SDK ships its native library as five immutable data-only Go modules, one per
supported target. This page records what those modules contain, which bytes they
carry, and how a release is validated. Publication mechanics live in
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

| Target | Module | Version | Library |
| --- | --- | --- | --- |
| `linux/amd64` | `go.lance.org/liblance/linux-amd64` | `v0.2.0-preview.1` | `liblance_c.so` |
| `linux/arm64` | `go.lance.org/liblance/linux-arm64` | `v0.2.0-preview.1` | `liblance_c.so` |
| `darwin/arm64` | `go.lance.org/liblance/darwin-arm64` | `v0.2.0-preview.1` | `liblance_c.dylib` |
| `windows/amd64` | `go.lance.org/liblance/windows-amd64` | `v0.2.0-preview.1` | `lance_c.dll` |
| `windows/arm64` | `go.lance.org/liblance/windows-arm64` | `v0.2.0-preview.1` | `lance_c.dll` |

## Payload identity

All five modules come from the annotated `lance-c v0.2.1-beta.3` tag, peeled to
`a800fd5f28636bfff34778e3ad6663f3cf099a38`, with canonical header SHA-256
`6bab6899ce240b93cf47a1c93f7fb4c8bb2b20ba0a82e3c4a0ec04e7bfc915ad`. The payload
sizes and hashes below are the values `artifact.go` declares and the loader
verifies.

| Target | Payload | Compressed size | Compressed SHA-256 | Library size | Library SHA-256 |
| --- | --- | --- | --- | --- | --- |
| `linux/amd64` | `liblance_c.so.zst` | 88,580,128 | `48c83c5ded9f376b3aecfd6bb6b1994de6a1d7790031df9134d8749405aa9231` | 345,989,752 | `011c3bcc207ff5dd4ee0376d63338d9d05a76083ef45c616f7047978c4c59ece` |
| `linux/arm64` | `liblance_c.so.zst` | 84,254,349 | `5315893c2987c2998ddd87e84999e0770fc079b844d484613c05f4c5e1e7b3d8` | 309,833,920 | `752c5d8d8c90b873a4158b0001399b0460dccb0a7e19d7392131582e32daaa40` |
| `darwin/arm64` | `liblance_c.dylib.zst` | 78,140,628 | `6e4971c0f150a5db0e2fd6f0b7f4c432ecf663f73e3cb87135516fd08c2ee0a8` | 291,530,320 | `1e1502fec5c7d7e02b02ab1ce2f28e3ade263faf8058076ac25178abe6263816` |
| `windows/amd64` | `lance_c.dll.zst` | 99,988,834 | `d4c51d7f6bd7fba506b64d7569bcd99dd7a30351c9320b59b771ccba450c75cd` | 405,414,912 | `49544bd8d9d5bf2069a53cd3c43ccd0643a6a3767c2510058b13bc21ca2990d5` |
| `windows/arm64` | `lance_c.dll.zst` | 90,011,278 | `4b3f3e87f02c90bfdb47d89536416d8ed9c3ecbded8243d2f35e359ce16c62b1` | 331,538,432 | `091179ddb272c47d426fe1a334c4059fc1c17072874dc600a7e6696509a243f8` |

`lance-c` reports its binding release through `lance_c_version()`, which returns
`0.2.1-beta.3` for this pin, but that string does not identify the Git commit,
the header or the built bytes. The commit and header hash in `artifact.go` are
packaging claims made by the release pipeline; the Go package does not bind or
check them at runtime. What is verifiable is that the payload decompresses to
those bytes, that the module version resolves through the public proxy and
checksum database, and that the bytes match the official release archive:

- `fetch-lance-c-release` in the `internal/tools` module acquires the archive
  for the pinned release, verifies the published per-asset checksum and the
  canonical header set, and then compares the extracted library with the resolved
  artifact module that this target's generated selector imports. A legacy
  archive keeps the single-file `upstreamHeaderSHA256` check; a split archive
  extracts every public `include/lance/*.h` (excluding `lance.hpp`) and verifies
  the sorted manifest against an independent composite identity. A release
  archive and a replacement checksum cannot change the bytes CI tests without
  failing that comparison. The check reports itself as inactive while a new pin
  has no republished module yet.
- `scripts/check-native-library.sh` validates the acquired library against the
  pinned header, object format and architecture, dependency closure, libc or
  macOS deployment floor, and every symbol the Go bindings bind.
- `check-abi-symbols` in the `internal/tools` module verifies that each bound
  symbol is declared by the header with compatible arity and pointer/integer
  classes. It accepts a single legacy header or a verified `include/lance`
  directory; the directory form checks the manifest and parses the union,
  including type-only headers.

The pinned `lance-c v0.2.1-beta.3` library exports 890 `lance_*` symbols; the Go
bindings bind 608 of them. The ABI check covers that bound subset and does not
establish runtime coverage of every export.

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
