package lance_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"

	lance "github.com/lance-format/lance-go"
)

func TestWriteModesAndParams(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()

	first := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, first), lance.WithWriteParams(lance.WriteParams{MaxRowsPerFile: 1, MaxRowsPerGroup: 1}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("Close create dataset: %v", err)
	}

	second := makeRecord(t, schema, []int64{3}, []string{"c"}, []bool{true})
	dataset, err = lance.Append(ctx, uri, makeReader(t, schema, second))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("Close append dataset: %v", err)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after append: %v", err)
	}
	rows, err := reopened.CountRows(ctx)
	if err != nil {
		t.Fatalf("CountRows after append: %v", err)
	}
	if rows != 3 {
		t.Fatalf("rows after append = %d, want 3", rows)
	}
	reopened.Close()

	replacement := makeRecord(t, schema, []int64{9}, []string{"z"}, []bool{true})
	dataset, err = lance.Overwrite(ctx, uri, makeReader(t, schema, replacement))
	if err != nil {
		t.Fatalf("Overwrite: %v", err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("Close overwrite dataset: %v", err)
	}
	reopened, err = lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after overwrite: %v", err)
	}
	defer reopened.Close()
	rows, err = reopened.CountRows(ctx)
	if err != nil {
		t.Fatalf("CountRows after overwrite: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows after overwrite = %d, want 1", rows)
	}
	latest, err := reopened.LatestVersion(ctx)
	if err != nil {
		t.Fatalf("LatestVersion after overwrite: %v", err)
	}
	if latest != 3 {
		t.Fatalf("LatestVersion after overwrite = %d, want 3", latest)
	}

	pinned, err := lance.Open(ctx, uri, lance.WithVersion(2))
	if err != nil {
		t.Fatalf("Open version 2: %v", err)
	}
	pinnedRows, err := pinned.CountRows(ctx)
	if err != nil {
		pinned.Close()
		t.Fatalf("version 2 CountRows: %v", err)
	}
	if pinnedRows != 3 {
		pinned.Close()
		t.Fatalf("version 2 rows = %d, want 3", pinnedRows)
	}
	if version, err := pinned.Version(); err != nil || version != 2 {
		pinned.Close()
		t.Fatalf("pinned version = %d, %v; want 2, nil", version, err)
	}
	pinned.Close()

	duplicate := makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})
	if _, err := lance.Create(ctx, uri, makeReader(t, schema, duplicate)); !errors.Is(err, lance.ErrAlreadyExists) {
		t.Fatalf("Create over existing dataset error = %v, want ErrAlreadyExists", err)
	}

	missing := filepath.Join(t.TempDir(), "missing.lance")
	created, err := lance.Append(ctx, missing, makeReader(t, schema, duplicate))
	if err != nil {
		t.Fatalf("Append to missing dataset: %v", err)
	}
	created.Close()
	created, err = lance.Open(ctx, missing)
	if err != nil {
		t.Fatalf("reopen dataset created by Append: %v", err)
	}
	defer created.Close()
	if rows, err := created.CountRows(ctx); err != nil || rows != 1 {
		t.Fatalf("Append-created rows = %d, %v; want 1, nil", rows, err)
	}
	if version, err := created.Version(); err != nil || version != 1 {
		t.Fatalf("Append-created version = %d, %v; want 1, nil", version, err)
	}
}

func TestWriteEmptyDatasetAndSlicedNullableBatch(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	schema := testSchema()

	emptyURI := filepath.Join(t.TempDir(), "empty.lance")
	emptyReader, err := array.NewRecordReader(schema, nil)
	if err != nil {
		t.Fatalf("empty reader: %v", err)
	}
	t.Cleanup(emptyReader.Release)
	emptyDataset, err := lance.Create(ctx, emptyURI, emptyReader)
	if err != nil {
		t.Fatalf("Create empty: %v", err)
	}
	rows, err := emptyDataset.CountRows(ctx)
	if err != nil {
		t.Fatalf("empty CountRows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("empty CountRows = %d, want 0", rows)
	}
	scanner, err := emptyDataset.Scan()
	if err != nil {
		t.Fatalf("empty Scan: %v", err)
	}
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("empty Records: %v", err)
		}
		if batch.NumRows() != 0 {
			t.Fatalf("empty scan yielded %d rows", batch.NumRows())
		}
	}
	emptyDataset.Close()

	uri := filepath.Join(t.TempDir(), "sliced.lance")
	record := makeRecord(t, schema, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"}, []bool{true, true, false, true})
	sliced := record.NewSlice(1, 4)
	t.Cleanup(sliced.Release)
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, sliced))
	if err != nil {
		t.Fatalf("Create sliced: %v", err)
	}
	defer dataset.Close()
	rows, err = dataset.CountRows(ctx)
	if err != nil {
		t.Fatalf("sliced CountRows: %v", err)
	}
	if rows != 3 {
		t.Fatalf("sliced CountRows = %d, want 3", rows)
	}
	scanner, err = dataset.Scan(lance.WithColumns("id", "name"))
	if err != nil {
		t.Fatalf("sliced Scan: %v", err)
	}
	var ids []int64
	var nulls int
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("sliced Records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
		names := findColumn(t, batch, "name")
		for i := 0; i < names.Len(); i++ {
			if names.IsNull(i) {
				nulls++
			}
		}
	}
	if want := []int64{2, 3, 4}; !equalInt64(ids, want) {
		t.Fatalf("sliced ids = %v, want %v", ids, want)
	}
	if nulls != 1 {
		t.Fatalf("sliced null count = %d, want 1", nulls)
	}
}

func TestWriteOptionValidationWithoutNative(t *testing.T) {
	schema := testSchema()
	empty, err := array.NewRecordReader(schema, nil)
	if err != nil {
		t.Fatalf("empty reader: %v", err)
	}
	defer empty.Release()

	if _, err := lance.Write(context.Background(), "/tmp/x", empty, lance.WithWriteMode(lance.WriteMode(99))); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidArgument", err)
	}
	if _, err := lance.Write(context.Background(), "/tmp/x", empty, lance.WithWriteParams(lance.WriteParams{DataStorageVersion: "v\x00z"})); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("NUL data storage version error = %v, want ErrInvalidArgument", err)
	}
}

func TestOpenRejectsNULAndClosedDataset(t *testing.T) {
	if _, err := lance.Open(context.Background(), "/tmp/x\x00y"); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("NUL URI error = %v, want ErrInvalidArgument", err)
	}

	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := dataset.CountRows(ctx); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("CountRows after Close error = %v, want ErrClosed", err)
	}
	if _, err := dataset.Scan(); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("Scan after Close error = %v, want ErrClosed", err)
	}
}
