package lance

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
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
