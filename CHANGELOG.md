# Changelog

Notable user-facing changes to `lance-go`, the pure-Go Lance SDK that reads,
writes and searches Lance datasets through the `lance-c` C ABI with
`CGO_ENABLED=0`. Each release records the exact native bundle it carries: the
`lance-c` release, its upstream commit, the canonical header hash and the
per-target library checksums. Versions follow Semantic Versioning; within
`0.2.x`, patch releases preserve source compatibility for published Go APIs,
and necessary breaking changes move to the next minor release with migration
notes.

## [v0.3.0] - Unreleased

`v0.3.0` is the next minor release. It raises the module minimum to Go 1.26 and
collects the minor-release API changes. No `v0.3.0` tag exists yet.

### Added

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

### Migration from v0.2.0

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
  and checksums are new; see [artifacts/README.md](artifacts/README.md).
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
- The preview preparation notes moved to [docs/PREVIEW.md](docs/PREVIEW.md) and
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
  [artifacts/README.md](artifacts/README.md) record which runtime evidence
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
