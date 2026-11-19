// Command namespace writes and reopens a table in a local directory namespace.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/examples/internal/exampleutil"
	"github.com/lance-format/lance-go/namespace"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	root, err := os.MkdirTemp("", "lance-go-namespace-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	ns, err := namespace.NewDirectory(map[string]string{"root": root})
	if err != nil {
		return err
	}
	defer ns.Close()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	reader, err := exampleutil.NewReader(schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{10, 20, 30}, nil)
	})
	if err != nil {
		return err
	}
	defer reader.Release()

	tableID := []string{"events"}
	created, err := lance.WriteToNamespace(ctx, ns, tableID, reader)
	if err != nil {
		return err
	}
	if err := created.Close(); err != nil {
		return err
	}

	dataset, err := lance.OpenFromNamespace(ctx, ns, tableID)
	if err != nil {
		return err
	}
	defer dataset.Close()

	ids, err := exampleutil.ScanInt64(ctx, dataset, "id")
	if err != nil {
		return err
	}
	slices.Sort(ids)
	fmt.Println("namespace rows:", ids)
	return nil
}
