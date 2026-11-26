# lance-go

Read, write and search [Lance](https://lance.org/) datasets from Go with Apache
Arrow record batches. The package calls into Lance through the
[`lance-c`](https://github.com/lance-format/lance-c) C ABI, and every shipped
binary is built with `CGO_ENABLED=0`: no C toolchain, Rust toolchain or system
Lance installation is needed to build or run a program that uses it.

The module requires the Go minimum declared in [`go.mod`](go.mod) and carries
official prebuilt `lance-c` libraries for Linux amd64 and arm64, macOS arm64,
and Windows amd64 and arm64. Each release's notes record the exact native
chain; [`docs/native-packages.md`](docs/native-packages.md) describes the module
design and validation, while `go.mod`, `internal/ffi/upstream.go`, the generated
artifact modules and each release's notes carry the exact values.

## What you can do

- **Search** with exact or indexed vector similarity, full-text match, phrase, boolean and boost queries, or MinHash similarity.
- **Modify** rows with `Delete`, `Update`, `MergeInsert`, `Merge` and `Truncate`; evolve columns with SQL expressions, null or streamed columns, renames, type changes and drops.
- **Keep history** with versions, tags, branches, checkout and restore. Mutations return new snapshots, while existing readers keep their original version.
- **Store anywhere** on local files, S3, GCS or Azure object storage, or through Directory, REST and Connect namespaces.
- **Use Arrow** to read and write `array.RecordReader` streams without cgo.

The [API guide](docs/api-guide.md) covers these workflows in detail, and the
[examples](examples/) directory contains focused runnable programs for vector
search, full-text search, mutations and versions, cloud storage and namespaces.

## Install

```sh
go get github.com/lance-format/lance-go@latest
```

```go
import lance "github.com/lance-format/lance-go"
```

Resolving the dependency also downloads the native package for each supported
target. The program below creates a temporary dataset, reopens it and scans it,
so it needs neither a repository checkout nor a local installation of Lance.

## Quickstart

Run the bundled example straight from the published module:

```console
$ CGO_ENABLED=0 go run github.com/lance-format/lance-go/examples/roundtrip@latest
Created, reopened and scanned 3 rows: [10 20 30]
```

The same program in your own module, saved as `main.go` and run with
`CGO_ENABLED=0 go run .` in a directory where `data.lance` does not yet exist:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	lance "github.com/lance-format/lance-go"
)

func main() {
	ctx := context.Background()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{10, 20, 30}, nil)

	batch := builder.NewRecordBatch()
	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
	batch.Release()
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Release() // Caller retains ownership of the input RecordReader.

	created, err := lance.Create(ctx, "data.lance", reader)
	if err != nil {
		log.Fatal(err)
	}
	if err := created.Close(); err != nil {
		log.Fatal(err)
	}

	ds, err := lance.Open(ctx, "data.lance")
	if err != nil {
		log.Fatal(err)
	}
	defer ds.Close()

	scan, err := ds.Scan(lance.WithColumns("id"), lance.WithSQLFilter("id >= 10"))
	if err != nil {
		log.Fatal(err)
	}
	defer scan.Close()

	for rec, err := range scan.Records(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		// rec is borrowed until the next iteration step; call rec.Retain()
		// (and later rec.Release()) only if keeping it past this step.
		fmt.Println(rec.NumRows(), rec.Column(0).(*array.Int64).Int64Values())
	}
}
```

Expected output:

```text
3 [10 20 30]
```

Each record yielded by `Records` is borrowed until iteration advances. Call
`Retain` before keeping a record beyond the loop step and `Release` when
finished. Dataset mutations return a new snapshot. Close each dataset handle
after use.

## How the native library is delivered

A default build compiles in one data-only Go module for its target. Those
modules are published as ordinary Go modules at `go.lance.org/liblance/<target>`,
not as Git tags, so the public Go proxy and checksum database pin the payload
bytes that a build embeds. The first native call verifies the payload, extracts
the shared library into a private per-user cache, verifies the extracted bytes,
and loads them.

`LANCE_LIB_PATH` takes precedence over the bundle and is the only candidate when
it is set. A build with `-tags lance_external_lib` excludes the payload and uses
only `LANCE_LIB_PATH` or the platform search.

The loader itself never uses the network. Module downloads happen while Go
resolves the build, so building from a warm module cache and running the
resulting binary need no network access.

[docs/native-packages.md](docs/native-packages.md) describes the module design
and the validation lanes that accept them; the exact versions, sizes, checksums,
commit and header are in `go.mod`, `internal/ffi/upstream.go`, each generated
artifact module and the release notes.

## Supported targets

| Platform | Targets |
| --- | --- |
| Linux (glibc 2.28+) | amd64, arm64 |
| macOS | arm64 (Apple silicon) |
| Windows | amd64, arm64 |

Intel macOS (`darwin/amd64`) is not a supported target: no release selects,
downloads or bundles a library for it. Other operating systems and musl builds
must supply their own `lance-c` library through `LANCE_LIB_PATH` or the platform
search.

RTree indices and raw storage 2.2 Blob reads work with the bundled library. The
[API guide](docs/api-guide.md) describes their support boundaries, and
[native packages](docs/native-packages.md) describes the lanes that run those
regressions.

## Read-only or no-cache deployment

When the runtime environment has a read-only root or no writable user cache,
extract the exact native library for your SDK release while building the image
and point `LANCE_LIB_PATH` at it. Build the application with
`-tags lance_external_lib` so the binary does not embed the compressed payload.
Because `LANCE_LIB_PATH` is checked first, no extraction or cache write happens
at runtime.

For `linux/amd64`, extract the native module payload for the release you are
deploying. Set `ARTIFACT_VERSION` to the native module version recorded in that
release's notes:

```console
$ : "${ARTIFACT_VERSION:?set to the native module version in the release notes}"
$ module=go.lance.org/liblance/linux-amd64
$ go mod download "$module@$ARTIFACT_VERSION"
$ src="$(go env GOMODCACHE)/$module@$ARTIFACT_VERSION/liblance_c.so.zst"
$ install -d -m 0755 /opt/lance-go
$ zstd -d "$src" -o /opt/lance-go/liblance_c.so
$ chmod 0555 /opt/lance-go/liblance_c.so
$ sha256sum /opt/lance-go/liblance_c.so
```

Compare the printed SHA-256 with the uncompressed library hash in that
release's notes.

Build the application without the embedded payload and run it against the
extracted library:

```console
$ CGO_ENABLED=0 go build -tags lance_external_lib -o app .
$ LANCE_LIB_PATH=/opt/lance-go/liblance_c.so ./app
```

The tag spelling is `lance_external_lib` (underscores). The staged file must be
the official `lance-c` library for that target and release; do not mix libraries
from different SDK releases.

The exact compressed and uncompressed sizes and hashes are recorded in each
module's generated `artifact.go` and the release notes. A
default build embeds the payload for its own target and writes the uncompressed
library to the cache on first use. An external build embeds nothing, but the
staged library still occupies its uncompressed size in the image. The tag does
not remove the artifact module from `go.mod`, so module-graph commands such as
`go mod tidy` may still download the artifact archives.

### Cache behavior

The cache entry lives at
`<cache-root>/lance-go/<uncompressed-sha256>/<libraryName>`, where
`<cache-root>` is `LANCE_CACHE_DIR` when non-empty and `os.UserCacheDir`
otherwise. On Linux, `XDG_CACHE_HOME` moves the default user cache. Entries are
content-addressed. The SDK keeps sibling `lance-go/<sha256>` directories after
an upgrade instead of pruning them: the entry lock covers verification, repair
and publication, and ends before the library is opened, so it cannot prove that
another process or an older SDK has finished using an entry. Reclaiming old
directories is an explicit operator decision; remove them only when no process
that may load that entry is running. `LANCE_CACHE_DIR` only relocates the
cache; it does not skip the size and SHA-256 verification performed under a
cross-process lock on every process start, and an explicit `LANCE_LIB_PATH`
avoids that verification, so verify the staged file once during image build as
shown above.

## Stability and compatibility

The SDK follows Semantic Versioning for `v0.x`: within a minor line, patch
releases preserve source compatibility for published Go APIs, and breaking
changes move to a new minor release with migration notes. Each release records
the `lance-c` release, native commit, canonical header hash and per-target
library checksums it carries. See the [changelog](CHANGELOG.md) and
[GitHub releases](https://github.com/lance-format/lance-go/releases) for the
history and the migrations that apply to the version you use.

The historical cross-language compatibility record covers Lance storage 2.1
reads and appends across Go, Python, Java and Rust on Linux amd64. It was
produced before the 0.1 releases and is evidence for that fixture and native
pin only; see the [suite record](internal/compat/README.md#accepted-execution).

### Go version support

The module's minimum is the `go` directive in [`go.mod`](go.mod). CI runs that
minimum and the `stable` Go channel on Linux
and Windows, and the native and release lanes run the floor with
`GOTOOLCHAIN=local`. The `go` directive is the enforced minimum: with
`GOTOOLCHAIN=local`, an older toolchain stops with `go.mod requires go >= ...`;
with the default `GOTOOLCHAIN=auto`, Go downloads a toolchain that satisfies the
directive instead. Any change to the minimum is recorded in that release's
migration notes.

## Troubleshooting

- **Alpine/musl:** the Linux libraries are generic glibc 2.28+ builds. There is no musl build, and the loader reports the missing native dependency instead of falling back to another library.
- **Intel macOS (`darwin/amd64`):** no release selects or bundles a library for this target. Use an Apple-silicon host.
- **Non-writable cache:** a default bundled build must create `<cache-root>/lance-go/...` on first use. Point `LANCE_CACHE_DIR` at a writable absolute path, or use the external-build deployment above. An empty `LANCE_CACHE_DIR` means the platform user cache, which may be unavailable when `HOME` is unset in a minimal container.
- **Mixing releases:** the loader cannot detect a library built for a different SDK release before native calls fail. Use the module that matches your SDK version, and do not substitute a library from another release.

## Documentation

- [Go API reference](https://pkg.go.dev/github.com/lance-format/lance-go) and [usage guide](docs/api-guide.md) for scans, mutations, search and dataset maintenance.
- [Runnable workflow examples](examples/) for vector search, full-text search, mutations and versions, cloud storage and namespaces.
- [Native packages](docs/native-packages.md) for module design, platform scope and validation.
- [Changelog](CHANGELOG.md) and [GitHub releases](https://github.com/lance-format/lance-go/releases) for user-facing changes and the native chain by release.
- [Contributing](CONTRIBUTING.md) for the contributor entry point, [development and testing](docs/development.md) for build, ABI and test details, and [release process](RELEASING.md) for release handoff.
