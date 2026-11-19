// Command mutations applies deletes, updates, a merge-insert and a schema
// change, then reads an older version of the resulting dataset.
package main

import (
	"context"
	"fmt"
	"log"
	"slices"

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
	uri, cleanup, err := exampleutil.TempURI("mutations")
	if err != nil {
		return err
	}
	defer cleanup()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	dataset, err := exampleutil.Create(ctx, uri, schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3, 4}, nil)
		builder.Field(1).(*array.StringBuilder).AppendValues([]string{"a", "b", "c", "d"}, nil)
	})
	if err != nil {
		return err
	}
	defer dataset.Close()

	deleted, deletedRows, err := dataset.Delete(ctx, "id > 2")
	if err != nil {
		return err
	}
	defer deleted.Close()

	updated, updatedRows, err := deleted.Update(ctx, "id = 2",
		lance.ColumnUpdate{Column: "name", Value: "'b2'"},
	)
	if err != nil {
		return err
	}
	defer updated.Close()

	source, err := exampleutil.NewReader(schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{2, 5}, nil)
		builder.Field(1).(*array.StringBuilder).AppendValues([]string{"b3", "e"}, nil)
	})
	if err != nil {
		return err
	}
	defer source.Release()

	merged, stats, err := updated.MergeInsert(ctx, []string{"id"}, source, lance.MergeInsertParams{
		WhenMatched: lance.MergeWhenMatchedUpdateAll,
	})
	if err != nil {
		return err
	}
	defer merged.Close()

	evolved, err := merged.AddColumnsSQL(ctx, []lance.SQLColumn{
		{Name: "double_id", Expression: "id * 2"},
	})
	if err != nil {
		return err
	}
	defer evolved.Close()

	history, err := evolved.Versions(ctx)
	if err != nil {
		return err
	}
	versionIDs := make([]uint64, len(history))
	for i, version := range history {
		versionIDs[i] = version.ID
	}

	first, err := evolved.CheckoutVersion(ctx, 1)
	if err != nil {
		return err
	}
	defer first.Close()

	firstIDs, err := exampleutil.ScanInt64(ctx, first, "id")
	if err != nil {
		return err
	}
	latestIDs, err := exampleutil.ScanInt64(ctx, evolved, "id")
	if err != nil {
		return err
	}
	slices.Sort(firstIDs)
	slices.Sort(latestIDs)

	latestSchema, err := evolved.Schema()
	if err != nil {
		return err
	}

	fmt.Printf("deleted=%d updated=%d merge_inserted=%d merge_updated=%d\n",
		deletedRows, updatedRows, stats.InsertedRows, stats.UpdatedRows)
	fmt.Printf("versions=%v latest=%d latest_rows=%v version1_rows=%v double_id=%t\n",
		versionIDs, versionIDs[len(versionIDs)-1], latestIDs, firstIDs, latestSchema.HasField("double_id"))
	return nil
}
