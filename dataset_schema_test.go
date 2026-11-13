package lance_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
)

func stringPointer(value string) *string { return &value }

func boolPointer(value bool) *bool { return &value }

func makeOneStringRecord(t *testing.T, schema *arrow.Schema, values []string, valid []bool) arrow.RecordBatch {
	t.Helper()
	if len(values) != len(valid) {
		t.Fatalf("string record values and validity have different lengths")
	}
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)
	strings := builder.Field(0).(*array.StringBuilder)
	for i, value := range values {
		if !valid[i] {
			strings.AppendNull()
			continue
		}
		strings.Append(value)
	}
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return record
}

func fieldByName(t *testing.T, schema *arrow.Schema, name string) arrow.Field {
	t.Helper()
	for _, field := range schema.Fields() {
		if field.Name == name {
			return field
		}
	}
	t.Fatalf("schema has no field %q: %v", name, schema)
	return arrow.Field{}
}

func TestSchemaEvolutionColumnsMetadataAndConfig(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, uri, _ := newMutationDataset(t)
	baseVersion := mutationVersion(t, dataset)
	baseSchema, err := dataset.Schema()
	if err != nil {
		t.Fatalf("base Schema: %v", err)
	}

	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("base Scan: %v", err)
	}
	defer scanner.Close()

	next := dataset
	defer func() {
		if next != nil {
			_ = next.Close()
		}
	}()

	emptyMetadata, err := next.TableMetadata()
	if err != nil {
		t.Fatalf("TableMetadata: %v", err)
	}
	if len(emptyMetadata) != 0 {
		t.Fatalf("initial table metadata = %v, want empty", emptyMetadata)
	}
	emptyConfig, err := next.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if len(emptyConfig) != 0 {
		t.Fatalf("initial config = %v, want empty", emptyConfig)
	}

	owner := "alice"
	empty := ""
	next, metadata, err := next.UpdateTableMetadata(ctx, lance.MetadataMerge,
		lance.MetadataUpdate{Key: "owner", Value: &owner},
		lance.MetadataUpdate{Key: "empty", Value: &empty},
	)
	if err != nil {
		t.Fatalf("UpdateTableMetadata merge: %v", err)
	}
	if metadata["owner"] != "alice" || metadata["empty"] != "" {
		t.Fatalf("merged table metadata = %v", metadata)
	}
	oldMetadata, err := dataset.TableMetadata()
	if err != nil {
		t.Fatalf("source TableMetadata: %v", err)
	}
	if len(oldMetadata) != 0 {
		t.Fatalf("source table metadata changed in place: %v", oldMetadata)
	}

	next, metadata, err = next.UpdateTableMetadata(ctx, lance.MetadataReplace,
		lance.MetadataUpdate{Key: "stage", Value: stringPointer("prod")},
	)
	if err != nil {
		t.Fatalf("UpdateTableMetadata replace: %v", err)
	}
	if _, ok := metadata["owner"]; ok {
		t.Fatalf("replace retained owner: %v", metadata)
	}
	if metadata["stage"] != "prod" {
		t.Fatalf("replace metadata = %v", metadata)
	}
	next, metadata, err = next.UpdateTableMetadata(ctx, lance.MetadataMerge,
		lance.MetadataUpdate{Key: "empty", Value: nil},
	)
	if err != nil {
		t.Fatalf("UpdateTableMetadata delete: %v", err)
	}
	if _, ok := metadata["empty"]; ok {
		t.Fatalf("delete retained empty: %v", metadata)
	}

	next, schemaMetadata, err := next.UpdateSchemaMetadata(ctx, lance.MetadataMerge,
		lance.MetadataUpdate{Key: "producer", Value: stringPointer("go-test")},
	)
	if err != nil {
		t.Fatalf("UpdateSchemaMetadata: %v", err)
	}
	if schemaMetadata["producer"] != "go-test" {
		t.Fatalf("schema metadata = %v", schemaMetadata)
	}
	next, schemaMetadata, err = next.UpdateSchemaMetadata(ctx, lance.MetadataReplace,
		lance.MetadataUpdate{Key: "pipeline", Value: stringPointer("merge")},
	)
	if err != nil {
		t.Fatalf("UpdateSchemaMetadata replace: %v", err)
	}
	if _, ok := schemaMetadata["producer"]; ok || schemaMetadata["pipeline"] != "merge" {
		t.Fatalf("replaced schema metadata = %v", schemaMetadata)
	}

	next, err = next.AddColumnsSQL(ctx, []lance.SQLColumn{
		{Name: "double_id", Expression: "id * 2"},
	}, 2)
	if err != nil {
		t.Fatalf("AddColumnsSQL: %v", err)
	}

	extraSchema := arrow.NewSchema([]arrow.Field{
		{Name: "extra", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "small", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
		{Name: "nested", Type: arrow.StructOf(arrow.Field{Name: "inner", Type: arrow.BinaryTypes.String, Nullable: true}), Nullable: true},
	}, nil)
	next, err = next.AddColumnsNulls(ctx, extraSchema)
	if err != nil {
		t.Fatalf("AddColumnsNulls: %v", err)
	}

	tagSchema := arrow.NewSchema([]arrow.Field{
		{Name: "tag", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	tagReader := &releaseCountingReader{RecordReader: makeReader(t, tagSchema,
		makeOneStringRecord(t, tagSchema, []string{"tag-1", "tag-2"}, []bool{true, true}),
		makeOneStringRecord(t, tagSchema, []string{"tag-3", "tag-4"}, []bool{true, true}),
	)}
	next, err = next.AddColumnsStream(ctx, tagReader, 2)
	if err != nil {
		t.Fatalf("AddColumnsStream: %v", err)
	}
	if tagReader.releases != 1 {
		t.Fatalf("AddColumnsStream released reader %d times, want 1", tagReader.releases)
	}

	nestedUpdate := next
	nestedUpdate, err = nestedUpdate.UpdateFieldMetadata(ctx, lance.FieldMetadataUpdate{
		Field:   lance.FieldReference{Kind: lance.FieldReferenceByPath, Path: "nested.inner"},
		Updates: []lance.MetadataUpdate{{Key: "unit", Value: stringPointer("text")}},
		Mode:    lance.MetadataMerge,
	})
	if err != nil {
		t.Fatalf("UpdateFieldMetadata nested: %v", err)
	}
	nestedMetadata, err := nestedUpdate.FieldMetadata(lance.FieldReference{Kind: lance.FieldReferenceByPath, Path: "nested.inner"})
	if err != nil {
		t.Fatalf("FieldMetadata nested: %v", err)
	}
	if value, ok := nestedMetadata.GetValue("unit"); !ok || value != "text" {
		t.Fatalf("nested field metadata = %v", nestedMetadata)
	}
	next = nestedUpdate

	next, err = next.UpdateFieldMetadata(ctx, lance.FieldMetadataUpdate{
		Field:   lance.FieldReference{Kind: lance.FieldReferenceByPath, Path: "tag"},
		Updates: []lance.MetadataUpdate{{Key: "unit", Value: stringPointer("tag")}},
		Mode:    lance.MetadataMerge,
	})
	if err != nil {
		t.Fatalf("UpdateFieldMetadata tag: %v", err)
	}
	tagMetadata, err := next.FieldMetadata(lance.FieldReference{Kind: lance.FieldReferenceByPath, Path: "tag"})
	if err != nil {
		t.Fatalf("FieldMetadata tag: %v", err)
	}
	if value, ok := tagMetadata.GetValue("unit"); !ok || value != "tag" {
		t.Fatalf("tag field metadata = %v", tagMetadata)
	}
	if _, err := next.FieldMetadata(lance.FieldReference{Kind: lance.FieldReferenceByID, FieldID: 0}); err != nil {
		t.Fatalf("FieldMetadata by id: %v", err)
	}

	next, err = next.AlterColumns(ctx,
		lance.ColumnAlteration{Path: "name", Rename: stringPointer("label")},
		lance.ColumnAlteration{Path: "id", Nullable: boolPointer(false)},
	)
	if err != nil {
		t.Fatalf("AlterColumns rename/nullability: %v", err)
	}
	next, err = next.AlterColumns(ctx, lance.ColumnAlteration{
		Path:     "small",
		DataType: arrow.PrimitiveTypes.Int64,
	})
	if err != nil {
		t.Fatalf("AlterColumns type: %v", err)
	}

	next, err = next.DropColumns(ctx, "extra", "nested")
	if err != nil {
		t.Fatalf("DropColumns: %v", err)
	}

	configValue := "one"
	configSecond := "two"
	next, config, err := next.UpdateConfig(ctx, lance.MetadataMerge,
		lance.MetadataUpdate{Key: "first", Value: &configValue},
		lance.MetadataUpdate{Key: "second", Value: &configSecond},
	)
	if err != nil {
		t.Fatalf("UpdateConfig merge: %v", err)
	}
	if config["first"] != "one" || config["second"] != "two" {
		t.Fatalf("merged config = %v", config)
	}
	next, config, err = next.UpdateConfig(ctx, lance.MetadataMerge, lance.MetadataUpdate{Key: "second", Value: nil})
	if err != nil {
		t.Fatalf("UpdateConfig delete: %v", err)
	}
	if _, ok := config["second"]; ok || config["first"] != "one" {
		t.Fatalf("deleted config = %v", config)
	}
	next, config, err = next.UpdateConfig(ctx, lance.MetadataReplace,
		lance.MetadataUpdate{Key: "final", Value: stringPointer("value")},
	)
	if err != nil {
		t.Fatalf("UpdateConfig replace: %v", err)
	}
	if _, ok := config["first"]; ok || config["final"] != "value" {
		t.Fatalf("replaced config = %v", config)
	}

	schema, err := next.Schema()
	if err != nil {
		t.Fatalf("final Schema: %v", err)
	}
	for _, name := range []string{"extra", "nested"} {
		if _, err := schemaField(schema, name); err == nil {
			t.Fatalf("dropped field %q remains in schema %v", name, schema)
		}
	}
	if field := fieldByName(t, schema, "label"); field.Name != "label" {
		t.Fatalf("renamed label field = %v", field)
	}
	if field := fieldByName(t, schema, "id"); field.Nullable {
		t.Fatalf("id remained nullable after alter")
	}
	if field := fieldByName(t, schema, "small"); field.Type.ID() != arrow.INT64 {
		t.Fatalf("small type = %v, want int64", field.Type)
	}
	if field := fieldByName(t, schema, "double_id"); field.Type.ID() != arrow.INT64 {
		t.Fatalf("double_id type = %v, want int64", field.Type)
	}
	if _, err := schemaField(schema, "label"); err != nil {
		t.Fatalf("label missing: %v", err)
	}

	labels := map[int64]string{1: "a", 2: "b", 3: "c", 4: "d"}
	tags := map[int64]string{1: "tag-1", 2: "tag-2", 3: "tag-3", 4: "tag-4"}
	seen := map[int64]bool{}
	finalScanner, err := next.Scan()
	if err != nil {
		t.Fatalf("final Scan: %v", err)
	}
	defer finalScanner.Close()
	for batch, err := range finalScanner.Records(ctx) {
		if err != nil {
			t.Fatalf("final Records: %v", err)
		}
		ids := collectInt64(t, batch, "id")
		doubled := collectInt64(t, batch, "double_id")
		small := findColumn(t, batch, "small").(*array.Int64)
		label := findColumn(t, batch, "label").(*array.String)
		tag := findColumn(t, batch, "tag").(*array.String)
		for i, id := range ids {
			seen[id] = true
			if doubled[i] != id*2 {
				t.Fatalf("id %d doubled = %d, want %d", id, doubled[i], id*2)
			}
			if !small.IsNull(i) {
				t.Fatalf("small value for id %d = %v, want null", id, small.Value(i))
			}
			if label.IsNull(i) || label.Value(i) != labels[id] {
				t.Fatalf("label for id %d = %v, want %q", id, label.Value(i), labels[id])
			}
			if tag.IsNull(i) || tag.Value(i) != tags[id] {
				t.Fatalf("tag for id %d = %v, want %q", id, tag.Value(i), tags[id])
			}
		}
	}
	if len(seen) != 4 {
		t.Fatalf("saw ids %v, want 1..4", seen)
	}

	if version := mutationVersion(t, next); version <= baseVersion {
		t.Fatalf("final version = %d, want > %d", version, baseVersion)
	}
	if version := mutationVersion(t, dataset); version != baseVersion {
		t.Fatalf("source version = %d, want unchanged %d", version, baseVersion)
	}
	sourceSchema, err := dataset.Schema()
	if err != nil {
		t.Fatalf("source Schema: %v", err)
	}
	if len(sourceSchema.Fields()) != len(baseSchema.Fields()) {
		t.Fatalf("source schema changed: %v", sourceSchema)
	}
	sourceScanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("source Scan after mutations: %v", err)
	}
	defer sourceScanner.Close()
	sourceRows := 0
	for batch, err := range sourceScanner.Records(ctx) {
		if err != nil {
			t.Fatalf("source Records: %v", err)
		}
		sourceRows += int(batch.NumRows())
		if _, err := schemaField(batch.Schema(), "double_id"); err == nil {
			t.Fatalf("source scanner sees new column: %v", batch.Schema())
		}
	}
	if sourceRows != 4 {
		t.Fatalf("source scanner rows = %d, want 4", sourceRows)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open final: %v", err)
	}
	defer reopened.Close()
	reopenedConfig, err := reopened.Config()
	if err != nil {
		t.Fatalf("reopened Config: %v", err)
	}
	if reopenedConfig["final"] != "value" {
		t.Fatalf("persisted config = %v", reopenedConfig)
	}

	invalid := "bad\x00key"
	if _, _, err := next.UpdateConfig(ctx, lance.MetadataMerge, lance.MetadataUpdate{Key: invalid, Value: stringPointer("x")}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("invalid config key error = %v", err)
	}
	if _, err := next.DropColumns(ctx); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("empty DropColumns error = %v", err)
	}
	if _, err := next.AlterColumns(ctx, lance.ColumnAlteration{Path: "id"}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("no-op AlterColumns error = %v", err)
	}
	if _, err := next.AddColumnsStream(ctx, nil); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("nil AddColumnsStream reader error = %v", err)
	}
}

func schemaField(schema *arrow.Schema, name string) (arrow.Field, error) {
	if schema == nil {
		return arrow.Field{}, fmt.Errorf("nil schema")
	}
	for _, field := range schema.Fields() {
		if field.Name == name {
			return field, nil
		}
	}
	return arrow.Field{}, fmt.Errorf("field %q not found", name)
}

type batchUDFRecordingStore struct {
	mu sync.Mutex

	mapperCalls    int
	getBatch       []lance.BatchInfo
	insertBatch    []lance.BatchInfo
	getFragment    []uint32
	insertFragment []uint64
	fragments      []string
	batches        []arrow.RecordBatch
	mapperInputs   []arrow.RecordBatch
}

func (s *batchUDFRecordingStore) GetBatch(info lance.BatchInfo) (arrow.RecordBatch, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getBatch = append(s.getBatch, info)
	return nil, false, nil
}

func (s *batchUDFRecordingStore) InsertBatch(info lance.BatchInfo, batch arrow.RecordBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.insertBatch = append(s.insertBatch, info)
	batch.Retain()
	s.batches = append(s.batches, batch)
	return nil
}

func (s *batchUDFRecordingStore) GetFragment(fragmentID uint32) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getFragment = append(s.getFragment, fragmentID)
	return "", false, nil
}

func (s *batchUDFRecordingStore) InsertFragment(fragmentID uint64, fragmentJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.insertFragment = append(s.insertFragment, fragmentID)
	s.fragments = append(s.fragments, fragmentJSON)
	return nil
}

func (s *batchUDFRecordingStore) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, batch := range s.batches {
		batch.Release()
	}
	for _, batch := range s.mapperInputs {
		batch.Release()
	}
	s.batches = nil
	s.mapperInputs = nil
}

func TestBatchUDFCheckpointCallbacksAndBorrowedInput(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"}, []bool{true, true, true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record), lance.WithWriteParams(lance.WriteParams{MaxRowsPerFile: 2}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	outputMetadata := arrow.NewMetadata([]string{"producer"}, []string{"callback-test"})
	outputSchema := arrow.NewSchema([]arrow.Field{
		{
			Name:     "doubled",
			Type:     arrow.PrimitiveTypes.Int64,
			Nullable: true,
			Metadata: arrow.NewMetadata([]string{"unit"}, []string{"native"}),
		},
	}, &outputMetadata)

	store := &batchUDFRecordingStore{}
	var mapperInputSchema *arrow.Schema
	mapper := func(input arrow.RecordBatch) (arrow.RecordBatch, error) {
		store.mu.Lock()
		store.mapperCalls++
		if mapperInputSchema == nil {
			mapperInputSchema = input.Schema()
		}
		input.Retain()
		store.mapperInputs = append(store.mapperInputs, input)
		store.mu.Unlock()

		// The returned column aliases the borrowed-input copy. Export must
		// retain it before the adapter releases the mapper input.
		return array.NewRecordBatch(outputSchema, []arrow.Array{input.Column(0)}, input.NumRows()), nil
	}

	next, err := dataset.AddColumnsBatchUDF(ctx, lance.BatchUDF{
		Mapper:       mapper,
		OutputSchema: outputSchema,
		ReadColumns:  []string{"id"},
		Store:        store,
	})
	if err != nil {
		t.Fatalf("AddColumnsBatchUDF: %v", err)
	}
	defer next.Close()
	defer store.release()

	store.mu.Lock()
	mapperCalls := store.mapperCalls
	getBatch := append([]lance.BatchInfo(nil), store.getBatch...)
	insertBatch := append([]lance.BatchInfo(nil), store.insertBatch...)
	getFragment := append([]uint32(nil), store.getFragment...)
	insertFragment := append([]uint64(nil), store.insertFragment...)
	fragments := append([]string(nil), store.fragments...)
	batches := append([]arrow.RecordBatch(nil), store.batches...)
	inputs := append([]arrow.RecordBatch(nil), store.mapperInputs...)
	store.mu.Unlock()

	if mapperCalls != 2 {
		t.Fatalf("mapper calls = %d, want 2", mapperCalls)
	}
	if mapperInputSchema == nil || len(mapperInputSchema.Fields()) != 1 || mapperInputSchema.Field(0).Name != "id" {
		t.Fatalf("mapper input schema = %v, want only id", mapperInputSchema)
	}
	if len(getBatch) == 0 || len(insertBatch) == 0 {
		t.Fatalf("batch checkpoints missing: get=%v insert=%v", getBatch, insertBatch)
	}
	if len(getFragment) != 2 || len(insertFragment) != 2 || len(batches) != 2 || len(inputs) != 2 {
		t.Fatalf("callback counts getFragment=%d insertFragment=%d batches=%d inputs=%d, want 2 each", len(getFragment), len(insertFragment), len(batches), len(inputs))
	}
	if len(fragments) != 2 {
		t.Fatalf("fragment JSON count = %d, want 2", len(fragments))
	}
	for i, fragment := range fragments {
		if !jsonValid(fragment) {
			t.Fatalf("fragment %d is not JSON: %q", i, fragment)
		}
	}
	for _, info := range insertBatch {
		found := false
		for _, get := range getBatch {
			if get == info {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("insert batch %+v has no matching get", info)
		}
	}

	schemaAfter, err := next.Schema()
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if field := fieldByName(t, schemaAfter, "doubled"); field.Type.ID() != arrow.INT64 {
		t.Fatalf("doubled type = %v", field.Type)
	}
	scanner, err := next.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()
	seen := map[int64]bool{}
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids := collectInt64(t, batch, "id")
		doubled := collectInt64(t, batch, "doubled")
		for i, id := range ids {
			if doubled[i] != id {
				t.Fatalf("aliased output id %d = %d, want %d", id, doubled[i], id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 4 {
		t.Fatalf("saw ids %v, want 1..4", seen)
	}

	if err := next.Close(); err != nil {
		t.Fatalf("close next: %v", err)
	}
	next = nil

	// Retained mapper inputs and checkpoint batches must remain valid after
	// the dataset and adapter are gone.
	for _, input := range inputs {
		if input.NumRows() != 2 || input.NumCols() != 1 {
			t.Fatalf("retained mapper input shape = %d x %d, want 2 x 1", input.NumRows(), input.NumCols())
		}
	}
	for _, batch := range batches {
		if batch.NumRows() != 2 || batch.NumCols() != 1 {
			t.Fatalf("retained checkpoint shape = %d x %d, want 2 x 1", batch.NumRows(), batch.NumCols())
		}
		if _, ok := batch.Schema().Field(0).Metadata.GetValue("unit"); !ok {
			t.Fatalf("output field metadata missing")
		}
		if value, ok := batch.Schema().Metadata().GetValue("producer"); !ok || value != "callback-test" {
			t.Fatalf("retained batch schema metadata = %v", batch.Schema().Metadata())
		}
	}

	if _, err := dataset.CountRows(ctx); err != nil {
		t.Fatalf("source unusable after UDF: %v", err)
	}
}

func jsonValid(value string) bool {
	return json.Valid([]byte(value))
}

func TestBatchUDFErrorsAndPanicsKeepSourceUsable(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})
	outputSchema := arrow.NewSchema([]arrow.Field{{Name: "derived", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil)

	for _, tc := range []struct {
		operation  string
		panicValue bool
	}{
		{"insert_batch", false},
		{"get_batch", false},
		{"get_fragment", false},
		{"get_batch", true},
		{"get_fragment", true},
	} {
		t.Run(fmt.Sprintf("%s/panic=%t", tc.operation, tc.panicValue), func(t *testing.T) {
			uri := filepath.Join(t.TempDir(), "store-error.lance")
			dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			defer dataset.Close()
			store := &failingBatchUDFStore{operation: tc.operation, panicValue: tc.panicValue}
			_, err = dataset.AddColumnsBatchUDF(ctx, lance.BatchUDF{
				Mapper: func(input arrow.RecordBatch) (arrow.RecordBatch, error) {
					return array.NewRecordBatch(outputSchema, []arrow.Array{input.Column(0)}, input.NumRows()), nil
				},
				OutputSchema: outputSchema,
				Store:        store,
			})
			want := lance.ErrInvalidArgument
			if tc.panicValue {
				want = lance.ErrInternal
			}
			if !errors.Is(err, want) || !strings.Contains(err.Error(), "checkpoint "+tc.operation+" rejected") {
				t.Fatalf("store error = %v, want %v and original checkpoint message", err, want)
			}
			if count, err := dataset.CountRows(ctx); err != nil || count != 2 {
				t.Fatalf("source after checkpoint error: rows=%d, err=%v", count, err)
			}
		})
	}

	t.Run("mapper panic", func(t *testing.T) {
		uri := filepath.Join(t.TempDir(), "panic.lance")
		dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		defer dataset.Close()
		_, err = dataset.AddColumnsBatchUDF(ctx, lance.BatchUDF{
			Mapper: func(arrow.RecordBatch) (arrow.RecordBatch, error) {
				panic("mapper boom")
			},
			OutputSchema: outputSchema,
			Store:        &batchUDFRecordingStore{},
		})
		if !errors.Is(err, lance.ErrInternal) {
			t.Fatalf("mapper panic error = %v, want ErrInternal", err)
		}
		if _, err := dataset.CountRows(ctx); err != nil {
			t.Fatalf("source unusable after panic: %v", err)
		}
	})
}

type failingBatchUDFStore struct {
	operation  string
	panicValue bool
}

func (s failingBatchUDFStore) failure(operation string) error {
	if s.operation != operation {
		return nil
	}
	message := "checkpoint " + operation + " rejected"
	if s.panicValue {
		panic(message)
	}
	return &lance.Error{Code: lance.CodeInvalidArgument, Message: message}
}

func (s failingBatchUDFStore) GetBatch(lance.BatchInfo) (arrow.RecordBatch, bool, error) {
	return nil, false, s.failure("get_batch")
}

func (s failingBatchUDFStore) InsertBatch(lance.BatchInfo, arrow.RecordBatch) error {
	return s.failure("insert_batch")
}

func (s failingBatchUDFStore) GetFragment(uint32) (string, bool, error) {
	return "", false, s.failure("get_fragment")
}

func (failingBatchUDFStore) InsertFragment(uint64, string) error { return nil }

type batchUDFHitStore struct {
	mu             sync.Mutex
	batches        map[lance.BatchInfo]arrow.RecordBatch
	getBatch       int
	hits           []lance.BatchInfo
	insertBatch    []lance.BatchInfo
	getFragment    int
	insertFragment int
}

func (s *batchUDFHitStore) GetBatch(info lance.BatchInfo) (arrow.RecordBatch, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getBatch++
	batch, ok := s.batches[info]
	if !ok {
		return nil, false, nil
	}
	s.hits = append(s.hits, info)
	batch.Retain()
	return batch, true, nil
}

func (s *batchUDFHitStore) InsertBatch(info lance.BatchInfo, batch arrow.RecordBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.insertBatch = append(s.insertBatch, info)
	if _, ok := s.batches[info]; ok {
		return fmt.Errorf("duplicate batch checkpoint %+v", info)
	}
	batch.Retain()
	s.batches[info] = batch
	return nil
}

func (s *batchUDFHitStore) GetFragment(uint32) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getFragment++
	return "", false, nil
}

func (s *batchUDFHitStore) InsertFragment(uint64, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.insertFragment++
	return nil
}

func (s *batchUDFHitStore) resetCounts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getBatch = 0
	s.hits = nil
	s.insertBatch = nil
	s.getFragment = 0
	s.insertFragment = 0
}

func (s *batchUDFHitStore) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, batch := range s.batches {
		batch.Release()
	}
	s.batches = nil
}

func TestBatchUDFCheckpointBatchHitSkipsMapper(t *testing.T) {
	dataset, _, _ := newMutationDataset(t)
	ctx := context.Background()
	baseVersion := mutationVersion(t, dataset)

	outputSchema := arrow.NewSchema([]arrow.Field{
		{Name: "doubled", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)

	makeOutput := func(input arrow.RecordBatch, offset int64) (arrow.RecordBatch, error) {
		if input.NumCols() != 1 {
			return nil, fmt.Errorf("mapper input columns = %d, want 1", input.NumCols())
		}
		ids, ok := input.Column(0).(*array.Int64)
		if !ok {
			return nil, fmt.Errorf("mapper input type = %T, want *array.Int64", input.Column(0))
		}
		builder := array.NewRecordBuilder(memory.DefaultAllocator, outputSchema)
		defer builder.Release()
		values := builder.Field(0).(*array.Int64Builder)
		for i := 0; i < int(input.NumRows()); i++ {
			values.Append(ids.Value(i) + offset)
		}
		return builder.NewRecordBatch(), nil
	}

	const interruption = "interrupt after first cached batch"
	store := &batchUDFHitStore{batches: make(map[lance.BatchInfo]arrow.RecordBatch)}
	defer store.release()
	mapperCalls := 0
	seeding := true
	mapper := func(input arrow.RecordBatch) (arrow.RecordBatch, error) {
		mapperCalls++
		if seeding && mapperCalls == 2 {
			return nil, errors.New(interruption)
		}
		if seeding {
			return makeOutput(input, 100)
		}
		return makeOutput(input, 200)
	}
	batchSize := uint32(2)
	udf := lance.BatchUDF{
		Mapper:       mapper,
		OutputSchema: outputSchema,
		ReadColumns:  []string{"id"},
		BatchSize:    &batchSize,
		Store:        store,
	}

	seeded, err := dataset.AddColumnsBatchUDF(ctx, udf)
	if seeded != nil {
		seeded.Close()
	}
	if err == nil {
		t.Fatal("seeding AddColumnsBatchUDF succeeded, want an interrupted run")
	}
	if !strings.Contains(err.Error(), interruption) {
		t.Fatalf("seeding AddColumnsBatchUDF error = %v, want %q", err, interruption)
	}
	if got := mutationVersion(t, dataset); got != baseVersion {
		t.Fatalf("version after interrupted seeding run = %d, want %d", got, baseVersion)
	}
	if mapperCalls != 2 {
		t.Fatalf("seeding mapper calls = %d, want 2", mapperCalls)
	}

	store.mu.Lock()
	seedGetBatch := store.getBatch
	seedHits := len(store.hits)
	seedInserts := len(store.insertBatch)
	seedFragmentReads := store.getFragment
	seedFragments := store.insertFragment
	seedBatches := len(store.batches)
	var cachedInfo lance.BatchInfo
	for info := range store.batches {
		cachedInfo = info
	}
	store.mu.Unlock()
	if seedGetBatch != 2 || seedHits != 0 {
		t.Fatalf("seeding checkpoint reads = %d, hits = %d, want 2 misses", seedGetBatch, seedHits)
	}
	if seedFragmentReads != 1 {
		t.Fatalf("seeding fragment reads = %d, want 1", seedFragmentReads)
	}
	if seedInserts != 1 || seedFragments != 0 || seedBatches != 1 {
		t.Fatalf("seeding checkpoints = batches %d, fragment writes %d, stored batches %d, want one batch only",
			seedInserts, seedFragments, seedBatches)
	}

	store.resetCounts()
	mapperCalls = 0
	seeding = false
	next, err := dataset.AddColumnsBatchUDF(ctx, udf)
	if err != nil {
		t.Fatalf("AddColumnsBatchUDF retry: %v", err)
	}
	defer next.Close()

	store.mu.Lock()
	retryGetBatch := store.getBatch
	retryHits := append([]lance.BatchInfo(nil), store.hits...)
	retryInserts := append([]lance.BatchInfo(nil), store.insertBatch...)
	retryFragments := store.insertFragment
	retryBatches := len(store.batches)
	store.mu.Unlock()
	if retryGetBatch != 2 {
		t.Fatalf("retry checkpoint reads = %d, want 2", retryGetBatch)
	}
	if len(retryHits) != 1 || retryHits[0] != cachedInfo {
		t.Fatalf("retry checkpoint hits = %v, want [%+v]", retryHits, cachedInfo)
	}
	if mapperCalls != 1 {
		t.Fatalf("retry mapper calls = %d, want 1 after one batch hit", mapperCalls)
	}
	if len(retryInserts) != 1 || retryInserts[0] == cachedInfo {
		t.Fatalf("retry batch writes = %v, want the unchecked batch only", retryInserts)
	}
	if retryFragments != 1 || retryBatches != 2 {
		t.Fatalf("retry checkpoints = fragment writes %d, stored batches %d, want 1 and 2",
			retryFragments, retryBatches)
	}

	scanner, err := next.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()
	var values []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		values = append(values, collectInt64(t, batch, "doubled")...)
	}
	if !equalInt64(values, []int64{101, 102, 203, 204}) {
		t.Fatalf("checkpointed output = %v, want [101 102 203 204]", values)
	}
}
