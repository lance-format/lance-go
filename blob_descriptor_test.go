package lance_test

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	lance "github.com/lance-format/lance-go"
)

func TestBlobDescriptorBuilderPreparedArray(t *testing.T) {
	requireNative(t)

	builder, err := lance.NewBlobDescriptorBuilder("blob")
	if err != nil {
		t.Fatalf("NewBlobDescriptorBuilder: %v", err)
	}
	defer builder.Close()

	field, err := builder.Field()
	if err != nil {
		t.Fatalf("Field: %v", err)
	}
	if got, ok := field.Metadata.GetValue(blobExtensionKey); !ok || got != blobExtensionName {
		t.Fatalf("descriptor extension = %q ok=%v, want %q", got, ok, blobExtensionName)
	}
	if !field.Nullable {
		t.Fatal("descriptor field is not nullable")
	}
	structType, ok := field.Type.(*arrow.StructType)
	if !ok {
		t.Fatalf("descriptor field type = %T, want struct", field.Type)
	}
	wantChildren := []string{"kind", "data", "uri", "blob_id", "blob_size", "position"}
	if structType.NumFields() != len(wantChildren) {
		t.Fatalf("descriptor has %d children, want %d", structType.NumFields(), len(wantChildren))
	}
	for i, name := range wantChildren {
		if got := structType.Field(i).Name; got != name {
			t.Fatalf("descriptor child %d = %q, want %q", i, got, name)
		}
	}

	baseZero := uint32(0)
	steps := []struct {
		call func() error
	}{
		{func() error { return builder.AppendNull() }},
		{func() error { return builder.AppendInline([]byte("alpha")) }},
		{func() error { return builder.AppendInline(nil) }},
		{func() error { return builder.AppendPacked(7, 10, 4) }},
		{func() error { return builder.AppendDedicated(8, 12) }},
		{func() error { return builder.AppendManaged("dir/object.bin", 3, 5, nil) }},
		{func() error { return builder.AppendManaged("dir/object.bin", 0, 0, &baseZero) }},
		{func() error { return builder.AppendExternal(4, "s3://bucket/key", 1, 2) }},
		{func() error { return builder.PushExternal("s3://bucket/key", false, 100, 200) }},
		{func() error { return builder.PushExternal("s3://bucket/key", true, 0, 0) }},
		{func() error {
			return builder.ExtendPacked(9, []lance.BlobRange{{Offset: 1, Length: 2}, {Offset: 5, Length: 6}})
		}},
	}
	for i, step := range steps {
		if err := step.call(); err != nil {
			t.Fatalf("descriptor step %d: %v", i, err)
		}
	}
	prepared, finishedField, err := builder.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	defer prepared.Release()
	if !finishedField.Equal(field) {
		t.Fatalf("finished field differs from field():\n got %v\nwant %v", finishedField, field)
	}
	structArray, ok := prepared.(*array.Struct)
	if !ok {
		t.Fatalf("prepared array = %T, want *array.Struct", prepared)
	}
	if structArray.Len() != 12 {
		t.Fatalf("prepared rows = %d, want 12", structArray.Len())
	}

	kind := structArray.Field(0).(*array.Uint8)
	data := structArray.Field(1).(*array.LargeBinary)
	uri := structArray.Field(2).(*array.String)
	blobID := structArray.Field(3).(*array.Uint32)
	blobSize := structArray.Field(4).(*array.Uint64)
	position := structArray.Field(5).(*array.Uint64)

	if !structArray.IsNull(0) {
		t.Fatal("null descriptor row is not null")
	}
	for i := 0; i < 6; i++ {
		if !structArray.Field(i).IsNull(0) {
			t.Fatalf("null descriptor child %d is not null", i)
		}
	}

	checks := []struct {
		row      int
		kind     uint8
		data     string
		dataNull bool
		uri      string
		uriNull  bool
		blobID   uint32
		idNull   bool
		size     uint64
		sizeNull bool
		position uint64
		posNull  bool
	}{
		{1, 0, "alpha", false, "", true, 0, true, 0, true, 0, true},
		{2, 0, "", false, "", true, 0, true, 0, true, 0, true},
		{3, 1, "", true, "", true, 7, false, 4, false, 10, false},
		{4, 2, "", true, "", true, 8, false, 12, false, 0, true},
		{5, 4, "", true, "dir/object.bin", false, 0, true, 5, false, 3, false},
		{6, 5, "", true, "dir/object.bin", false, 0, false, 0, false, 0, false},
		{7, 3, "", true, "s3://bucket/key", false, 4, false, 2, false, 1, false},
		{8, 3, "", true, "s3://bucket/key", false, 0, false, 0, false, 0, false},
		{9, 3, "", true, "s3://bucket/key", false, 0, false, 0, false, 0, false},
		{10, 1, "", true, "", true, 9, false, 2, false, 1, false},
		{11, 1, "", true, "", true, 9, false, 6, false, 5, false},
	}
	for _, check := range checks {
		if got := kind.Value(check.row); got != check.kind {
			t.Fatalf("row %d kind = %d, want %d", check.row, got, check.kind)
		}
		if data.IsNull(check.row) != check.dataNull {
			t.Fatalf("row %d data null = %v, want %v", check.row, data.IsNull(check.row), check.dataNull)
		}
		if !check.dataNull && string(data.Value(check.row)) != check.data {
			t.Fatalf("row %d data = %q, want %q", check.row, data.Value(check.row), check.data)
		}
		if uri.IsNull(check.row) != check.uriNull {
			t.Fatalf("row %d uri null = %v, want %v", check.row, uri.IsNull(check.row), check.uriNull)
		}
		if !check.uriNull && uri.Value(check.row) != check.uri {
			t.Fatalf("row %d uri = %q, want %q", check.row, uri.Value(check.row), check.uri)
		}
		if blobID.IsNull(check.row) != check.idNull {
			t.Fatalf("row %d blob_id null = %v, want %v", check.row, blobID.IsNull(check.row), check.idNull)
		}
		if !check.idNull && blobID.Value(check.row) != check.blobID {
			t.Fatalf("row %d blob_id = %d, want %d", check.row, blobID.Value(check.row), check.blobID)
		}
		if blobSize.IsNull(check.row) != check.sizeNull {
			t.Fatalf("row %d blob_size null = %v, want %v", check.row, blobSize.IsNull(check.row), check.sizeNull)
		}
		if !check.sizeNull && blobSize.Value(check.row) != check.size {
			t.Fatalf("row %d blob_size = %d, want %d", check.row, blobSize.Value(check.row), check.size)
		}
		if position.IsNull(check.row) != check.posNull {
			t.Fatalf("row %d position null = %v, want %v", check.row, position.IsNull(check.row), check.posNull)
		}
		if !check.posNull && position.Value(check.row) != check.position {
			t.Fatalf("row %d position = %d, want %d", check.row, position.Value(check.row), check.position)
		}
	}

	if err := builder.AppendInline([]byte("late")); err == nil {
		t.Fatal("append after Finish was accepted")
	}
	if _, err := builder.Field(); err != nil {
		t.Fatalf("Field after Finish: %v", err)
	}
	if _, _, err := builder.Finish(); err == nil {
		t.Fatal("second Finish was accepted")
	}
}

func TestBlobDescriptorBuilderValidationAndPrefix(t *testing.T) {
	requireNative(t)

	builder, err := lance.NewBlobDescriptorBuilder("blob")
	if err != nil {
		t.Fatalf("NewBlobDescriptorBuilder: %v", err)
	}
	defer builder.Close()

	if err := builder.AppendPacked(0, 0, 1); err == nil {
		t.Fatal("packed blob id 0 was accepted")
	}
	if err := builder.AppendDedicated(0, 1); err == nil {
		t.Fatal("dedicated blob id 0 was accepted")
	}
	if err := builder.AppendPacked(1, math.MaxUint64, 2); err == nil {
		t.Fatal("overflowing packed range was accepted")
	}
	if err := builder.AppendManaged("../escape", 0, 1, nil); err == nil {
		t.Fatal("non-canonical managed URI was accepted")
	}
	if err := builder.AppendExternal(1, "", 0, 1); err == nil {
		t.Fatal("empty external URI was accepted")
	}
	if err := builder.AppendExternal(1, "s3://bucket/key", math.MaxUint64, 2); err == nil {
		t.Fatal("overflowing external range was accepted")
	}

	if err := builder.ExtendPacked(3, []lance.BlobRange{{Offset: 0, Length: 1}, {Offset: math.MaxUint64, Length: 2}}); err == nil {
		t.Fatal("extend with an overflowing tail was accepted")
	}
	prepared, _, err := builder.Finish()
	if err != nil {
		t.Fatalf("Finish after prefix error: %v", err)
	}
	defer prepared.Release()
	structArray := prepared.(*array.Struct)
	if structArray.Len() != 1 {
		t.Fatalf("prepared rows after failed extend = %d, want 1 retained prefix", structArray.Len())
	}
	if got := structArray.Field(0).(*array.Uint8).Value(0); got != 1 {
		t.Fatalf("retained prefix kind = %d, want packed", got)
	}
}

func TestBlobDescriptorPreparedColumnWrites(t *testing.T) {
	requireNative(t)

	builder, err := lance.NewBlobDescriptorBuilder("blob")
	if err != nil {
		t.Fatalf("NewBlobDescriptorBuilder: %v", err)
	}
	defer builder.Close()
	if err := builder.AppendInline([]byte("prepared")); err != nil {
		t.Fatalf("AppendInline: %v", err)
	}
	if err := builder.AppendNull(); err != nil {
		t.Fatalf("AppendNull: %v", err)
	}
	prepared, field, err := builder.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	defer prepared.Release()

	schema := arrow.NewSchema([]arrow.Field{field}, nil)
	batch := array.NewRecordBatch(schema, []arrow.Array{prepared}, int64(prepared.Len()))
	defer batch.Release()
	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	defer reader.Release()

	uri := filepath.Join(t.TempDir(), "data.lance")
	dataset, err := lance.Create(context.Background(), uri, reader)
	if err != nil {
		t.Fatalf("Create prepared blob dataset: %v", err)
	}
	defer dataset.Close()

	files, err := dataset.TakeBlobsByIndices(context.Background(), []uint64{0, 1}, "blob")
	if err != nil {
		t.Fatalf("TakeBlobsByIndices: %v", err)
	}
	defer closeBlobFilesForTest(t, files)
	if files[0] == nil {
		t.Fatal("inline prepared blob is null")
	}
	if files[1] != nil {
		t.Fatal("prepared null blob is not nil")
	}
	got, err := readBlobAll(files[0])
	if err != nil {
		t.Fatalf("read inline prepared blob: %v", err)
	}
	if string(got) != "prepared" {
		t.Fatalf("inline prepared blob = %q, want prepared", got)
	}
}

func TestBlobDescriptorBuilderNilAndClosed(t *testing.T) {
	var nilBuilder *lance.BlobDescriptorBuilder
	if err := nilBuilder.Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}
	if _, err := nilBuilder.Field(); err == nil {
		t.Fatal("nil Field succeeded")
	}
	if err := nilBuilder.AppendInline([]byte("x")); err == nil {
		t.Fatal("nil AppendInline succeeded")
	}
	if _, _, err := nilBuilder.Finish(); err == nil {
		t.Fatal("nil Finish succeeded")
	}

	requireNative(t)
	builder, err := lance.NewBlobDescriptorBuilder("blob")
	if err != nil {
		t.Fatalf("NewBlobDescriptorBuilder: %v", err)
	}
	if err := builder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := builder.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := builder.Field(); err == nil {
		t.Fatal("Field after Close succeeded")
	}
}
