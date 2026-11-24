# Go API guide

See the [quickstart](../README.md#quickstart) to install Lance and create a sample dataset.

This guide describes the `main` branch and the public Go APIs it contains.
Each release's notes and [native packages](native-packages.md) record its
exact native chain.

Signatures, option fields, defaults and error contracts are documented on the
symbols themselves, so this guide does not repeat them. The
[package reference](https://pkg.go.dev/github.com/lance-format/lance-go) is the
authoritative source for the version you build against; the subpackages are
[`compaction`](https://pkg.go.dev/github.com/lance-format/lance-go/compaction),
[`file`](https://pkg.go.dev/github.com/lance-format/lance-go/file),
[`index`](https://pkg.go.dev/github.com/lance-format/lance-go/index) and
[`namespace`](https://pkg.go.dev/github.com/lance-format/lance-go/namespace).

## Topics

- Read and inspect: [Read a dataset](#read-a-dataset),
  [Scanner controls](#scanner-controls), [Storage options](#storage-options),
  [Raw `.lance` files](#raw-lance-files), [Blob columns](#blob-columns)
- Write and organize: [Mutations and schema evolution](#mutations-and-schema-evolution),
  [Versions, tags and branches](#versions-tags-and-branches),
  [Native namespaces](#native-namespaces)
- Search: [Scalar indexes](#scalar-indexes), [Vector search](#vector-search),
  [Full-text search](#full-text-search), [Distributed index segments](#distributed-index-segments),
  [Index optimization](#index-optimization)
- Maintain: [Compaction and cleanup](#compaction-and-cleanup)
- Reference: [Options](#options), [Runtime diagnostics](#runtime-diagnostics), [Optional values](#optional-values),
  [Errors](#errors), [Context cancellation](#context-cancellation)

## Options

Every entry point is configured with values produced by `With...`
constructors. Each operation accepts its own interface — `OpenOption`,
`WriteOption`, `CommitOption`, `BranchOption`, `TransactionOption` and
`AddColumnsOption` — and the interfaces have unexported methods, so only values
from this package can be passed.

Options that more than one operation supports use one shared constructor.
`WithSession` returns a `SessionOption` accepted by `Open`, the write entry
points and `CommitTransaction`. `WithStorageOptions` returns a
`StorageOptionsOption` accepted by `Open`, writes, commits and `CreateBranch`.
`WithStorageOptionsAccessor` returns a `StorageOptionsAccessorOption`,
`WithCommitLock` returns a `CommitLockOption`, `WithSkipAutoCleanup` returns a
`SkipAutoCleanupOption`, and `WithTransactionProperties` returns a
`TransactionPropertiesOption`. The prefixed v0.2.0 spellings such as
`WithWriteSession` and `WithCommitStorageOptions` no longer exist.

Option values can be collected in slices and passed programmatically. A
constructor stored as a function value must name the constructor's return type:

```go
var openSession func(*lance.Session) *lance.SessionOption = lance.WithSession
```

Namespace entry points and fragment creation share the option interface of
their operation family but cannot express every value; they validate at call
time and return `ErrInvalidArgument` for options such as
`WithStorageOptionsAccessor` and `WithCommitLock`.

## Read a dataset

On a target with a published selector (`linux/amd64`, `linux/arm64`,
`darwin/arm64`, `windows/amd64`, or `windows/arm64`),
a default build needs no environment
variable: the first call loads the bundled artifact. Targets without a selector
need an explicit `LANCE_LIB_PATH` or a system `liblance_c`. Run your Go program
with `CGO_ENABLED=0`:

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
The caller owns the reader and releases it after the write; the call borrows
and advances it. When starting from one or a few `arrow.RecordBatch` values,
pass them to `array.NewRecordReader`. That reader retains each batch for its own
lifetime, so release the caller's batch references after constructing it and
release the reader after the write. `WriteToNamespace` follows the same reader
ownership contract. `Append` creates an absent dataset; `Overwrite` commits a
replacement version while preserving older versions. `WriteParams` exposes the
options available in the pinned C ABI.

`WriteParams` also exposes native write options for V2 manifest selection,
initial and target bases, per-base storage parameters, external blob handling,
dataset file-writer controls, sessions and write progress. These fields use the
native write-options entry point; `WithWriteParams` copies their values and
the slices and maps they contain. Progress callbacks may run on native worker
threads, and native write errors do not guarantee rollback. Namespace writes
reject these fields because the namespace ABI accepts only the older write
options structure. Fragment-create entry points remain on the older options
structure.

`NewCommitLock` creates an external commit lock from Go `Acquire`, `Release`
and optional `Destroy` callbacks. `WithCommitLock` routes opens, ordinary and
staged writes, and transaction commits through the lock. The callbacks run
synchronously on native worker threads and may be called concurrently.
`Acquire` may return a nil lease for stateless coordination; an error with a
non-nil lease transfers that partial lease to native code for a single
`Destroy` without `Release`. `Release` may be called repeatedly and can
report an error after native code has already published the commit. `Close`
only drops the caller's owner; native datasets retain the lock they need.
Namespace opens, writes, and commits reject `WithCommitLock`, and fragment
creation rejects it as well.

Row mutations and schema evolution return independent dataset snapshots; see
[Mutations and schema evolution](#mutations-and-schema-evolution) for the
snapshot and reader ownership rules. Metadata and config updates use the same
contract, with explicit merge/replace modes; a nil metadata value deletes a
key, while an empty string is stored.

`BatchUDF` supports batch and fragment checkpoints. The mapper borrows a Go-owned
copy of its native input and returns an owned batch reference. Call `Retain`
before keeping the input or returning a reference owned elsewhere.
`InsertBatch` receives the exact schema native attached to the checkpoint,
including opaque native schema metadata. `GetBatch` must return the retained
batch with that original schema; rebuilding it from `OutputSchema` drops the
metadata and native rejects the hit. `GetBatch` also returns an owned
reference; `InsertBatch` lends its batch for the callback, so retaining it is
required to keep it. These operations run synchronously and check context
cancellation before entering native work.

A `Dataset` is a snapshot safe for concurrent reads. Close it when finished;
`Close` waits for in-flight operations. `WithVersion` selects a historical
snapshot, and `WithSession` shares a `Session`'s native caches across datasets.
Close sessions explicitly; datasets already opened from a session remain valid.
`v0.2.0` declares `Session`, `CacheStats` and the storage-options types directly
in the root package, so their members appear in `go doc` and pkg.go.dev. Source
that uses them remains compatible; reflection now reports the root package
identity.

`Dataset.URI()` returns the fully qualified URI native resolved when the
snapshot was produced. For a namespace dataset it is the
resolved `describe_table` location, not the `$`-joined table identifier used in
error diagnostics. Every returned snapshot reports its own native URI; for
example, a created branch includes its branch path. The Go string is captured
once, remains available after `Close`, and `URI` does not acquire the dataset
handle or call native code.

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
storage options, so `WithStorageOptionsAccessor` and `WithWriteBranch` are
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
logical row indices. With no column arguments it returns the full logical
schema; with an explicit empty column list (`fragment.Take(ctx, indices,
[]string{}...)`) it returns one zero-column batch whose row count is the number
of requested indices. That empty-projection shape follows lance-c
v0.2.1-beta.1; earlier native libraries rejected the same call. `Delete` stages a deletion and returns a new scannable
fragment; build a Delete operation from it and commit the transaction to
publish the deletion. Detached fragments cloned from an Append operation expose
metadata and data-file information but reject logical schema and data access.

`Fragment.AddColumnsSQL`, `AddColumnsNulls`, `AddColumnsStream` and
`AddColumnsBatchUDF` stage new columns; `MergeColumns` joins source columns;
`UpdateColumns` stages row updates; `WriteColumns` and
`WriteColumnWithVersion` stage replacement data files; and `DeleteRows` stages
physical deletion offsets. The `AddColumnsSQL` and `AddColumnsStream` methods on
both `Dataset` and `Fragment` accept `WithAddColumnsBatchSize`. Dataset calls
forward the value as the native `uint64` batch size, where zero selects the
Lance default. Fragment calls preserve the native optional `uint32` batch size:
omitting the option forwards `None`, while passing it forwards an explicit
value, including zero, so the two cases stay distinguishable. These methods
clone native fragment metadata and do
not publish a version. Add-columns and merge return detached fragment metadata
plus a complete `NativeSchema` with persistent field IDs. `UpdateColumns`
returns an owned result carrying the detached fragment, modified field IDs and
matched physical offsets. `WriteColumns` returns a `DataReplacementGroup`;
`DeleteRows` returns a scannable fragment unless every physical row was deleted.

Publish staged results explicitly. Use `NewMergeOperation` with the complete
final fragment list and complete schema for add-columns and merge results, and
`NewUpdateOperation` with the updated fragment, modified field IDs and optional
`UpdatedFragmentOffsets` for update results. Matched offsets and
`UpdatedFragmentOffsets.OffsetsAt` results are independent copies in ascending
native bitmap order. Update operations select native RewriteColumns mode
because `UpdateColumns` stages a horizontal column rewrite. Pass `nil` offsets
to represent native None; pass `NewUpdatedFragmentOffsets(nil)` for native
Some(empty).
`NewDataReplacementOperation` publishes replacement files, and
`NewDeleteOperation` publishes partial or full physical deletion. The
corresponding native Update options for removed or new fragments, compacted
SSTables, preservation bitmaps, caller-selected update mode and inserted-row
filters are not exposed as options; staging pins the native mode required by
`UpdateColumns` and produces none of the other fields.

Arrow streams retain one reader reference. Native consumes that transferred
stream exactly once after ownership transfer, while the caller keeps and
releases its original reader; do not inspect or reuse the stream after the
call. A rejected or early-failing call may return before exhausting the stream.
`FragmentFileVersionV1` is forwarded to native, which rejects V1 replacement
writes; use `FragmentFileVersionV2_2` or `FragmentFileVersionV2_3` for
supported files. Closing a staged fragment, schema, group or result does not
publish it, and closing the source dataset or scanner does not invalidate an
already-staged detached result.

`DataFile` parses an existing Lance data file; `Dataset.CreateDataFile` does
not write one. A `DataReplacementGroup` pairs a fragment ID with a parsed
replacement file, and `NewDataReplacementOperation` builds the corresponding
operation. `NewProjectOperation` accepts a `NativeSchema`, which preserves
native field IDs that the lossy Arrow schema view does not. Schemas, data files
and fragments returned by operations are independently owned and remain valid
after their parent object is closed.

## Runtime diagnostics

`RuntimeInfo()` returns what this process has actually observed without
inspecting the environment, extracting a bundle, loading a library, resolving
symbols, or retrying an earlier failure. Before the first native operation,
`Loaded` and `FFIReady` are false. After a successful open, `Library.Source` is
`RuntimeSourceLibPath`, `RuntimeSourceBundle`, or `RuntimeSourceSystem`, and
`Library.Path` is the explicit file, extracted bundle cache path, or bare
platform library name for a system lookup.

`Loaded` only reports that opening the library succeeded. `FFIReady` separately
reports that the mandatory C symbols were bound. `Expected` is the lance-c
release, commit and header this Go module was built against. The SDK does not
bind `lance_c_version()`, which in any case reports only the binding release,
so a loaded `liblance_c` is not verified against that pin. For a bundled load,
`Library.Bundle` carries the artifact's recorded packaging metadata; for
external and system loads, only the source and path are known.

## Mutations and schema evolution

`Delete`, `Update`, `MergeInsert`, `Merge` and `Truncate` apply one mutation to
a clone of the receiver and return a new committed `Dataset`. The receiver is
unchanged: its version, rows and open scanners keep the pre-mutation snapshot.
Close each result when finished, and use `Versions` or a fresh open to inspect
the committed history.

```go
deleted, deletedRows, err := ds.Delete(ctx, "id > 100")
if err != nil {
    return err
}
defer deleted.Close()

updated, updatedRows, err := deleted.Update(ctx, "id = 7",
    lance.ColumnUpdate{Column: "status", Value: "'archived'"},
    lance.ColumnUpdate{Column: "attempts", Value: "attempts + 1"},
)
if err != nil {
    return err
}
defer updated.Close()
```

`Delete` removes rows matching its SQL predicate. `Update` requires at least one
`ColumnUpdate`; each `Value` is a SQL scalar expression evaluated per matched
row, not a Go value or a SQL fragment embedded by the caller. An empty update
predicate updates every row.

`MergeInsert` joins a compatible `array.RecordReader` on one or more key
columns. Its zero `MergeInsertParams` value performs find-or-create: matching
rows are kept and missing source rows are inserted. Explicit modes can update,
delete or fail on matches, and can delete unmatched target rows:

```go
merged, stats, err := updated.MergeInsert(ctx, []string{"id"}, source,
    lance.MergeInsertParams{
        WhenMatched:    lance.MergeWhenMatchedUpdateAll,
        WhenNotMatched: lance.MergeWhenNotMatchedInsertAll,
    })
if err != nil {
    return err
}
defer merged.Close()
fmt.Println(stats.InsertedRows, stats.UpdatedRows, stats.DeletedRows)
```

The source reader is borrowed and may be partially or fully advanced, including
on failure. The caller retains ownership and releases it after `MergeInsert`.
`Merge` similarly left-joins new non-key columns into the dataset, and
`Truncate` returns a new snapshot with no rows. Historical versions remain
readable after any of these operations.

Schema evolution uses the same snapshot contract:

```go
evolved, err := merged.AddColumnsSQL(ctx, []lance.SQLColumn{
    {Name: "double_id", Expression: "id * 2"},
})
if err != nil {
    return err
}
defer evolved.Close()

renamed := "state"
nullable := true
renamedDataset, err := evolved.AlterColumns(ctx,
    lance.ColumnAlteration{
        Path:     "status",
        Rename:   &renamed,
        Nullable: &nullable,
    },
)
if err != nil {
    return err
}
defer renamedDataset.Close()

final, err := renamedDataset.DropColumns(ctx, "scratch")
if err != nil {
    return err
}
defer final.Close()
```

`AddColumnsNulls` adds nullable fields filled with nulls, `AddColumnsStream`
appends columns from an equal-length reader, and `AddColumnsBatchUDF` supports
checkpointed batch mapping. `AlterColumns` changes a name, nullability or type;
`DropColumns` removes columns. All return independent datasets and leave the
receiver's schema and version unchanged. The add-columns methods take
`WithAddColumnsBatchSize`; without it, the backfill uses the Lance default.

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
`ToArrowReader` returns a reader whose final `Release` closes the scanner. The
`file.Reader` record iterators and the dataset blob iterators borrow batches the
same way and release their streams on any exit.
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

### Scan statistics

`WithScanStatistics` receives one `ScanStatistics` snapshot when a scan reaches
EOF, and `Scanner.Statistics` returns the same snapshot after iteration. The
fixed fields summarize I/O and index work. `Metrics` carries additional native
diagnostics; metric names and the set of metrics are not a stable API.

`ScanMetric.Kind` is a `ScanMetricKind` matching the native
`LanceScanMetricKind` value. `ScanMetricKindCount` identifies a count and
`ScanMetricKindTimeNanoseconds` identifies a duration in nanoseconds. Use the
field directly in comparisons and switches:

```go
for _, metric := range stats.Metrics {
    switch metric.Kind {
    case lance.ScanMetricKindCount:
        fmt.Printf("%s: %d\n", metric.Name, metric.Value)
    case lance.ScanMetricKindTimeNanoseconds:
        fmt.Printf("%s: %dns\n", metric.Name, metric.Value)
    default:
        fmt.Printf("%s: %d (%s)\n", metric.Name, metric.Value, metric.Kind)
    }
}
```

`ScanMetricKind.String` returns the native enumerator name, or
`ScanMetricKind(N)` for a value added by a newer native library. Unknown kinds
are preserved in `ScanMetric.Kind` rather than dropped.

v0.3.0 changed `ScanMetric.Kind` from `int32` to `ScanMetricKind`. Code that
stores it in an `int32` variable or passes it to an `int32` parameter needs an
explicit conversion, such as `int32(metric.Kind)` or
`lance.ScanMetricKind(kind)`. Untyped literals such as `Kind: 0` still work.

### Query plan diagnostics

`Scanner.ExplainPlan(ctx, verbose)` returns the native plan for the configured
scan without starting it. `Scanner.AnalyzePlan(ctx)` executes that plan and
returns the native runtime analysis, including elapsed time and operator
metrics. The plan reflects the selected filter, projection, fragment scope and
scalar index, so it is useful for checking pushdown without matching a complete
plan string.

`Scanner.CountRows(ctx)` applies the native `COUNT(*)` aggregate to the
configured scanner plan. `Scanner.AnalyzeCountPlan(ctx)` returns the runtime
analysis of that count plan. Count planning rejects limits, offsets, ordering
and MinHash queries; the scan-statistics callback is not invoked for the count
operation. Use `Dataset.CountRows` or `Dataset.CountRowsFiltered` when only a
dataset-wide count is needed.

```go
plan, err := scan.ExplainPlan(ctx, true)
if err != nil {
    log.Fatal(err)
}
fmt.Println(plan)

count, err := scan.CountRows(ctx)
if err != nil {
    log.Fatal(err)
}
```

These calls do not start or advance `Records` or `ToArrowReader`, and do not
make the scanner single-use. The scanner must still be open, and plan
diagnostics and counts are serialized with iteration rather than being called
concurrently with it.

Scan cancellation parks and wakes the goroutine without holding an OS thread
while waiting. Synchronous native operations cannot be interrupted once started.
In particular, a write that commits after context cancellation returns its
committed dataset, avoiding an error that could encourage duplicate writes.

## Storage options

Static options and a dynamic accessor are mutually exclusive. Passing
`WithStorageOptions` together with `WithStorageOptionsAccessor` returns
`ErrInvalidArgument` regardless of option order on every entry point that
accepts both.

### Static object-store options

A static map is the simplest cloud configuration. Use `WithStorageOptions` for
opens, writes, appends, overwrites and transaction commits. The map is copied before
the native call; the same keys work for dataset operations and, through the
`file` package, for raw files.

```go
storageOptions := map[string]string{
    "aws_access_key_id":     os.Getenv("AWS_ACCESS_KEY_ID"),
    "aws_secret_access_key": os.Getenv("AWS_SECRET_ACCESS_KEY"),
    "aws_session_token":     os.Getenv("AWS_SESSION_TOKEN"),
    "aws_region":            os.Getenv("AWS_REGION"),
}
created, err := lance.Create(ctx, "s3://bucket/prefix/data.lance", reader,
    lance.WithStorageOptions(storageOptions))
if err != nil {
    return err
}
defer created.Close()

ds, err := lance.Open(ctx, "s3://bucket/prefix/data.lance",
    lance.WithStorageOptions(storageOptions))
if err != nil {
    return err
}
defer ds.Close()
```

Canonical keys for the common providers include:

| Provider | URI examples | Common static keys |
| --- | --- | --- |
| S3 and S3-compatible | `s3://bucket/prefix` | `aws_access_key_id`, `aws_secret_access_key`, `aws_session_token`, `aws_region`, `aws_endpoint`, `aws_allow_http` |
| Google Cloud Storage | `gs://bucket/prefix` | `google_service_account`, `google_service_account_key`, `google_application_credentials`, `google_bearer_token`, `google_base_url` |
| Azure Blob or Data Lake | `az://container/prefix`, `abfs://mycontainer@account.dfs.core.windows.net/prefix` | `azure_storage_account_name`, `azure_storage_account_key`, `azure_storage_sas_key`, `azure_storage_token`, `azure_storage_endpoint`, `azure_storage_client_id`, `azure_storage_client_secret`, `azure_storage_tenant_id` |

Aliases such as `region` and `endpoint` are accepted by native code, but the
canonical names above avoid ambiguity when several spellings are present. For
S3-compatible services, set `aws_endpoint` and usually `aws_allow_http=true`;
for Azure, `az://` selects Blob and `abfs://` selects Data Lake.

When no static options are supplied, native uses its normal ambient credential
chain, such as environment variables, profiles or workload identity. Prefer
that path when the runtime already provides credentials, and prefer a dynamic
accessor when credentials expire during a long-lived process. Never log the
storage map or provider results, because they can contain secrets.

### Dynamic providers

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
    lance.WithStorageOptionsAccessor(accessor))
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
`Close() error` stops new local acquisitions, waits for in-flight snapshots and
native calls that already acquired the accessor, and implements `io.Closer`;
it always returns nil. Native datasets, readers,
writers and sessions retain their own native reference, so they remain usable
after the caller closes its accessor and do not block `Close`. A provider has
no Close hook and the SDK never calls a provider `Close` method; the adapter
keeps the Go provider reachable, and may call `FetchStorageOptions`, until
native releases the final owner. That can happen after the caller closes its
accessor or file value. The provider value stays caller-owned and can be
reused by another accessor.

Do not call `Close` on an accessor or its owner from inside its own callback.
Callbacks may be invoked while an operation is using the accessor; external
synchronization must not depend on `Close` returning from that callback.

### Migrating accessor ownership from v0.2.0

`StorageOptionsAccessor.Close` now returns `error`, so the type implements
`io.Closer`. `defer accessor.Close()` still compiles, but a stored method value
changes from `func()` to `func() error`, and a custom interface or wrapper that
declared `Close()` must add the error result. Use `io.Closer` and check the
error where it matters:

```go
// v0.2.0: accessor.Close is a func() method value.
closeFn := accessor.Close

// v0.3.0: the method value is a func() error; adapt it when a func() is required.
closeFn := func() { _ = accessor.Close() }
```

`file.StorageOptions` is now a type alias for `lance.StorageOptionsAccessor`,
so raw file APIs accept a root accessor directly. `NewStorageOptions`,
`NewStorageOptionsWithProvider` and the deprecated
`NewStorageOptionsFromAccessor` all return that same root accessor type.
Passing an accessor directly borrows it for the open call and leaves closing
with its creator; there is no second wrapper. `NewStorageOptionsFromAccessor`
is retained for source compatibility and returns the accessor it receives, so
closing that value still closes the same accessor as closing the root value.

```go
// v0.2.0: the file wrapper owned the accessor after adoption.
storage, err := file.NewStorageOptionsFromAccessor(accessor)
reader, err := file.Open(ctx, uri, &file.ReaderOptions{StorageOptions: storage})
defer storage.Close()

// v0.3.0: pass the accessor and close it once from its creator.
reader, err := file.Open(ctx, uri, &file.ReaderOptions{StorageOptions: accessor})
defer accessor.Close()
```

For raw files, pass either `file.NewStorageOptions` (static) or
`file.NewStorageOptionsWithProvider` to `ReaderOptions`, `WriterOptions` or
`SessionOptions`, or pass a root `StorageOptionsAccessor` directly. A nil
`*file.StorageOptions` means native defaults; `file.NewStorageOptions(nil)` or
an empty map means an explicitly empty option set. Readers, writers and
sessions retain their own native accessor reference, so it is safe to close the
accessor after their open calls return; already-open handles remain usable and
`Close` does not wait for them. Close the accessor once, from its creator. A
`file.Session` rejects per-call Session, StorageOptions and Scheduler
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
manifest. Importing it also initializes the default native bundle on a
published Unix target; `CGO_ENABLED=0` is sufficient. Dataset APIs remain in
the root `lance` package.

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

for batch, err := range r.Records(ctx, file.ReadOptions{}) {
    if err != nil {
        log.Fatal(err)
    }
    _ = batch // borrowed until iteration advances
}
```

`Reader` reads all rows, a row range, or explicit row indices. Each `ReadAll`,
`ReadRange` and `ReadIndices` returns an `array.RecordReader` for Arrow
interop. The matching `Records`, `RecordsRange` and `RecordsIndices` iterators
open the stream when iteration starts and release it when iteration ends,
including an early break and an error, so the caller releases no stream and the
`Reader` stays usable. For an `array.RecordReader` you already hold,
`array.IterFromReader` gives the same loop shape, but the caller keeps its own
reference and must release it. Batches are borrowed until iteration advances;
call `Retain` before keeping one, including the batch current at a break.
`RecordsIndices` reads its index slice each time a traversal opens its stream,
so a change between traversals affects the next result, and changing the slice
during that read races.
`Reader.ReadGlobalBuffer(0)` returns the serialized
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
  `file.NewStorageOptionsWithProvider` to a standalone reader or writer, or
  pass a root `StorageOptionsAccessor` directly. A nil `*file.StorageOptions`
  means native defaults, while `file.NewStorageOptions(nil)` or an empty map
  means an explicitly empty set. Readers, writers and sessions retain the
  native accessor and remain usable after the caller closes it; closing the
  accessor does not wait for them. The provider stays caller-owned and is never
  closed by the SDK. Dynamic provider tests use local fixtures and provider
  counters; real cloud authentication and remote object-store acceptance are
  separate and are not claimed here.
- `PackedBlobWriter.WriteBlobs` borrows a Binary, LargeBinary, or
  FixedSizeBinary Arrow array and appends its payloads in order. Null and empty
  values remain distinct. Sliced arrays and repeated calls are supported; the
  caller retains ownership of the array. `FinishArray` returns the prepared
  blob descriptors. BinaryView is not supported by the Arrow bridge.
- Raw v2.2 Blob files, including the released Lance 14.0.2 fixture, read
  through default bundles from preview.2 onward. Default and explicit
  projection, descriptors, NULL/empty values and multi-column ownership follow
  the native reader contract; this package does not add a second parser or
  rewrite the physical Blob schema.

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

`BlobRecords` and `BlobRangeRecords` iterate the same planned reads: the plan
starts when iteration starts, every exit releases the stream, the dataset stays
usable, and the caller releases no stream. Batches still own their buffers but
stay borrowed until iteration advances; call `Retain` before keeping one. Each
traversal reads its rows or requests and `BlobReadOptions`, including the value
`IOBufferSize` points to, so a change between traversals affects the next
result, and changing them during that read races.

`WithBlobHandling` controls scanner output for blob columns. The default
descriptor mode keeps other binary columns inline; `BlobHandlingAllBinary`
materializes every blob column as binary payloads, while
`BlobHandlingAllDescriptions` returns descriptors for every column carrying
blob metadata. `file.Session.OpenPackedBlobWriter` and
`file.Session.OpenDedicatedBlobWriter` write blob payload files and return
prepared descriptors through `FinishArray`. The bundled native library supports
raw v2.2 Blob reads, including projection and nested descriptors; the native
reader remains authoritative for descriptor and projection semantics.

## Scalar indexes

`CreateScalarIndex` creates a native scalar index and returns a new dataset
snapshot. `indexType` selects the plugin and `params` is forwarded verbatim as
plugin JSON; nil keeps native defaults. Use the returned dataset or reopen the
source to query the new index; the receiver keeps its current version.

RTree (`index.ScalarRTree`, native value 9) indexes a GeoArrow geometry
column; the SDK coverage exercises LineString. Its optional plugin
parameter is `page_size`; native code supplies the default and rejects values
below 2. Filters use native geometry predicates, and native code owns candidate
generation and exact intersection checks.

The Go API and the native library are versioned separately. The bundled native
library is RTree-capable, so the example below runs on the default bundle. Use
an explicit RTree-capable `LANCE_LIB_PATH` library only when testing an older
SDK or library. The README and the [native package
record](native-packages.md) identify the exact native pin for the installed Go
tag.

```go
indexed, err := ds.CreateScalarIndex(ctx, "geometry", index.ScalarRTree,
    json.RawMessage(`{"page_size": 8}`), index.ScalarOptions{},
    lance.WithIndexName("geometry_rtree"))
if err != nil {
    log.Fatal(err)
}
scan, err := indexed.Scan(
    lance.WithColumns("id"),
    lance.WithSQLFilter("ST_Intersects(geometry, ST_GeomFromText('POINT (0 10)'))"),
)
```

RTree uses the generic scalar-index ABI. A native library that predates RTree
still loads, but rejects the type with a native error. Scalar-index symbol
presence is not proof that enum value 9 is supported; the SDK neither emulates
RTree nor substitutes a different index. The README and the [native package
record](native-packages.md) state the exact native requirement for the installed
release.

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

## Full-text search

Full-text search uses an inverted scalar index. Build positions into the index
when you need phrase queries or position-aware prewarming:

```go
indexed, err := ds.CreateScalarIndex(ctx, "body", index.ScalarInverted,
    json.RawMessage(`{"with_position": true}`), index.ScalarOptions{},
    lance.WithIndexName("body_fts"))
if err != nil {
    return err
}
defer indexed.Close()

query, err := lance.NewFTSMatchQuery("lance search", "", lance.FTSMatchOptions{
    Operator: lance.MatchOperatorAnd,
})
if err != nil {
    return err
}
defer query.Close()

scan, err := indexed.Scan(lance.WithColumns("id"), lance.WithFTSQuery(query))
if err != nil {
    return err
}
defer scan.Close()
```

An empty column lets native scan planning resolve an indexed column; pass a
column name to restrict the query to that column. `NewFTSPhraseQuery`,
`NewFTSMultiMatchQuery`, `NewFTSCombinedFieldsQuery`, `NewFTSBooleanQuery` and
`NewFTSBoostQuery` build the other query types. Boolean clauses use
`FTSOccurShould`, `FTSOccurMust` or `FTSOccurMustNot`. A scanner retains its own
reference to the query, so closing the query after attaching it does not affect
the scan.

The FTS query constructors return `*lance.Error` matching `ErrInvalidArgument`
for invalid arguments, including embedded NUL bytes, invalid operators and nil
or closed subqueries. This typed-error normalization is part of the public
error contract.

FTS and vector scans project `_score` or `_distance` automatically, even when a
column projection is explicit. Call `WithoutScoringAutoprojection()` to disable
that behavior; project the score column explicitly if needed.

`PrepareFTSMatchQuery` and `PrepareFTSPhraseQuery` plan a query against the
current process-local dataset snapshot. A prepared query cannot be serialized
or reused with another dataset or version. `FTSCoverageStrict` fails
preparation when any current fragment is outside the FTS index;
`FTSCoverageIndexOnly` allows those fragments but excludes them from scoring
and results. `WithFTSIndexSegments` restricts a prepared query to physical
index segments and requires `WithFTSQueryContext`.

MinHash similarity uses a MinHash LSH scalar index and a limit:

```go
indexed, err := ds.CreateScalarIndex(ctx, "text", index.ScalarMinHashLSH,
    json.RawMessage(`{}`), index.ScalarOptions{},
    lance.WithIndexName("text_minhash"))
if err != nil {
    return err
}
scan, err := indexed.Scan(
    lance.WithMinHashQuery("text", "hello world"),
    lance.WithLimit(10),
)
```

MinHash results expose `_distance`. A MinHash query requires `WithLimit`, and it
cannot be combined with an FTS query or prepared query context. Count planning
also rejects MinHash queries.

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

Models preserve absent, explicit-null and concrete values with
`namespace.Nullable[T]` by value. Native namespace errors retain their separate
error code space, including code zero (`Unsupported`). The standalone `namespace`
package activates the same platform bundle as the root package.

## Optional values

The packages use different optional representations on purpose because they
encode different native contracts. Do not unify them into one type or change a
signature only to make them look alike.

- Root option structs such as `WriteParams` use pointer fields when native must
distinguish an absent value from an explicit zero or false. A nil pointer leaves
the native default in place; a non-nil pointer forwards the value, including
zero. Scalar zero values documented as defaults keep that meaning.
- `index` option structs use bare pointers for the same absent-versus-explicit
  value distinction. Slices follow their documented contract: nil generally
  means unset, while a non-nil empty slice is an explicit empty selection.
- `compaction.Optional[T]` distinguishes three states: `Unset` leaves the
  native default unchanged, `None` sets native `None` explicitly, and `Some`
  sets a value including zero or an empty slice. A nil `*compaction.Options`
  selects the native from-dataset configuration; ordinary pointer fields still
  distinguish only absent from an explicit value.
- `namespace` models use `namespace.Nullable[T]` by value: `namespace.Absent[T]()`
  omits the JSON field, `namespace.Null[T]()` sends an explicit JSON null, and
  `namespace.Some(value)` sends a concrete value, including a zero value or an
  empty slice or map. The zero `Nullable[T]` is the absent state. Decoding a
  model that carries a `Nullable` field replaces the whole destination, so a
  field the document omits is cleared even when it is an ordinary field; models
  without a `Nullable` field keep `encoding/json`'s behavior of leaving omitted
  fields untouched when a destination is reused.
- The `file` package uses a nil `*file.ReaderOptions` or writer/session options
  pointer for native defaults. Pointer fields inside a non-nil options value
  keep the absent-versus-explicit distinction.

## Resource lifetime

Every owned handle follows one `Close` contract. Closing a nil or zero-value
receiver is a no-op; when `Close` returns an error, an absent receiver returns
nil. Repeated and concurrent `Close` calls are safe; each caller returns only
after the single native teardown has completed. Operations that require the
released handle report `ErrClosed`. This covers root handles such as `Dataset`,
`Fragment`, `Transaction`, `SegmentBuilder`, `Scanner` and `Session`, the
snapshots they return, and package `file` handles.

In v0.3.0, `SegmentBuilder.Close` returns nil for a nil or zero-value builder
instead of `ErrInvalidArgument`, matching every other handle. The other
`SegmentBuilder` methods still reject an absent builder.

## Errors

Dataset, scanner, writer, session and transaction operations return
`*lance.Error` for failures reported by native code and for argument
validation. Record readers returned by dataset blob reads and by the `file`
package report iteration failures from `Err()` the same way. A native Arrow
stream failure classifies as `ErrIO` because the C Data Interface carries only
the error text; schema and batch import failures classify as `ErrInternal`.
Native metadata JSON that cannot be decoded reports `CodeInternal`, and its
decode failure remains reachable through `errors.Unwrap`. The value carries:

- `Op`: the operation name, such as `open`, `delete` or `scan`;
- `URI`: the dataset or namespace identifier when one is available;
- `Code`: a `lance.Code` such as `CodeNotFound` or `CodeCommitConflict`;
- `Message`: the native or validation message.

`errors.Is` matches the sentinel for the code, and `errors.As` exposes the
full value:

```go
ds, err := lance.Open(ctx, "missing.lance")
if errors.Is(err, lance.ErrNotFound) {
    // The dataset does not exist.
}
var lanceErr *lance.Error
if errors.As(err, &lanceErr) {
    fmt.Println(lanceErr.Op, lanceErr.URI, lanceErr.Code, lanceErr.Message)
}
```

The sentinels are `ErrClosed`, `ErrInvalidArgument`, `ErrIO`, `ErrNotFound`,
`ErrAlreadyExists`, `ErrIndex`, `ErrInternal`, `ErrNotSupported`,
`ErrCommitConflict` and `ErrPanic`. Compare with `errors.Is` and `errors.As`,
not with `==` or message strings. `ErrCommitConflict` also covers conflicting
tag and branch operations.

The `namespace` package has its own error code space. Namespace operations
return `*namespace.Error` for native namespace failures, `*namespace.NativeError`
for other native failures, `namespace.ErrClosed` for a closed handle, and
`*namespace.ProtocolError` for malformed requests or responses.
`namespace.NamespaceError` and `namespace.NamespaceErrorCode` remain as
deprecated aliases that name the same types, so existing `errors.As` targets
and composite literals keep working. The reflected type name is `Error`
(`reflect.Type.Name()`), `%T` prints `namespace.Error` (`*namespace.Error` for
pointers), and `reflect.Type.PkgPath()` stays
`github.com/lance-format/lance-go/namespace`; the aliases cannot restore the
old reflected spelling.

`namespace.ErrorCode.String()` returns the native Lance Namespace name, and
`namespace.Error` prints the code space, the name and the operation:

```text
namespace describe_table: namespace error TableNotFound: table users not found
```

Codes added by a newer native library keep their numeric value, as in
`ErrorCode(99)`, and stay available through the `Code` field. `NativeError`
prints its code as the `lance-c` enumerator, such as `LANCE_ERR_NOT_FOUND`, so
the two code spaces never look alike. A `NativeError` carries a `lance-c` code,
not a namespace code; match it with `errors.As` and compare `Code`. Use
`errors.As` to recover the operation, the exact code and the native message:

```go
var namespaceErr *namespace.Error
if errors.As(err, &namespaceErr) {
    fmt.Println(namespaceErr.Operation, namespaceErr.Code, namespaceErr.Message)
}
```

`errors.Is` matches a category sentinel: `namespace.ErrNotFound`,
`ErrAlreadyExists`, `ErrInvalidInput`, `ErrPermissionDenied`,
`ErrUnauthenticated`, `ErrConflict`, `ErrUnsupported`, `ErrThrottled`,
`ErrUnavailable` and `ErrInternal`. The categories follow the native code
meaning, and each code keeps its exact value. `namespace.IsNotFound`
is shorthand for `errors.Is(err, namespace.ErrNotFound)` and includes
`namespace.ErrorCodeTableColumnNotFound`, which the pinned native
implementation reports as not found. Code zero is
`namespace.ErrorCodeUnsupported` and is still an error; never treat a zero code
as success.

Root dataset operations that use a namespace, `OpenFromNamespace`,
`WriteToNamespace` and `CommitTransactionToNamespace`, do not preserve the
typed namespace failure: the pinned `lance-c` entry points lose the original
`*namespace.Error`, so namespace sentinels never match a root error and the
namespace code cannot be recovered from the root error.

The root code depends on the native failure route. In the tested case,
`OpenFromNamespace` on a directory namespace with a missing table returns
`*lance.Error` with `CodeInternal` (`ErrInternal`). Other routes report other
codes: creation through a managed manifest can verify a create-table-version
conflict and return `CodeAlreadyExists` (`LANCE_ERR_DATASET_ALREADY_EXISTS`).
Do not infer the namespace code from the root `Code` or by parsing the message
text; call the `namespace` API directly when you need the namespace code and
category. Preserving the typed code on the root C entry points is a `lance-c`
prerequisite.

## Context cancellation

Methods that accept a `context.Context` check it before acquiring a native
handle or starting native work. Iterative APIs such as scanner records and the
reader and blob iterators also check between batches. Cancellation is
cooperative: once a synchronous native
write, index build, compaction, optimization, cleanup or namespace request has
started, it cannot be interrupted.

A write, mutation, compaction or index build that commits after cancellation
returns its committed dataset or result instead of a cancellation error that
could encourage duplicate work. A returned `context.Canceled` or
`context.DeadlineExceeded` therefore means the operation stopped before native
work started or while it was waiting between batches. It does not mean files
were rolled back.

Scanner cancellation parks and wakes the waiting goroutine without holding an
OS thread. Ending iteration, including an early break, closes the scanner.
`Dataset.Close` and namespace `Close` wait for in-flight operations; they do not
cancel them. Index and write progress callbacks are advisory and cannot cancel
native work. Dynamic storage-provider callbacks receive a background-scoped
context that is valid only for that call and cannot cancel native I/O. Use the
request context to decide whether to start work, not as a rollback or
interruption guarantee for work already in progress.
