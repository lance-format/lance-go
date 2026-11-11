# lance-go

Pure-Go bindings for [Lance](https://lancedb.github.io/lance/), built on the
official `lance-c` C ABI without cgo.

## Status

The core API supports dataset creation, append/overwrite, metadata, shared
sessions, projected and filtered scans, row selection, bounded immutable
mutations, staged transactions, and fragment inspection. It uses Arrow Go v18
and requires Go 1.23 or newer. Supply the pinned
native library as described below; bundled artifacts and zero-configuration
installation are still pending.

Linux amd64 has native test coverage. Linux and macOS on amd64 and arm64, plus
Windows arm64, are cross-compiled in CI. Windows amd64 also runs the loader,
cache and Arrow callback unit tests on `windows-2022`. macOS runtime behavior
and real Windows DLL acceptance remain pending; acceptance of a packaged
Windows DLL belongs to the lance-c release process. Index, blob, maintenance
and namespace APIs are tracked in
[issue #1](https://github.com/lance-format/lance-go/issues/1).

## Read a dataset

After building the native library, set `LANCE_LIB_PATH` to its installed path
and run your Go program with `CGO_ENABLED=0`:

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

`WriteUncommitted` writes data files without publishing a version and returns
a `Transaction`. The transaction can be serialized with `MarshalBinary` and
restored with `ParseTransaction`, or built directly from an `Operation`.
`CommitTransaction` publishes one transaction and returns the committed
dataset; `CommitTransactionBatch` atomically commits append transactions.
`Dataset.ReadTransaction` and `ReadTransactionByVersion` read transactions
that the manifest references. Committing is explicit: closing a transaction
does not publish it or delete uncommitted data files.

```go
tx, err := lance.WriteUncommitted(ctx, "data.lance", reader)
if err != nil {
    log.Fatal(err)
}
defer tx.Close()
encoded, err := tx.MarshalBinary()
if err != nil {
    log.Fatal(err)
}
parsed, err := lance.ParseTransaction(encoded)
if err != nil {
    log.Fatal(err)
}
defer parsed.Close()
ds, err := lance.CommitTransaction(ctx, "data.lance", parsed)
if err != nil {
    log.Fatal(err)
}
defer ds.Close()
```

Commit options expose stable row IDs, v2 manifest paths, retry and timeout
policy, detached commits, storage options and sessions. A detached commit
requires v2 manifest paths, does not create a new dataset, and returns a
dataset at the detached version. Transactions and operations are independent;
closing a transaction does not invalidate a dataset that was returned by a
successful commit.

Transaction UUID, tag and properties distinguish absent values from explicitly
empty ones in memory, but protobuf transport is not lossless: empty tags and
property maps normalize to absent, Overwrite schemas may omit metadata, and an
Unknown operation can be inspected but not serialized or committed. Public
writes reassign field IDs; `NativeSchema.Project` preserves the selected
schema's IDs, which can therefore be nonconsecutive.

`Dataset.FragmentIDs` and `Dataset.OpenFragment` expose fragment snapshots.
A bound fragment supports `Scan`, `Take`, `CountRows` and `Delete` without the
source dataset. Fragment scans accept projection, limit, offset, batch sizing,
ordering and row-address options, but not `WithColumns` or `WithSQLFilter`,
which the native fragment scanner cannot apply. `Take` uses fragment-local
logical row indices. `Delete` stages a deletion and returns a new scannable
fragment; build a Delete operation from it and commit the transaction to
publish the deletion. Detached fragments cloned from an Append operation expose
metadata and data-file information but reject data access.

`DataFile` parses an existing Lance data file; `Dataset.CreateDataFile` does
not write one. A `DataReplacementGroup` pairs a fragment ID with a parsed
replacement file, and `NewDataReplacementOperation` builds the corresponding
operation. `NewProjectOperation` accepts a `NativeSchema`, which preserves
native field IDs that the lossy Arrow schema view does not. Schemas, data files
and fragments returned by operations are independently owned and remain valid
after their parent object is closed.

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

## How loading works

`internal/loader` resolves the native library once per process:

1. `LANCE_LIB_PATH`, which must point at an existing regular file. When set,
   it is the only candidate. A bad value fails with the path in the error
   instead of falling back to another library.
2. Otherwise, in the default build, a compiled-in artifact bundle registered
   by a generated platform selector is verified and unpacked into the user
   cache. Cache, extraction and loading errors fail loudly; the loader does
   not fall back to a system copy.
3. Otherwise the platform library name (`liblance_c.so` on Linux,
   `liblance_c.dylib` on macOS, `lance_c.dll` on Windows) is opened through
   the platform loader. Unix honors `LD_LIBRARY_PATH`, `DYLD_LIBRARY_PATH`,
   ldconfig and the usual system directories. Windows applies the process's
   DLL search policy, which can include the application, system and current
   directories and `PATH`.

The library is never unloaded. `lance-c` owns a Tokio runtime, thread-local
error state and handles that may outlive a dataset, so unloading while native
threads or callbacks may still run is not safe. Changing `LANCE_LIB_PATH`
after a successful load has no effect in that process.

Build tags:

- Default builds include the artifact bundle, cache and decompression path,
  but ship no compiled-in artifacts. Without a generated selector they use
  the environment and system-loader steps.
- `-tags lance_external_lib` compiles out the bundle, cache and decompression
  path. Only explicit `LANCE_LIB_PATH` and platform discovery remain.

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
git -C /path/to/lance-c show 407a751a9cfcaa0e868c005ab7a78ee9be599a31:include/lance/lance.h > lance.h
LANCE_C_HEADER=./lance.h go run ./scripts/check-abi-symbols.go
```

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

Unit tests run without the native library. Native tests skip unless
`LANCE_LIB_PATH` names a built `liblance_c`; a configured but broken library
fails instead of skipping.

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
shared sessions, uncommitted and detached transactions, fragment scans and
deletions, protobuf round trips, data replacement, project operations, and
cancellation/close interactions. Arrow bridge tests check
C layouts, producer ownership, release callbacks and stream reader lifetimes.
The race lane also enables Go's pointer checks; native Rust internals are outside
Go's race detector.

The CI workflow runs on pull requests and pushes to `main`. It builds the
pinned native library on Linux and requires the native tests to run rather
than skip. A separate job uses that library with Go's race detector. The
cross-build matrix checks Linux and macOS on amd64 and arm64. A separate
Windows workflow tests Windows amd64 and cross-builds Windows arm64; neither
establishes native Windows DLL support.

## Layout

```text
*.go              public Dataset, Session, Write, Scanner, transaction, fragment and option APIs
internal/cdata/   Arrow C Data import/export and release callbacks
internal/handle/  Go callback state handles
internal/loader/  discovery, platform loading, cache and process-wide singleton
internal/ffi/     native bindings, resource handles, TLS error extraction
scripts/          build-lance-c.sh, check-abi-symbols.go
```

The Arrow bridge is independent of the loader and Lance FFI. Native calls and
thread-local error extraction stay in `internal/ffi`; public resource ownership
and contextual errors stay in the root package.
