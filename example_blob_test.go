//go:build !lance_external_lib

package lance_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
)

// This example writes a small blob dataset and ranges over a whole-blob read.
// BlobRecords plans the read when iteration starts and releases it when
// iteration ends, so the caller releases no stream. Each yielded batch stays
// borrowed until iteration advances; call Retain to keep one.
func ExampleDataset_BlobRecords() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "lance-go-example-blob-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	uri := filepath.Join(dir, "data.lance")

	inlineThreshold := uint64(1 << 20)
	field, err := lance.BlobFieldWithOptions("blob", true, lance.BlobFieldOptions{InlineSizeThreshold: &inlineThreshold})
	if err != nil {
		log.Fatal(err)
	}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		field,
	}, nil)

	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
	blobs := builder.Field(1).(*array.StructBuilder)
	data := blobs.FieldBuilder(0).(*array.BinaryBuilder)
	blobURI := blobs.FieldBuilder(1).(*array.StringBuilder)
	for _, value := range [][]byte{[]byte("alpha"), []byte("beta")} {
		blobs.Append(true)
		data.Append(value)
		blobURI.AppendNull()
	}
	batch := builder.NewRecordBatch()
	defer batch.Release()
	builder.Release()

	records, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
	if err != nil {
		log.Fatal(err)
	}
	dataset, err := lance.Create(ctx, uri, records, lance.WithWriteParams(lance.WriteParams{EnableStableRowIDs: true}))
	records.Release()
	if err != nil {
		log.Fatal(err)
	}
	defer dataset.Close()

	for batch, err := range dataset.BlobRecords(ctx, "blob", lance.BlobRowSelectorIndices, []uint64{0, 1}, nil) {
		if err != nil {
			log.Fatal(err)
		}
		data := batch.Column(1).(*array.LargeBinary)
		for i := 0; i < int(batch.NumRows()); i++ {
			fmt.Printf("%s\n", data.Value(i))
		}
	}
	// Output:
	// alpha
	// beta
}
