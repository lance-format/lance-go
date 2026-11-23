package file_test

import (
	"context"
	"errors"
	"iter"
	"math"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/file"
)

// openIterReader writes the shared four-row test file and opens it for the
// iterator tests.
func openIterReader(t *testing.T) *file.Reader {
	t.Helper()
	requireNative(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "iterator.lance")
	schema := testSchema()

	writer, err := file.Create(ctx, path, schema, nil)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	batch := testBatch(t, schema)
	defer batch.Release()
	if err := writer.Write(ctx, batch); err != nil {
		t.Fatalf("write batch: %v", err)
	}
	if _, err := writer.Finish(ctx); err != nil {
		t.Fatalf("finish writer: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	reader, err := file.Open(ctx, path, nil)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	return reader
}

func countIterRecords(t *testing.T, seq iter.Seq2[arrow.RecordBatch, error]) (rows int64, batches int) {
	t.Helper()
	for batch, err := range seq {
		if err != nil {
			t.Fatalf("iteration error: %v", err)
		}
		rows += batch.NumRows()
		batches++
	}
	return rows, batches
}

func TestReaderRecordsIterateAllRows(t *testing.T) {
	reader := openIterReader(t)
	ctx := context.Background()
	oneRow := file.ReadOptions{BatchSize: 1}

	tests := []struct {
		name    string
		seq     iter.Seq2[arrow.RecordBatch, error]
		rows    int64
		batches int
	}{
		{"Records", reader.Records(ctx, oneRow), 4, 4},
		{"RecordsRange", reader.RecordsRange(ctx, 1, 2, oneRow), 2, 2},
		{"RecordsIndices", reader.RecordsIndices(ctx, []uint32{0, 3}, oneRow), 2, 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows, batches := countIterRecords(t, test.seq)
			if rows != test.rows || batches != test.batches {
				t.Fatalf("iteration = %d rows in %d batches, want %d rows in %d batches",
					rows, batches, test.rows, test.batches)
			}
		})
	}
}

// TestReaderRecordsEarlyBreak verifies that breaking out of an iterator
// releases its stream, leaves the parent reader usable, and keeps a retained
// batch readable.
func TestReaderRecordsEarlyBreak(t *testing.T) {
	reader := openIterReader(t)
	ctx := context.Background()

	var kept arrow.RecordBatch
	batches := 0
	for batch, err := range reader.Records(ctx, file.ReadOptions{BatchSize: 1}) {
		if err != nil {
			t.Fatalf("iteration error: %v", err)
		}
		batches++
		kept = batch
		kept.Retain()
		break
	}
	if batches != 1 {
		t.Fatalf("break iterated %d batches, want 1", batches)
	}

	// The stream the break released does not hold the parent reader.
	if rows, _ := countIterRecords(t, reader.Records(ctx, file.ReadOptions{})); rows != 4 {
		t.Fatalf("read after break = %d rows, want 4", rows)
	}

	// A retained batch outlives the released stream.
	ids := kept.Column(0).(*array.Int64)
	if ids.Len() != 1 || ids.IsNull(0) || ids.Value(0) != 1 {
		t.Fatalf("retained batch = %v, want the first row", ids)
	}
	kept.Release()
}

// TestReaderRecordsLazyOpen verifies that a sequence opens its stream at
// iteration, not when it is created.
func TestReaderRecordsLazyOpen(t *testing.T) {
	reader := openIterReader(t)
	ctx := context.Background()

	afterBreak := reader.Records(ctx, file.ReadOptions{BatchSize: 1})
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	var gotErr error
	for _, err := range afterBreak {
		gotErr = err
	}
	if !errors.Is(gotErr, lance.ErrClosed) {
		t.Fatalf("iteration after close = %v, want ErrClosed", gotErr)
	}
}

func TestReaderRecordsCancellation(t *testing.T) {
	reader := openIterReader(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var gotErr error
	for batch, err := range reader.Records(ctx, file.ReadOptions{BatchSize: 1}) {
		if err != nil {
			gotErr = err
			continue
		}
		_ = batch
		cancel()
	}
	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("iteration error = %v, want context.Canceled", gotErr)
	}

	// Cancellation ends iteration without releasing the parent reader.
	if rows, _ := countIterRecords(t, reader.Records(context.Background(), file.ReadOptions{})); rows != 4 {
		t.Fatalf("read after cancellation = %d rows, want 4", rows)
	}
}

func TestReaderRecordsErrors(t *testing.T) {
	reader := openIterReader(t)
	ctx := context.Background()

	// Native rejects disorder when the stream advances or when it opens.
	var gotErr error
	for batch, err := range reader.RecordsIndices(ctx, []uint32{3, 1}, file.ReadOptions{BatchSize: 1}) {
		if err != nil {
			gotErr = err
			continue
		}
		_ = batch
	}
	if gotErr == nil {
		t.Fatal("out-of-order indices were accepted")
	}

	// An empty selection is valid and yields no rows and no error.
	rows, batches := countIterRecords(t, reader.RecordsIndices(ctx, nil, file.ReadOptions{}))
	if rows != 0 || batches != 0 {
		t.Fatalf("empty selection = %d rows in %d batches, want none", rows, batches)
	}

	// Range overflow is rejected at iteration, not when the sequence is built.
	overflow := reader.RecordsRange(ctx, math.MaxUint64, 2, file.ReadOptions{})
	gotErr = nil
	for _, err := range overflow {
		gotErr = err
	}
	if gotErr == nil {
		t.Fatal("overflowing range was accepted")
	}
}
