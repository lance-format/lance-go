# lance-go

Read, write, and search [Lance](https://lance.org/) datasets from Go using Apache Arrow record batches.
The [current release](https://github.com/lance-format/lance-go/releases/tag/v0.1.0), `v0.1.0`, bundles the official prebuilt shared library from **lance-c v0.2.0**, built against Lance **v15.0.0-beta.25**. Applications use Go 1.23+ with `CGO_ENABLED=0`.

The opt-in `v0.1.1-preview.1` preview moves the bundle to **lance-c v0.2.1-beta.1**, built against Lance **v15.0.0-beta.26**. It keeps the same five targets and published Go APIs; `v0.1.0` remains the recommended stable release.

## What you can do

- **Search** with exact or indexed vector similarity, full-text match, phrase, boolean and boost queries, or MinHash similarity.
- **Modify** rows with `Delete`, `Update`, `MergeInsert`, `Merge` and `Truncate`; evolve columns with SQL expressions, null or streamed columns, renames, type changes and drops.
- **Keep history** with versions, tags, branches, checkout and restore. Mutations return new snapshots, while existing readers keep their original version.
- **Store anywhere** on local files, S3, GCS or Azure object storage, or through Directory, REST and Connect namespaces.
- **Use Arrow** to read and write `array.RecordReader` streams without cgo.

The [API guide](docs/api-guide.md) covers these workflows in detail. The [examples](examples/) directory contains focused runnable programs for vector search, full-text search, mutations and versions, cloud storage and namespaces. These examples track `main`; the published `v0.1.0` tag contains the same examples.

## Quickstart

Run this example from any directory. It creates a temporary dataset, reopens it, and scans it without a repository checkout or a local installation of Lance.

```console
$ CGO_ENABLED=0 go run github.com/lance-format/lance-go/examples/roundtrip@v0.1.0
Created, reopened and scanned 3 rows: [10 20 30]
```

To try the preview instead, run the same example at the preview tag:

```console
$ CGO_ENABLED=0 go run github.com/lance-format/lance-go/examples/roundtrip@v0.1.1-preview.1
Created, reopened and scanned 3 rows: [10 20 30]
```

## Use Lance in an application

In your Go module, install the same version. Dependency setup can fetch native archives for multiple platforms. The standalone example only needs the host platform’s archive.

```sh
go get github.com/lance-format/lance-go@v0.1.0
```

For the preview, install `github.com/lance-format/lance-go@v0.1.1-preview.1` instead and keep the same program. Production applications should stay on the stable release unless they are testing the preview native pin.

Save this program as `main.go` and run `CGO_ENABLED=0 go run .` in a directory where `data.lance` does not already exist.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/lance-format/lance-go"
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

Each record yielded by `Records` is borrowed until iteration advances. Call `Retain` before keeping a record beyond the loop step and `Release` when finished. Dataset mutations return a new snapshot. Close each dataset handle after use.

## Stability and compatibility

`v0.1.0` is the first stable release of the 0.1 line. The earlier `v0.1.0-preview.1` and `v0.1.0-preview.2` pre-releases remain available. `v0.1.1-preview.1` is an opt-in preview of the next patch; it preserves the published `v0.1.0` Go API and adds `Dataset.URI()`.

`lance-go` follows Semantic Versioning for `v0.x`: `0.1.x` patch releases preserve source compatibility for published Go APIs. Necessary breaking changes move to the next minor release, `0.2`, with migration notes.

Applications require Go 1.23+ and `CGO_ENABLED=0`. The current release bundles five targets: Linux amd64 and arm64 with the generic glibc 2.28 floor, macOS arm64, and Windows amd64 and arm64. Intel macOS (`darwin/amd64`) is not a current target.

The historical Lance storage 2.1 acceptance record covers 56 cells: four producer writes, 16 cross-reader reads, and 36 append cells across six directions (both ways for Go/Python, Go/Java and Go/Rust), checking versions 1 and 2. That run used SDK `807cb843` and native `ac2634c` on Linux amd64, before `v0.1.0`; it is not evidence for the released `v0.1.0` or for other platforms and formats. See the [cross-language suite evidence](https://github.com/lance-format/lance-go/blob/0e186d62d0fe3758e190502f9273df7dd7409c55/compat/README.md) for pins, cells and evidence boundaries.

Raw storage 2.2 Blob reads are verified for `v0.1.0`, including the released Lance 14.0.2 fixture and explicit projection. This is read evidence for the raw Blob path only. It does not establish general storage 2.2 read/write interoperability, cross-SDK acceptance, or support for newer format features.

## Supported consumer configuration

| Platform | Bundled targets in v0.1.0 and v0.1.1-preview.1 |
| --- | --- |
| Linux (glibc 2.28+) | amd64, arm64 |
| macOS | arm64 (Apple silicon) |
| Windows | amd64, arm64 |

`v0.1.0` supports RTree indices and raw storage 2.2 Blob reads with the default bundle. `v0.1.1-preview.1` keeps both and carries the newer lance-c pin. The [preview.2](https://github.com/lance-format/lance-go/releases/tag/v0.1.0-preview.2) and [preview.1](https://github.com/lance-format/lance-go/releases/tag/v0.1.0-preview.1) releases remain available; preview.1 included Intel macOS and used an earlier lance-c commit, `ac2634c`. Use [examples from the installed tag](https://github.com/lance-format/lance-go/tree/v0.1.0/examples).

### How loading works

The first native call verifies and extracts the bundled library to a writable cache at `<cache-root>/lance-go/<uncompressed-sha256>/<library>`. The cache root is `LANCE_CACHE_DIR` when it is non-empty, otherwise the platform user cache. On Linux, `XDG_CACHE_HOME` also moves the default user cache. An explicit `LANCE_LIB_PATH` is checked first, bypasses extraction and the cache, and must point to the matching official shared library. See [loader behavior](docs/development.md#how-loading-works) for details.

### Read-only or no-cache deployment

When the runtime environment has a read-only filesystem or no writable user cache, pre-extract the exact official companion library for your SDK version during image build or staging and set `LANCE_LIB_PATH`. Build with `-tags lance_external_lib` so the binary does not embed the compressed payload. Because `LANCE_LIB_PATH` is checked before the embedded bundle, no extraction or cache write occurs at runtime.

The `v0.1.0` companion payloads are 77.0–99.8 MB compressed and 288–403 MB uncompressed; the `v0.1.1-preview.1` payloads are 78.0–99.7 MB compressed and 291–405 MB uncompressed. A default build embeds the compressed payload for its target and writes the uncompressed library to the cache on first use. A tagged build does not embed the payload, but the staged library still occupies its uncompressed size in the image. The tag does not remove the artifact module from `go.mod`, so module-graph commands may still download the companion archives.

For `v0.1.0` on `linux/amd64`, extract `github.com/lance-format/lance-go/artifacts/linux-amd64@v0.1.0`:

```console
$ module=github.com/lance-format/lance-go/artifacts/linux-amd64
$ version=v0.1.0
$ go mod download "$module@$version"
$ src="$(go env GOMODCACHE)/$module@$version/liblance_c.so.zst"
$ install -d -m 0755 /opt/lance-go
$ zstd -d "$src" -o /opt/lance-go/liblance_c.so
$ chmod 0555 /opt/lance-go/liblance_c.so
$ sha256sum /opt/lance-go/liblance_c.so
4617c425e7e7660ef6d0d77f2e14dba48359400d78261b0072cd46e48e87e2ac  /opt/lance-go/liblance_c.so
```

Build the application without the embedded payload and run it against the extracted library:

```console
$ CGO_ENABLED=0 go build -tags lance_external_lib -o app .
$ LANCE_LIB_PATH=/opt/lance-go/liblance_c.so ./app
```

The tag spelling is `lance_external_lib` (underscores).

For the preview on `linux/amd64`, use `version=v0.1.1-preview.1` in the recipe above. The extracted library is 345903736 bytes with SHA-256 `bea1f774ce9ff07ad8565ff3afafe1b49249d2dbb901e58d5bcce529f484cfa5`. The staged file is the official `lance-c v0.2.1-beta.1` library; do not mix it with a `v0.1.0` SDK.

Cache entries are content-addressed by `<uncompressed-sha256>`. After upgrading, remove stale `<cache-root>/lance-go/<sha256>` directories when no process is using them. `LANCE_CACHE_DIR` only relocates the cache; it does not skip the size and SHA-256 verification performed under the file lock on each process start. An explicit `LANCE_LIB_PATH` avoids that cache extraction and verification, so verify the staged file once during image build as shown above.

## Troubleshooting

- **Alpine/musl:** current Linux companions are glibc builds for glibc 2.28+. There is no musl companion. Use a glibc-based image; the loader reports the missing native dependency instead of falling back to another library.
- **Intel macOS (`darwin/amd64`):** `v0.1.0` has no bundled selector or companion archive for Intel macOS. Use an Apple-silicon host. The older preview.1 Intel bundle is historical and must not be mixed with a newer SDK.
- **Non-writable cache:** a default bundled build needs to create and write `<cache-root>/lance-go/...` on first use. Set `LANCE_CACHE_DIR` to a writable absolute path, or use the tagged `LANCE_LIB_PATH` deployment above for a read-only image. An empty `LANCE_CACHE_DIR` uses the platform user cache, which may be unavailable when `HOME` is unset in a minimal container.

## Documentation

- [Go API reference for v0.1.0](https://pkg.go.dev/github.com/lance-format/lance-go@v0.1.0) and [usage guide](docs/api-guide.md) for scans, mutations, search, and dataset maintenance. The [v0.1.1-preview.1 reference](https://pkg.go.dev/github.com/lance-format/lance-go@v0.1.1-preview.1) documents the preview.
- [Runnable workflow examples](examples/) for vector search, full-text search, mutations and versions, cloud storage and namespaces.
- [Native package details](artifacts/README.md) for platform coverage, library identity, checksums, and the exact-tag consumer records.
- [Changelog](CHANGELOG.md) for user-facing changes by release.
- [Development and testing](docs/development.md#testing) and [release process](RELEASING.md) for contributors.
