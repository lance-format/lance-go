# Initial preview notes (unpublished)

This is the release preparation record, not a published SDK release. The
validated SDK candidate is `807cb84305302dc3ff3b40809d817a876efd2909` (PR #47).
Its public pseudo-version is `v0.0.0-20261112185256-807cb8430530`. A release
version and final merged commit have not been selected or tagged.

## Delivered behavior

The SDK provides pure-Go bindings to Lance with `CGO_ENABLED=0`. The API covers
dataset writes and immutable snapshots, scans and row selection, vector and
full-text queries, staged fragments and transactions, Blob reads, raw files,
table maintenance and directory/REST namespace integration. See
[README.md](README.md) for API examples and the external-library build option.

Default builds select a published native companion for each of six targets.
Each companion embeds compressed native bytes that the loader verifies and
extracts into the user cache. Ordinary `go get` can download all six companion
archives. Cold target builds are measured separately and select only the
matching archive. `lance_external_lib` excludes artifact linkage and extraction;
it does not promise that dependency setup downloads no artifact modules.

## Accepted candidate and platform scope

All 23 checks passed on the candidate. The two public-consumer workflows
executed default, namespace-only and external modes on every target: 18 runtime
executions, using public proxy/checksum-database resolution and independent
target module, build and runtime caches.

| Target | Tested host | Public consumer evidence |
| --- | --- | --- |
| Linux amd64 | Ubuntu 24.04.5, glibc 2.39 | [Unix run](https://github.com/lance-format/lance-go/actions/runs/43284438211) |
| Linux arm64 | Ubuntu 24.04.5, glibc 2.39 | [Unix run](https://github.com/lance-format/lance-go/actions/runs/43284438211) |
| macOS amd64 | macOS 15.x, Darwin 24.6.0 | [Unix run](https://github.com/lance-format/lance-go/actions/runs/43284438211) |
| macOS arm64 | macOS 15.x, Darwin 24.6.0 | [Unix run](https://github.com/lance-format/lance-go/actions/runs/43284438211) |
| Windows amd64 | Windows Server 2022 | [Windows run](https://github.com/lance-format/lance-go/actions/runs/43284438526) |
| Windows arm64 | Windows 11 | [Windows run](https://github.com/lance-format/lance-go/actions/runs/43284438526) |

These runs used Go 1.23.0. They verify root archive identity, paired artifact
metadata and payload, binary linkage, cold extraction and real native calls.
Windows additionally verifies the loaded DLL path/hash and rejects
redistributable CRT modules. Windows amd64 native SDK race/checkptr passes;
Go does not support Windows arm64 race builds. These hosts do not establish
older OS floors, musl compatibility or arbitrary CPU targets.

The native source is `lance-c@ac2634c553b450c7787577ffe964fc2b283b8334`, with
header SHA-256 `c19789e9e003930c6065181b5cd9f6f7b464e08a5dc4daca8b21ad82d5160f5b`.
It declares 878 C exports; the SDK binds 603. This is an exact native commit,
not a claim that the older `v0.1.9` release contains the same ABI. Preserve the
existing published versions, payload hashes and publication branches listed
in [artifacts/README.md](artifacts/README.md); SDK metadata snapshots alone are
not complete publishable modules.

## Remaining release gates and limitations

- PR #47 requires a maintainer's approval of its latest head before merge,
  alongside green CI and GitHub `MERGEABLE/CLEAN`.
- Storage 2.1 compatibility passed all 56 cells on Linux amd64 at SDK
  `807cb84`, using the published ac5955a library built from ac263. All four
  producers, 16 cross-reader cells and six bidirectional Go/foreign append
  paths matched the committed literal reports. Foreign SDK pins are pylance
  14.0.2 / pyarrow 22.0.0, lance-java 14.0.2 / Arrow 18.3.0, and Rust core
  `40f15ed1b71b66950961b175829fdf17a763f108` / Arrow 58.3.0. This is Linux
  storage 2.1 evidence; it does not establish storage 2.2 or a cross-platform
  compatibility matrix. See [the suite record](compat/README.md#accepted-execution).
- The current ac263 Linux lifecycle/RSS run completed 30 measured minutes
  with zero workload errors and zero sampled callback-handle delta. RSS grew;
  the measurement does not establish leak absence or a memory budget.
- Real S3 refresh passed in direct and operator-owned namespace modes against
  historical native88. This does not establish current ac263 cloud acceptance,
  a commercial namespace service or a production account configuration.
- RTree non-finite-bounds reload and raw v2.2 Blob reader compatibility retain
  the official-core release holds in [lance-c#153](https://github.com/lance-format/lance-c/issues/153)
  and [lance-c#167](https://github.com/lance-format/lance-c/issues/167). No fork or
  backport is included. Do not claim complete SDK parity or issue #1 completion.
- Select the final merged source and preview version, then follow
  [RELEASING.md](RELEASING.md). After publishing the authorized tag, run both
  public-consumer workflows against that tag with fresh evidence/cache roots.
  A successful pseudo-version run does not prove the tag resolves correctly.

Release preparation and compatibility validation can proceed while the PR
awaits approval. Tag publication and post-tag acceptance remain separate steps.
