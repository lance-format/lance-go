// Package lance provides pure-Go bindings for Lance datasets.
//
// Lance stores versioned datasets as Arrow record batches in local files or
// object storage. This package creates, opens, scans, mutates and maintains
// those datasets without cgo. Native operations use the lance-c shared library,
// which the bindings load on first use.
//
// # Dataset snapshots
//
// A Dataset is an immutable, checked-out snapshot. Mutating methods commit a new
// version and return a new Dataset; the receiver keeps its version and any open
// scanners. Close each Dataset when finished. Close is idempotent and waits for
// in-flight operations. Objects that already own native state, such as a bound
// Fragment, a returned BlobFile or a dataset returned by a successful mutation,
// remain usable after the source Dataset is closed.
//
// Dataset.URI reports the fully qualified URI native resolved when the snapshot
// was produced. A namespace dataset reports its resolved describe_table location
// rather than the table identifier used in error diagnostics. Each returned
// snapshot reports its own native URI, and the value remains available after
// Close.
//
// Scanners are single-use. Each batch yielded by Scanner.Records is borrowed
// until the next iteration step. Retain the batch before keeping it past that
// step and Release it when finished. Batches returned by Take and TakeRows are
// owned by the caller and must each be released.
//
// A [Session] shares index and metadata caches across datasets and raw files.
// Close the caller's Session after opening those objects; opened objects keep
// the cache state they retained. A [StorageOptionsAccessor] similarly owns
// reusable storage options. Its Close implements io.Closer, reports no failure,
// and does not invalidate native datasets or stores that retained their own
// reference. A provider value stays caller-owned: the SDK never calls a
// provider Close method and keeps the provider reachable until native releases
// the final owner.
//
// [Session], [CacheStats], [StorageOptionsAccessor], [StorageOptionsProvider]
// and [ForceStorageOptionsProvider] are declared in this package, so go doc and
// pkg.go.dev index their methods, fields and interface signatures directly.
//
// # Errors
//
// Operations return *Error for native and argument failures. Error.Code
// identifies the failure; Error.Op and Error.URI identify the operation and
// dataset. Error.Unwrap maps the code to a sentinel such as ErrNotFound,
// ErrInvalidArgument or ErrCommitConflict, so errors.Is works across the API.
// ErrClosed reports use after Close. Context cancellation is checked before a
// synchronous native call but cannot interrupt a call already in progress.
//
// # Native library
//
// The package supports CGO_ENABLED=0. The first native call verifies and loads
// the lance-c shared library. Default builds on published targets use the
// compressed companion library bundled with the matching module version; the
// loader verifies it and extracts a private copy into a per-user cache.
// LANCE_LIB_PATH selects an explicit library and takes precedence over the
// bundle and the platform search. A build with -tags lance_external_lib
// omits the bundle and uses only LANCE_LIB_PATH or the platform search.
//
// # Subpackages
//
// The root package provides dataset, scanner, transaction, session and storage
// option APIs. The supporting packages are:
//
//   - compaction: options, plans, tasks and transport values for dataset
//     compaction and index optimization.
//   - file: raw .lance file readers, writers and sessions without a dataset
//     manifest.
//   - index: vector and scalar index options, enums and segment metadata.
//   - namespace: typed Lance namespace access through Directory, REST, Connect
//     and custom adapters.
//
// See the repository README and docs/api-guide.md for installation and
// end-to-end examples.
package lance
