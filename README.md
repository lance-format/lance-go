# lance-go

Read, write, and search [Lance](https://lance.org/) datasets from Go using Apache Arrow record batches.
The [current preview](https://github.com/lance-format/lance-go/releases/tag/v0.1.0-preview.2), `v0.1.0-preview.2`, bundles the official prebuilt shared library from **lance-c v0.2.0-beta.1**. Applications use Go 1.23+ with `CGO_ENABLED=0`.

## Quickstart

Run this example from any directory. It creates a temporary dataset, reopens it, and scans it without a repository checkout or a local installation of Lance.

```console
$ CGO_ENABLED=0 go run github.com/lance-format/lance-go/examples/roundtrip@v0.1.0-preview.2
Created, reopened and scanned 3 rows: [10 20 30]
```

## Use Lance in an application

In your Go module, install the same version. Dependency setup can fetch native archives for multiple platforms. The standalone example only needs the host platform’s archive.

```sh
go get github.com/lance-format/lance-go@v0.1.0-preview.2
```

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

## Supported consumer configuration

| Platform | Bundled targets in preview.2 |
| --- | --- |
| Linux (glibc 2.28+) | amd64, arm64 |
| macOS | arm64 (Apple silicon) |
| Windows | amd64, arm64 |

Preview.2 supports RTree indices and raw storage 2.2 Blob reads with the default bundle. The [previous Go preview](https://github.com/lance-format/lance-go/releases/tag/v0.1.0-preview.1) included Intel macOS and used an earlier lance-c commit, `ac2634c`. Use [examples from the installed tag](https://github.com/lance-format/lance-go/tree/v0.1.0-preview.2/examples).

### How loading works

The first native call verifies and extracts the bundled library to a writable cache at `<cache-root>/lance-go/<uncompressed-sha256>/<library>`. The cache root is `LANCE_CACHE_DIR` when it is non-empty, otherwise the platform user cache. On Linux, `XDG_CACHE_HOME` also moves the default user cache. An explicit `LANCE_LIB_PATH` is checked first, bypasses extraction and the cache, and must point to the matching official shared library. See [loader behavior](docs/development.md#how-loading-works) for details.

### Read-only or no-cache deployment

When the runtime environment has a read-only filesystem or no writable user cache, pre-extract the exact official companion library for your SDK version during image build or staging and set `LANCE_LIB_PATH`. Build with `-tags lance_external_lib` so the binary does not embed the compressed payload. Because `LANCE_LIB_PATH` is checked before the embedded bundle, no extraction or cache write occurs at runtime.

The active preview.2 companion payloads are 76.9–99.6 MB compressed and 288–403 MB uncompressed. A default build embeds the compressed payload for its target and writes the uncompressed library to the cache on first use. A tagged build does not embed the payload, but the staged library still occupies its uncompressed size in the image. The tag does not remove the artifact module from `go.mod`, so module-graph commands may still download the companion archives.

For `v0.1.0-preview.2` on `linux/amd64`, extract `github.com/lance-format/lance-go/artifacts/linux-amd64@v0.1.0-preview.2`:

```console
$ module=github.com/lance-format/lance-go/artifacts/linux-amd64
$ version=v0.1.0-preview.2
$ go mod download "$module@$version"
$ src="$(go env GOMODCACHE)/$module@$version/liblance_c.so.zst"
$ install -d -m 0755 /opt/lance-go
$ zstd -d "$src" -o /opt/lance-go/liblance_c.so
$ chmod 0555 /opt/lance-go/liblance_c.so
$ sha256sum /opt/lance-go/liblance_c.so
8e00a4c9fd401d0e99723222d6dfeea47855c57a2e442409b1c32dfed0559c13  /opt/lance-go/liblance_c.so
```

Build the application without the embedded payload and run it against the extracted library:

```console
$ CGO_ENABLED=0 go build -tags lance_external_lib -o app .
$ LANCE_LIB_PATH=/opt/lance-go/liblance_c.so ./app
```

The tag spelling is `lance_external_lib` (underscores).

Cache entries are content-addressed by `<uncompressed-sha256>`. After upgrading, remove stale `<cache-root>/lance-go/<sha256>` directories when no process is using them. `LANCE_CACHE_DIR` only relocates the cache; it does not skip the size and SHA-256 verification performed under the file lock on each process start. An explicit `LANCE_LIB_PATH` avoids that cache extraction and verification, so verify the staged file once during image build as shown above.

## Troubleshooting

- **Alpine/musl:** current Linux companions are glibc builds for glibc 2.28+. There is no musl companion. Use a glibc-based image; the loader reports the missing native dependency instead of falling back to another library.
- **Intel macOS (`darwin/amd64`):** preview.2 has no bundled selector or companion archive for Intel macOS. Use an Apple-silicon host. The older preview.1 Intel bundle is historical and must not be mixed with a newer SDK.
- **Non-writable cache:** a default bundled build needs to create and write `<cache-root>/lance-go/...` on first use. Set `LANCE_CACHE_DIR` to a writable absolute path, or use the tagged `LANCE_LIB_PATH` deployment above for a read-only image. An empty `LANCE_CACHE_DIR` uses the platform user cache, which may be unavailable when `HOME` is unset in a minimal container.

## Documentation

- [Go API reference for preview.2](https://pkg.go.dev/github.com/lance-format/lance-go@v0.1.0-preview.2) and [usage guide](docs/api-guide.md) for scans, mutations, search, and dataset maintenance.
- [Native package details](artifacts/README.md) for platform coverage, library identity, checksums, and the preview.2 exact-tag consumer record.
- [Development and testing](docs/development.md#testing) and [release process](RELEASING.md) for contributors.
