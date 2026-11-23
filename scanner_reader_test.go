package lance

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go/internal/ffi"
	"github.com/lance-format/lance-go/internal/loader"
)

// blockingScannerSource parks Next inside the native poll until Close is
// called. Tests use the entered channel to know that Next is in flight before
// starting Release, so the ordering does not depend on timers.
type blockingScannerSource struct {
	batch     arrow.RecordBatch
	err       error
	entered   chan struct{}
	closed    chan struct{}
	enterOnce sync.Once
	closeOnce sync.Once
}

func newBlockingScannerSource() *blockingScannerSource {
	return &blockingScannerSource{
		entered: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (s *blockingScannerSource) Next(context.Context) (arrow.RecordBatch, bool, error) {
	s.enterOnce.Do(func() { close(s.entered) })
	<-s.closed
	return s.batch, false, s.err
}

func (s *blockingScannerSource) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

// countingBatch records Release calls without owning any buffers. Only Retain
// and Release are exercised by the reader.
type countingBatch struct {
	arrow.RecordBatch
	releases atomic.Int32
}

func (b *countingBatch) Retain() {}

func (b *countingBatch) Release() {
	b.releases.Add(1)
}

func TestScannerReaderReleaseWakesBlockedNext(t *testing.T) {
	source := newBlockingScannerSource()
	source.err = ffi.ErrClosed
	reader := newScannerReader(&Scanner{uri: "test"}, source, context.Background(), arrow.NewSchema(nil, nil))

	nextDone := make(chan struct{})
	go func() {
		defer close(nextDone)
		if reader.Next() {
			t.Error("Next returned true after the scanner was closed")
		}
	}()
	<-source.entered

	releaseDone := make(chan struct{})
	go func() {
		defer close(releaseDone)
		reader.Release()
	}()

	waitFor(t, releaseDone, "Release returned while Next was blocked in the scanner")
	waitFor(t, nextDone, "blocked Next did not wake after Release")
	if err := reader.Err(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Err after release = %v, want ErrClosed", err)
	}
}

// staticScannerSource serves one batch and then reports end of stream.
type staticScannerSource struct {
	batch arrow.RecordBatch
}

func (s *staticScannerSource) Next(context.Context) (arrow.RecordBatch, bool, error) {
	if s.batch == nil {
		return nil, true, nil
	}
	batch := s.batch
	s.batch = nil
	return batch, false, nil
}

func (s *staticScannerSource) Close() error { return nil }

func TestScannerReaderReleaseReleasesCurrentBatch(t *testing.T) {
	batch := &countingBatch{}
	reader := newScannerReader(&Scanner{uri: "test"}, &staticScannerSource{batch: batch}, context.Background(), arrow.NewSchema(nil, nil))
	if !reader.Next() {
		t.Fatal("Next = false, want true")
	}
	if reader.RecordBatch() != batch {
		t.Fatal("RecordBatch did not return the current batch")
	}

	reader.Release()
	if got := batch.releases.Load(); got != 1 {
		t.Fatalf("batch releases = %d, want 1", got)
	}
}

func TestScannerReaderReleasesBatchThatArrivesAfterRelease(t *testing.T) {
	source := newBlockingScannerSource()
	batch := &countingBatch{}
	source.batch = batch
	reader := newScannerReader(&Scanner{uri: "test"}, source, context.Background(), arrow.NewSchema(nil, nil))

	nextDone := make(chan struct{})
	go func() {
		defer close(nextDone)
		if reader.Next() {
			t.Error("Next returned true after the scanner was closed")
		}
	}()
	<-source.entered

	reader.Release()
	waitFor(t, nextDone, "blocked Next did not wake after Release")
	if got := batch.releases.Load(); got != 1 {
		t.Fatalf("batch releases = %d, want 1", got)
	}
}

func waitFor(t *testing.T, done <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal(message)
	}
}

func TestScannerReaderStatisticsCallbackCanReenterNext(t *testing.T) {
	if _, err := loader.Load(); err != nil {
		if errors.Is(err, loader.ErrNotFound) {
			t.Skipf("lance-c is not available: %v", err)
		}
		t.Fatalf("load lance-c: %v", err)
	}

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	builder.Field(0).(*array.Int64Builder).Append(1)
	record := builder.NewRecordBatch()
	builder.Release()
	source, err := array.NewRecordReader(schema, []arrow.RecordBatch{record})
	if err != nil {
		record.Release()
		t.Fatalf("NewRecordReader: %v", err)
	}
	record.Release()

	ctx := context.Background()
	dataset, err := Create(ctx, filepath.Join(t.TempDir(), "data.lance"), source)
	source.Release()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	var reader *scannerReader
	callbackRan := false
	scanner, err := dataset.Scan(WithScanStatistics(func(ScanStatistics) {
		callbackRan = true
		if !reader.nextMu.TryLock() {
			t.Error("statistics callback ran while nextMu was held")
			return
		}
		reader.nextMu.Unlock()
		if !reader.mu.TryLock() {
			t.Error("statistics callback ran while mu was held")
			return
		}
		reader.mu.Unlock()
		if reader.Next() {
			t.Error("Next from the statistics callback returned true")
		}
	}))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	recordReader, err := scanner.ToArrowReader(ctx)
	if err != nil {
		t.Fatalf("ToArrowReader: %v", err)
	}
	reader = recordReader.(*scannerReader)
	defer recordReader.Release()

	if !reader.Next() {
		t.Fatalf("first Next = false: %v", reader.Err())
	}
	if reader.Next() {
		t.Fatal("second Next = true, want end of stream")
	}
	if !callbackRan {
		t.Fatal("statistics callback did not run at end of stream")
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("Err = %v, want nil", err)
	}
}
