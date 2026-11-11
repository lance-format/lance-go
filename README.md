# lance-go

Pure-Go bindings for [Lance](https://lancedb.github.io/lance/), built on the
official `lance-c` C ABI without cgo.

## Status

The core API supports dataset creation, append/overwrite, metadata, shared
sessions, projected and filtered scans, row selection, bounded immutable
mutations, and blob columns with handle-based and planned reads. It uses Arrow Go v18 and requires Go 1.23 or newer.

Default `linux/amd64` builds include a verified compressed `lance-c` payload
and load it automatically after verifying and unpacking it into the user cache.
The payload is a normal Go module dependency, so building or testing fetches it
through the Go module proxy; it is not a download-free installation. Other
targets still need an explicit `LANCE_LIB_PATH` or a system `liblance_c`.

The bundled `linux/amd64` library was built on Ubuntu 24.04.5 with glibc 2.39
and requires libc, libm, libgcc_s and the dynamic loader. Older glibc, musl,
and other targets have no compatible bundle. Linux amd64 has native test
coverage, including the bundled default path. Linux and macOS on amd64 and
arm64, plus Windows arm64, are cross-compiled in CI. Windows amd64 runs loader,
cache and Arrow callback unit tests on `windows-2022`. macOS runtime behavior
and real Windows DLL acceptance remain pending; acceptance of a packaged
Windows DLL belongs to the lance-c release process. Index, maintenance
and namespace APIs are tracked in
[issue #1](https://github.com/lance-format/lance-go/issues/1).

Try the self-contained [roundtrip example](examples/roundtrip/main.go), which
creates a temporary dataset, closes and reopens it, and checks the scanned rows:

```sh
CGO_ENABLED=0 go run ./examples/roundtrip
```

## Read a dataset

On `linux/amd64`, a default build needs no environment variable: the first
call loads the bundled artifact. For other targets, build the native library
and set `LANCE_LIB_PATH` to its installed path. Run your Go program with
`CGO_ENABLED=0`:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/lance-format/lance-go"
)

func main() {
    ctx := context.Background()
    ds, err := lance.Open(ctx, "data.lance")
    if err != nil {
        log.Fatal(err)
    }
    defer ds.Close()
    scan, err := ds.Scan(lance.WithColumns("id"), lance.WithSQLFilter("id >= 0"))
    if err != nil {
        log.Fatal(err)
    }
    defer scan.Close()
    for batch, err := range scan.Records(ctx) {
        if err != nil {
            log.Fatal(err)
        }
        fmt.Println(batch.NumRows())
    }
}
```

`Create`, `Append`, `Overwrite` and `Write` accept an `array.RecordReader`.
The caller owns the reader and releases it after the write. `Append` creates
an absent dataset; `Overwrite` commits a replacement version while preserving
older versions. `WriteParams` exposes the options available in the pinned C ABI.

`Delete`, `Update`, `MergeInsert`, `Merge` and `Truncate` mutate a cloned native
snapshot and return a new committed `Dataset`. The receiver keeps its version
and any open scanners. `MergeInsert` and `Merge` may advance the source reader, including on failure.
The caller retains ownership and releases the reader after the call.

A `Dataset` is a snapshot safe for concurrent reads. Close it when finished;
`Close` waits for in-flight operations. `WithVersion` selects a historical
snapshot, and `WithSession` shares a `Session`'s native caches across datasets.
Close sessions explicitly; datasets already opened from a session remain valid.

A scanner is single-use. Each batch yielded by `Records` is borrowed until
iteration advances: call `Retain` before keeping it and later call `Release`.
Ending iteration, including an early break, closes the scanner. Alternatively,
`ToArrowReader` returns a reader whose final `Release` closes the scanner.
Batches returned by `Take` and `TakeRows` are owned by the caller and must each
be released.

Scan cancellation parks and wakes the goroutine without holding an OS thread
while waiting. Synchronous native operations cannot be interrupted once started.
In particular, a write that commits after context cancellation returns its
committed dataset, avoiding an error that could encourage duplicate writes.

## Raw `.lance` files

The `file` package reads and writes raw Lance files without a dataset or
manifest. Importing it also initializes the default native bundle on
`linux/amd64`; `CGO_ENABLED=0` is sufficient. Dataset APIs remain in the root
`lance` package.

```go
import (
    "github.com/lance-format/lance-go/file"
)

w, err := file.Create(ctx, "data.lance", schema, nil)
if err != nil {
    log.Fatal(err)
}
if err := w.Write(ctx, batch); err != nil {
    log.Fatal(err)
}
summary, err := w.Finish(ctx)
if err != nil {
    log.Fatal(err)
}
if err := w.Close(); err != nil {
    log.Fatal(err)
}

r, err := file.Open(ctx, "data.lance", nil)
if err != nil {
    log.Fatal(err)
}
metadata, err := r.Metadata()
if err != nil {
    log.Fatal(err)
}
info, err := metadata.Info()
if err != nil {
    log.Fatal(err)
}
fmt.Println(info.NumRows, info.NumColumns)

rr, err := r.ReadAll(ctx, file.ReadOptions{})
if err != nil {
    log.Fatal(err)
}
defer rr.Release()
for rr.Next() {
    batch := rr.RecordBatch()
    _ = batch // borrowed until the next Next call
}
if err := rr.Err(); err != nil {
    log.Fatal(err)
}
```

`Reader` reads all rows, a row range, or explicit row indices; each returns an
`array.RecordReader`. Batches are borrowed until the next `Next` call; call
`Retain` before keeping one. `Reader.ReadGlobalBuffer(0)` returns the serialized
schema and user buffers start at index 1. `Writer` accepts Arrow batches and
can add schema metadata and global buffers. `Session` provides local
`contains`, `list`, `list_with_delimiter`, byte-range read, upload, download,
delete, and scheduler/object-store statistics, plus packed and dedicated blob
writers that accept raw bytes.

Contracts and limits:

- `Reader`, `Writer`, `Session`, and blob writers are safe for concurrent use.
  `Close` waits for in-flight calls and is idempotent. Record readers, metadata
  and statistics snapshots, and blob writers already opened stay usable after
  their reader or session is closed.
- Metadata and statistics snapshots copy native-owned strings and bytes.
  Close each snapshot when finished. `Reader.NumRows` and `Reader.Schema` are
  convenience calls that create short-lived native snapshots.
- Option pointers distinguish an unset value from an explicit zero. Batches
  passed to `Writer.Write` are borrowed for the duration of the call.
- Contexts are checked before a native call; they cannot interrupt a native
  blocking call once it starts. No method claims that cancellation stopped I/O.
- Storage options are static only. Pass `file.NewStorageOptions` to a
  standalone reader or writer, and close it after those open calls return.
  Dynamic Go providers, shared `lance.Session` registry reuse, and remote
  cloud acceptance are not part of this slice.
- Blob support here is byte-level: write a blob payload and obtain its
  descriptor array. The descriptor-list `write_blobs` helper is pending.
- Raw v2.2 projection follows native lance-c behavior. The upstream projection
  issue (issue 167) is tracked there; this package does not work around it.

Pending gate: real-library worker tests for file roundtrip, range/index reads,
metadata, global buffers, local session I/O and packed/dedicated blob writers.
Dynamic providers, shared root sessions, `write_blobs`, and remote object-store
acceptance remain open.

## Blob columns

`BlobField` and `BlobFieldWithOptions` return the native `lance.blob.v2` field.
A logical blob row carries either inline `data` bytes or a `uri` reference;
native write validates the layout. `NewBlobDescriptorBuilder` builds the
writer-prepared descriptor representation directly: inline, null, packed,
dedicated, managed and external values, including explicit `None` versus
`Some(0)` base IDs. `Finish` consumes the builder and returns the prepared
array and field; `Field` keeps working until `Close`.

`Dataset.TakeBlobs`, `TakeBlobsByIndices` and `TakeBlobsByAddresses` return one
handle per requested row, preserving order and duplicates. A `nil` handle means
a null blob; an empty blob has size zero. Handles stay readable after the
dataset is closed, and the caller must close each non-nil handle. `BlobFile`
implements `io.Reader`, `io.ReaderAt`, `io.Seeker` and `io.Closer`:
`ReadAt` and `ReadRanges` do not move the cursor, `ReadRanges` validates the
whole request before writing any destination, and a short `ReadAt` returns the
available prefix with `io.EOF`. Seeking past the end is allowed, and
`SetReadAhead` only changes native read-ahead behavior. `BlobFile.Close` waits
for in-flight calls, releases the opaque native handle exactly once and makes
later calls fail with `ErrClosed`; this Go lifecycle is distinct from the
native `mark_closed` operation, which changes native file state without
freeing the handle and is intentionally not exposed.

`ReadBlobs` and `ReadBlobRanges` plan a native read and return an incremental
`array.RecordReader`. `ReadBlobs` yields `row_address` and nullable `data`;
`ReadBlobRanges` yields `request_index`, `row_address`, `offset`, `length` and
nullable `data`, preserving the zero-based input position for duplicate or
unordered requests. Null and empty blobs remain distinct. Returned batches own
their buffers and survive reader release and dataset close; release batches
kept across `Next` calls. The context is checked before planning and between
batches, but a native planning or payload call already in flight cannot be
interrupted.

Scanner blob-handling modes and raw packed/dedicated file writers are not
exposed yet. The lance-c#167 raw v2.2 projection gap is not worked around here;
blob reads use native descriptors.

## How loading works

`internal/loader` resolves the native library once per process:

1. `LANCE_LIB_PATH`, which must point at an existing regular file. When set,
   it is the only candidate. A bad value fails with the path in the error
   instead of falling back to another library.
2. Otherwise, in a default `linux/amd64` build, the artifact bundle registered
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
`<os.UserCacheDir>/lance-go/<uncompressed-sha256>/<libraryName>`; setting
`XDG_CACHE_HOME` moves it on Linux. Cached bytes are checked by exact size and
SHA-256 before `dlopen`. Extraction writes a temporary file, verifies the
decompressed size and hash, syncs it, and publishes it with an atomic rename
under a cross-process lock. A corrupt regular entry is replaced; failed
extraction publishes nothing.

The library is never unloaded. `lance-c` owns a Tokio runtime, thread-local
error state and handles that may outlive a dataset, so unloading while native
threads or callbacks may still run is not safe. Changing `LANCE_LIB_PATH`
after a successful load has no effect in that process.

Build tags:

- Default builds for `linux/amd64` compile the generated selector, the artifact
  package, and its compressed payload into the binary. Other targets have no
  selector, so they use explicit or system discovery.
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

The Go module does not vendor the C header or the native library. To build
both from the exact upstream revision this binding targets:

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

Requirements: git, CMake 3.22+, a C compiler, Cargo with Rust 1.94.0 or newer,
network access for crates and CMake's Corrosion dependency. The first build
compiles all of Lance and takes a long time.

## Pinned ABI and verification

`internal/ffi/upstream.go` records:

- upstream repository and the latest release at pin time,
- the exact commit (`upstreamCommit`),
- the SHA-256 of `include/lance/lance.h` at that commit.

`lance-c` has no version symbol in the pinned ABI, so a loaded library cannot
be identified at runtime. Provenance is checked at build
time instead:

```console
git -C /path/to/lance-c show ab5c2d39c8b912fff19a05486732dde4b49c3f33:include/lance/lance.h > lance.h
LANCE_C_HEADER=./lance.h go run ./scripts/check-abi-symbols.go
```

The published `artifacts/linux-amd64` module records the same upstream commit
and header SHA-256. Packaging verifies the header against this pin; a library
built from a different commit is not detectable at runtime.

The checker refuses a header whose SHA-256 differs from the pin, then verifies
that every bound symbol is declared, that C and Go agree on arity, and that
parameters and return values are pointer or integer classes that `purego` maps
compatibly. It does not model struct layout or distinguish same-width integer
types beyond their signedness and integer width.

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

On Windows, the same package tests run with `CGO_ENABLED=0`; set
`LANCE_LIB_PATH` to a real `lance_c.dll` to run native loader tests. The
Windows CI job currently proves compilation and the loader/cache/callback
unit tests, not a packaged DLL.

Native tests exercise create/close/reopen, metadata, scans, projection/filter/
ordering, row selection, retained batches, write modes, nullable sliced data,
shared sessions, blob fields and descriptors, blob handle and planned reads,
and cancellation/close interactions. Arrow bridge tests check
C layouts, producer ownership, release callbacks and stream reader lifetimes.
The race lane also enables Go's pointer checks; native Rust internals are outside
Go's race detector.

The CI workflow runs on pull requests and pushes to `main`. Its default Go lane
runs `TestCreateReopenMetadataAndScan` with `LANCE_LIB_PATH` unset and a fresh
`XDG_CACHE_HOME`, requires it to pass rather than skip, and checks the unpacked
cache entry's SHA-256. That cache entry can only be created by the registered
bundle, so the check cannot pass by silently loading a system library. A
separate job builds the pinned native library, and a race job uses that library
with Go's race detector. The cross-build matrix checks Linux and macOS on amd64
and arm64; it does not establish native runtime support on those other
platforms. A separate Windows workflow tests Windows amd64 and cross-builds
Windows arm64; neither establishes native Windows DLL support.

## Layout

```text
*.go              public Dataset, Session, Write, Scanner, mutation and option APIs
lance_bundle_linux_amd64.go  generated selector that registers the bundle
artifacts/linux-amd64/       published data-only module and compressed payload
internal/cdata/   Arrow C Data import/export and release callbacks
internal/handle/  Go callback state handles
internal/loader/  discovery, platform loading, cache and process-wide singleton
internal/ffi/     native bindings, resource handles, TLS error extraction
scripts/          build-lance-c.sh, check-abi-symbols.go
```

The Arrow bridge is independent of the loader and Lance FFI. Native calls and
thread-local error extraction stay in `internal/ffi`; public resource ownership
and contextual errors stay in the root package.
