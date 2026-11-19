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

## Stability and compatibility

The 0.1 line is currently published as previews (`v0.1.0-preview.1` and `v0.1.0-preview.2`); no stable `v0.1.0` has been published.

`lance-go` follows Semantic Versioning for `v0.x`: `0.1.x` patch releases preserve source compatibility for published Go APIs. Necessary breaking changes move to the next minor release, `0.2`, with migration notes.

Applications require Go 1.23+ and `CGO_ENABLED=0`. The current preview has five bundled targets: Linux amd64 and arm64 with the generic glibc 2.28 floor, macOS arm64, and Windows amd64 and arm64. Intel macOS (`darwin/amd64`) is not a current target.

Lance storage 2.1 has cross-SDK acceptance for the retained version/operation matrix: four producer writes, 16 cross-reader reads, and six two-way Go/Python, Go/Java and Go/Rust append paths checking version 1 and version 2. This is Linux amd64 evidence against the pinned SDKs, not a claim for other platforms or formats. See the [cross-language suite](compat/README.md) for pins, cells and evidence boundaries.

Raw storage 2.2 Blob reads are verified for preview.2, including the released Lance 14.0.2 fixture and explicit projection. This is read evidence for the raw Blob path only. It does not establish general storage 2.2 read/write interoperability, cross-SDK acceptance, or support for newer format features.

## Supported consumer configuration

| Platform | Bundled targets in preview.2 |
| --- | --- |
| Linux (glibc 2.28+) | amd64, arm64 |
| macOS | arm64 (Apple silicon) |
| Windows | amd64, arm64 |

Preview.2 supports RTree indices and raw storage 2.2 Blob reads with the default bundle. The [previous Go preview](https://github.com/lance-format/lance-go/releases/tag/v0.1.0-preview.1) included Intel macOS and used an earlier lance-c commit, `ac2634c`. Use [examples from the installed tag](https://github.com/lance-format/lance-go/tree/v0.1.0-preview.2/examples).

### How loading works

The first native call verifies and extracts the bundled library to a writable per-user cache. Later runs reuse it. On Linux, set `XDG_CACHE_HOME` to use a different cache root. An explicit `LANCE_LIB_PATH` takes precedence and must point to the matching official shared library. See [loader behavior](docs/development.md#how-loading-works) for details.

### Read-only or no-cache deployment

When the runtime environment has a read-only filesystem or no writable user cache, pre-extract the exact official companion library for your SDK version during image build or staging and set `LANCE_LIB_PATH`. Because `LANCE_LIB_PATH` is checked before the embedded bundle, no extraction or cache write occurs at runtime.

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

Run the application against the extracted library:

```console
$ LANCE_LIB_PATH=/opt/lance-go/liblance_c.so CGO_ENABLED=0 go run .
```

## Documentation

- [Go API reference for preview.2](https://pkg.go.dev/github.com/lance-format/lance-go@v0.1.0-preview.2) and [usage guide](docs/api-guide.md) for scans, mutations, search, and dataset maintenance.
- [Native package details](artifacts/README.md) for platform coverage, library identity, checksums, and the preview.2 exact-tag consumer record.
- [Development and testing](docs/development.md#testing) and [release process](RELEASING.md) for contributors.
