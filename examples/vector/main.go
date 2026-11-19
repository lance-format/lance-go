// Command vector runs an exact nearest-neighbor search on a temporary dataset.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/examples/internal/exampleutil"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	uri, cleanup, err := exampleutil.TempURI("vector")
	if err != nil {
		return err
	}
	defer cleanup()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "embedding", Type: arrow.FixedSizeListOfNonNullable(2, arrow.PrimitiveTypes.Float32)},
	}, nil)
	dataset, err := exampleutil.Create(ctx, uri, schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
		list := builder.Field(1).(*array.FixedSizeListBuilder)
		values := list.ValueBuilder().(*array.Float32Builder)
		for _, embedding := range [][]float32{{0, 0}, {1, 0}, {0, 2}} {
			list.Append(true)
			values.AppendValues(embedding, nil)
		}
	})
	if err != nil {
		return err
	}
	defer dataset.Close()

	scan, err := dataset.Scan(
		lance.WithColumns("id"),
		lance.WithNearest("embedding", lance.NewFloat32Vector([]float32{0, 0}), 2),
		lance.WithVectorMetric(lance.VectorMetricL2),
	)
	if err != nil {
		return err
	}
	defer scan.Close()

	for batch, err := range scan.Records(ctx) {
		if err != nil {
			return err
		}
		ids, err := exampleutil.Int64Column(batch, "id")
		if err != nil {
			return err
		}
		distances, err := exampleutil.Float32Column(batch, "_distance")
		if err != nil {
			return err
		}
		for i, id := range ids {
			fmt.Printf("id=%d distance=%g\n", id, distances[i])
		}
	}
	return nil
}
