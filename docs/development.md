# Development and testing

The [consumer installation](../README.md#supported-targets) uses official
prebuilt libraries. The source-build and ABI checks below are for binding
development.

## How loading works

`internal/loader` resolves the native library once per process:

1. `LANCE_LIB_PATH`, which must point at an existing regular file. When set,
   it is the only candidate. A bad value fails with the path in the error
   instead of falling back to another library.
2. Otherwise, in a default build with a published selector, the artifact bundle registered
   by the generated selector. The compressed payload is verified, unpacked
   into the private user cache, verified again, and loaded. Once a bundle is
   registered for the running target, cache, extraction, or `dlopen` errors
   fail with the bundle module in the error; the loader does not fall back to
   a system copy.
3. Otherwise the platform library name (`liblance_c.so` on Linux,
   `liblance_c.dylib` on macOS, `lance_c.dll` on Windows) is opened through
   the platform loader. Unix honors `LD_LIBRARY_PATH`, `DYLD_LIBRARY_PATH`,
   ldconfig and the usual system directories. Windows applies the process's
   DLL search policy, which can include the application, system and current
   directories and `PATH`.

The bundle cache is
`<cache-root>/lance-go/<uncompressed-sha256>/<libraryName>`, where
`<cache-root>` is `LANCE_CACHE_DIR` when non-empty and `os.UserCacheDir`
otherwise. On Linux, `XDG_CACHE_HOME` moves the default user cache. The first
bundled load must be able to create and write this directory. If the cache is
read-only or unavailable, set `LANCE_CACHE_DIR` to a writable path, or set
`LANCE_LIB_PATH` to the exact library for that SDK release; the environment
path is checked first, so no cache write is attempted. Cached bytes are checked
by exact size and
SHA-256 before `dlopen`. Extraction writes a temporary file, verifies the
decompressed size and hash, syncs it, and publishes it with an atomic rename
under a cross-process lock. A corrupt regular entry is replaced; failed
extraction publishes nothing. Sibling `<uncompressed-sha256>` directories are
retained on purpose: the entry lock covers verification, repair and
publication, and ends before the returned path is opened, so it cannot prove
that another process or an older SDK has finished with an entry. Reclamation
is an explicit operator decision, not an automatic loader policy.

All artifact module downloads happen through Go module resolution and the
build; the loader itself does not use the network. A default build embeds the selected
payload, while an external build expects the operator to supply the library.

The library is never unloaded. `lance-c` owns a Tokio runtime, thread-local
error state and handles that may outlive a dataset, so unloading while native
threads or callbacks may still run is not safe. Changing `LANCE_LIB_PATH`
after a successful load has no effect in that process.

If a bundled load fails with a permission error under the cache path, provide a
writable cache for the process or set `LANCE_LIB_PATH` to the exact library for
the installed SDK. Do not substitute a library from another Go or `lance-c`
release; the loader cannot detect that mismatch before native calls fail.

### Build tags

- Default builds for the five selected targets compile the generated selector,
  the artifact package, and its compressed payload into the binary. Other
  targets, including `darwin/amd64`, have no selector, so they use explicit or
  system discovery. The loader never falls back to an Intel payload that a new
  release does not require.
- `-tags lance_external_lib` excludes the selector, bundle/cache/decompression
  code, and the artifact package from compilation and embedding. The artifact
  module requirement remains in `go.mod` and in the module graph; commands
  that load all modules, such as `go mod tidy`, may still download its archive.
  External builds use only `LANCE_LIB_PATH` and system discovery.

All Go code is `CGO_ENABLED=0` compatible. `purego` provides the Unix
`dlopen`/`dlsym` calls and the callback trampolines on every platform. Windows
uses `golang.org/x/sys/windows` for `LoadLibrary`, `GetProcAddress` and
`LockFileEx`, because purego v0.10 does not export those loading entry points.

## Building liblance_c

This is maintainer and native-development work, not a supported consumer
installation path. The Go module does not vendor the C header or the native
library. To build both from the exact upstream revision this binding targets:

```console
./scripts/build-lance-c.sh
```

The script reads the pinned commit and header SHA-256 from
`internal/ffi/upstream.go`, clones/fetches `lance-c` into
`${XDG_CACHE_HOME:-$HOME/.cache}/lance-c-build/src`, checks out the pinned
commit, verifies the header hash, builds with CMake/Corrosion in Release mode,
and installs into `dist/lance-c`. The output directory must not already exist.
Use `--out DIR` to choose another prefix and `LANCE_C_WORK_DIR` to move the
checkout/build cache. Build intermediates are reused across runs.

Requirements: git, CMake 3.22+, a C compiler, Cargo with Rust 1.95.0 or newer
(the pinned `lance-c` `Cargo.toml` declares this minimum and
`scripts/build-lance-c.sh` rejects older toolchains), network access for crates
and CMake's Corrosion dependency. The first build compiles all of Lance and
takes a long time.

CI and release packaging do not use this path. They acquire the official release
archive for `upstreamRelease` through `internal/tools/fetch-lance-c-release`,
which verifies the published per-asset checksum, the pinned header identity, and
the target machine. It also compares the acquired library with the
`UncompressedSHA256` that the adopted artifact module declares; after a pin bump
that check stays inactive until the new modules are published. Build from source
when you are changing `lance-c`, or when the pin has no matching release
archive.

A legacy archive ships only `include/lance/lance.h`, which is verified against
`upstreamHeaderSHA256`. A split archive ships every public
`include/lance/*.h`; the tool extracts all of them, excludes `lance.hpp`, and
requires an independent composite identity from `-expected-header-identity`,
`-expected-header-manifest`, or the optional `upstreamHeaderIdentitySHA256` pin
constant. The identity is the SHA-256 of the sorted
`include/lance/<name> <sha256>` manifest text, and the acquisition writes
`HEADER-MANIFEST` plus `COMPOSITE-SHA256` for split archives. The acquisition
record keeps `header=` as the legacy single file for file consumers and adds
`abi_header=` as the path to pass to the ABI checker; `scripts/abi-checker-input.sh`
selects it and falls back to `header=` for historical records.

## Pinned ABI and verification

`internal/ffi/upstream.go` records:

- upstream repository and the latest release at pin time,
- the exact commit (`upstreamCommit`),
- the SHA-256 of `include/lance/lance.h` at that commit.

The Go package does not bind lance-c's `lance_c_version`, which reports only
the binding release, so the SDK does not report or check a loaded library's
version at runtime. Provenance is checked at build time instead:

```console
PINNED_COMMIT=$(sed -n 's/^[[:space:]]*upstreamCommit[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' internal/ffi/upstream.go)
test -n "$PINNED_COMMIT"
git -C /path/to/lance-c show "$PINNED_COMMIT:include/lance/lance.h" > lance.h
LANCE_C_HEADER="$PWD/lance.h" go -C internal/tools run ./check-abi-symbols
```

Packaging verifies the header against this pin; a library built from a different
commit is not detected by the SDK at runtime. When the pin changes, all five
modules (`linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`,
`windows/arm64`) must be regenerated, published as new immutable module
versions, and adopted in the selectors and root requirements together. Follow
[RELEASING.md](../RELEASING.md) for the exact source, package, publication and
verification sequence.

For a single legacy header, the checker refuses bytes whose SHA-256 differs from
`upstreamHeaderSHA256`. For a split release, pass the `include/lance` directory
with `-header` and supply `-header-manifest` or `-header-identity` (the pin's
optional `upstreamHeaderIdentitySHA256` is another source). The directory check
reports missing, changed and unexpected headers before parsing the union of all
files. Type-only headers such as `fwd.h` may contain no `lance_*` function
declarations but remain part of the manifest. The checker then verifies that every bound
symbol is declared, that C and Go agree on arity, and that parameters and return
values are pointer or integer classes that `purego` maps compatibly. It does not
model struct layout or distinguish same-width integer types beyond their
signedness and integer width.

## Native error contract

`lance-c` signals failures through return values and stores error details in
Rust thread-local storage:

1. The failing call runs while the goroutine is locked to its OS thread.
2. `lance_last_error_code()` is read first.
3. `lance_last_error_message()` is then read; it takes the pending error out
   of TLS and returns an owned C string.
4. The message is copied into Go memory and released with
   `lance_free_string()`.

Reading the message before the code would always observe `LANCE_OK`, and using
the message after the free would be a use-after-free. `internal/ffi` owns this
order in `(*bindings).takeError`; keep native calls that can fail on the same
locked thread until the error has been consumed.

`Dataset.Close` is idempotent: only the first call reaches
`lance_dataset_close`, so double closes cannot free the same handle twice.

## Testing

Unit tests run without a native library. On `linux/amd64` the default build
registers the published bundle, so the public tests run without
`LANCE_LIB_PATH`; a configured but broken library fails instead of skipping.

```console
gofmt -l .
go mod tidy -diff
go vet ./...
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go build -tags lance_external_lib ./...
go test ./...
go test -tags lance_external_lib ./...

export LANCE_LIB_PATH="$PWD/dist/lance-c/lib/liblance_c.so"   # or .dylib
CGO_ENABLED=0 go test -count=1 -v ./...
CGO_ENABLED=0 go test -count=1 -tags lance_external_lib ./...
CGO_ENABLED=1 go test -count=1 -race ./...
```

The RTree regression test is build-tagged so it can be selected explicitly.
The bundled library is RTree-capable; run the test through the default bundle
with no external library:

```console
env -u LANCE_LIB_PATH XDG_CACHE_HOME="$(mktemp -d)" \
  CGO_ENABLED=0 go test -tags lance_rtree_native -run '^TestRTree' -count=1 -v .
```

`TestFragmentTakeExplicitEmptyProjection` covers the fragment projection
compatibility contract from lance-c v0.2.1-beta.1: an explicit empty column
list returns a zero-column batch with the requested row count, while a nil
column list still selects the full schema. It runs in the default suite against
the bundled library.

`internal/benchmarks/soak` is an opt-in Linux RSS and lifecycle benchmark for
long-running repeated use of the public API. CI compiles the package and runs
its tests, but does not execute the long benchmark.
See [`internal/benchmarks/soak/README.md`](../internal/benchmarks/soak/README.md)
for build and run commands, the measurement contract and the limits of RSS
evidence.

### Microbenchmarks

Standard Go benchmarks cover the Arrow C Data export, import and borrowed-batch
copy paths, handle registration, `Scanner.Records` streaming and both loader
phases. They use the Go 1.26 minimum with `b.Loop()` and normal public inputs:

```console
CGO_ENABLED=0 go test -run '^$' -bench . -benchmem ./...
```

Benchmarks that need the native library skip unless it is available through the
bundled library or `LANCE_LIB_PATH`. The root `BenchmarkLoaderWarmLoad` measures
repeated in-process `Load` calls after the first open, while
`BenchmarkBundleWarmCacheVerify` in `internal/loader` measures a warm cache hit:
the shared lock plus size and SHA-256 verification. The cache benchmark uses a
temporary cache root, so it does not read or write the user's cache.

On Windows, the same package tests run with `CGO_ENABLED=0`; set
`LANCE_LIB_PATH` to a real `lance_c.dll` to run native tests. The native Windows
workflow builds the pinned DLL with a static CRT on windows/amd64 and
windows/arm64 and runs the full external-tag and default-tag suites against it,
requiring every test to pass without skips. It also classifies direct and
delay-load imports and runs a `/MT` native consumer that rejects redistributable
CRT modules before and after native operations. On windows/amd64 it also runs
the full external-tag suite with `-race`; the Go race detector has no
windows/arm64 support, so that lane is recorded as unsupported rather than
skipped. The CI workflow does not publish modules; published Windows modules and
their cold public consumers are recorded in [native
packages](../docs/native-packages.md).

Native tests exercise create/close/reopen, metadata, scans, projection/filter/
ordering, row selection, retained batches, write modes, nullable sliced data,
shared sessions, version history and restore, tags, branches, uncommitted and
detached transactions, fragment scans and deletions, protobuf round trips,
data replacement, project operations, blob fields and descriptors, blob handle
and planned reads, and cancellation/close interactions. Arrow bridge tests check
C layouts, producer ownership, release callbacks and stream reader lifetimes.
The race lane also enables Go's pointer checks; native Rust internals are outside
Go's race detector.

The CI workflow runs on pull requests and pushes to `main`. It also runs
[`scripts/verify-quickstart.sh`](../scripts/verify-quickstart.sh) in local-source
mode with `LANCE_GO_QUICKSTART_SOURCE_DIR`, which builds a consumer module that
replaces the root module with the checkout, checks the roundtrip output and
the selected artifact module, then reruns the example with the exact native
library extracted and a read-only runtime cache. The same job runs the bundled RTree and
raw Blob v2 regressions from the checkout. Neither step resolves the root
revision through the public proxy; the artifact module version still comes from
the public proxy and checksum database. The default Go lane
runs `TestCreateReopenMetadataAndScan` with `LANCE_LIB_PATH` unset and a fresh
`XDG_CACHE_HOME`, requires it to pass rather than skip, and checks the unpacked
cache entry's SHA-256. That cache entry can only be created by the registered
bundle, so the check cannot pass by silently loading a system library. A
separate job acquires the pinned `lance-c` release library and runs the native
suite against it; a race job reuses that library with Go's race detector. The
cross-build matrix checks Linux amd64 and arm64 plus macOS arm64; it does not
establish native runtime support on those other platforms. A separate Windows
workflow tests Windows amd64 and arm64; its native evidence lane runs the full
Go SDK suite against the pinned release DLL on both hosted runners, with a
`CGO 1` race lane on amd64.

## Go version support

The module declares and tests an explicit Go floor; there is no automatic bump
schedule. As of 2026-11-24 the approved floor is Go 1.26, with Go 1.27 as the
current stable release. CI sets `GOTOOLCHAIN=local` so each lane runs the floor
or the exact installed stable toolchain instead of silently switching.

- `.github/workflows/ci.yml` and `windows.yml` test `1.26.x` and `stable`.
- Lanes with `go-version-file: go.mod` (native, cross, module-origin and
  public-consumer workflows) run the floor.
- `scripts/public-consumer/run.sh` and `run-windows.ps1` require `go1.26.x` from
  `GOTOOLCHAIN=local`, and their fixture modules declare `go 1.26.0`.
- `internal/tools/artifactgen` and `scripts/go-module-origin/generator` write
  the same directive into newly published artifact modules. Published artifact
  modules are immutable and keep the directive they were published with.
- `internal/tools` is a nested module and declares the same floor; CI runs its
  tidy, vet, build and tests with the matrix toolchains.

A future floor change is a separate minor-release decision, not a consequence
of an upstream Go release. When one is approved, update these declarations
together:

- the `go` directive in `go.mod` and `internal/tools/go.mod`;
- the Go matrices and their minimum-version conditionals in the CI workflows;
- the artifact module generators and their fixture tests;
- the public-consumer drivers, fixtures and toolchain assertions;
- README, this document and RELEASING.md.

Patch releases do not change the floor. The minor release's migration notes
state the new minimum and the toolchain behavior change.

## Layout

Maintenance commands, compatibility fixtures and the soak benchmark live under
`internal/` so they are not part of the public SDK package surface. On the root
module, `go list ./...` exposes only the public SDK packages, `internal/...`,
the `scripts/`-hosted harness sources, and user-facing `examples/...`.

```text
*.go              public Dataset, Session, Write, Scanner, transaction, fragment and option APIs
namespace/        typed native namespace API and managed dataset operations
internal/bundled/ shared zero-config selector registration for root and namespace
internal/cdata/   Arrow C Data import/export and release callbacks
internal/handle/  Go callback state handles
internal/loader/  discovery, platform loading, cache and process-wide singleton
internal/ffi/     native bindings, resource handles, TLS error extraction
internal/tools/   maintainer Go commands and artifact tooling, in their own Go module
internal/benchmarks/soak/  opt-in Linux RSS and lifecycle benchmark (long runs are opt-in)
internal/compat/            cross-language compatibility harness
scripts/          build-lance-c.sh, verify-quickstart.sh, shell/PS/Python harnesses
```

`internal/tools` is a nested Go module
(`github.com/lance-format/lance-go/internal/tools`), so its dependencies —
currently `github.com/klauspost/compress` and `github.com/ulikunitz/xz` — never
enter the root module graph or the published root module ZIP. Run its commands
from the repository root with `go -C internal/tools run ./<command>`; the tools
locate the repository root from their working directory, and `-root` or `-pin`
still override it. `go -C` makes `internal/tools` the tool process's working
directory, so pass absolute paths for `-root`, `-pin`, `-header`, `-out`,
`-module`, `-library` and `LANCE_C_HEADER`; relative values resolve there, not
in the caller's directory. The public-consumer fixture programs live in
`scripts/public-consumer/consumer-src/` because they deliberately compile
against the published root SDK rather than the working tree.

The Arrow bridge is independent of the loader and Lance FFI. Native calls and
thread-local error extraction stay in `internal/ffi`; public resource ownership
and contextual errors stay in the root package.
