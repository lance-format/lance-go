package lance_test

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	lance "github.com/lance-format/lance-go"
)

func countBlobIter(t *testing.T, seq iter.Seq2[arrow.RecordBatch, error]) (rows int64, batches int) {
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

func TestDatasetBlobRecordsIterateAllRows(t *testing.T) {
	ctx := context.Background()
	dataset, _ := newBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)

	rows, batches := countBlobIter(t, dataset.BlobRecords(ctx, "blob", lance.BlobRowSelectorIDs,
		[]uint64{refs[3].RowID, refs[0].RowID, refs[1].RowID}, nil))
	if rows != 3 || batches == 0 {
		t.Fatalf("BlobRecords = %d rows in %d batches, want 3 rows", rows, batches)
	}

	requests := []lance.BlobRangeRequest{
		{Row: refs[0].Addr, Offset: 1, Length: 3},
		{Row: refs[0].Addr, Offset: 0, Length: 5},
		{Row: refs[1].Addr, Offset: 0, Length: 10},
	}
	rows, batches = countBlobIter(t, dataset.BlobRangeRecords(ctx, "blob", lance.BlobRowSelectorAddresses, requests, nil))
	if rows != 3 || batches == 0 {
		t.Fatalf("BlobRangeRecords = %d rows in %d batches, want 3 rows", rows, batches)
	}
}

// TestDatasetBlobRecordsEarlyBreak verifies that breaking out of a planned
// blob read releases its stream, leaves the dataset usable, and keeps a
// retained batch readable after the dataset closes.
func TestDatasetBlobRecordsEarlyBreak(t *testing.T) {
	ctx := context.Background()
	dataset, _ := newBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)

	var kept arrow.RecordBatch
	batches := 0
	for batch, err := range dataset.BlobRecords(ctx, "blob", lance.BlobRowSelectorIndices, []uint64{0, 1}, nil) {
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

	// The released stream does not hold the dataset.
	rows, _ := countBlobIter(t, dataset.BlobRecords(ctx, "blob", lance.BlobRowSelectorIDs,
		[]uint64{refs[0].RowID, refs[1].RowID, refs[2].RowID, refs[3].RowID}, nil))
	if rows != 4 {
		t.Fatalf("read after break = %d rows, want 4", rows)
	}

	// A retained batch owns its buffers and outlives both the stream and the
	// dataset.
	if err := dataset.Close(); err != nil {
		t.Fatalf("close dataset: %v", err)
	}
	data := kept.Column(1).(*array.LargeBinary)
	if kept.NumRows() == 0 || !bytes.Equal(data.Value(0), []byte("alpha")) {
		t.Fatalf("retained batch data = %v, want alpha", data.Value(0))
	}
	kept.Release()
}

// TestDatasetBlobRecordsLazyOpen verifies that a sequence plans its read at
// iteration, not when it is created.
func TestDatasetBlobRecordsLazyOpen(t *testing.T) {
	ctx := context.Background()
	dataset, _ := newBlobDataset(t, blobTestValues())

	seq := dataset.BlobRecords(ctx, "blob", lance.BlobRowSelectorIndices, []uint64{0}, nil)
	if err := dataset.Close(); err != nil {
		t.Fatalf("close dataset: %v", err)
	}
	var gotErr error
	for _, err := range seq {
		gotErr = err
	}
	if !errors.Is(gotErr, lance.ErrClosed) {
		t.Fatalf("iteration after close = %v, want ErrClosed", gotErr)
	}
}

func TestDatasetBlobRecordsCancellation(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var gotErr error
	for batch, err := range dataset.BlobRecords(ctx, "blob", lance.BlobRowSelectorIndices, []uint64{0, 1, 2, 3}, nil) {
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

	// Cancellation ends iteration without releasing the dataset.
	rows, _ := countBlobIter(t, dataset.BlobRecords(context.Background(), "blob", lance.BlobRowSelectorIndices, []uint64{0}, nil))
	if rows != 1 {
		t.Fatalf("read after cancellation = %d rows, want 1", rows)
	}
}

func TestDatasetBlobRecordsErrors(t *testing.T) {
	ctx := context.Background()
	dataset, _ := newBlobDataset(t, blobTestValues())

	// An invalid selector is rejected at iteration, not when the sequence is
	// built.
	var gotErr error
	for _, err := range dataset.BlobRecords(ctx, "blob", lance.BlobRowSelectorNone, []uint64{0}, nil) {
		gotErr = err
	}
	if gotErr == nil {
		t.Fatal("selector None with rows was accepted")
	}

	// A missing column keeps its native error code and diagnostic.
	gotErr = nil
	for _, err := range dataset.BlobRecords(ctx, "missing", lance.BlobRowSelectorIndices, []uint64{0}, nil) {
		gotErr = err
	}
	if !errors.Is(gotErr, lance.ErrInternal) {
		t.Fatalf("missing column error = %v, want native Internal", gotErr)
	}

	// An invalid range selector is rejected the same way.
	gotErr = nil
	for _, err := range dataset.BlobRangeRecords(ctx, "blob", lance.BlobRowSelectorNone, []lance.BlobRangeRequest{{Row: 0, Offset: 0, Length: 1}}, nil) {
		gotErr = err
	}
	if gotErr == nil {
		t.Fatal("range selector None was accepted")
	}
}
