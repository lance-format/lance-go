package lance_test

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/loader"
)

func requireNative(t *testing.T) {
	t.Helper()
	if _, err := loader.Load(); err != nil {
		if errors.Is(err, loader.ErrNotFound) {
			t.Skipf("lance-c is not available: %v", err)
		}
		t.Fatalf("load lance-c: %v", err)
	}
}

func testSchema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
}

func makeRecord(t *testing.T, schema *arrow.Schema, ids []int64, names []string, valid []bool) arrow.RecordBatch {
	t.Helper()
	if len(ids) != len(names) || len(ids) != len(valid) {
		t.Fatalf("record columns have different lengths")
	}
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)

	idBuilder := builder.Field(0).(*array.Int64Builder)
	idBuilder.AppendValues(ids, valid)
	nameBuilder := builder.Field(1).(*array.StringBuilder)
	nameBuilder.AppendValues(names, valid)
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return record
}

func makeReader(t *testing.T, schema *arrow.Schema, records ...arrow.RecordBatch) array.RecordReader {
	t.Helper()
	reader, err := array.NewRecordReader(schema, records)
	if err != nil {
		t.Fatalf("new record reader: %v", err)
	}
	t.Cleanup(reader.Release)
	return reader
}

func collectInt64(t *testing.T, batch arrow.RecordBatch, field string) []int64 {
	t.Helper()
	column := findColumn(t, batch, field)
	values, ok := column.(*array.Int64)
	if !ok {
		t.Fatalf("column %q has type %T, want *array.Int64", field, column)
	}
	result := make([]int64, values.Len())
	for i := range result {
		result[i] = values.Value(i)
	}
	return result
}

func findColumn(t *testing.T, batch arrow.RecordBatch, field string) arrow.Array {
	t.Helper()
	for i := 0; i < int(batch.NumCols()); i++ {
		if batch.Schema().Field(i).Name == field {
			return batch.Column(i)
		}
	}
	t.Fatalf("batch has no column %q (schema %v)", field, batch.Schema())
	return nil
}

func TestCreateReopenMetadataAndScan(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3, 4}, []string{"alice", "bob", "carol", "dave"}, []bool{true, true, true, true})

	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	version, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version != 1 {
		t.Fatalf("Version = %d, want 1", version)
	}
	latest, err := dataset.LatestVersion(ctx)
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if latest != version {
		t.Fatalf("LatestVersion = %d, want %d", latest, version)
	}
	rows, err := dataset.CountRows(ctx)
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if rows != 4 {
		t.Fatalf("CountRows = %d, want 4", rows)
	}
	filtered, err := dataset.CountRowsFiltered(ctx, "id >= 3")
	if err != nil {
		t.Fatalf("CountRowsFiltered: %v", err)
	}
	if filtered != 2 {
		t.Fatalf("CountRowsFiltered = %d, want 2", filtered)
	}
	if err := dataset.Validate(ctx); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	gotSchema, err := dataset.Schema()
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if gotSchema.NumFields() != schema.NumFields() {
		t.Fatalf("schema fields = %d, want %d", gotSchema.NumFields(), schema.NumFields())
	}
	if gotSchema.Field(0).Name != "id" || gotSchema.Field(1).Name != "name" {
		t.Fatalf("unexpected schema fields: %v", gotSchema)
	}
	stats, err := dataset.DataStatistics(ctx)
	if err != nil {
		t.Fatalf("DataStatistics: %v", err)
	}
	if len(stats) == 0 {
		t.Fatal("DataStatistics returned no fields")
	}

	if err := dataset.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()
	rows, err = reopened.CountRows(ctx)
	if err != nil {
		t.Fatalf("reopened CountRows: %v", err)
	}
	if rows != 4 {
		t.Fatalf("reopened CountRows = %d, want 4", rows)
	}

	scanner, err := reopened.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var ids []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) != 4 || ids[0] != 1 || ids[3] != 4 {
		t.Fatalf("scanned ids = %v, want [1 2 3 4]", ids)
	}
}

func TestProjectionFilterOrderLimitOffsetTakeAndRowIDs(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3, 4, 5}, []string{"a", "b", "c", "d", "e"}, []bool{true, true, true, true, true})

	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record), lance.WithWriteParams(lance.WriteParams{EnableStableRowIDs: true}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	scanner, err := dataset.Scan(
		lance.WithColumns("id"),
		lance.WithSQLFilter("id >= 2"),
		lance.WithOrderBy(lance.Ordering{Column: "id", Direction: lance.Descending}),
		lance.WithOffset(1),
		lance.WithLimit(2),
	)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var ids []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	if want := []int64{4, 3}; !equalInt64(ids, want) {
		t.Fatalf("filtered ids = %v, want %v", ids, want)
	}

	taken, err := dataset.Take(ctx, []uint64{0, 2, 4}, "id")
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	var takenIDs []int64
	for _, batch := range taken {
		takenIDs = append(takenIDs, collectInt64(t, batch, "id")...)
		batch.Release()
	}
	if want := []int64{1, 3, 5}; !equalInt64(takenIDs, want) {
		t.Fatalf("Take ids = %v, want %v", takenIDs, want)
	}

	rowScanner, err := dataset.Scan(lance.WithColumns("id"), lance.WithRowID(true), lance.WithRowAddress(true))
	if err != nil {
		t.Fatalf("row Scan: %v", err)
	}
	var rowIDs []uint64
	var originalIDs []int64
	for batch, err := range rowScanner.Records(ctx) {
		if err != nil {
			t.Fatalf("row Records: %v", err)
		}
		originalIDs = append(originalIDs, collectInt64(t, batch, "id")...)
		column := findColumn(t, batch, "_rowid")
		values, ok := column.(*array.Uint64)
		if !ok {
			t.Fatalf("_rowid has type %T, want *array.Uint64", column)
		}
		for i := 0; i < values.Len(); i++ {
			rowIDs = append(rowIDs, values.Value(i))
		}
		findColumn(t, batch, "_rowaddr")
	}
	byRowID, err := dataset.TakeRows(ctx, rowIDs, "id")
	if err != nil {
		t.Fatalf("TakeRows: %v", err)
	}
	var byRowIDs []int64
	for _, batch := range byRowID {
		byRowIDs = append(byRowIDs, collectInt64(t, batch, "id")...)
		batch.Release()
	}
	if !equalInt64(byRowIDs, originalIDs) {
		t.Fatalf("TakeRows ids = %v, want %v", byRowIDs, originalIDs)
	}
}

func TestRetainedBatchAndEarlyBreak(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3}, []string{"a", "b", "c"}, []bool{true, true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	scanner, err := dataset.Scan(lance.WithBatchSize(1))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var retained arrow.RecordBatch
	batches := 0
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		batches++
		if retained == nil {
			batch.Retain()
			retained = batch
		}
	}
	if batches < 2 {
		t.Fatalf("got %d batches, want at least 2", batches)
	}
	if retained == nil || retained.NumRows() == 0 {
		t.Fatal("retained batch is empty")
	}
	retained.Release()

	early, err := dataset.Scan(lance.WithBatchSize(1))
	if err != nil {
		t.Fatalf("early Scan: %v", err)
	}
	seen := 0
	for batch, err := range early.Records(ctx) {
		if err != nil {
			t.Fatalf("early Records: %v", err)
		}
		_ = batch
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("early scan yielded %d batches, want 1", seen)
	}
}

func TestToArrowReaderAndStatistics(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	statsCalled := false
	scanner, err := dataset.Scan(
		lance.WithColumns("id"),
		lance.WithScanStatistics(func(stats lance.ScanStatistics) {
			statsCalled = true
		}),
	)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	reader, err := scanner.ToArrowReader(ctx)
	if err != nil {
		t.Fatalf("ToArrowReader: %v", err)
	}
	rows := int64(0)
	for reader.Next() {
		rows += reader.RecordBatch().NumRows()
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("reader Err: %v", err)
	}
	reader.Release()
	if rows != 2 {
		t.Fatalf("reader rows = %d, want 2", rows)
	}
	if !statsCalled {
		t.Fatal("scan statistics callback was not called")
	}
}

func TestCanceledScanContext(t *testing.T) {
	requireNative(t)
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})
	dataset, err := lance.Create(context.Background(), uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var got error
	for _, err := range scanner.Records(ctx) {
		got = err
		break
	}
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("Records error = %v, want context.Canceled", got)
	}
}

func equalInt64(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestProjectionExpressionAndAdditionalFilter(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3}, []string{"a", "b", "c"}, []bool{true, true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	scanner, err := dataset.Scan(
		lance.WithProjection(lance.ProjectionColumn{Name: "double_id", Expression: "id * 2"}),
		lance.WithAdditionalSQLFilter("id >= 2"),
		lance.WithBatchSizeBytes(1<<20),
	)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var ids []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "double_id")...)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if want := []int64{4, 6}; !equalInt64(ids, want) {
		t.Fatalf("projected ids = %v, want %v", ids, want)
	}
}

func TestDatasetCloseWhileScannerAlive(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("Close dataset: %v", err)
	}
	rows := int64(0)
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("Records after dataset close: %v", err)
		}
		rows += batch.NumRows()
	}
	if rows != 2 {
		t.Fatalf("rows after dataset close = %d, want 2", rows)
	}
}

func TestConcurrentScannerClose(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	ids := make([]int64, 200)
	names := make([]string, 200)
	valid := make([]bool, 200)
	for i := range ids {
		ids[i] = int64(i)
		names[i] = "row"
		valid[i] = true
	}
	record := makeRecord(t, schema, ids, names, valid)
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()
	scanner, err := dataset.Scan(lance.WithBatchSize(1))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	first := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		for _, err := range scanner.Records(ctx) {
			if err != nil {
				if !errors.Is(err, lance.ErrClosed) {
					done <- err
					return
				}
				done <- nil
				return
			}
			select {
			case <-first:
			default:
				close(first)
			}
		}
		done <- nil
	}()

	<-first
	if err := scanner.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("iteration after Close: %v", err)
	}
}
