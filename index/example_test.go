//go:build !lance_external_lib

package index_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
)

// This example creates an inverted scalar index and lists it through the
// index metadata APIs.
func ExampleScalarType() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "lance-go-index-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "body", Type: arrow.BinaryTypes.String},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
	builder.Field(1).(*array.StringBuilder).AppendValues([]string{"lance vector search", "vector database"}, nil)
	batch := builder.NewRecordBatch()
	builder.Release()

	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
	if err != nil {
		log.Fatal(err)
	}
	batch.Release()

	dataset, err := lance.Create(ctx, filepath.Join(dir, "data.lance"), reader)
	reader.Release()
	if err != nil {
		log.Fatal(err)
	}
	defer dataset.Close()

	indexed, err := dataset.CreateScalarIndex(
		ctx,
		"body",
		index.ScalarInverted,
		json.RawMessage(`{"with_position": true}`),
		index.ScalarOptions{},
		lance.WithIndexName("body_fts"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer indexed.Close()

	indices, err := indexed.ListIndices()
	if err != nil {
		log.Fatal(err)
	}
	for _, summary := range indices {
		fmt.Println(summary.Name, summary.Columns)
	}
	// Output:
	// body_fts [body]
}

// This example classifies native index type discriminants, which is how segment
// metadata reports whether a segment belongs to a vector or a scalar index.
func ExampleSegmentIndexType_Vector() {
	for _, indexType := range []index.SegmentIndexType{
		index.SegmentIndexType(index.VectorTypeIVFPQ),
		index.SegmentIndexType(index.ScalarInverted),
	} {
		vectorType, isVector := indexType.Vector()
		scalarType, isScalar := indexType.Scalar()
		fmt.Printf("%s: vector=%v(%d) scalar=%v(%d)\n",
			indexType, isVector, vectorType, isScalar, scalarType)
	}
	// Output:
	// IVF_PQ: vector=true(103) scalar=false(0)
	// INVERTED: vector=false(0) scalar=true(4)
}
