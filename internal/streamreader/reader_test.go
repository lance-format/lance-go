package streamreader

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
)

// countingBatch records Release calls without owning buffers.
type countingBatch struct {
	arrow.RecordBatch
	releases atomic.Int32
}

func (b *countingBatch) Retain() {}

func (b *countingBatch) Release() {
	b.releases.Add(1)
}

// fakeSource is an in-memory Source. Batches are handed to the reader in
// order; err, when set, is reported after every batch is consumed.
type fakeSource struct {
	batches   []arrow.RecordBatch
	err       error
	nextCalls int
	closes    atomic.Int32
}

func (s *fakeSource) Next() (arrow.RecordBatch, bool, error) {
	s.nextCalls++
	if len(s.batches) > 0 {
		batch := s.batches[0]
		s.batches = s.batches[1:]
		return batch, true, nil
	}
	if s.err != nil {
		return nil, false, s.err
	}
	return nil, false, nil
}

func (s *fakeSource) Close() {
	s.closes.Add(1)
}

func TestReaderDrainsBatchesAndClosesSource(t *testing.T) {
	first := &countingBatch{}
	second := &countingBatch{}
	source := &fakeSource{batches: []arrow.RecordBatch{first, second}}
	schema := arrow.NewSchema(nil, nil)
	// NewReader documents nil as context.Background; this test covers that path.
	reader := NewReader(nil, source, schema, nil) //nolint:staticcheck // documented nil-context input

	if !reader.Next() || reader.RecordBatch() != first {
		t.Fatalf("first Next did not return the first batch")
	}
	if !reader.Next() || reader.RecordBatch() != second {
		t.Fatalf("second Next did not return the second batch")
	}
	if got := first.releases.Load(); got != 1 {
		t.Fatalf("first batch releases = %d, want 1", got)
	}
	if reader.Next() {
		t.Fatal("Next returned true at the end of the stream")
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("Err at end of stream = %v, want nil", err)
	}
	if got := source.closes.Load(); got != 0 {
		t.Fatalf("source closed %d times before Release", got)
	}

	reader.Release()
	if got := second.releases.Load(); got != 1 {
		t.Fatalf("current batch releases = %d, want 1", got)
	}
	if got := source.closes.Load(); got != 1 {
		t.Fatalf("source closes = %d, want 1", got)
	}
	if reader.Next() {
		t.Fatal("Next returned true after Release")
	}
}

func TestReaderWrapsSourceErrorOnce(t *testing.T) {
	sourceErr := errors.New("native stream failed")
	publicErr := errors.New("public stream failed")
	source := &fakeSource{err: sourceErr}
	var wraps int
	reader := NewReader(context.Background(), source, arrow.NewSchema(nil, nil), func(err error) error {
		wraps++
		if !errors.Is(err, sourceErr) {
			t.Errorf("wrap received %v, want %v", err, sourceErr)
		}
		return publicErr
	})

	if reader.Next() {
		t.Fatal("Next returned true for a failing stream")
	}
	if !errors.Is(reader.Err(), publicErr) {
		t.Fatalf("Err = %v, want %v", reader.Err(), publicErr)
	}
	calls := source.nextCalls
	if reader.Next() {
		t.Fatal("Next returned true after a stream error")
	}
	if source.nextCalls != calls {
		t.Fatalf("Next read the source again after an error: calls %d -> %d", calls, source.nextCalls)
	}
	if wraps != 1 {
		t.Fatalf("wrap calls = %d, want 1", wraps)
	}
	reader.Release()
}

func TestReaderStopsOnContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := &fakeSource{batches: []arrow.RecordBatch{&countingBatch{}}}
	reader := NewReader(ctx, source, arrow.NewSchema(nil, nil), nil)

	if reader.Next() {
		t.Fatal("Next returned true with a canceled context")
	}
	if !errors.Is(reader.Err(), context.Canceled) {
		t.Fatalf("Err = %v, want context.Canceled", reader.Err())
	}
	if source.nextCalls != 0 {
		t.Fatalf("Next read the source %d times despite a canceled context", source.nextCalls)
	}
	reader.Release()
	if got := source.closes.Load(); got != 1 {
		t.Fatalf("source closes = %d, want 1", got)
	}
}

func TestReaderRetainKeepsSourceOpen(t *testing.T) {
	source := &fakeSource{batches: []arrow.RecordBatch{&countingBatch{}}}
	reader := NewReader(context.Background(), source, arrow.NewSchema(nil, nil), nil)

	reader.Retain()
	reader.Release()
	if got := source.closes.Load(); got != 0 {
		t.Fatalf("source closed %d times while retained", got)
	}
	if !reader.Next() {
		t.Fatalf("Next after a retained Release failed: %v", reader.Err())
	}
	reader.Release()
	if got := source.closes.Load(); got != 1 {
		t.Fatalf("source closes = %d, want 1", got)
	}
}
