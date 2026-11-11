// Command roundtrip creates, reopens and scans a temporary Lance dataset.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/lance-format/lance-go"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dir, err := os.MkdirTemp("", "lance-go-roundtrip-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ctx := context.Background()
	want := []int64{10, 20, 30}
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).AppendValues(want, nil)
	batch := builder.NewRecordBatch()
	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
	batch.Release()
	if err != nil {
		return err
	}
	defer reader.Release()
	uri := filepath.Join(dir, "data.lance")
	created, err := lance.Create(ctx, uri, reader)
	if err != nil {
		return err
	}
	if err := created.Close(); err != nil {
		return err
	}
	ds, err := lance.Open(ctx, uri)
	if err != nil {
		return err
	}
	defer ds.Close()
	count, err := ds.CountRows(ctx)
	if err != nil {
		return err
	}
	if count != uint64(len(want)) {
		return fmt.Errorf("row count = %d, want %d", count, len(want))
	}
	scan, err := ds.Scan(lance.WithColumns("id"))
	if err != nil {
		return err
	}
	defer scan.Close()
	var got []int64
	for batch, err := range scan.Records(ctx) {
		if err != nil {
			return err
		}
		ids := batch.Column(0).(*array.Int64)
		for i := 0; i < ids.Len(); i++ {
			got = append(got, ids.Value(i))
		}
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("scanned IDs = %v, want %v", got, want)
	}
	fmt.Printf("Created, reopened and scanned %d rows: %v\n", count, got)
	return nil
}
