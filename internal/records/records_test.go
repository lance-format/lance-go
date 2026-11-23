package records_test

import (
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go/internal/records"
)

func testReader(t *testing.T, batches int) array.RecordReader {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false}}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	ids := builder.Field(0).(*array.Int64Builder)

	owned := make([]arrow.RecordBatch, 0, batches)
	for i := 0; i < batches; i++ {
		ids.Append(int64(i))
		batch := builder.NewRecordBatch()
		owned = append(owned, batch)
	}
	reader, err := array.NewRecordReader(schema, owned)
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	for _, batch := range owned {
		batch.Release()
	}
	return reader
}

// countingReader records how often the adapter releases the stream it opened.
type countingReader struct {
	array.RecordReader
	releases int
}

func (r *countingReader) Release() {
	r.releases++
	r.RecordReader.Release()
}

// failingReader reports err after the batches the wrapped reader still holds.
type failingReader struct {
	array.RecordReader
	err   error
	after int
	seen  int
	done  bool
}

func (r *failingReader) Next() bool {
	if r.seen >= r.after {
		r.done = true
		return false
	}
	if !r.RecordReader.Next() {
		return false
	}
	r.seen++
	return true
}

func (r *failingReader) Err() error {
	if r.done {
		return r.err
	}
	return r.RecordReader.Err()
}

func TestSequenceOpensOnIteration(t *testing.T) {
	opened := 0
	stream := &countingReader{RecordReader: testReader(t, 2)}

	// Building the sequence must not open a stream.
	seq := records.Sequence(func() (array.RecordReader, error) {
		opened++
		return stream, nil
	})
	if opened != 0 {
		t.Fatalf("sequence opened %d streams before iteration", opened)
	}

	var schemas []*arrow.Schema
	for batch, err := range seq {
		if err != nil {
			t.Fatalf("iteration error: %v", err)
		}
		schemas = append(schemas, batch.Schema())
	}
	if opened != 1 {
		t.Fatalf("sequence opened %d streams, want 1", opened)
	}
	if stream.releases != 1 {
		t.Fatalf("adapter released the stream %d times, want 1", stream.releases)
	}
	if len(schemas) != 2 {
		t.Fatalf("yielded %d batches, want 2", len(schemas))
	}
}

func TestSequenceReleasesOnEarlyBreak(t *testing.T) {
	stream := &countingReader{RecordReader: testReader(t, 3)}

	var kept arrow.RecordBatch
	batches := 0
	for batch, err := range records.Sequence(func() (array.RecordReader, error) {
		return stream, nil
	}) {
		if err != nil {
			t.Fatalf("iteration error: %v", err)
		}
		batches++
		kept = batch
		kept.Retain()
		break
	}
	if batches != 1 {
		t.Fatalf("iterated %d batches, want 1", batches)
	}
	if stream.releases != 1 {
		t.Fatalf("break released the stream %d times, want 1", stream.releases)
	}

	// A retained batch outlives the stream the adapter closed.
	ids := kept.Column(0).(*array.Int64)
	if ids.Len() != 1 || ids.Value(0) != 0 {
		t.Fatalf("retained batch = %v, want the first row", ids)
	}
	kept.Release()
}

func TestSequenceOpenError(t *testing.T) {
	wantErr := errors.New("open failed")
	opened := 0
	for batch, err := range records.Sequence(func() (array.RecordReader, error) {
		opened++
		return nil, wantErr
	}) {
		if batch != nil {
			t.Fatalf("open error yielded batch %v", batch)
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("open error = %v, want %v", err, wantErr)
		}
	}
	if opened != 1 {
		t.Fatalf("sequence opened %d streams, want 1", opened)
	}
}

func TestSequenceStreamError(t *testing.T) {
	wantErr := errors.New("stream failed")
	stream := &countingReader{
		RecordReader: &failingReader{RecordReader: testReader(t, 3), err: wantErr, after: 2},
	}

	var gotErr error
	batches := 0
	for batch, err := range records.Sequence(func() (array.RecordReader, error) {
		return stream, nil
	}) {
		if err != nil {
			gotErr = err
			continue
		}
		batches++
		_ = batch
	}
	if batches != 2 {
		t.Fatalf("yielded %d batches, want 2", batches)
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("stream error = %v, want %v", gotErr, wantErr)
	}
	if stream.releases != 1 {
		t.Fatalf("adapter released the stream %d times, want 1", stream.releases)
	}
}

func TestSequenceBreaksOnStreamError(t *testing.T) {
	wantErr := errors.New("stream failed")
	stream := &countingReader{
		RecordReader: &failingReader{RecordReader: testReader(t, 1), err: wantErr, after: 1},
	}

	yielded := 0
	for _, err := range records.Sequence(func() (array.RecordReader, error) {
		return stream, nil
	}) {
		yielded++
		if err == nil {
			continue
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("stream error = %v, want %v", err, wantErr)
		}
		// Leaving the loop from an error still releases the stream.
		break
	}
	if yielded != 2 {
		t.Fatalf("yielded %d values, want a batch and an error", yielded)
	}
	if stream.releases != 1 {
		t.Fatalf("adapter released the stream %d times, want 1", stream.releases)
	}
}
