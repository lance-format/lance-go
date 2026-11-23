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

// This example writes one raw Lance file and reads it back without a dataset
// manifest.
func ExampleCreate() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "lance-go-file-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	uri := filepath.Join(dir, "data.lance")

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
	builder.Field(1).(*array.StringBuilder).AppendValues([]string{"a", "b"}, nil)
	batch := builder.NewRecordBatch()
	builder.Release()

	writer, err := file.Create(ctx, uri, schema, nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := writer.Write(ctx, batch); err != nil {
		log.Fatal(err)
	}
	batch.Release()
	summary, err := writer.Finish(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("rows written:", summary.NumRows)

	reader, err := file.Open(ctx, uri, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	rows, err := reader.ReadAll(ctx, file.ReadOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Release()
	for rows.Next() {
		record := rows.RecordBatch()
		ids := record.Column(record.Schema().FieldIndices("id")[0]).(*array.Int64)
		names := record.Column(record.Schema().FieldIndices("name")[0]).(*array.String)
		for i := 0; i < ids.Len(); i++ {
			fmt.Printf("%d:%s\n", ids.Value(i), names.Value(i))
		}
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
	// Output:
	// rows written: 2
	// 1:a
	// 2:b
}
