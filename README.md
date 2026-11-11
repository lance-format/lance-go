# lance-go

Pure-Go bindings for [Lance](https://lancedb.github.io/lance/), built on the
official `lance-c` C ABI without cgo.

## Status

The core API supports dataset creation, append/overwrite, metadata, shared
sessions, projected and filtered scans, row selection, vector and full-text search,
immutable row/schema mutations, version history, tags, branches, staged transactions,
fragment inspection, Blob reads, and native directory/REST namespaces with managed
dataset open, write, and transaction commits. It uses Arrow Go v18 and requires Go 1.23
or newer.

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
Windows DLL belongs to the lance-c release process. Remaining API parity and
platform distribution work is tracked in
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

`WriteParams` also exposes native write options for V2 manifest selection,
initial and target bases, per-base storage parameters, external blob handling,
dataset file-writer controls, sessions and write progress. These fields use the
native write-options entry point; `WithWriteParams` copies their values and
the slices and maps they contain. Progress callbacks may run on native worker
threads, and native write errors do not guarantee rollback. Namespace writes
reject these fields because the namespace ABI accepts only the older write
options structure. A commit-lock handle is not represented in Go yet, and
fragment-create entry points remain on the older options structure.

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

`WriteUncommitted` writes data files without publishing a version and returns
a `Transaction`. The transaction can be serialized with `MarshalBinary` and
restored with `ParseTransaction`, or built directly from an `Operation`.
`CommitTransaction` publishes one transaction and returns the committed
dataset; `CommitTransactionBatch` atomically commits append transactions.
`Dataset.ReadTransaction` and `ReadTransactionByVersion` read transactions
that the manifest references. Committing is explicit: closing a transaction
does not publish it or delete uncommitted data files. A failed staged write
does not publish a version, but may leave uncommitted data files.

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

`CreateFragment` writes one detached fragment with an explicit ID;
`WriteFragments` splits a stream using the file-layout write options. Neither
publishes a version. Combine returned fragments in `NewAppendOperation`, then
use `NewTransaction` and `CommitTransaction` to publish them. Closing metadata
does not delete uncommitted files. `WithFragmentSchema` preserves native field
IDs. Cleanup, stable row ID generation and transaction properties belong to
publication; fragment creation rejects those settings. The fragment ABI accepts only static
storage options, so `WithWriteStorageOptionsAccessor` and `WithWriteBranch` are
also rejected before writing files. Use commit options for
stable row IDs and cleanup policy. A failed write releases its retained reader
reference but may have prefetched input; the caller still releases its reader.
`Dataset.CreateFragmentFromFile` accepts an optional physical-row hint: legacy
files use it, while V2 files use their stored row count.

`Dataset.FragmentIDs` and `Dataset.OpenFragment` expose fragment snapshots.
A bound fragment supports `Scan`, `Take`, `CountRows` and `Delete` without the
source dataset. Fragment scans accept projection, limit, offset, batch sizing,
ordering, row-address, fragment-selection and scanner-tuning options.
`WithColumns` and `WithSQLFilter` require constructor arguments that the native
fragment API does not expose; they return `ErrNotSupported`. For filtering,
`WithAdditionalSQLFilter`, `WithSubstraitFilter` and `WithSubstraitAggregate`
use the ordinary scanner setters after fragment selection and retain native
validation semantics. `WithFragmentIDs` replaces the fragment scan's initial
metadata selection, including its staged deletions. `Take` uses fragment-local
logical row indices. `Delete` stages a deletion and returns a new scannable
fragment; build a Delete operation from it and commit the transaction to
publish the deletion. Detached fragments cloned from an Append operation expose
metadata and data-file information but reject logical schema and data access.

`DataFile` parses an existing Lance data file; `Dataset.CreateDataFile` does
not write one. A `DataReplacementGroup` pairs a fragment ID with a parsed
replacement file, and `NewDataReplacementOperation` builds the corresponding
operation. `NewProjectOperation` accepts a `NativeSchema`, which preserves
native field IDs that the lossy Arrow schema view does not. Schemas, data files
and fragments returned by operations are independently owned and remain valid
after their parent object is closed.

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

## Scanner controls

`WithIOBufferSize`, `WithBatchReadahead`, `WithFragmentReadahead`,
`WithTargetParallelism`, `WithScanInOrder`, `WithUseStats` and
`WithUseScalarIndex` tune native scan planning. Scans are ordered and use scalar
indexes and file statistics by default; fragment readahead only applies when
ordered scans are disabled. `WithStrictBatchSize(true)` requires every batch
except the last to contain exactly the configured `WithBatchSize` rows, and
cannot be combined with `WithBatchSizeBytes`. The batch buffer and readahead
sizes must be positive.

`WithIncludeDeletedRows(true)` includes rows deleted from the scanned snapshot.
It requires `WithRowID(true)`; deleted rows have a null `_rowid`. For filtered
scans, set `WithUseScalarIndex(false)` to prevent index plans that omit
tombstoned rows. Enabling scalar indexes does not itself reject a scan when
no index is selected. Explicit scalar index segments cannot include deleted rows.

`WithFragmentIDs` restricts a scan to the given native fragment IDs. Calling it
without IDs selects no fragments, and IDs absent from the snapshot contribute
no fragments. This is useful for assigning disjoint fragments to independent
scanners. `WithScalarIndexSegment` restricts a filtered scan to one scalar index
segment identified by its `index.UUID`; it requires an explicit, non-empty
fragment selection that defines both the segment read and fallback domains. It
does not support vector, MinHash or FTS queries, or scans of deleted rows.

`WithMaterializationStyle` selects heuristic, all-late, all-early or
all-early-except column loading. For all-early-except, pass Lance schema column
paths whose payloads should load late; an empty exception list loads every
projected column early. `WithBlobHandling` selects blob descriptors (the
default), binary payloads for every blob column, or descriptions for every
column carrying blob metadata.

Scan cancellation parks and wakes the goroutine without holding an OS thread
while waiting. Synchronous native operations cannot be interrupted once started.
In particular, a write that commits after context cancellation returns its
committed dataset, avoiding an error that could encourage duplicate writes.

## Storage options providers

Static options and a dynamic accessor are mutually exclusive. Passing
`WithStorageOptions` together with `WithStorageOptionsAccessor` (or the write
and commit equivalents) returns `ErrInvalidArgument` regardless of option
order. The static path is unchanged.

A provider supplies credentials and endpoint options when native code asks for
them. Native owns caching, expiration, refresh timing and fallback; Go must not
run a refresh loop or add a second credential cache.

```go
type vendingProvider struct{}

func (vendingProvider) StorageOptionsProviderID() string { return "tenant-a/read" }

func (vendingProvider) FetchStorageOptions(ctx context.Context) (map[string]string, error) {
    return fetchCredentials(ctx) // nil means native None
}

// Optional. Without this method native force refresh falls back to Fetch.
func (vendingProvider) ForceFetchStorageOptions(ctx context.Context) (map[string]string, error) {
    return fetchCredentials(ctx)
}

accessor, err := lance.NewStorageOptionsAccessor(nil, vendingProvider{})
if err != nil {
    log.Fatal(err)
}
defer accessor.Close()

ds, err := lance.Write(ctx, "data.lance", reader,
    lance.WithWriteStorageOptionsAccessor(accessor))
```

`StorageOptionsProviderID` is the native identity of the provider
configuration and credential scope. Providers with the same ID share native
equality and object-store registry entries. Use a distinct, stable ID for each
tenant, role or credential scope; never put secrets in the ID.

`FetchStorageOptions` and optional `ForceFetchStorageOptions` may run
concurrently on native worker threads. The callback boundary preserves typed
error codes; an ordinary error maps to an internal callback error and a panic
becomes a callback error. The Rust accessor wraps provider fetch failures as
IO errors, so `Snapshot` returns `*lance.Error` matching `lance.ErrIO`, with the
provider message retained. Do not log returned values or error text that
contains credentials. The callback context is background-scoped and valid only
for that call: it is not a request context and cannot cancel native I/O.

`StorageOptionsAccessor` is reusable across datasets, raw files and file
sessions. It stores optional initial options and a provider. A nil initial map
means native None; a non-nil map, including an empty one, means an explicit
initial set. Valid initial options are cached without fetching; once expired,
a provider result replaces them rather than merging maps. An absent provider
result falls back to the initial options. At the callback boundary a fetch returning nil means None and an
empty map means Some(empty); native then applies its fallback, so a resolved
snapshot with a provider but no initial options is Some(empty). `Snapshot` returns an
independent Go map and preserves whether native resolved Some or None.
`Close` stops new local acquisitions and waits for in-flight snapshots and
native calls that already acquired the accessor. Native datasets, readers,
writers and sessions retain their own native reference, so they remain usable
after the caller closes its accessor and do not block `Close`. A provider has no Close hook; the adapter context is
released when the final native owner is dropped, while the caller still owns
the Go provider value.

Do not call `Close` on an accessor or its owner from inside its own callback.
Callbacks may be invoked while an operation is using the accessor; external
synchronization must not depend on `Close` returning from that callback.

For raw files, pass either `file.NewStorageOptions` (static) or
`file.NewStorageOptionsWithProvider` to `ReaderOptions`, `WriterOptions` or
`SessionOptions`. To reuse a root `StorageOptionsAccessor`, wrap it with
`file.NewStorageOptionsFromAccessor`; ownership of the accessor transfers to
the returned `file.StorageOptions`. Readers, writers and sessions retain the
accessor; close the caller-owned `StorageOptions` after the open calls return.
A `file.Session` rejects per-call Session, StorageOptions and Scheduler
overrides.

A transaction returned by `WriteUncommitted` does not retain the accessor.
Pass the same accessor again to `CommitTransaction` or
`CommitTransactionBatch`; datasets returned by those calls do retain it.

`NewNamespaceStorageOptionsAccessor` builds a table-scoped accessor backed by a
native namespace. It uses the namespace's credential vending and retains the
namespace natively, so the accessor remains usable after the public namespace
is closed. It does not add a Go credential cache or issue duplicate namespace
requests.

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
- Storage options may be static or dynamic. Pass `file.NewStorageOptions` or
  `file.NewStorageOptionsWithProvider` to a standalone reader or writer, and
  close the caller-owned `StorageOptions` after those open calls return.
  Readers, writers and sessions retain the native accessor. Dynamic provider
  tests use local fixtures and provider counters; real cloud authentication
  and remote object-store acceptance are separate and are not claimed here.
- `PackedBlobWriter.WriteBlobs` borrows a Binary, LargeBinary, or
  FixedSizeBinary Arrow array and appends its payloads in order. Null and empty
  values remain distinct. Sliced arrays and repeated calls are supported; the
  caller retains ownership of the array. `FinishArray` returns the prepared
  blob descriptors. BinaryView is not supported by the Arrow bridge.
- Raw v2.2 projection follows native lance-c behavior. The upstream projection
  issue (issue 167) is tracked there; this package does not work around it.

Pass an existing `*lance.Session` through `file.ReaderOptions.Session`,
`file.WriterOptions.Session`, or `file.SessionOptions.Session` to share its
registry and caches with datasets and raw files. `file.CacheSession` selects
that cache for a standalone reader. Opened objects retain the native session;
closing the root session prevents new opens but does not invalidate them.
Methods on a `file.Session` use its existing session and reject per-call
Session, StorageOptions, or Scheduler overrides.

```go
shared, err := lance.NewSession(64<<20, 64<<20)
if err != nil {
    log.Fatal(err)
}
defer shared.Close()
r, err := file.Open(ctx, "data.lance", &file.ReaderOptions{
    Session: shared,
    CacheMode: file.CacheSession,
})
if err != nil {
    log.Fatal(err)
}
defer r.Close()
```

Real cloud authentication and remote object-store acceptance remain open.

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

`WithBlobHandling` controls scanner output for blob columns. The default
descriptor mode keeps other binary columns inline; `BlobHandlingAllBinary`
materializes every blob column as binary payloads, while
`BlobHandlingAllDescriptions` returns descriptors for every column carrying
blob metadata. Raw packed/dedicated file writers are not exposed yet. The
lance-c#167 raw v2.2 projection gap is not worked around here; blob reads use
native descriptors.

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

FTS and vector scans add `_score` or `_distance` automatically, including when
a column projection is explicit. `WithoutScoringAutoprojection()` disables that
behavior for the scanner; project the score column explicitly to keep it.

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

`WithIndexProgress` observes ordinary index creation through any of the public
create methods; nil preserves the previous behavior. Observations are advisory:
native code owns event delivery, stage names, counts and the build outcome, and
the observer cannot cancel the build. The observer runs on native worker threads,
may be called concurrently, must be fast and thread-safe, and must not call back
into Lance, including the dataset being indexed. A panic is recovered. The
observer is retired and in-flight calls are drained before the create method
returns, including when native reports an error.

Create methods return a new dataset snapshot and leave the receiver unchanged.
Native Lance commits the index and then reloads metadata, so an error can occur
after a version was published. Reopen the dataset to determine the committed
state before retrying; the binding does not claim rollback.

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

## Index optimization

`OptimizeIndices` runs native single-process index optimization and returns a
new `Dataset` at the committed version. The receiver keeps its version, rows
and open scanners. A clean dataset is a native no-op and keeps the version
unchanged. A nil `*compaction.OptimizeOptions` selects native defaults; a
non-nil value starts from those defaults and applies only the fields that are
set.

```go
taskRows := uint64(1 << 20)
segmentRows := uint64(1 << 20)
optimized, err := ds.OptimizeIndices(ctx, &compaction.OptimizeOptions{
    IndexNames:        compaction.Some([]string{"vector_idx"}),
    MaxRowsPerTask:    compaction.Some(taskRows),
    MaxRowsPerSegment: compaction.Some(segmentRows),
})
if err != nil {
    return err
}
defer optimized.Close()
```

For distributed work, `PlanOptimizeIndices` captures an immutable plan without
publishing a version. Plan, task, merge and result snapshots expose native JSON
with `JSON`; `OptimizeIndicesPlanFromJSON`, `OptimizeIndexTaskFromJSON`,
`OptimizeIndexMergeFromJSON` and `IndexSegmentResultFromJSON` restore them.
`OptimizeIndexTask.Execute` writes one uncommitted segment, and
`OptimizeIndexMerge.Execute` combines the task results of one merge group.
`CommitOptimizeIndices` publishes the result segments in one version and returns
a new dataset.

When a plan contains merges, commit the merge outputs rather than the raw task
results: task outputs in a split group are inputs to that group's merge and do
not by themselves replace the old segments. Plans expose `TaskCount`/`Task` and
`MergeCount`/`Merge`. Tasks expose their fragment IDs, reference metadata and
old segments; merges expose their old segments and replaced segment UUIDs;
results expose the produced `Segment` metadata.

```go
plan, err := ds.PlanOptimizeIndices(ctx, &compaction.OptimizeOptions{
    IndexNames: compaction.Some([]string{"vector_idx"}),
})
if err != nil {
    return err
}
var taskResults []*lance.IndexSegmentResult
for i := uint64(0); i < plan.TaskCount(); i++ {
    task, err := plan.Task(i)
    if err != nil {
        return err
    }
    result, err := task.Execute(ctx, ds)
    if err != nil {
        return err
    }
    if result != nil {
        taskResults = append(taskResults, result)
    }
}

results := taskResults
if plan.MergeCount() > 0 {
    results = nil
    for i := uint64(0); i < plan.MergeCount(); i++ {
        merge, err := plan.Merge(i)
        if err != nil {
            return err
        }
        var inputs []*lance.IndexSegmentResult
        for _, result := range taskResults {
            if result.Group() == merge.Group() {
                inputs = append(inputs, result)
            }
        }
        result, err := merge.Execute(ctx, ds, inputs)
        if err != nil {
            return err
        }
        if result != nil {
            results = append(results, result)
        }
    }
}

committed, err := ds.CommitOptimizeIndices(ctx, results,
    &compaction.OptimizeOptions{
        TransactionProperties: map[string]string{"job": "reindex-2026-11"},
    })
if err != nil {
    return err
}
defer committed.Close()
```

Native JSON is authoritative across a transport boundary. Task JSON carries
the dataset version and the merge options that execution re-applies;
`IndexNames` and the row bounds only affect planning. `TransactionProperties`
apply only to the commit, so pass them again to `CommitOptimizeIndices` after a
JSON round trip. Native validates plan ID, merge group, read version and
replacement ownership. Planning, execution and merging write index files but
publish nothing; only the commit changes the dataset version.

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

Base paths record alternate storage locations in a dataset's manifest.
`BasePaths` lists the registry keyed by native base ID. `AddBases` commits
bases as a new version and returns an independent dataset; an ID of zero asks
Lance to allocate one, conflicting paths or names fail with
`ErrInvalidArgument`, and the receiver keeps its own registry and version. A
nil `Name` means no name, while a pointer to an empty string records an empty
name. Transaction properties belong to the commit: a nil map passes native
None, and a non-nil map, including an empty one, passes a present map.
Native protobuf persistence normalizes empty property maps to absent on readback.

Registering bases does not route new writes to them. The pinned C API does not
expose write placement, so `AddBases` only records and lists the registry.

## Native namespaces

The `namespace` package exposes typed namespace requests and responses through
native Directory, REST and Connect implementations. A Directory namespace can
manage local tables:

```go
ns, err := namespace.NewDirectory(map[string]string{"root": "./catalog"})
if err != nil {
    return err
}
defer ns.Close()

ds, err := lance.OpenFromNamespace(ctx, ns, []string{"events"},
    lance.WithSession(session))
if err != nil {
    return err
}
defer ds.Close()
```

Use `WriteToNamespace` to create or append Arrow batches, and
`CommitTransactionToNamespace` to publish a staged transaction through the
catalog. Readers remain caller-owned. `WithWriteBranch` applies only to
namespace-managed writes. Opened datasets and REST adapters retain native
ownership and remain usable after their namespace is closed.

`namespace.NewRest` takes native properties such as `uri`.
`namespace.NewCustom` adapts a Go `namespace.CustomClient` for native
`DescribeTable`, `DescribeTableVersion`, `CreateTableVersion`, and
`ListTableVersions` calls. The native operation string selects the client
method, so overlapping request fields cannot misroute a call. The namespace
retains the client until the final native owner is released; it does not close
the client. Callbacks run synchronously on the native caller's goroutine
without context propagation and must be safe for concurrent use. A callback
must not close the namespace that invoked it, because Close waits for in-flight
callbacks. Other namespace operations keep the native `Unsupported` error.

`WithDynamicContextProvider` supplies per-operation request context. The native
contract logs provider errors or panics and continues with empty context; do
not rely on provider failure to deny a request. Context cancellation is checked
before synchronous native calls and does not interrupt an operation in flight.

Models preserve absent, explicit-null and concrete values using pointers to
`namespace.Nullable[T]`. Native namespace errors retain their separate error
code space, including code zero (`Unsupported`). The standalone `namespace`
package activates the same platform bundle as the root package.

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
git -C /path/to/lance-c show c13182cfa72c6a5b78873ea34cac152163fabe11:include/lance/lance.h > lance.h
LANCE_C_HEADER=./lance.h go run ./scripts/check-abi-symbols.go
```

Packaging verifies the header against this pin; a library built from a
different commit is not detectable at runtime. The `artifacts/linux-amd64`
module must be regenerated when this pin changes and carries the previous pin
until that happens.

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

`benchmarks/soak` is an opt-in Linux RSS and lifecycle benchmark for
long-running repeated use of the public API. CI compiles the package and runs
its tests, but does not execute the long benchmark.
See [`benchmarks/soak/README.md`](benchmarks/soak/README.md) for build and run
commands, the measurement contract and the limits of RSS evidence.

On Windows, the same package tests run with `CGO_ENABLED=0`; set
`LANCE_LIB_PATH` to a real `lance_c.dll` to run native loader tests. The
Windows CI job currently proves compilation and the loader/cache/callback
unit tests, not a packaged DLL.

Native tests exercise create/close/reopen, metadata, scans, projection/filter/
ordering, row selection, retained batches, write modes, nullable sliced data,
shared sessions, version history and restore, tags, branches, uncommitted and
detached transactions, fragment scans and deletions, protobuf round trips,
data replacement, project operations, blob fields and descriptors, blob handle
and planned reads, and cancellation/close interactions. Arrow bridge tests check
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
*.go              public Dataset, Session, Write, Scanner, transaction, fragment and option APIs
namespace/        typed native namespace API and managed dataset operations
internal/bundled/ shared zero-config selector registration for root and namespace
artifacts/linux-amd64/       published data-only module and compressed payload
internal/cdata/   Arrow C Data import/export and release callbacks
internal/handle/  Go callback state handles
internal/loader/  discovery, platform loading, cache and process-wide singleton
internal/ffi/     native bindings, resource handles, TLS error extraction
benchmarks/soak/  opt-in Linux RSS and lifecycle benchmark (long runs are opt-in)
scripts/          build-lance-c.sh, check-abi-symbols.go
```

The Arrow bridge is independent of the loader and Lance FFI. Native calls and
thread-local error extraction stay in `internal/ffi`; public resource ownership
and contextual errors stay in the root package.
