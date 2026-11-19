//go:build !lance_external_lib

package lance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
	"github.com/lance-format/lance-go/namespace"
)

func exampleTempURI(prefix string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "lance-go-example-"+prefix+"-")
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(dir, "data.lance"), func() { _ = os.RemoveAll(dir) }, nil
}

func exampleReader(schema *arrow.Schema, fill func(*array.RecordBuilder)) (array.RecordReader, error) {
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	fill(builder)
	batch := builder.NewRecordBatch()
	defer batch.Release()
	return array.NewRecordReader(schema, []arrow.RecordBatch{batch})
}

func exampleColumn(batch arrow.RecordBatch, name string) (arrow.Array, bool) {
	for i := 0; i < int(batch.NumCols()); i++ {
		if batch.Schema().Field(i).Name == name {
			return batch.Column(i), true
		}
	}
	return nil, false
}

func exampleScanInt64(ctx context.Context, dataset *lance.Dataset, column string, opts ...lance.ScanOption) ([]int64, error) {
	scanner, err := dataset.Scan(append([]lance.ScanOption{lance.WithColumns(column)}, opts...)...)
	if err != nil {
		return nil, err
	}
	defer scanner.Close()

	var result []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			return nil, err
		}
		values, ok := exampleColumn(batch, column)
		if !ok {
			return nil, fmt.Errorf("batch has no column %q", column)
		}
		int64Values, ok := values.(*array.Int64)
		if !ok {
			return nil, fmt.Errorf("column %q has type %T", column, values)
		}
		for i := 0; i < int64Values.Len(); i++ {
			result = append(result, int64Values.Value(i))
		}
	}
	return result, nil
}

func exampleScanInt64String(ctx context.Context, dataset *lance.Dataset, idColumn, textColumn string, opts ...lance.ScanOption) ([]int64, []string, error) {
	scanner, err := dataset.Scan(append([]lance.ScanOption{lance.WithColumns(idColumn, textColumn)}, opts...)...)
	if err != nil {
		return nil, nil, err
	}
	defer scanner.Close()

	var ids []int64
	var texts []string
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			return nil, nil, err
		}
		idValues, ok := exampleColumn(batch, idColumn)
		if !ok {
			return nil, nil, fmt.Errorf("batch has no column %q", idColumn)
		}
		idArray, ok := idValues.(*array.Int64)
		if !ok {
			return nil, nil, fmt.Errorf("column %q has type %T", idColumn, idValues)
		}
		textValues, ok := exampleColumn(batch, textColumn)
		if !ok {
			return nil, nil, fmt.Errorf("batch has no column %q", textColumn)
		}
		textArray, ok := textValues.(*array.String)
		if !ok {
			return nil, nil, fmt.Errorf("column %q has type %T", textColumn, textValues)
		}
		for i := 0; i < idArray.Len(); i++ {
			ids = append(ids, idArray.Value(i))
			texts = append(texts, strings.Clone(textArray.Value(i)))
		}
	}
	return ids, texts, nil
}

// This example runs an exact nearest-neighbor search. The result contains
// `_distance` in addition to the projected dataset columns.
func ExampleDataset_Scan_vectorSearch() {
	ctx := context.Background()
	uri, cleanup, err := exampleTempURI("vector")
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "embedding", Type: arrow.FixedSizeListOfNonNullable(2, arrow.PrimitiveTypes.Float32)},
	}, nil)
	reader, err := exampleReader(schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
		list := builder.Field(1).(*array.FixedSizeListBuilder)
		values := list.ValueBuilder().(*array.Float32Builder)
		for _, embedding := range [][]float32{{0, 0}, {1, 0}, {0, 2}} {
			list.Append(true)
			values.AppendValues(embedding, nil)
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	dataset, err := lance.Create(ctx, uri, reader)
	reader.Release()
	if err != nil {
		log.Fatal(err)
	}
	defer dataset.Close()

	scan, err := dataset.Scan(
		lance.WithColumns("id"),
		lance.WithNearest("embedding", lance.NewFloat32Vector([]float32{0, 0}), 2),
		lance.WithVectorMetric(lance.VectorMetricL2),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer scan.Close()

	for batch, err := range scan.Records(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		ids, ok := exampleColumn(batch, "id")
		if !ok {
			log.Fatal("batch has no id column")
		}
		distances, ok := exampleColumn(batch, "_distance")
		if !ok {
			log.Fatal("batch has no _distance column")
		}
		idArray := ids.(*array.Int64)
		distanceArray := distances.(*array.Float32)
		for i := 0; i < idArray.Len(); i++ {
			fmt.Printf("id=%d distance=%g\n", idArray.Value(i), distanceArray.Value(i))
		}
	}
	// Output:
	// id=1 distance=0
	// id=2 distance=1
}

// This example creates an inverted index and runs a full-text match query.
func ExampleNewFTSMatchQuery() {
	ctx := context.Background()
	uri, cleanup, err := exampleTempURI("fts")
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "body", Type: arrow.BinaryTypes.String},
	}, nil)
	reader, err := exampleReader(schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3, 4}, nil)
		builder.Field(1).(*array.StringBuilder).AppendValues([]string{
			"lance vector search",
			"vector database",
			"full text search",
			"search the vector index",
		}, nil)
	})
	if err != nil {
		log.Fatal(err)
	}
	dataset, err := lance.Create(ctx, uri, reader)
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

	query, err := lance.NewFTSMatchQuery("vector search", "", lance.FTSMatchOptions{
		Operator: lance.MatchOperatorAnd,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer query.Close()

	ids, err := exampleScanInt64(ctx, indexed, "id", lance.WithFTSQuery(query))
	if err != nil {
		log.Fatal(err)
	}
	slices.Sort(ids)
	fmt.Println(ids)
	// Output:
	// [1 4]
}

// This example updates matching rows and shows that the receiver keeps its
// original snapshot.
func ExampleDataset_Update() {
	ctx := context.Background()
	uri, cleanup, err := exampleTempURI("update")
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	reader, err := exampleReader(schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
		builder.Field(1).(*array.StringBuilder).AppendValues([]string{"a", "b", "c"}, nil)
	})
	if err != nil {
		log.Fatal(err)
	}
	dataset, err := lance.Create(ctx, uri, reader)
	reader.Release()
	if err != nil {
		log.Fatal(err)
	}
	defer dataset.Close()

	updated, updatedRows, err := dataset.Update(ctx, "id = 2",
		lance.ColumnUpdate{Column: "name", Value: "'b2'"},
	)
	if err != nil {
		log.Fatal(err)
	}
	defer updated.Close()

	ids, names, err := exampleScanInt64String(ctx, updated, "id", "name",
		lance.WithOrderBy(lance.Ordering{Column: "id", Direction: lance.Ascending}))
	if err != nil {
		log.Fatal(err)
	}
	for i := range ids {
		fmt.Printf("%d:%s\n", ids[i], names[i])
	}
	fmt.Println("updated", updatedRows)
	// Output:
	// 1:a
	// 2:b2
	// 3:c
	// updated 1
}

// This example reads dataset history and checks out an older version.
func ExampleDataset_Versions() {
	ctx := context.Background()
	uri, cleanup, err := exampleTempURI("versions")
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	first, err := exampleReader(schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
	})
	if err != nil {
		log.Fatal(err)
	}
	created, err := lance.Create(ctx, uri, first)
	first.Release()
	if err != nil {
		log.Fatal(err)
	}
	if err := created.Close(); err != nil {
		log.Fatal(err)
	}

	second, err := exampleReader(schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{3, 4}, nil)
	})
	if err != nil {
		log.Fatal(err)
	}
	dataset, err := lance.Append(ctx, uri, second)
	second.Release()
	if err != nil {
		log.Fatal(err)
	}
	defer dataset.Close()

	versions, err := dataset.Versions(ctx)
	if err != nil {
		log.Fatal(err)
	}
	versionIDs := make([]uint64, len(versions))
	for i, version := range versions {
		versionIDs[i] = version.ID
	}
	old, err := dataset.CheckoutVersion(ctx, 1)
	if err != nil {
		log.Fatal(err)
	}
	defer old.Close()
	oldIDs, err := exampleScanInt64(ctx, old, "id")
	if err != nil {
		log.Fatal(err)
	}
	latestIDs, err := exampleScanInt64(ctx, dataset, "id")
	if err != nil {
		log.Fatal(err)
	}
	slices.Sort(oldIDs)
	slices.Sort(latestIDs)
	fmt.Println("versions", versionIDs)
	fmt.Println("version1", oldIDs)
	fmt.Println("latest", latestIDs)
	// Output:
	// versions [1 2]
	// version1 [1 2]
	// latest [1 2 3 4]
}

// This example writes and reopens a table through a local directory namespace.
func ExampleWriteToNamespace() {
	ctx := context.Background()
	root, err := os.MkdirTemp("", "lance-go-example-namespace-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(root)

	ns, err := namespace.NewDirectory(map[string]string{"root": root})
	if err != nil {
		log.Fatal(err)
	}
	defer ns.Close()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	reader, err := exampleReader(schema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{10, 20, 30}, nil)
	})
	if err != nil {
		log.Fatal(err)
	}
	tableID := []string{"events"}
	created, err := lance.WriteToNamespace(ctx, ns, tableID, reader)
	reader.Release()
	if err != nil {
		log.Fatal(err)
	}
	if err := created.Close(); err != nil {
		log.Fatal(err)
	}

	dataset, err := lance.OpenFromNamespace(ctx, ns, tableID)
	if err != nil {
		log.Fatal(err)
	}
	defer dataset.Close()
	ids, err := exampleScanInt64(ctx, dataset, "id")
	if err != nil {
		log.Fatal(err)
	}
	slices.Sort(ids)
	fmt.Println(ids)
	// Output:
	// [10 20 30]
}

// This example shows the static storage options used for S3, GCS or Azure.
// The same map can be passed to WithWriteStorageOptions for writes. Credentials
// should come from the environment or a secret store, never from source code.
func ExampleWithStorageOptions() {
	storageOptions := map[string]string{
		"aws_access_key_id":     os.Getenv("AWS_ACCESS_KEY_ID"),
		"aws_secret_access_key": os.Getenv("AWS_SECRET_ACCESS_KEY"),
		"aws_region":            os.Getenv("AWS_REGION"),
	}
	_, err := lance.Open(context.Background(),
		"s3://bucket/prefix/data.lance",
		lance.WithStorageOptions(storageOptions))
	if err != nil {
		log.Fatal(err)
	}
}
