package ffi

import (
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go/internal/cdata"
)

func newTrackedReader(t *testing.T) *ffiReleaseCountingReader {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1}, []bool{true})
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	reader, err := array.NewRecordReader(schema, []arrow.Record{record})
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	tracked := &ffiReleaseCountingReader{RecordReader: reader}
	t.Cleanup(func() {
		if tracked.releases == 0 {
			reader.Release()
		}
	})
	return tracked
}

type ffiReleaseCountingReader struct {
	array.RecordReader
	releases int
}

func (r *ffiReleaseCountingReader) Release() {
	r.releases++
	r.RecordReader.Release()
}

func TestMutationMethodsRejectClosedDataset(t *testing.T) {
	dataset := &Dataset{}
	if _, err := dataset.Delete("id > 0"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Delete error = %v, want ErrClosed", err)
	}
	if _, err := dataset.Update("", []ColumnUpdate{{Column: "id", Value: "1"}}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Update error = %v, want ErrClosed", err)
	}
	if _, err := dataset.MergeInsert([]string{"id"}, nil, MergeInsertParams{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("MergeInsert error = %v, want ErrClosed", err)
	}
	if err := dataset.Merge("id", "id", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Merge error = %v, want ErrClosed", err)
	}
	if err := dataset.Truncate(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Truncate error = %v, want ErrClosed", err)
	}
}

func TestMutationArgumentValidation(t *testing.T) {
	dataset := &Dataset{b: &bindings{}}
	dataset.handle.Store(1)
	source := newTrackedReader(t)
	validUpdate := []ColumnUpdate{{Column: "id", Value: "1"}}

	checks := []struct {
		name string
		call func() error
	}{
		{"delete empty predicate", func() error {
			_, err := dataset.Delete("")
			return err
		}},
		{"delete NUL", func() error {
			_, err := dataset.Delete("id\x00")
			return err
		}},
		{"update no columns", func() error {
			_, err := dataset.Update("", nil)
			return err
		}},
		{"update empty column", func() error {
			_, err := dataset.Update("", []ColumnUpdate{{Value: "1"}})
			return err
		}},
		{"update empty value", func() error {
			_, err := dataset.Update("", []ColumnUpdate{{Column: "id"}})
			return err
		}},
		{"update NUL predicate", func() error {
			_, err := dataset.Update("id\x00", validUpdate)
			return err
		}},
		{"merge insert nil reader", func() error {
			_, err := dataset.MergeInsert([]string{"id"}, nil, MergeInsertParams{})
			return err
		}},
		{"merge insert no keys", func() error {
			_, err := dataset.MergeInsert(nil, source, MergeInsertParams{})
			return err
		}},
		{"merge insert empty key", func() error {
			_, err := dataset.MergeInsert([]string{""}, source, MergeInsertParams{})
			return err
		}},
		{"merge insert NUL key", func() error {
			_, err := dataset.MergeInsert([]string{"id\x00"}, source, MergeInsertParams{})
			return err
		}},
		{"merge nil reader", func() error {
			return dataset.Merge("id", "id", nil)
		}},
		{"merge NUL left key", func() error {
			return dataset.Merge("id\x00", "id", source)
		}},
		{"merge NUL right key", func() error {
			return dataset.Merge("id", "id\x00", source)
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); err == nil {
				t.Fatal("call succeeded, want validation error")
			}
		})
	}
	if source.releases != 0 {
		t.Fatalf("validation released source %d times, want 0", source.releases)
	}
}

func TestDeleteCallsNativeAndReturnsCount(t *testing.T) {
	b := &bindings{}
	dataset := &Dataset{b: b}
	dataset.handle.Store(7)
	b.datasetDelete = func(handle uintptr, predicate string, out *uint64) int32 {
		if handle != 7 || predicate != "id > 2" {
			t.Fatalf("delete call = handle %d predicate %q", handle, predicate)
		}
		*out = 2
		return int32(CodeOK)
	}

	deleted, err := dataset.Delete("id > 2")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}
}

func TestUpdateCallsNativeAndReturnsCount(t *testing.T) {
	b := &bindings{}
	dataset := &Dataset{b: b}
	dataset.handle.Store(7)
	b.datasetUpdate = func(handle uintptr, predicate uintptr, columns []uintptr, values []uintptr, numUpdates uint64, out *uint64) int32 {
		if handle != 7 || predicate == 0 || numUpdates != 2 || len(columns) < 2 || len(values) < 2 {
			t.Fatalf("update call = handle %d predicate %d updates %d", handle, predicate, numUpdates)
		}
		*out = 2
		return int32(CodeOK)
	}

	updated, err := dataset.Update("id > 1", []ColumnUpdate{
		{Column: "name", Value: "'x'"},
		{Column: "id", Value: "id + 1"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated != 2 {
		t.Fatalf("updated = %d, want 2", updated)
	}
}

func TestMergeInsertConsumesStreamAndMapsResult(t *testing.T) {
	b := &bindings{}
	dataset := &Dataset{b: b}
	dataset.handle.Store(7)
	source := newTrackedReader(t)
	b.datasetMergeInsert = func(handle uintptr, onColumns []uintptr, numOnColumns uint64, stream *cdata.ArrowArrayStream, params *mergeInsertParams, out *mergeInsertResult) int32 {
		if handle != 7 || numOnColumns != 1 || len(onColumns) < 1 || onColumns[0] == 0 {
			t.Fatalf("merge insert call = handle %d keys %d", handle, numOnColumns)
		}
		if params.WhenMatched != 2 || params.WhenMatchedExpr == nil {
			t.Fatalf("merge insert params = %+v", params)
		}
		if got := goString(params.WhenMatchedExpr); got != "target.id > 0" {
			t.Fatalf("when matched expression = %q", got)
		}
		if params.WhenNotMatched != 1 {
			t.Fatalf("when not matched = %d, want 1", params.WhenNotMatched)
		}
		cdata.ReleaseStream(stream)
		*out = mergeInsertResult{NumInsertedRows: 3, NumUpdatedRows: 4, NumDeletedRows: 5}
		return int32(CodeOK)
	}

	result, err := dataset.MergeInsert([]string{"id"}, source, MergeInsertParams{
		WhenMatched:           2,
		WhenMatchedExpression: "target.id > 0",
		WhenNotMatched:        1,
	})
	if err != nil {
		t.Fatalf("MergeInsert: %v", err)
	}
	if result != (MergeInsertResult{InsertedRows: 3, UpdatedRows: 4, DeletedRows: 5}) {
		t.Fatalf("result = %+v", result)
	}
	if source.releases != 1 {
		t.Fatalf("stream released reader %d times, want 1", source.releases)
	}
}

func TestMergeConsumesStream(t *testing.T) {
	b := &bindings{}
	dataset := &Dataset{b: b}
	dataset.handle.Store(7)
	source := newTrackedReader(t)
	b.datasetMerge = func(handle uintptr, leftOn, rightOn string, stream *cdata.ArrowArrayStream) int32 {
		if handle != 7 || leftOn != "id" || rightOn != "id" {
			t.Fatalf("merge call = handle %d left %q right %q", handle, leftOn, rightOn)
		}
		cdata.ReleaseStream(stream)
		return int32(CodeOK)
	}

	if err := dataset.Merge("id", "id", source); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if source.releases != 1 {
		t.Fatalf("stream released reader %d times, want 1", source.releases)
	}
}

func TestMergeInsertNativeErrorDoesNotDoubleRelease(t *testing.T) {
	b := &bindings{}
	dataset := &Dataset{b: b}
	dataset.handle.Store(7)
	source := newTrackedReader(t)
	b.datasetMergeInsert = func(_ uintptr, _ []uintptr, _ uint64, stream *cdata.ArrowArrayStream, _ *mergeInsertParams, _ *mergeInsertResult) int32 {
		cdata.ReleaseStream(stream)
		return int32(CodeInvalidArgument)
	}
	b.lastErrorCode = func() int32 { return int32(CodeInvalidArgument) }
	b.lastErrorMessage = func() *byte { return nil }

	_, err := dataset.MergeInsert([]string{"id"}, source, MergeInsertParams{})
	if err == nil {
		t.Fatal("MergeInsert succeeded, want native error")
	}
	var nativeErr *Error
	if !errors.As(err, &nativeErr) || nativeErr.Code != CodeInvalidArgument {
		t.Fatalf("MergeInsert error = %v, want native invalid argument", err)
	}
	if source.releases != 1 {
		t.Fatalf("stream released reader %d times, want 1", source.releases)
	}
}

func TestTruncateCallsNative(t *testing.T) {
	b := &bindings{}
	dataset := &Dataset{b: b}
	dataset.handle.Store(7)
	called := false
	b.datasetTruncateTable = func(handle uintptr) int32 {
		called = true
		if handle != 7 {
			t.Fatalf("truncate handle = %d, want 7", handle)
		}
		return int32(CodeOK)
	}

	if err := dataset.Truncate(); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	if !called {
		t.Fatal("Truncate did not call native")
	}
}
