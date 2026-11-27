# Changelog

Notable user-facing changes to `lance-go`, the pure-Go Lance SDK that reads,
writes and searches Lance datasets through the `lance-c` C ABI with
`CGO_ENABLED=0`. Each release records the exact native bundle it carries: the
`lance-c` release, its upstream commit, the canonical header hash and the
per-target library checksums. Versions follow Semantic Versioning; within a
minor line, patch releases preserve source compatibility for published Go APIs,
and necessary breaking changes move to the next minor release with migration
notes.

## [v0.3.1-preview.4] - 2026-11-27

This preview bundles `lance-c v0.2.1-beta.7` (tag peeled to
`b7c10e3df23d3596eddb5e7c2b0cffef442c2fce`) on Lance `v16.0.0-beta.3` (tag
peeled to `a93690c0a82ad8ea5e0dd621e3a311a5fce9a487`). The five companions use
`go.lance.org/liblance/<target>@v0.3.1-preview.4` for Linux amd64 and arm64, macOS
arm64, and Windows amd64 and arm64. The SDK still requires Go 1.26.0, and the
shared libraries export the same 890 `lance_*` names in the public C ABI.
[Companion identities](scripts/go-module-origin/v0.3.1-preview.4.identity.json)

### Native behavior

This preview carries forward these fixes and index improvements from
`v0.3.1-preview.3`.

- `Dataset.Restore` returns `ErrNotSupported` when the current or requested
  version has a MemWAL.
- LabelList index builds and updates skip values under null lists. Rebuild
  affected LabelList indexes to remove old postings.
- Deletion masks handle multi-range batches near `u32::MAX` without overflow.
- Segment-scoped ANN scans can skip per-row checks when a partition passes
  the deletion-only mask in full. Segment ownership and user filters still
  apply.
- IVF_PQ index builds use faster sample reads, partition assignment, and
  encoding with the existing index format.

### Migration from v0.3.1-preview.3

Update all five companion requirements to `v0.3.1-preview.4`. No Go API migration
is required. The default bundle loads offline with `CGO_ENABLED=0` after the
Go modules are cached. `LANCE_LIB_PATH` selects a deployed library.

## [v0.3.1-preview.3] - 2026-11-26

`v0.3.1-preview.3` is a preview of the next 0.3.x release. It moves the bundled
native library to `lance-c v0.2.1-beta.6` (annotated tag
`f63dd087a380a78a56b1e4411210924c280e0a9e`, peeled to
`7d9922bdfee9d42e90ec2fdea8d4fba6bb7fe437`) on Lance `15.1.0-beta.2`
(annotated `31d917614e1456f2dba3652bb8bd21fa7ed227b6`, peeled to
`fc8f59e53233a46f0d73604d66254142802389c9`). The five companions are
`go.lance.org/liblance/<target>@v0.3.1-preview.3`; the Go 1.26.0 module minimum
is unchanged. The public C ABI is unchanged: the shared libraries export the
same 890 `lance_*` names as the v0.2.0 baseline, and the installed header set
and archive layout are unchanged from `v0.3.1-preview.2`.

### Fixed

- `Dataset.Restore` returns `ErrNotSupported` when either the current version
  or the requested version has a MemWAL. Restoring the table manifest cannot
  roll back writes or garbage collection in the shards. Restore between
  versions without a MemWAL is unchanged.
- LabelList index builds and updates skip values stored under a null list
  parent, so those child values no longer produce postings. An existing
  LabelList index built over this shape must be rebuilt to remove old postings;
  the dataset itself needs no rewrite.
- Constructing deletion masks for multi-range batches near `u32::MAX` no
  longer overflows, so reads with deletions at high offsets remain correct.

### Changed

- Segment-scoped ANN scans can skip per-row filtering when every row in a
  partition passes a deletion-only mask; segment ownership and user filters
  still apply.
- IVF_PQ index builds use faster sample reads, partition assignment and
  encoding. The index format is unchanged.
- Building `lance-c` from source now requires Rust 1.98.0 or newer. Prebuilt
  users need no Rust; all five companion binaries are updated, and the Go
  1.26.0 module minimum remains unchanged.

### Migration from v0.3.1-preview.2

Update all five companion requirements to `v0.3.1-preview.3`. There is no Go API
migration; the Go 1.26.0 module minimum, import paths, `CGO_ENABLED=0` default
bundle and `LANCE_LIB_PATH` external-library behavior are unchanged.

## [v0.3.1-preview.2] - 2026-11-26

`v0.3.1-preview.2` is a preview of the next 0.3.x release. It moves the bundled
native library to `lance-c v0.2.1-beta.5` (annotated tag
`28e096139bbffb719ef260e9e4ab1d768a438543`, peeled to
`7482a15c57da79bc138e221a1c122701f7304205`) on Lance `15.0.0-rc.1`
(annotated `d45f68de294c80c0f5dd825c9de2ec3f92d2ad11`, peeled to
`2e7eb3156b2fd962352a41d62e24a80369f0724a`). The five companions are
`go.lance.org/liblance/<target>@v0.3.1-preview.2`; the Go 1.26.0 module minimum
is unchanged. The public C ABI is unchanged: the shared libraries export the
same 890 `lance_*` names as the v0.2.0 baseline, and the installed header set
and archive layout are unchanged from `v0.3.1-preview.1`.

### Fixed

- Namespace writes and namespace URI transaction commits run their publication
  futures on a scoped 8 MiB thread, so `WriteToNamespace` and
  `CommitTransactionToNamespace` no longer depend on the caller's stack size.
  This fixes debug namespace appends and URI commits from Windows programs whose
  main thread has the default 1 MiB stack.
- `MergeInsert` rejects a source that supplies a partial `list<struct>` item
  schema with `ErrInvalidArgument` instead of accepting the incomplete value.
- Searching an `IVF_HNSW_PQ` index reads PQ metadata written by Lance 0.17–0.24,
  so 0.20–0.24 datasets no longer silently skip the index. Indices written by
  0.10–0.16 (the v0,2 format) remain unsupported and report that a rebuild is
  required.
- A commit that has started is no longer cancelled by the retry budget. It runs
  to completion and returns its real result, so a landed non-idempotent update
  is not retried and applied twice after spurious write contention.
- Remote streaming reads share the configured download retry budget across
  request and body failures and resume at the first undelivered byte. They use
  an identity condition where the store supports one (ETag preferred, GCS
  `if_match`) and validate identity, size and range before yielding more data;
  providers without a safe condition still return the original error.
- `Dataset.Cleanup` measures the seven-day unverified-file protection from the
  start of manifest discovery, so a file cannot age into eligibility while the
  run inspects manifests.
- `OptimizeIndices` reports IVF_RQ merge failures with the differing field
  (`fast_rotation_signs` or `query_estimator`) instead of printing identical
  structures.

### Changed

- Vector search over datasets with deletions filters deleted candidates before
  the top-k heap update. IVF and flat results are unchanged; HNSW remains
  approximate and can return different neighbours.
- v1 ZoneMap write seeds require explicit opt-in, and writes default to v0
  seeds so Lance versions before 15 keep reading and writing the dataset.
- Namespace `ListTableVersions` requests with ascending order now paginate and
  return `PageToken` when more versions remain.

### Migration from v0.3.1-preview.1

Update all five companion requirements to `v0.3.1-preview.2`. There is no Go API
migration; the Go 1.26.0 module minimum, import paths, `CGO_ENABLED=0` default
bundle and `LANCE_LIB_PATH` external-library behavior are unchanged.

## [v0.3.1-preview.1] - 2026-11-25

`v0.3.1-preview.1` is a preview of the next 0.3.x release. It moves the bundled
native library to `lance-c v0.2.1-beta.4` (annotated tag
`557d3a864a91bd1930c0b39c093c2ca6a85f2c15`, peeled to
`2f80f2aa0c7fdb955e8cdc799c827c011617e05b`) on Lance `15.0.0-beta.29`
(`ded121cd9a99dc83040cf41049dc2bd3963241b9`). The five companions are
`go.lance.org/liblance/<target>@v0.3.1-preview.1`; the Go 1.26.0 module minimum
is unchanged.

### Fixed

- `MergeInsert` no longer fails when the source supplies a nested subset and the
  table also has fragments the index does not cover.
- IVF_HNSW partitions load through the fragment-reuse index, so deferred index
  remap works after compaction with deletions. Cleanup releases settled
  fragment-reuse transitions.
- `CreateVectorIndex` and its progress variant reject an out-of-range native
  index type or metric with `ErrInvalidArgument` instead of constructing an
  invalid native enum.
- Native S3 reads use per-operation AIMD settings with a higher default read
  ceiling and retry bodyless 503 responses.
- Scalar index results on stable-row-ID datasets are normalized to the
  row-address domain before the scanner prefilter; stable row IDs, allow/block
  and NULL semantics and exact results are unchanged.

### Changed

- Tracked-files progress reports an optional total that may be absent (zero) for
  some or all updates, including the last one. `index.Progress.Total` remains
  zero when the native producer omits it.
- The public C ABI is unchanged: the shared libraries export the same 890
  `lance_*` names as the v0.2.0 baseline. The installed C headers are split into
  domain headers; this is a packaging change for C/C++ consumers, and the Go
  bindings and ABI checks were updated without a Go API change.

## [v0.3.0] - 2026-11-24

`v0.3.0` is the stable minor release. It raises the module minimum to Go 1.26
and collects the minor-release API changes.

### Added

- `file.Reader.Records`, `file.Reader.RecordsRange` and
  `file.Reader.RecordsIndices` return `iter.Seq2[arrow.RecordBatch, error]`
  over the same rows as `ReadAll`, `ReadRange` and `ReadIndices`. The stream
  opens when iteration starts and is released when iteration ends, including
  an early break, an error and a cancelled context, so callers no longer write
  the `Release`/`Next`/`Err` loop. The parent `Reader` stays usable and each
  yielded batch stays borrowed until iteration advances.
- `Dataset.BlobRecords` and `Dataset.BlobRangeRecords` iterate the reads of
  `ReadBlobs` and `ReadBlobRanges` with the stream lifecycle and borrowing
  rules of the file iterators. Creating the sequence plans nothing; the parent
  `Dataset` stays usable and the caller releases no stream.
- `namespace.ErrorCode.String()` returns the native Lance Namespace name for
  every pinned code, and `namespace.Error` prints that name with the operation
  and code space, for example
  `namespace describe_table: namespace error TableNotFound: table users not found`.
  `namespace.NativeError` prints its `lance-c` enumerator, for example
  `namespace open: native error LANCE_ERR_NOT_FOUND: dataset not found`, so the
  two code spaces stay distinguishable. A code added by a newer native library
  keeps its numeric value as `ErrorCode(N)` and stays available through `Code`.
- `namespace.Error.Unwrap()` exposes category sentinels, so `errors.Is` works
  without unpacking the value: `ErrNotFound`, `ErrAlreadyExists`,
  `ErrInvalidInput`, `ErrPermissionDenied`, `ErrUnauthenticated`, `ErrConflict`,
  `ErrUnsupported`, `ErrThrottled`, `ErrUnavailable` and `ErrInternal`. The
  categories follow the native namespace code meaning, and every code keeps its
  exact value for `errors.As`.
- `namespace.Some`, `namespace.Null` and `namespace.Absent` construct the three
  `Nullable[T]` states.
- `ScanMetricKind` identifies native scan metric kinds. `ScanMetricKindCount`
  and `ScanMetricKindTimeNanoseconds` match the pinned lance-c ABI, and
  `String()` returns the native enumerator name or `ScanMetricKind(N)` for a
  kind added by a newer native library. Unknown numeric kinds are preserved.

### Changed

- `StorageOptionsAccessor.Close()` now returns an `error` and implements
  `io.Closer`. `defer accessor.Close()` keeps working; code that used the
  method as a `func()` value or interface needs `func() error`. Close is
  idempotent, waits for in-flight local snapshot calls, and does not wait for
  native datasets, readers, writers, sessions or stores that retained their own
  reference; those remain usable afterwards.
- `file.StorageOptions` is now a type alias for
  `lance.StorageOptionsAccessor`, so raw-file APIs share the root accessor and
  its single native lifecycle. Pass the root accessor directly to
  `ReaderOptions`, `WriterOptions` or `SessionOptions` and close it once.
  `file.NewStorageOptions` still treats a nil or empty map as an explicit empty
  option set, while a nil `*StorageOptions` selects native defaults.
- `file.NewStorageOptionsWithProvider` treats a nil initial map as native None
  until the provider resolves, and a non-nil map, including an empty one, as an
  explicit initial set. The provider remains caller-owned; the SDK never calls
  its Close method and keeps it reachable until native releases the final owner.
- Invalid static options, such as an empty option key, keep their v0.2.0
  `*lance.Error` identity (Op `"storage options"`). An invalid initial map
  passed to `file.NewStorageOptionsWithProvider` now returns a direct
  `*lance.Error` with Op `"storage options"` instead of the previous wrapped
  error with Op `"create storage options accessor"` and nested prefixes.
  `errors.Is(err, lance.ErrInvalidArgument)` still holds.
- The root module no longer requires `github.com/ulikunitz/xz`; maintainer
  tools now live in the nested `internal/tools` module. Code that imported
  `github.com/ulikunitz/xz` only through the root module's graph must require it
  directly.
- `namespace.Error` and `namespace.ErrorCode` are the canonical type names.
  `namespace.NamespaceError` and `namespace.NamespaceErrorCode` remain as
  deprecated aliases naming the same types, so existing `errors.As` targets,
  composite literals and struct fields keep compiling. The reflected type name
  is `Error`: `reflect.Type.Name()` returns `Error`, `%T` prints
  `namespace.Error` (`*namespace.Error` for pointers), and
  `reflect.Type.PkgPath()` stays `github.com/lance-format/lance-go/namespace`.
  The aliases cannot restore the old reflected name.
- Direct namespace calls name the code space and the code instead of printing
  two different integers with the same `native error %d` format:
  `namespace describe_table: namespace error TableNotFound: ...` for
  `*namespace.Error`, and the `lance-c` enumerator for `*namespace.NativeError`.
  Root namespace operations keep the native route's error; see below.
- `OpenOption`, `WriteOption`, `CommitOption`, `BranchOption` and
  `TransactionOption` are interfaces with unexported methods instead of
  concrete function types. Per-operation constructors keep their return types;
  `WithSession`, `WithStorageOptions`, `WithStorageOptionsAccessor`,
  `WithCommitLock`, `WithSkipAutoCleanup` and `WithTransactionProperties`
  return concrete option values accepted by every operation that supports them.
- Shared option constructors replace the per-operation spellings:
  `WithSession` replaces `WithWriteSession` and `WithCommitSession`;
  `WithStorageOptions` replaces `WithWriteStorageOptions`,
  `WithCommitStorageOptions` and `WithBranchStorageOptions`;
  `WithStorageOptionsAccessor` replaces `WithWriteStorageOptionsAccessor` and
  `WithCommitStorageOptionsAccessor`; `WithCommitLock` replaces
  `WithOpenCommitLock` and `WithWriteCommitLock`; `WithSkipAutoCleanup`
  replaces `WithCommitSkipAutoCleanup`; and `WithTransactionProperties`
  replaces `WithWriteTransactionProperties`.
- `Dataset.AddColumnsSQL`, `Dataset.AddColumnsStream`,
  `Fragment.AddColumnsSQL` and `Fragment.AddColumnsStream` take
  `AddColumnsOption` instead of `batchSize ...uint64`; use
  `WithAddColumnsBatchSize(uint64)`. Dataset calls keep the native `uint64`
  batch size, where zero selects the default. Fragment calls keep the native
  optional `uint32`: omitting the option means `None`, while passing it,
  including zero, means `Some(size)`. Repeating the option keeps the last
  value.
- `CreateBranch` rejects an explicit nil `BranchOption` with
  `ErrInvalidArgument` instead of ignoring it.
- `namespace.Nullable[T]` is a value type that models store directly. The three
  wire states are unchanged: absent omits the field, explicit null encodes as
  `null`, and a concrete value encodes as itself, including zero values and
  empty slices and maps. Construct a field with `namespace.Some(value)`,
  `namespace.Null[T]()` or `namespace.Absent[T]()`, or leave the zero
  `namespace.Nullable[T]`.
- `namespace/models_nullable.go` no longer lists every optional field. Each of
  the ten models decodes into a fresh destination in one pass, so a field absent
  from the document still resets when a response is decoded into a reused value.
  Those models replace the whole destination, so an omitted ordinary field
  resets too; models without a `Nullable` field keep `encoding/json`'s behavior
  of leaving omitted fields untouched.

### Fixed

- `Err()` on record readers from `Dataset.ReadBlobs`/`ReadBlobRanges` and
  `file.Reader.ReadAll`/`ReadRange`/`ReadIndices` returns `*lance.Error`
  instead of raw native text. Native Arrow stream failures match `ErrIO`; Arrow
  schema and batch import failures match `ErrInternal`; the internal `ffi:`
  prefix no longer leaks. `Fragment.Take` and `Dataset.TakeRows` receive the
  same typed import failures.
- `Dataset.ListIndices`, `DescribeIndices` and `IndexSegments` return
  `*lance.Error` matching `ErrInternal` when native metadata JSON cannot be
  decoded; the decode failure is reachable through `errors.Unwrap`.
- `file.NewStorageOptionsWithProvider` returns `*lance.Error` matching
  `ErrInvalidArgument` for a nil provider, and
  `file.NewStorageOptionsFromAccessor` does the same for a nil accessor. A
  non-nil zero accessor is returned unchanged and behaves as already closed.
- The reader returned by `Scanner.ToArrowReader` releases safely while a
  `Next` call is blocked: the final `Release` wakes that call and releases a
  batch that arrives after release. At end of stream, a `WithScanStatistics`
  callback may call `Next` on the same reader and receives `false`.
- Direct namespace calls classify column-not-found as not found. The pinned
  native implementation reports `TableColumnNotFound` (12) in the not-found
  group, and `lance-c` maps it to `LANCE_ERR_NOT_FOUND`. `namespace.IsNotFound`
  now returns true for code 12 and the error matches `namespace.ErrNotFound`;
  in `v0.2.0` `IsNotFound` returned false and `namespace.ErrNotFound` did not
  exist yet.
- Root namespace operations are unchanged. `OpenFromNamespace` on a missing
  table returns `*lance.Error` with `CodeInternal` (`ErrInternal`) in the
  tested case; the root code depends on the native failure route, and creation
  through a managed manifest can report `CodeAlreadyExists`
  (`DatasetAlreadyExists`) after native conflict verification. In every route
  the original `*namespace.Error` is unavailable, so namespace sentinels do not
  match. This change does not infer a category from native message text.
- Every exported root and `file` handle follows one `Close` contract: a nil or
  zero-value receiver is a no-op, and an absent receiver returns nil where
  `Close` returns an error. `Dataset.Close` and `Scanner.Close` no longer panic
  on a nil receiver, other zero-value root handles no longer panic, and
  `SegmentBuilder.Close` returns nil instead of `ErrInvalidArgument`. Repeated
  and concurrent calls wait for the single native teardown, and operations that
  require the released handle report `ErrClosed`.
- Empty native metadata arrays no longer produce invalid Go pointers.
  `FileMetadata.Columns` and the related borrowed metadata getters check the
  count before exposing an array address, so zero-column files and empty
  column or statistics collections can be read safely under Go 1.26 stack
  scanning. The exported types and copies are unchanged.

### Migration from v0.2.0

- The module and import paths stay `github.com/lance-format/lance-go`; existing
  imports keep resolving.
- **Go 1.26 or newer is required.** The root module declares `go 1.26.0`, and
  artifact modules published from this commit onward declare the same directive;
  the immutable `v0.2.0-preview.1` companions keep `go 1.23.0` and remain
  buildable. CI tests Go 1.26 and Go 1.27. With `GOTOOLCHAIN=local`, an older
  toolchain stops with `go.mod requires go >= 1.26.0`; the default
  `GOTOOLCHAIN=auto` downloads a toolchain that satisfies the directive. `v0.2.0`
  keeps its published `go 1.23.0` directive and still builds with Go 1.23. See
  [Go version support](README.md#go-version-support).
- `lance.Error` gains a private cause field so a classified failure can keep
  its underlying Go error reachable through `errors.Unwrap`. `errors.Is`,
  `errors.As` and keyed literals are unchanged; positional literals no longer
  compile. This compiles:
  `lance.Error{Op: "open", URI: uri, Code: lance.CodeNotFound, Message: "..."}`.
  This does not: `lance.Error{"open", uri, lance.CodeNotFound, "..."}`.
- Reader iteration failures that previously arrived as unstructured text now
  classify as `ErrIO`; code that matched on `"ffi:"` should switch to
  `errors.Is(err, lance.ErrIO)` or `errors.As(err, &lanceErr)`.
- **Storage options:** `StorageOptionsAccessor.Close()` returns `error` to
  satisfy `io.Closer`; deferred calls are unchanged, but method values and
  interfaces typed as `func()` need `func() error`. `file.StorageOptions` is a
  type alias for `lance.StorageOptionsAccessor`, so reflection and `%T` now
  report the root type, and callers should pass the root accessor directly.
  `NewStorageOptionsFromAccessor` is deprecated and returns the same accessor.
  A nil initial map still means native None, while a non-nil empty map is
  explicit; keep that distinction. Closing the accessor does not invalidate
  native readers, writers or sessions that already opened, and the provider
  stays caller-owned.
- **Close contract:** closing a nil or zero-value handle is now a no-op, and an
  absent receiver returns nil where `Close` returns an error. `Dataset.Close`
  and `Scanner.Close` no longer panic on nil, other zero-value root handles no
  longer panic, and `SegmentBuilder.Close` returns nil instead of
  `ErrInvalidArgument` (the other `SegmentBuilder` methods still reject an
  absent builder). Repeated and concurrent calls wait for the single native
  teardown; operations that need the released handle report `ErrClosed`. See
  [Resource lifetime](docs/api-guide.md#resource-lifetime).
- `namespace.ErrorCodeTableColumnNotFound` (12) now classifies as not found in
  the `namespace` package: `namespace.IsNotFound` returns true and the error
  matches `namespace.ErrNotFound`. In `v0.2.0` `IsNotFound` returned false and
  `namespace.ErrNotFound` did not exist yet. Root namespace operations do not
  observe this code at the pinned native library.
- `namespace.Error` and `namespace.ErrorCode` are the canonical type names.
  `namespace.NamespaceError` and `namespace.NamespaceErrorCode` are deprecated
  aliases of the same types, so `errors.As` targets, composite literals and
  struct fields keep compiling. The reflected name changes even when the old
  spelling is used: `reflect.Type.Name()` returns `Error`, `%T` prints
  `namespace.Error` (or `*namespace.Error`), and `reflect.Type.PkgPath()`
  stays `github.com/lance-format/lance-go/namespace`. `go doc` indexes the
  canonical names.
- Namespace diagnostics for direct `namespace` calls name the code space and
  the code. Compare with `errors.Is`/`errors.As`; the text is informational and
  the root namespace path does not include a structured code name.
- Namespace classification differs by layer. Direct `namespace` calls return
  `*namespace.Error`, so `errors.As` into that type and `errors.Is` against the
  namespace sentinels work there. Root namespace operations return
  `*lance.Error` whose root code depends on the native failure route (the
  tested missing-table `OpenFromNamespace` reports `CodeInternal`, while a
  verified managed-manifest creation conflict can report `CodeAlreadyExists`)
  and never expose the namespace value or code. Call the `namespace` API
  directly when you need namespace-specific classification. Preserving the
  typed code on the root C entry points is a `lance-c` prerequisite; do not
  infer a category by parsing the native message text.
- Code that already used `errors.As` to read `Operation`, `Code` and `Message`
  from a direct namespace call needs no source change and can match categories
  instead of switching over codes:

```go
var namespaceErr *namespace.Error
if errors.As(err, &namespaceErr) {
    fmt.Println(namespaceErr.Operation, namespaceErr.Code, namespaceErr.Message)
}
if errors.Is(err, namespace.ErrNotFound) {
    // Covers the not-found codes, including TableColumnNotFound (12).
}
```
- **Option constructors:** the option types are interfaces, and the shared
  constructors return `*SessionOption`, `*StorageOptionsOption`,
  `*StorageOptionsAccessorOption`, `*CommitLockOption`,
  `*SkipAutoCleanupOption` or `*TransactionPropertiesOption`. A shared
  constructor stored as a function value must name the concrete return type:
  `var f func(*lance.Session) *lance.SessionOption = lance.WithSession`.
  Per-operation constructors keep their interface return types, and a nil
  shared option value returns `ErrInvalidArgument`.
- **Add-columns batch size:** replace `sizes ...uint64` forwards with
  `[]lance.AddColumnsOption` and `WithAddColumnsBatchSize`. Dataset calls keep
  the native `uint64` batch size and zero-as-default; Fragment calls keep the
  native optional `uint32`, so omitting the option means `None` and passing it,
  including zero, means `Some`. A Fragment value above `math.MaxUint32` is
  rejected before the native call. `CreateBranch` now rejects an explicit nil
  option.
- `namespace` model fields for native `Option<Option<T>>` values no longer use
  pointers. Replace `SrcVersion: nil` with `namespace.Absent[int32]()`,
  `SrcVersion: &namespace.Nullable[int32]{}` with `namespace.Null[int32]()`, and
  `SrcVersion: &namespace.Nullable[int32]{Value: &version}` with
  `namespace.Some(version)`. Read presence through `field.Present` and
  `field.Value`, or `field.IsZero()` for absence. The JSON encoding is
  unchanged.
- `ScanMetric.Kind` is now `ScanMetricKind` instead of `int32` (#108). Code that
  assigns the field to `int32`, passes it to an `int32` parameter, or stores an
  `int32` in the field needs an explicit conversion:
  - `var kind int32 = metric.Kind` becomes `var kind int32 = int32(metric.Kind)`.
  - `ScanMetric{Kind: kind}` with an `int32` `kind` becomes
    `ScanMetric{Kind: ScanMetricKind(kind)}`.
  - `f(metric.Kind)` with an `int32` parameter becomes `f(int32(metric.Kind))`.

  Untyped numeric literals still work, including `ScanMetric{Kind: 0}` and
  `metric.Kind == 1`; a typed literal such as `int32(1)` needs
  `ScanMetricKind(1)`. Typed constants can now be used directly in switches.
  Because `ScanMetricKind` implements `String`, `%v` and `%s` print the
  enumerator name; use `%d` or `int32(metric.Kind)` for the numeric value.

## [v0.2.0] - 2026-11-23

`v0.2.0` is the current stable minor release. It bundles `lance-c v0.2.1-beta.3`
through the static `go.lance.org/liblance/<target>@v0.2.0-preview.1` modules and
keeps the published 0.1 Go APIs source compatible, except for the reflection
identity change described below.

### Added

- `Dataset.URI()` returns the fully qualified URI native resolved for that
  snapshot, including a branch or namespace-resolved location. The string is
  captured once and stays available after `Close`.

### Changed

- `Session`, `CacheStats`, `StorageOptionsProvider`,
  `ForceStorageOptionsProvider` and `StorageOptionsAccessor` are declared
  directly in the root `lance` package, so `go doc` and pkg.go.dev index their
  methods, fields and interface signatures. Source that names these types keeps
  compiling, but `reflect.Type.PkgPath` and named type identity now report the
  root package instead of `internal/session` or `internal/accessor`.

## [v0.1.1-preview.1] - 2026-11-23

Preview of the next 0.1 patch. It preserves the published `v0.1.0` Go API and
moves the bundled native library to lance-c `v0.2.1-beta.1` on Lance
`v15.0.0-beta.26`. `v0.1.0` remains the recommended stable release.

### Added

- `Dataset.URI()` returns the fully qualified URI native resolved for that
  snapshot, including a branch or namespace-resolved location. The string is
  captured once and stays available after `Close`.

### Changed

- Default builds now bundle the official prebuilt `lance-c v0.2.1-beta.1`
  shared libraries, which are built against Lance `v15.0.0-beta.26`. The five
  supported targets are unchanged, and each companion module is published at
  `v0.1.1-preview.1`. The native commit, canonical header hash, payload sizes
  and checksums are new; see [artifacts/README.md](https://github.com/lance-format/lance-go/blob/v0.1.1-preview.1/artifacts/README.md).
- `Fragment.Take` with an explicit empty column list now returns one
  zero-column batch with the requested row count. The previous native library
  rejected the same call with `LANCE_ERR_NOT_FOUND`. Passing no columns still
  selects the full logical schema, and `Dataset.Take` is unchanged.
- Scalar-index `params` JSON keeps its Go contract. `CreateScalarIndex` treats
  `nil` as native plugin defaults and rejects a non-nil empty or otherwise
  invalid `json.RawMessage` with `ErrInvalidArgument` before any native call.
  `NewScalarSegmentBuilder` maps both nil and empty params to `NULL`, so they
  still select native defaults. The bundled native library now also rejects an
  empty JSON string with `ErrInvalidArgument` instead of treating it as
  defaults.
- Index segment metadata carrying local fragment aliases is rejected instead
  of being interpreted incompletely: `ParseIndexSegmentMetadata` returns
  `ErrNotSupported`, while `CommitIndexSegments` and `MergeIndexSegments`
  return `ErrInvalidArgument` without changing the dataset version.

```sh
go get github.com/lance-format/lance-go@v0.1.1-preview.1
```

## [v0.1.0] - 2026-11-20

### Added

- `lance.RuntimeInfo()` reports what the process has actually observed: whether
  a library is loaded, which source selected it (bundled artifact, explicit
  `LANCE_LIB_PATH`, or the system loader), the bundle identity, and whether
  native calls are ready. It does not extract a bundle, load a library, resolve
  symbols or retry an earlier failure.
- `Scanner.ExplainPlan` returns the native plan for a configured scan without
  starting it. `Scanner.AnalyzePlan` and `Scanner.AnalyzeCountPlan` return the
  native runtime analysis of that plan. `Scanner.CountRows` reports the row
  count of the same configured scan without materializing records.
- `LANCE_CACHE_DIR` relocates the extracted-library cache. It changes where the
  cache lives, not the size and SHA-256 verification the loader performs on
  every process start.

### Changed

- This is the first stable release. `v0.1.0` replaces `v0.1.0-preview.2` as the
  recommended version, and published Go APIs remain source compatible with the
  preview.2 module.
- Default builds now bundle the official prebuilt `lance-c v0.2.0` shared
  libraries, which are built against Lance `v15.0.0-beta.25`. The five
  supported targets are unchanged; the bundled native bytes, payload sizes and
  checksums are new, and each companion module is published at its own
  `v0.1.0` tag.
- Invalid arguments to the full-text query constructors now return
  `*lance.Error` matching `ErrInvalidArgument` instead of plain errors. The
  index-segment optimize path reports nil segment metadata the same way and no
  longer exposes an internal `ffi:` prefix. Match errors with
  `errors.Is`/`errors.As`; message text is informational.

### Documentation

- The README is now a consumer quickstart: install, a first dataset, stability
  and compatibility scope, supported targets, loader behavior, and a read-only
  or no-cache deployment recipe using `LANCE_LIB_PATH`.
- The API guide gained runnable examples for vector search, full-text search,
  mutations and versions, cloud storage and namespaces, and documents the
  runtime, scanner-plan and error contracts above.
- The preview preparation notes moved to [docs/PREVIEW.md](https://github.com/lance-format/lance-go/blob/v0.1.0/docs/PREVIEW.md) and
  remain a historical record.

### Upgrade from v0.1.0-preview.2

```sh
go get github.com/lance-format/lance-go@v0.1.0
```

Rebuild default-bundle applications so they embed the new payload. The loader
verifies and extracts the new library into a new content-addressed cache entry,
so the previous entry is simply unused; remove stale
`<cache-root>/lance-go/<sha256>` directories after upgrading when no process is
using them. Deployments that stage a library explicitly must stage the `v0.1.0`
companion library and its new checksum. No Go source changes are required.

### Compatibility

- Go 1.23 or newer, `CGO_ENABLED=0`, and five bundled targets:
  `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64` and
  `windows/arm64`. Intel macOS (`darwin/amd64`) stays retired for new releases;
  its published preview module and tags remain available.
- Linux companions are generic glibc 2.28+ builds and there is no musl
  companion. The macOS companion uses deployment target 11.0. The README and
  [artifacts/README.md](https://github.com/lance-format/lance-go/blob/v0.1.0/artifacts/README.md) record which runtime evidence
  exists for each target and what remains unclaimed.

See the [v0.1.0 release notes](https://github.com/lance-format/lance-go/releases/tag/v0.1.0)
for the exact artifact identities, the post-tag consumer runs and the evidence
archive.

## [v0.1.0-preview.2] - 2026-11-19

Preview of the pure-Go SDK with bundled native libraries for five targets,
RTree index support (`index.ScalarRTree`) and raw storage 2.2 Blob reads. It
bundled `lance-c v0.2.0-beta.1`. See the
[release notes](https://github.com/lance-format/lance-go/releases/tag/v0.1.0-preview.2)
for the exact artifact identities, tag-based consumer runs and evidence.

## [v0.1.0-preview.1] - 2026-11-13

First public preview: pure-Go bindings over the `lance-c` C ABI with
`CGO_ENABLED=0`, immutable dataset snapshots, scans, vector and full-text
search, staged fragments and transactions, Blob reads, table maintenance, and
directory and REST namespaces. It also included Intel macOS and pinned
`lance-c ac2634c`. See the
[release notes](https://github.com/lance-format/lance-go/releases/tag/v0.1.0-preview.1).
