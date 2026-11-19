// Command fts creates an inverted index and runs a full-text match query.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/examples/internal/exampleutil"
	"github.com/lance-format/lance-go/index"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	uri, cleanup, err := exampleutil.TempURI("fts")
	if err != nil {
		return err
	}
	defer cleanup()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "body", Type: arrow.BinaryTypes.String},
	}, nil)
	dataset, err := exampleutil.Create(ctx, uri, schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3, 4}, nil)
		builder.Field(1).(*array.StringBuilder).AppendValues([]string{
			"lance vector search",
			"vector database",
			"full text search",
			"search the vector index",
		}, nil)
	})
	if err != nil {
		return err
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
		return err
	}
	defer indexed.Close()

	query, err := lance.NewFTSMatchQuery("vector search", "", lance.FTSMatchOptions{
		Operator: lance.MatchOperatorAnd,
	})
	if err != nil {
		return err
	}
	defer query.Close()

	ids, err := exampleutil.ScanInt64(ctx, indexed, "id", lance.WithFTSQuery(query))
	if err != nil {
		return err
	}
	slices.Sort(ids)
	fmt.Println("FTS matches:", ids)
	return nil
}
