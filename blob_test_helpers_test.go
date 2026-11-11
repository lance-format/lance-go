package lance_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
)

type blobTestValue struct {
	Data []byte
	Null bool
}

type blobTestRefs struct {
	RowID uint64
	Addr  uint64
}

func newBlobDataset(t *testing.T, values []blobTestValue) (*lance.Dataset, *arrow.Schema) {
	t.Helper()
	requireNative(t)

	field, err := lance.BlobFieldWithOptions("blob", true, lance.BlobFieldOptions{
		InlineSizeThreshold: ptr(uint64(1 << 20)),
	})
	if err != nil {
		t.Fatalf("BlobFieldWithOptions: %v", err)
	}
	schema := blobTestSchema(field)
	uri := filepath.Join(t.TempDir(), "data.lance")

	first := values
	if len(first) > 3 {
		first = first[:3]
	}
	current := createBlobFragment(t, uri, schema, 0, first)
	if len(values) > 3 {
		tail := append([]blobTestValue{}, values[3:]...)
		tail = append(tail, blobTestValue{Data: []byte("deleted")})
		appended := appendBlobFragment(t, uri, schema, 3, tail)
		if err := current.Close(); err != nil {
			_ = appended.Close()
			t.Fatalf("close pre-append dataset: %v", err)
		}
		next, deleted, err := appended.Delete(context.Background(), fmt.Sprintf("id = %d", len(values)))
		if err != nil {
			_ = appended.Close()
			t.Fatalf("delete fixture row: %v", err)
		}
		if err := appended.Close(); err != nil {
			_ = next.Close()
			t.Fatalf("close appended dataset: %v", err)
		}
		if deleted != 1 {
			_ = next.Close()
			t.Fatalf("deleted %d fixture rows, want 1", deleted)
		}
		current = next
	}
	t.Cleanup(func() { _ = current.Close() })
	return current, schema
}

func newPackedBlobDataset(t *testing.T, values []blobTestValue) (*lance.Dataset, string) {
	t.Helper()
	requireNative(t)

	field, err := lance.BlobFieldWithOptions("blob", true, lance.BlobFieldOptions{
		InlineSizeThreshold: ptr(uint64(0)),
	})
	if err != nil {
		t.Fatalf("BlobFieldWithOptions: %v", err)
	}
	schema := blobTestSchema(field)
	uri := filepath.Join(t.TempDir(), "data.lance")
	dataset := createBlobFragment(t, uri, schema, 0, values)
	t.Cleanup(func() { _ = dataset.Close() })
	return dataset, uri
}

func blobTestSchema(field arrow.Field) *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		field,
	}, nil)
}

func createBlobFragment(t *testing.T, uri string, schema *arrow.Schema, firstID int, values []blobTestValue) *lance.Dataset {
	t.Helper()
	record := makeBlobRecord(t, schema, firstID, values)
	dataset, err := lance.Create(context.Background(), uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create blob dataset: %v", err)
	}
	return dataset
}

func appendBlobFragment(t *testing.T, uri string, schema *arrow.Schema, firstID int, values []blobTestValue) *lance.Dataset {
	t.Helper()
	record := makeBlobRecord(t, schema, firstID, values)
	dataset, err := lance.Append(context.Background(), uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Append blob fragment: %v", err)
	}
	return dataset
}

func makeBlobRecord(t *testing.T, schema *arrow.Schema, firstID int, values []blobTestValue) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)

	ids := builder.Field(0).(*array.Int64Builder)
	blobs := builder.Field(1).(*array.StructBuilder)
	data := blobs.FieldBuilder(0).(*array.BinaryBuilder)
	uri := blobs.FieldBuilder(1).(*array.StringBuilder)
	for i, value := range values {
		ids.Append(int64(firstID + i))
		blobs.Append(!value.Null)
		if value.Null {
			data.AppendNull()
			uri.AppendNull()
			continue
		}
		data.Append(value.Data)
		uri.AppendNull()
	}
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return record
}

func blobRefs(t *testing.T, dataset *lance.Dataset) []blobTestRefs {
	t.Helper()
	scanner, err := dataset.Scan(
		lance.WithColumns("id"),
		lance.WithRowID(true),
		lance.WithRowAddress(true),
	)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()

	byID := make(map[uint64]blobTestRefs)
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids := collectInt64(t, batch, "id")
		rowIDs := collectUint64(t, batch, "_rowid")
		addrs := collectUint64(t, batch, "_rowaddr")
		for i, id := range ids {
			byID[uint64(id)] = blobTestRefs{RowID: rowIDs[i], Addr: addrs[i]}
		}
	}
	refs := make([]blobTestRefs, len(byID))
	for id := range refs {
		ref, ok := byID[uint64(id)]
		if !ok {
			t.Fatalf("missing row id %d in scan", id)
		}
		refs[id] = ref
	}
	return refs
}

func collectUint64(t *testing.T, batch arrow.RecordBatch, field string) []uint64 {
	t.Helper()
	column, ok := findColumn(t, batch, field).(*array.Uint64)
	if !ok {
		t.Fatalf("column %q has type %T, want *array.Uint64", field, findColumn(t, batch, field))
	}
	values := make([]uint64, column.Len())
	for i := range values {
		values[i] = column.Value(i)
	}
	return values
}
