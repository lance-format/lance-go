# Changelog

Notable user-facing changes to `lance-go`, the pure-Go Lance SDK that reads,
writes and searches Lance datasets through the `lance-c` C ABI with
`CGO_ENABLED=0`. Each release records the exact native bundle it carries: the
`lance-c` release, its upstream commit, the canonical header hash and the
per-target library checksums. Versions follow Semantic Versioning; within
`0.1.x`, patch releases preserve source compatibility for published Go APIs,
and necessary breaking changes move to the next minor release with migration
notes.

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
