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

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
	"github.com/lance-format/lance-go/namespace"
)

// This example runs an exact nearest-neighbor search. The result contains
// `_distance` in addition to the projected dataset columns.
func ExampleDataset_Scan_vectorSearch() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "lance-go-vector-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "embedding", Type: arrow.FixedSizeListOfNonNullable(2, arrow.PrimitiveTypes.Float32)},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
	embeddings := builder.Field(1).(*array.FixedSizeListBuilder)
	embeddingValues := embeddings.ValueBuilder().(*array.Float32Builder)
	for _, embedding := range [][]float32{{0, 0}, {1, 0}, {0, 2}} {
		embeddings.Append(true)
		embeddingValues.AppendValues(embedding, nil)
	}
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
		ids := batch.Column(batch.Schema().FieldIndices("id")[0]).(*array.Int64)
		distances := batch.Column(batch.Schema().FieldIndices("_distance")[0]).(*array.Float32)
		for i := 0; i < ids.Len(); i++ {
			fmt.Printf("id=%d distance=%g\n", ids.Value(i), distances.Value(i))
		}
	}
	// Output:
	// id=1 distance=0
	// id=2 distance=1
}

// This example creates an inverted index and runs a full-text match query.
func ExampleNewFTSMatchQuery() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "lance-go-fts-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "body", Type: arrow.BinaryTypes.String},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3, 4}, nil)
	builder.Field(1).(*array.StringBuilder).AppendValues([]string{
		"lance vector search",
		"vector database",
		"full text search",
		"search the vector index",
	}, nil)
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

	query, err := lance.NewFTSMatchQuery("vector search", "", lance.FTSMatchOptions{
		Operator: lance.MatchOperatorAnd,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer query.Close()

	scan, err := indexed.Scan(
		lance.WithColumns("id"),
		lance.WithFTSQuery(query),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer scan.Close()

	var ids []int64
	for batch, err := range scan.Records(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		idColumn := batch.Column(batch.Schema().FieldIndices("id")[0]).(*array.Int64)
		for i := 0; i < idColumn.Len(); i++ {
			ids = append(ids, idColumn.Value(i))
		}
	}
	slices.Sort(ids)
	fmt.Println(ids)
	// Output:
	// [1 4]
}

// This example updates matching rows and scans the returned snapshot. The
// receiver keeps its original version.
func ExampleDataset_Update() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "lance-go-update-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2, 3}, nil)
	builder.Field(1).(*array.StringBuilder).AppendValues([]string{"a", "b", "c"}, nil)
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

	updated, updatedRows, err := dataset.Update(ctx, "id = 2",
		lance.ColumnUpdate{Column: "name", Value: "'b2'"},
	)
	if err != nil {
		log.Fatal(err)
	}
	defer updated.Close()

	scan, err := updated.Scan(
		lance.WithColumns("id", "name"),
		lance.WithOrderBy(lance.Ordering{Column: "id", Direction: lance.Ascending}),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer scan.Close()

	for batch, err := range scan.Records(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		idColumn := batch.Column(batch.Schema().FieldIndices("id")[0]).(*array.Int64)
		nameColumn := batch.Column(batch.Schema().FieldIndices("name")[0]).(*array.String)
		for i := 0; i < idColumn.Len(); i++ {
			fmt.Printf("%d:%s\n", idColumn.Value(i), nameColumn.Value(i))
		}
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
	dir, err := os.MkdirTemp("", "lance-go-versions-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	newReader := func(ids []int64) array.RecordReader {
		builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
		defer builder.Release()
		builder.Field(0).(*array.Int64Builder).AppendValues(ids, nil)
		batch := builder.NewRecordBatch()
		defer batch.Release()
		reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
		if err != nil {
			log.Fatal(err)
		}
		return reader
	}
	scanIDs := func(dataset *lance.Dataset) []int64 {
		scan, err := dataset.Scan(lance.WithColumns("id"))
		if err != nil {
			log.Fatal(err)
		}
		defer scan.Close()

		var ids []int64
		for batch, err := range scan.Records(ctx) {
			if err != nil {
				log.Fatal(err)
			}
			idColumn := batch.Column(batch.Schema().FieldIndices("id")[0]).(*array.Int64)
			for i := 0; i < idColumn.Len(); i++ {
				ids = append(ids, idColumn.Value(i))
			}
		}
		return ids
	}

	uri := filepath.Join(dir, "data.lance")
	first := newReader([]int64{1, 2})
	created, err := lance.Create(ctx, uri, first)
	first.Release()
	if err != nil {
		log.Fatal(err)
	}
	if err := created.Close(); err != nil {
		log.Fatal(err)
	}

	second := newReader([]int64{3, 4})
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

	oldIDs := scanIDs(old)
	latestIDs := scanIDs(dataset)
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
	root, err := os.MkdirTemp("", "lance-go-namespace-")
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
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{10, 20, 30}, nil)
	batch := builder.NewRecordBatch()
	builder.Release()

	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
	if err != nil {
		log.Fatal(err)
	}
	batch.Release()

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

	scan, err := dataset.Scan(lance.WithColumns("id"))
	if err != nil {
		log.Fatal(err)
	}
	defer scan.Close()

	var ids []int64
	for batch, err := range scan.Records(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		idColumn := batch.Column(batch.Schema().FieldIndices("id")[0]).(*array.Int64)
		for i := 0; i < idColumn.Len(); i++ {
			ids = append(ids, idColumn.Value(i))
		}
	}
	slices.Sort(ids)
	fmt.Println(ids)
	// Output:
	// [10 20 30]
}

// This example shows the static storage options used for S3, GCS or Azure.
// The same map can be passed to WithStorageOptions for writes. Credentials
// should come from the environment or a secret store, never from source code.
func ExampleWithStorageOptions() {
	storageOptions := map[string]string{
		"aws_access_key_id":     os.Getenv("AWS_ACCESS_KEY_ID"),
		"aws_secret_access_key": os.Getenv("AWS_SECRET_ACCESS_KEY"),
		"aws_region":            os.Getenv("AWS_REGION"),
	}
	dataset, err := lance.Open(context.Background(),
		"s3://bucket/prefix/data.lance",
		lance.WithStorageOptions(storageOptions))
	if err != nil {
		log.Fatal(err)
	}
	defer dataset.Close()
}
