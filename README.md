# lance-go

Pure-Go bindings for [Lance](https://lancedb.github.io/lance/), built on the
official `lance-c` C ABI without cgo.

## Status

The core API supports dataset creation, append/overwrite, metadata, shared
sessions, projected and filtered scans, row selection, vector search, bounded
immutable mutations, version history, tags, and branches. It uses Arrow Go v18
and requires Go 1.23 or newer.

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
Windows DLL belongs to the lance-c release process. Index, blob, maintenance
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

`AddColumnsSQL`, `AddColumnsNulls`, `AddColumnsStream`, `AddColumnsBatchUDF`,
`AlterColumns` and `DropColumns` return independent dataset snapshots. Metadata
and config updates use the same snapshot contract, with explicit merge/replace
modes; a nil metadata value deletes a key, while an empty string is stored.

`BatchUDF` supports batch and fragment checkpoints. The mapper borrows a Go-owned
copy of its native input and returns an owned batch reference. Call `Retain`
before keeping the input or returning a reference owned elsewhere. Checkpoint
`GetBatch` also returns an owned reference; `InsertBatch` lends its batch for
the callback, so retaining it is required to keep it. These operations run
synchronously and check context cancellation before entering native work.

A `Dataset` is a snapshot safe for concurrent reads. Close it when finished;
`Close` waits for in-flight operations. `WithVersion` selects a historical
snapshot, and `WithSession` shares a `Session`'s native caches across datasets.
Close sessions explicitly; datasets already opened from a session remain valid.

## Compaction and cleanup

`CompactFiles` rewrites small fragments through the native V2 compaction API
and returns a new committed `Dataset` plus fragment and file metrics. The
receiver keeps its version, rows and open scanners. Native compaction can commit
fragment-ID reservations before the rewrite, so a successful operation can advance
the stored version more than once. A nil options pointer uses
the dataset configuration; a non-nil `compaction.Options` starts from native
defaults and applies only the fields that are set.

For distributed work, `PlanCompaction` captures an immutable plan. Plans, tasks
and rewrite results expose native JSON with `JSON`, and
`CompactionPlanFromJSON`, `CompactionTaskFromJSON` and
`RewriteResultFromJSON` restore them. `CompactionTask.Execute` writes new data
files without publishing metadata; pass its result to `CommitCompaction` to
publish it. Plan and task options include the exact data storage version
resolved by native planning.

```go
targetRows := uint64(1 << 20)
plan, err := ds.PlanCompaction(ctx, &compaction.Options{
    TargetRowsPerFragment: &targetRows,
})
if err != nil {
    return err
}
task, err := plan.Task(0)
if err != nil {
    return err
}
result, err := task.Execute(ctx, ds)
if err != nil {
    return err
}
taskOptions := task.Options()
committed, _, err := ds.CommitCompaction(ctx,
    []*lance.RewriteResult{result}, &taskOptions)
if err != nil {
    return err
}
defer committed.Close()
```

`CleanupOldVersions`, `Cleanup` and `CleanupExplain` expose native old-version
deletion. Cleanup is destructive storage maintenance: it does not take a
snapshot lease, does not change the current dataset version, and cannot roll
back files already deleted after a later failure. An explanation observes the
current state and is not a reservation; `Cleanup` re-evaluates references when
it executes.

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

## Vector search

`WithNearest` performs exact k-nearest-neighbor search when the dataset has no
vector index, and returns rows ordered by `_distance`. Build the query with one
of the typed constructors; the constructor copies the input:

```go
query := lance.NewFloat32Vector([]float32{0.1, 0.2, 0.3, 0.4})
scan, err := ds.Scan(
    lance.WithNearest("embedding", query, 10),
    lance.WithVectorMetric(lance.VectorMetricL2),
)
if err != nil {
    log.Fatal(err)
}
for batch, err := range scan.Records(ctx) {
    // batch also contains the dataset columns and `_distance`.
}
```

`NewFloat16Vector` takes IEEE 754 binary16 bit patterns in `uint16` values,
not pre-converted `float16.Num` values. `NewFloat64Vector`, `NewUInt8Vector`
and `NewInt8Vector` cover the remaining element types. The query type and
dimension must match the column, which must be a fixed-size list of that
primitive type.

For independent queries, pass a row-major matrix and its dimension:

```go
queries := lance.NewFloat32Vector([]float32{
    0.1, 0.2, 0.3, 0.4,
    0.5, 0.6, 0.7, 0.8,
})
scan, err := ds.Scan(lance.WithNearestBatch("embedding", queries, 4, 10))
// Results carry `query_index`; rows from different queries are not globally ordered.
```

Batch search accepts at most 128 queries and 64 MiB of query data, and
`numQueries * k * refineFactor` (using 1 when unset) must not exceed 100000.
It cannot be combined with `WithLimit` or `WithOffset`; apply a window per
`query_index` in the caller instead.

A multi-vector column stores a list of vectors per row. Pass one or more query
subvectors as a row-major matrix:

```go
subvectors := lance.NewFloat32Vector([]float32{
    0.1, 0.2, 0.3, 0.4,
    0.5, 0.6, 0.7, 0.8,
})
scan, err := ds.Scan(lance.WithNearestMultiVector("embeddings", subvectors, 4, 10))
```

Multi-vector scores sum, for each query subvector, its smallest distance to any
stored subvector. Multi-vector search accepts float16, float32 and float64
queries, at most 128 subvectors, and at most 100000 candidates for both
`numVectors * k` and `k * refineFactor`. L2 is the default metric for
multi-vector search.

Distances are returned as `float32` in `_distance`. L2 is squared Euclidean
distance, cosine is `1 - cosine_similarity`, dot is `1 - dot_product`, and
Hamming is only valid for `uint8` columns.

### Query settings

These options configure native vector search. A setting is only sent when it
is set; otherwise the native default, dataset metric or index configuration
applies.

| Option | Effect |
| --- | --- |
| `WithVectorMetric` | Overrides the dataset or index metric. |
| `WithVectorNProbes` | Probes a fixed number of IVF partitions; cannot be combined with the min/max variants. |
| `WithVectorMinNProbes`, `WithVectorMaxNProbes` | Bounds the adaptive IVF probe count. |
| `WithVectorEF` | Sets the HNSW `ef` search parameter. |
| `WithVectorRefineFactor` | Re-ranks `k * factor` candidates using stored vectors; positive. |
| `WithVectorSearchEffort` | Trades accuracy for speed in `[0,1]`; overrides a fixed nprobes value, while explicit min/max bounds remain effective. |
| `WithVectorApproxMode` | Selects fast, normal or accurate index search. |
| `WithVectorQueryParallelism` | Sets parallel query batches; `-1` lets native code choose. |
| `WithVectorUseIndex` | Allows or disallows vector index use. |
| `WithVectorFastSearch` | Index-only search with weak consistency; never falls back to an exact scan. |
| `WithVectorPrefilter` | Applies the SQL filter before ranking instead of after. |
| `WithVectorDistanceRange` | Restricts a single-query search to `lower <= distance < upper`; either bound may be nil. |

`WithVectorFastSearch(true)` can return no rows when the dataset has no usable
vector index. `CreateVectorIndex` returns a new dataset snapshot containing
the index; use that result or reopen the dataset to search the new index.
`Metric` and `VectorMetric` name the same `index.Metric` type, so index creation
and query options accept the same metric values.

`WithRowIDAllowlist` and `WithRowIDBlocklist` restrict a scan by stable
`_rowid` values, including vector searches. Calling `WithRowIDAllowlist()` with
no IDs selects no rows. The IDs are not positions within a batch or fragment.

## Distributed index segments

Index shards can be built independently and published together.
`TrainIVFModel` and `TrainPQModel` return a `*Model` that carries the native
model provenance, so one trained model can be reused by many builders;
`BuildRQModel` returns the portable IVF-RQ rotation artifact. Pass those models
through `index.VectorOptions` and restrict `index.SegmentScope` to the fragments
of one shard.

`NewVectorSegmentBuilder` and `NewScalarSegmentBuilder` create one uncommitted
segment. A builder is single-use, including when execution fails, owns a dataset
snapshot, and must be closed. `Execute` writes the segment's index files to the
dataset storage and returns transportable metadata describing them; it never
publishes a version.

`ParseIndexSegmentMetadata` inspects or rewrites the coverage of one segment.
`MergeIndexSegments` merges uncommitted segment metadata, and
`MergeIndexMetadata` merges an index's root artifact without publishing a
version; only inverted indexes support that root merge at this native revision.
`CommitIndexSegments` publishes a complete set in one version and returns a new
dataset snapshot, leaving the receiver unchanged. Native validation owns
compatibility, coverage and replacement rules, so an invalid set, a mismatched
keyed column, or mixed index types fail without changing a version.

`SegmentBuilder.SetProgress` and `index.MergeMetadataOptions.Progress` observe
advisory build progress. Observers run on native worker threads, may be called
concurrently, must not block or call back into Lance, and are recovered from
panics. Stage names are diagnostic and may change between native releases.

## Versions, tags and branches

`CurrentVersion` reports the checked-out version with its nanosecond timestamp
and metadata. `Versions` returns the committed history in ascending version
order, and `VersionRefs` returns the lightweight attached-version manifest
reference IDs. Returned values and metadata maps are Go copies and do not need
to be closed.

`CheckoutVersion`, `CheckoutTag`, `CheckoutBranch` and `CheckoutRef` return
independent datasets that remain usable after the receiver is closed. Refs are
Go values built with `VersionRef`, `BranchRef`, `BranchVersionRef` or `TagRef`.
`VersionRef(0)` addresses native version number zero and is not the same as
`WithVersion(0)`, which opens the latest version. `BranchRef("main")` is
normalized by native code.

`Restore` copies an older version into a new commit and returns a dataset at
that commit. It always commits a new version, even when restoring the current
version, and leaves the receiver unchanged.

Tags and branches are shared catalog state rather than dataset-local snapshots.
`Tags`, `Tag`, `CreateTag`, `UpdateTag`, `DeleteTag` and `ReplaceTagMetadata`
manage tags. `CreateTag` rejects an existing name with `ErrCommitConflict`.
Updating a tag preserves its creation time. Branch creation returns a dataset on
the new branch; `Branches` lists explicit catalog entries ordered by name, while
the implicit `main` branch is not listed and has no `Branch` entry. Deleting a
branch fails while a child branch or tag still references it;
`ForceDeleteBranch` additionally tolerates missing branch contents left by an
interrupted creation. `CurrentBranchIdentifier` reports the version-to-manifest
lineage of the receiver's branch.

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
shared sessions, version history and restore, tags, branches, and
cancellation/close interactions. Arrow bridge tests check
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
