package file_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/file"
)

const rawBlobV2FixturePath = "testdata/v14.0.2/blob_v2_raw_reader/blob_v2_raw.lance"
const rawBlobV2ExtensionName = "lance." + "blob.v2"

type rawBlobV2Descriptor struct {
	Null     bool
	Kind     uint8
	Position uint64
	Size     uint64
	BlobID   uint32
}

type rawBlobV2Rows struct {
	Rows    int
	IDs     []int32
	BlobA   []rawBlobV2Descriptor
	BlobB   []rawBlobV2Descriptor
	Payload []rawBlobV2Descriptor
}

func TestRawBlobV2ReleasedFixtureSchemaAndValues(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	path := filepath.Join(rawBlobV2FixturePath)

	reader, err := file.Open(ctx, path, nil)
	if err != nil {
		t.Fatalf("open released Blob v2 fixture: %v", err)
	}
	defer reader.Close()

	metadata, err := reader.Metadata()
	if err != nil {
		t.Fatalf("reader metadata: %v", err)
	}
	defer metadata.Close()
	info, err := metadata.Info()
	if err != nil {
		t.Fatalf("metadata info: %v", err)
	}
	if info.FormatVersion != file.FormatV2_2 || info.NumRows != 3 {
		t.Fatalf("metadata info = %+v, want format V2_2 and 3 rows", info)
	}

	readerSchema, err := reader.Schema()
	if err != nil {
		t.Fatalf("reader schema: %v", err)
	}
	metadataSchema, err := metadata.Schema()
	if err != nil {
		t.Fatalf("metadata schema: %v", err)
	}
	assertRawBlobV2Schema(t, readerSchema)
	assertRawBlobV2Schema(t, metadataSchema)

	rows := collectRawBlobV2Rows(t, mustReadAll(t, reader))
	if rows.Rows != 3 {
		t.Fatalf("default read rows = %d, want 3", rows.Rows)
	}
	if want := []int32{10, 20, 30}; !reflect.DeepEqual(rows.IDs, want) {
		t.Fatalf("default read ids = %v, want %v", rows.IDs, want)
	}
	wantBlobA := []rawBlobV2Descriptor{
		{Kind: 0, Position: 0, Size: 5, BlobID: 0},
		{Null: true},
		{Kind: 0, Position: 64, Size: 0, BlobID: 0},
	}
	wantBlobB := []rawBlobV2Descriptor{
		{Kind: 0, Position: 64, Size: 3, BlobID: 0},
		{Kind: 0, Position: 128, Size: 5, BlobID: 0},
		{Null: true},
	}
	wantPayload := []rawBlobV2Descriptor{
		{Kind: 0, Position: 192, Size: 8, BlobID: 0},
		{Null: true},
		{Kind: 0, Position: 256, Size: 8, BlobID: 0},
	}
	if !reflect.DeepEqual(rows.BlobA, wantBlobA) {
		t.Fatalf("blob_a descriptors = %+v, want %+v", rows.BlobA, wantBlobA)
	}
	if !reflect.DeepEqual(rows.BlobB, wantBlobB) {
		t.Fatalf("blob_b descriptors = %+v, want %+v", rows.BlobB, wantBlobB)
	}
	if !reflect.DeepEqual(rows.Payload, wantPayload) {
		t.Fatalf("info.payload descriptors = %+v, want %+v", rows.Payload, wantPayload)
	}

	projected, err := file.Open(ctx, path, &file.ReaderOptions{
		Columns: []string{"blob_b", "info.payload"},
	})
	if err != nil {
		t.Fatalf("open blob_b/info.payload projection: %v", err)
	}
	projectedStream, err := projected.ReadAll(ctx, file.ReadOptions{})
	if err != nil {
		_ = projected.Close()
		t.Fatalf("read blob_b/info.payload projection: %v", err)
	}
	if names := rawBlobV2FieldNames(projectedStream.Schema()); !reflect.DeepEqual(names, []string{"blob_b", "info"}) {
		t.Fatalf("blob_b/info.payload projection schema = %v", names)
	}
	projectedRows := collectRawBlobV2Rows(t, projectedStream)
	if projectedRows.Rows != 3 {
		t.Fatalf("blob_b/info.payload projection rows = %d, want 3", projectedRows.Rows)
	}
	if !reflect.DeepEqual(projectedRows.BlobB, wantBlobB) {
		t.Fatalf("projected blob_b descriptors = %+v, want %+v", projectedRows.BlobB, wantBlobB)
	}
	if !reflect.DeepEqual(projectedRows.Payload, wantPayload) {
		t.Fatalf("projected info.payload descriptors = %+v, want %+v", projectedRows.Payload, wantPayload)
	}
	if err := projected.Close(); err != nil {
		t.Fatalf("close blob_b/info.payload projection reader: %v", err)
	}

	projected, err = file.Open(ctx, path, &file.ReaderOptions{
		Columns: []string{"id", "blob_a"},
	})
	if err != nil {
		t.Fatalf("open id/blob_a projection: %v", err)
	}
	projectedStream, err = projected.ReadAll(ctx, file.ReadOptions{})
	if err != nil {
		_ = projected.Close()
		t.Fatalf("read id/blob_a projection: %v", err)
	}
	if names := rawBlobV2FieldNames(projectedStream.Schema()); !reflect.DeepEqual(names, []string{"id", "blob_a"}) {
		t.Fatalf("id/blob_a projection schema = %v", names)
	}
	projectedRows = collectRawBlobV2Rows(t, projectedStream)
	if projectedRows.Rows != 3 {
		t.Fatalf("id/blob_a projection rows = %d, want 3", projectedRows.Rows)
	}
	if want := []int32{10, 20, 30}; !reflect.DeepEqual(projectedRows.IDs, want) {
		t.Fatalf("projected ids = %v, want %v", projectedRows.IDs, want)
	}
	if !reflect.DeepEqual(projectedRows.BlobA, wantBlobA) {
		t.Fatalf("projected blob_a descriptors = %+v, want %+v", projectedRows.BlobA, wantBlobA)
	}
	if err := projected.Close(); err != nil {
		t.Fatalf("close id/blob_a projection reader: %v", err)
	}
}

func TestRawBlobV2StreamOutlivesReader(t *testing.T) {
	requireNative(t)
	ctx := context.Background()

	reader, err := file.Open(ctx, filepath.Join(rawBlobV2FixturePath), &file.ReaderOptions{
		Columns: []string{"id"},
	})
	if err != nil {
		t.Fatalf("open released Blob v2 fixture: %v", err)
	}
	stream, err := reader.ReadAll(ctx, file.ReadOptions{})
	if err != nil {
		_ = reader.Close()
		t.Fatalf("read released Blob v2 fixture: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader with retained stream: %v", err)
	}

	if names := rawBlobV2FieldNames(stream.Schema()); !reflect.DeepEqual(names, []string{"id"}) {
		t.Fatalf("retained stream schema = %v", names)
	}
	rows := collectRawBlobV2Rows(t, stream)
	if rows.Rows != 3 {
		t.Fatalf("retained stream rows = %d, want 3", rows.Rows)
	}
	if want := []int32{10, 20, 30}; !reflect.DeepEqual(rows.IDs, want) {
		t.Fatalf("retained stream ids = %v, want %v", rows.IDs, want)
	}
}

func assertRawBlobV2Schema(t *testing.T, schema *arrow.Schema) {
	t.Helper()
	if got, want := rawBlobV2FieldNames(schema), []string{"id", "blob_a", "blob_b", "info"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Blob v2 fixture fields = %v, want %v", got, want)
	}
	id := schema.Field(0)
	if id.Type.ID() != arrow.INT32 || id.Nullable {
		t.Fatalf("id field = %v, want non-null Int32", id)
	}
	assertRawBlobV2DescriptorField(t, schema.Field(1))
	assertRawBlobV2DescriptorField(t, schema.Field(2))

	info := schema.Field(3)
	if !info.Nullable {
		t.Fatal("info field is non-nullable, want nullable")
	}
	infoType, ok := info.Type.(*arrow.StructType)
	if !ok || infoType.NumFields() != 1 {
		t.Fatalf("info type = %T with fields %v, want one-field struct", info.Type, info.Type)
	}
	payload := infoType.Field(0)
	if payload.Name != "payload" || !payload.Nullable {
		t.Fatalf("nested payload field = %q nullable=%v, want payload nullable", payload.Name, payload.Nullable)
	}
	assertRawBlobV2DescriptorField(t, payload)
}

func assertRawBlobV2DescriptorField(t *testing.T, field arrow.Field) {
	t.Helper()
	if !field.Nullable {
		t.Fatalf("%s field is non-nullable, want nullable", field.Name)
	}
	if got, ok := field.Metadata.GetValue("ARROW:extension:name"); !ok || got != rawBlobV2ExtensionName {
		t.Fatalf("%s extension metadata = %q ok=%v, want %q", field.Name, got, ok, rawBlobV2ExtensionName)
	}
	if got, ok := field.Metadata.GetValue("lance-encoding:blob"); !ok || got != "true" {
		t.Fatalf("%s Blob metadata = %q ok=%v, want true", field.Name, got, ok)
	}
	if got, ok := field.Metadata.GetValue("lance-encoding:packed"); !ok || got != "true" {
		t.Fatalf("%s packed metadata = %q ok=%v, want true", field.Name, got, ok)
	}
	structType, ok := field.Type.(*arrow.StructType)
	if !ok {
		t.Fatalf("%s type = %T, want struct", field.Name, field.Type)
	}
	wantChildren := []struct {
		name string
		typ  arrow.Type
	}{
		{"kind", arrow.UINT8},
		{"position", arrow.UINT64},
		{"size", arrow.UINT64},
		{"blob_id", arrow.UINT32},
		{"blob_uri", arrow.STRING},
	}
	if structType.NumFields() != len(wantChildren) {
		t.Fatalf("%s has %d descriptor children, want %d", field.Name, structType.NumFields(), len(wantChildren))
	}
	for i, want := range wantChildren {
		child := structType.Field(i)
		if child.Name != want.name || child.Type.ID() != want.typ {
			t.Fatalf("%s descriptor child %d = %q/%v, want %q/%v", field.Name, i, child.Name, child.Type, want.name, want.typ)
		}
	}
}

func rawBlobV2FieldNames(schema *arrow.Schema) []string {
	if schema == nil {
		return nil
	}
	fields := schema.Fields()
	names := make([]string, len(fields))
	for i, field := range fields {
		names[i] = field.Name
	}
	return names
}

func collectRawBlobV2Rows(t *testing.T, rr array.RecordReader) rawBlobV2Rows {
	t.Helper()
	var rows rawBlobV2Rows
	drain(t, rr, func(batch arrow.RecordBatch) {
		rows.Rows += int(batch.NumRows())
		for i := 0; i < int(batch.NumCols()); i++ {
			field := batch.Schema().Field(i)
			switch field.Name {
			case "id":
				ids, ok := batch.Column(i).(*array.Int32)
				if !ok {
					t.Fatalf("id column type = %T, want *array.Int32", batch.Column(i))
				}
				for row := 0; row < ids.Len(); row++ {
					if ids.IsNull(row) {
						t.Fatalf("id row %d is null", row)
					}
					rows.IDs = append(rows.IDs, ids.Value(row))
				}
			case "blob_a":
				rows.BlobA = append(rows.BlobA, readRawBlobV2Descriptors(t, batch.Column(i))...)
			case "blob_b":
				rows.BlobB = append(rows.BlobB, readRawBlobV2Descriptors(t, batch.Column(i))...)
			case "info":
				info, ok := batch.Column(i).(*array.Struct)
				if !ok {
					t.Fatalf("info column type = %T, want *array.Struct", batch.Column(i))
				}
				infoType, ok := field.Type.(*arrow.StructType)
				if !ok || infoType.NumFields() != 1 || infoType.Field(0).Name != "payload" {
					t.Fatalf("info type = %v, want payload-only struct", field.Type)
				}
				rows.Payload = append(rows.Payload, readRawBlobV2Descriptors(t, info.Field(0))...)
			}
		}
	})
	return rows
}

func readRawBlobV2Descriptors(t *testing.T, column arrow.Array) []rawBlobV2Descriptor {
	t.Helper()
	descriptor, ok := column.(*array.Struct)
	if !ok {
		t.Fatalf("Blob descriptor type = %T, want *array.Struct", column)
	}
	kind, ok := descriptor.Field(0).(*array.Uint8)
	if !ok {
		t.Fatalf("descriptor kind type = %T, want *array.Uint8", descriptor.Field(0))
	}
	position, ok := descriptor.Field(1).(*array.Uint64)
	if !ok {
		t.Fatalf("descriptor position type = %T, want *array.Uint64", descriptor.Field(1))
	}
	size, ok := descriptor.Field(2).(*array.Uint64)
	if !ok {
		t.Fatalf("descriptor size type = %T, want *array.Uint64", descriptor.Field(2))
	}
	blobID, ok := descriptor.Field(3).(*array.Uint32)
	if !ok {
		t.Fatalf("descriptor blob_id type = %T, want *array.Uint32", descriptor.Field(3))
	}
	if _, ok := descriptor.Field(4).(*array.String); !ok {
		t.Fatalf("descriptor blob_uri type = %T, want *array.String", descriptor.Field(4))
	}

	values := make([]rawBlobV2Descriptor, descriptor.Len())
	for row := 0; row < descriptor.Len(); row++ {
		if descriptor.IsNull(row) {
			values[row].Null = true
			continue
		}
		values[row] = rawBlobV2Descriptor{
			Kind:     kind.Value(row),
			Position: position.Value(row),
			Size:     size.Value(row),
			BlobID:   blobID.Value(row),
		}
	}
	return values
}
