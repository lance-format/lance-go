//go:build !lance_external_lib

package file_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go/file"
)

// This example writes a small raw Lance file and ranges over its batches.
// Records opens the stream when iteration starts and releases it when
// iteration ends, so the caller releases no stream. Each yielded batch stays
// borrowed until iteration advances; call Retain to keep one.
func ExampleReader_Records() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "lance-go-example-file-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "data.lance")

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
	builder.Field(1).(*array.StringBuilder).AppendValues([]string{"a", "b", "c"}, nil)
	batch := builder.NewRecordBatch()
	defer batch.Release()
	builder.Release()

	writer, err := file.Create(ctx, path, schema, nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := writer.Write(ctx, batch); err != nil {
		log.Fatal(err)
	}
	if _, err := writer.Finish(ctx); err != nil {
		log.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		log.Fatal(err)
	}

	reader, err := file.Open(ctx, path, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	for batch, err := range reader.Records(ctx, file.ReadOptions{BatchSize: 2}) {
		if err != nil {
			log.Fatal(err)
		}
		ids := batch.Column(0).(*array.Int64)
		for i := 0; i < ids.Len(); i++ {
			fmt.Println(ids.Value(i))
		}
	}
	// Output:
	// 1
	// 2
	// 3
}
