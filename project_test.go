package lance_test

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
)

// fieldWithID attaches a Lance field ID. Public writes reassign field IDs, so
// these values document native normalization rather than surviving Create.
func fieldWithID(name string, typ arrow.DataType, id string, nullable bool) arrow.Field {
	return arrow.Field{
		Name:     name,
		Type:     typ,
		Nullable: nullable,
		Metadata: arrow.MetadataFrom(map[string]string{"lance:field_id": id}),
	}
}

func projectTestSchema() (*arrow.Schema, *arrow.DictionaryType) {
	nested := arrow.StructOf(
		fieldWithID("child", arrow.PrimitiveTypes.Int64, "12", true),
		fieldWithID("label", arrow.BinaryTypes.String, "13", true),
	)
	dictionary := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}
	schema := arrow.NewSchema([]arrow.Field{
		fieldWithID("id", arrow.PrimitiveTypes.Int64, "2", false),
		fieldWithID("payload", nested, "7", true),
		fieldWithID("tags", dictionary, "11", true),
		fieldWithID("scratch", arrow.PrimitiveTypes.Int64, "20", true),
	}, nil)
	return schema, dictionary
}

func projectTestRecord(t *testing.T, schema *arrow.Schema, dictionary *arrow.DictionaryType) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)

	ids := builder.Field(0).(*array.Int64Builder)
	ids.AppendValues([]int64{1, 2, 3}, nil)

	payload := builder.Field(1).(*array.StructBuilder)
	child := payload.FieldBuilder(0).(*array.Int64Builder)
	label := payload.FieldBuilder(1).(*array.StringBuilder)
	for i, value := range []int64{10, 20, 30} {
		payload.Append(true)
		child.Append(value)
		label.Append([]string{"x", "y", "z"}[i])
	}

	tags := builder.Field(2).(array.DictionaryBuilder)
	values := array.NewStringBuilder(memory.DefaultAllocator)
	t.Cleanup(values.Release)
	values.AppendValues([]string{"red", "blue"}, nil)
	valueArray := values.NewArray()
	t.Cleanup(valueArray.Release)
	indices := array.NewInt32Builder(memory.DefaultAllocator)
	t.Cleanup(indices.Release)
	indices.AppendValues([]int32{0, 1, 0}, nil)
	indexArray := indices.NewArray()
	t.Cleanup(indexArray.Release)
	dictionaryArray := array.NewDictionaryArray(dictionary, indexArray, valueArray)
	t.Cleanup(dictionaryArray.Release)
	if err := tags.AppendArray(dictionaryArray); err != nil {
		t.Fatalf("append dictionary: %v", err)
	}

	scratch := builder.Field(3).(*array.Int64Builder)
	scratch.AppendValues([]int64{100, 200, 300}, nil)

	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return record
}

func TestNativeProjectPreservesFieldIDsAndTypes(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "project.lance")
	schema, dictionary := projectTestSchema()
	record := projectTestRecord(t, schema, dictionary)
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	version, err := dataset.Version()
	if err != nil {
		dataset.Close()
		t.Fatalf("Version: %v", err)
	}

	nativeSchema, err := dataset.NativeSchema()
	if err != nil {
		dataset.Close()
		t.Fatalf("NativeSchema: %v", err)
	}
	fieldIDs := make(map[string]int32, 4)
	for _, column := range []string{"id", "payload", "tags", "scratch"} {
		got, err := nativeSchema.FieldID(column)
		if err != nil {
			nativeSchema.Close()
			dataset.Close()
			t.Fatalf("FieldID(%q): %v", column, err)
		}
		fieldIDs[column] = got
	}
	selectedIDs := []int32{fieldIDs["id"], fieldIDs["payload"], fieldIDs["tags"]}
	sort.Slice(selectedIDs, func(i, j int) bool { return selectedIDs[i] < selectedIDs[j] })
	if selectedIDs[2] == selectedIDs[1]+1 {
		nativeSchema.Close()
		dataset.Close()
		t.Fatalf("selected field IDs = %v, want a nonconsecutive projection", selectedIDs)
	}
	projected, err := nativeSchema.Project([]string{"id", "payload", "tags"})
	if err != nil {
		nativeSchema.Close()
		dataset.Close()
		t.Fatalf("Project: %v", err)
	}
	nativeSchema.Close()

	for column, want := range map[string]int32{"id": fieldIDs["id"], "payload": fieldIDs["payload"], "tags": fieldIDs["tags"]} {
		got, err := projected.FieldID(column)
		if err != nil || got != want {
			projected.Close()
			dataset.Close()
			t.Fatalf("projected FieldID(%q) = %d, %v; want %d", column, got, err, want)
		}
	}
	projectedArrow, err := projected.Arrow()
	if err != nil {
		projected.Close()
		dataset.Close()
		t.Fatalf("projected Arrow: %v", err)
	}
	if got := len(projectedArrow.Fields()); got != 3 {
		projected.Close()
		dataset.Close()
		t.Fatalf("projected field count = %d, want 3", got)
	}
	if _, ok := projectedArrow.Field(2).Type.(*arrow.DictionaryType); !ok {
		projected.Close()
		dataset.Close()
		t.Fatalf("projected tags type = %T, want dictionary", projectedArrow.Field(2).Type)
	}
	payloadType, ok := projectedArrow.Field(1).Type.(*arrow.StructType)
	if !ok || len(payloadType.Fields()) != 2 {
		projected.Close()
		dataset.Close()
		t.Fatalf("projected payload type = %T, want struct with two fields", projectedArrow.Field(1).Type)
	}

	op, err := lance.NewProjectOperation(projected, true)
	if err != nil {
		projected.Close()
		dataset.Close()
		t.Fatalf("NewProjectOperation: %v", err)
	}
	projected.Close()
	tx, err := lance.NewTransaction(version, op)
	if err != nil {
		op.Close()
		dataset.Close()
		t.Fatalf("NewTransaction: %v", err)
	}
	op.Close()
	committed, err := lance.CommitTransaction(ctx, uri, tx)
	if err != nil {
		tx.Close()
		dataset.Close()
		t.Fatalf("Commit project: %v", err)
	}
	tx.Close()
	dataset.Close()

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		committed.Close()
		t.Fatalf("Open projected dataset: %v", err)
	}
	defer reopened.Close()
	if err := committed.Close(); err != nil {
		t.Fatalf("Close projected dataset: %v", err)
	}
	reopenedSchema, err := reopened.NativeSchema()
	if err != nil {
		t.Fatalf("reopened NativeSchema: %v", err)
	}
	defer reopenedSchema.Close()
	reopenedArrow, err := reopenedSchema.Arrow()
	if err != nil {
		t.Fatalf("reopened schema Arrow: %v", err)
	}
	if got := len(reopenedArrow.Fields()); got != 3 {
		t.Fatalf("reopened field count = %d, want 3", got)
	}
	for column, want := range map[string]int32{"id": fieldIDs["id"], "payload": fieldIDs["payload"], "tags": fieldIDs["tags"]} {
		got, err := reopenedSchema.FieldID(column)
		if err != nil || got != want {
			t.Fatalf("reopened FieldID(%q) = %d, %v; want %d", column, got, err, want)
		}
	}
	if ids := scanIDs(t, mustScanColumns(t, reopened, "id")); len(ids) != 3 {
		t.Fatalf("projected ids = %v, want 3 rows", ids)
	}
}

func mustScanColumns(t *testing.T, dataset *lance.Dataset, columns ...string) *lance.Scanner {
	t.Helper()
	scanner, err := dataset.Scan(lance.WithColumns(columns...))
	if err != nil {
		t.Fatalf("Scan columns: %v", err)
	}
	return scanner
}

func TestDataReplacementReadAfterCommit(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "replacement.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record), lance.WithWriteParams(lance.WriteParams{MaxRowsPerFile: 1, MaxRowsPerGroup: 1}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	version, err := dataset.Version()
	if err != nil {
		dataset.Close()
		t.Fatalf("Version: %v", err)
	}
	fragmentIDs, err := dataset.FragmentIDs()
	if err != nil || len(fragmentIDs) == 0 {
		dataset.Close()
		t.Fatalf("FragmentIDs = %v, %v", fragmentIDs, err)
	}
	fragment, err := dataset.OpenFragment(fragmentIDs[0])
	if err != nil {
		dataset.Close()
		t.Fatalf("OpenFragment: %v", err)
	}
	if count, err := fragment.DataFileCount(); err != nil || count == 0 {
		fragment.Close()
		dataset.Close()
		t.Fatalf("DataFileCount = %d, %v", count, err)
	}
	existing, err := fragment.DataFileAt(0)
	if err != nil {
		fragment.Close()
		dataset.Close()
		t.Fatalf("DataFileAt: %v", err)
	}
	path, err := existing.Path()
	if err != nil || path == "" {
		existing.Close()
		fragment.Close()
		dataset.Close()
		t.Fatalf("DataFile.Path = %q, %v", path, err)
	}
	if fields, err := existing.FieldIDs(); err != nil || len(fields) != 2 {
		existing.Close()
		fragment.Close()
		dataset.Close()
		t.Fatalf("DataFile.FieldIDs = %v, %v; want 2 fields", fields, err)
	}
	if columns, err := existing.ColumnIndices(); err != nil || len(columns) != 2 {
		existing.Close()
		fragment.Close()
		dataset.Close()
		t.Fatalf("DataFile.ColumnIndices = %v, %v; want 2", columns, err)
	}
	replacementFile, err := dataset.CreateDataFile(path, nil)
	if err != nil {
		existing.Close()
		fragment.Close()
		dataset.Close()
		t.Fatalf("CreateDataFile: %v", err)
	}
	existing.Close()
	group, err := lance.NewDataReplacementGroup(fragment.ID(), replacementFile)
	if err != nil {
		replacementFile.Close()
		fragment.Close()
		dataset.Close()
		t.Fatalf("NewDataReplacementGroup: %v", err)
	}
	replacementFile.Close()
	fragment.Close()
	op, err := lance.NewDataReplacementOperation(group)
	if err != nil {
		group.Close()
		dataset.Close()
		t.Fatalf("NewDataReplacementOperation: %v", err)
	}
	group.Close()
	if count, err := op.DataReplacementCount(); err != nil || count != 1 {
		op.Close()
		dataset.Close()
		t.Fatalf("DataReplacementCount = %d, %v", count, err)
	}
	inspected, err := op.DataReplacement(0)
	if err != nil {
		op.Close()
		dataset.Close()
		t.Fatalf("DataReplacement: %v", err)
	}
	if got := inspected.FragmentID(); got != fragmentIDs[0] {
		inspected.Close()
		op.Close()
		dataset.Close()
		t.Fatalf("replacement fragment ID = %d, want %d", got, fragmentIDs[0])
	}
	inspectedFile, err := inspected.File()
	if err != nil {
		inspected.Close()
		op.Close()
		dataset.Close()
		t.Fatalf("replacement File: %v", err)
	}
	if got, err := inspectedFile.Path(); err != nil || got != path {
		inspectedFile.Close()
		inspected.Close()
		op.Close()
		dataset.Close()
		t.Fatalf("replacement path = %q, %v; want %q", got, err, path)
	}
	inspectedFile.Close()
	inspected.Close()

	tx, err := lance.NewTransaction(version, op)
	if err != nil {
		op.Close()
		dataset.Close()
		t.Fatalf("NewTransaction: %v", err)
	}
	op.Close()
	committed, err := lance.CommitTransaction(ctx, uri, tx)
	if err != nil {
		tx.Close()
		dataset.Close()
		t.Fatalf("Commit data replacement: %v", err)
	}
	tx.Close()
	dataset.Close()
	if rows, err := committed.CountRows(ctx); err != nil || rows != 2 {
		committed.Close()
		t.Fatalf("rows after replacement = %d, %v; want 2", rows, err)
	}
	if ids := scanIDs(t, mustScan(t, committed)); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		committed.Close()
		t.Fatalf("ids after replacement = %v, want [1 2]", ids)
	}
	committed.Close()
}
