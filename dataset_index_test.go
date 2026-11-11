package lance_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
)

func ptr[T any](value T) *T {
	return &value
}

func TestIndexLifecycleAndMetadata(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3}, []string{"alpha beta", "alpha gamma", "beta gamma"}, []bool{true, true, true})

	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sourceVersion, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	sourceCount, err := dataset.IndexCount()
	if err != nil {
		t.Fatalf("IndexCount before create: %v", err)
	}
	if sourceCount != 0 {
		t.Fatalf("IndexCount before create = %d, want 0", sourceCount)
	}

	indexed, err := dataset.CreateScalarIndex(ctx, "name", index.ScalarInverted, json.RawMessage(`{"with_position":true}`), index.ScalarOptions{}, lance.WithIndexName("name_fts"))
	if err != nil {
		t.Fatalf("CreateScalarIndex: %v", err)
	}
	defer indexed.Close()

	if count, err := dataset.IndexCount(); err != nil || count != 0 {
		t.Fatalf("source IndexCount after create = %d, %v; want 0, nil", count, err)
	}
	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("source Version after create = %d, %v; want %d", version, err, sourceVersion)
	}
	if count, err := indexed.IndexCount(); err != nil || count != 1 {
		t.Fatalf("new IndexCount = %d, %v; want 1, nil", count, err)
	}
	indexedVersion, err := indexed.Version()
	if err != nil {
		t.Fatalf("indexed Version: %v", err)
	}
	if indexedVersion <= sourceVersion {
		t.Fatalf("indexed Version = %d, want > %d", indexedVersion, sourceVersion)
	}

	indices, err := indexed.ListIndices()
	if err != nil {
		t.Fatalf("ListIndices: %v", err)
	}
	if len(indices) != 1 {
		t.Fatalf("ListIndices length = %d, want 1", len(indices))
	}
	if indices[0].Name != "name_fts" {
		t.Fatalf("index name = %q, want name_fts", indices[0].Name)
	}
	if len(indices[0].Columns) != 1 || indices[0].Columns[0] != "name" {
		t.Fatalf("index columns = %v, want [name]", indices[0].Columns)
	}
	if indices[0].DatasetVersion != sourceVersion {
		t.Fatalf("index dataset version = %d, want %d", indices[0].DatasetVersion, sourceVersion)
	}
	if indices[0].Type == "" {
		t.Fatalf("index type is empty")
	}

	descriptions, err := indexed.DescribeIndices()
	if err != nil {
		t.Fatalf("DescribeIndices: %v", err)
	}
	var described *index.Description
	for i := range descriptions {
		if descriptions[i].Name == "name_fts" {
			described = &descriptions[i]
		}
	}
	if described == nil {
		t.Fatalf("DescribeIndices did not include name_fts")
	}
	if len(described.FieldNames) != 1 || described.FieldNames[0] != "name" {
		t.Fatalf("described fields = %v, want [name]", described.FieldNames)
	}
	if described.NumRowsIndexed != 3 {
		t.Fatalf("described rows = %d, want 3", described.NumRowsIndexed)
	}
	if len(described.Segments) == 0 {
		t.Fatalf("described index has no segments")
	}
	if _, err := index.ParseUUID(described.Segments[0].UUID); err != nil {
		t.Fatalf("described segment UUID: %v", err)
	}

	stats, err := indexed.IndexStatistics("name_fts")
	if err != nil {
		t.Fatalf("IndexStatistics: %v", err)
	}
	if !json.Valid(stats) {
		t.Fatalf("IndexStatistics is not valid JSON: %s", stats)
	}

	segments, err := indexed.IndexSegments()
	if err != nil {
		t.Fatalf("IndexSegments: %v", err)
	}
	if len(segments) != 1 || segments[0].Name != "name_fts" {
		t.Fatalf("IndexSegments = %+v, want one name_fts segment", segments)
	}
	uuids, err := indexed.IndexSegmentUUIDs("name_fts")
	if err != nil {
		t.Fatalf("IndexSegmentUUIDs: %v", err)
	}
	if len(uuids) == 0 {
		t.Fatalf("IndexSegmentUUIDs returned no UUIDs")
	}
	if uuids[0].String() != segments[0].UUID {
		t.Fatalf("segment UUID mismatch: %s vs %s", uuids[0].String(), segments[0].UUID)
	}

	if err := indexed.PrewarmIndex(ctx, "name_fts", index.PrewarmOptions{WithPosition: true}); err != nil {
		t.Fatalf("PrewarmIndex all: %v", err)
	}
	if err := indexed.PrewarmIndex(ctx, "name_fts", index.PrewarmOptions{SegmentUUIDs: []index.UUID{uuids[0]}}); err != nil {
		t.Fatalf("PrewarmIndex selected: %v", err)
	}
	var missing index.UUID
	missing[0] = 0xff
	if err := indexed.PrewarmIndex(ctx, "name_fts", index.PrewarmOptions{SegmentUUIDs: []index.UUID{missing}}); !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("PrewarmIndex missing segment = %v, want ErrNotFound", err)
	}

	dropped, err := indexed.DropIndex(ctx, "name_fts")
	if err != nil {
		t.Fatalf("DropIndex: %v", err)
	}
	defer dropped.Close()
	if count, err := dropped.IndexCount(); err != nil || count != 0 {
		t.Fatalf("IndexCount after drop = %d, %v; want 0, nil", count, err)
	}
	if count, err := indexed.IndexCount(); err != nil || count != 1 {
		t.Fatalf("dropped source IndexCount = %d, %v; want 1, nil", count, err)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after drop: %v", err)
	}
	defer reopened.Close()
	if count, err := reopened.IndexCount(); err != nil || count != 0 {
		t.Fatalf("reopened IndexCount = %d, %v; want 0, nil", count, err)
	}
}

func TestIndexNameReplaceAndInvalidScalarParams(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2}, []string{"alpha", "beta"}, []bool{true, true})

	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	first, err := dataset.CreateScalarIndex(ctx, "name", index.ScalarInverted, json.RawMessage(`{"with_position":true}`), index.ScalarOptions{}, lance.WithIndexName("dup"))
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	defer first.Close()
	if _, err := first.CreateScalarIndex(ctx, "name", index.ScalarInverted, json.RawMessage(`{"with_position":true}`), index.ScalarOptions{}, lance.WithIndexName("dup")); err == nil {
		t.Fatalf("duplicate create succeeded")
	}
	replaced, err := first.CreateScalarIndex(ctx, "name", index.ScalarInverted, json.RawMessage(`{"with_position":true}`), index.ScalarOptions{}, lance.WithIndexName("dup"), lance.WithIndexReplace(true))
	if err != nil {
		t.Fatalf("replace create: %v", err)
	}
	replaced.Close()

	badParams := json.RawMessage(`{"with_position":"not-a-bool"}`)
	if _, err := dataset.CreateScalarIndex(ctx, "name", index.ScalarInverted, badParams, index.ScalarOptions{}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("invalid plugin params = %v, want ErrInvalidArgument", err)
	}
	if _, err := first.CreateScalarIndex(ctx, "name", index.ScalarInverted, json.RawMessage(`{"broken"`), index.ScalarOptions{}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("malformed JSON params = %v, want ErrInvalidArgument", err)
	}
}

func TestVectorIndexTrainingAndOptions(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := vectorTestSchema()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)
	ids := builder.Field(0).(*array.Int64Builder)
	lists := builder.Field(1).(*array.FixedSizeListBuilder)
	values := lists.ValueBuilder().(*array.Float32Builder)
	for i := 0; i < 16; i++ {
		ids.Append(int64(i + 1))
		lists.Append(true)
		values.AppendValues([]float32{float32(i), float32(i + 1), float32(i + 2), float32(i + 3)}, nil)
	}
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)

	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	indexed, err := dataset.CreateVectorIndex(ctx, "vector", index.VectorOptions{
		IndexType:     index.VectorTypeIVFFlat,
		Metric:        index.MetricL2,
		NumPartitions: ptr(uint64(2)),
	}, lance.WithIndexName("vector_idx"))
	if err != nil {
		t.Fatalf("CreateVectorIndex: %v", err)
	}
	defer indexed.Close()

	indices, err := indexed.ListIndices()
	if err != nil {
		t.Fatalf("ListIndices: %v", err)
	}
	if len(indices) != 1 || indices[0].Name != "vector_idx" {
		t.Fatalf("ListIndices = %+v, want vector_idx", indices)
	}
	if len(indices[0].Columns) != 1 || indices[0].Columns[0] != "vector" {
		t.Fatalf("vector index columns = %v, want [vector]", indices[0].Columns)
	}
	if indices[0].Type == "" {
		t.Fatalf("vector index type is empty")
	}
}

func TestLegacyVectorParamsExpansion(t *testing.T) {
	options := index.VectorParams{
		IndexType:     index.VectorTypeIVFPQ,
		Metric:        index.MetricCosine,
		NumPartitions: 4,
		NumSubVectors: 2,
	}.Options()
	if options.IndexType != index.VectorTypeIVFPQ || options.Metric != index.MetricCosine {
		t.Fatalf("legacy expansion type/metric = %v/%v", options.IndexType, options.Metric)
	}
	if options.NumPartitions == nil || *options.NumPartitions != 4 {
		t.Fatalf("legacy expansion num partitions = %v, want 4", options.NumPartitions)
	}
	if options.NumSubVectors == nil || *options.NumSubVectors != 2 {
		t.Fatalf("legacy expansion num sub vectors = %v, want 2", options.NumSubVectors)
	}
	if options.NumBits != nil || options.TargetPartitionSize != nil {
		t.Fatalf("legacy expansion invented optional values")
	}
}

func vectorTestSchema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "vector", Type: arrow.FixedSizeListOf(4, arrow.PrimitiveTypes.Float32), Nullable: false},
	}, nil)
}
