package ffi

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go/internal/cdata"
)

func TestStreamReaderDrainsAndOwnsStream(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)
	records := []arrow.RecordBatch{
		newFFITestRecord(t, schema, []int64{1, 2}),
		newFFITestRecord(t, schema, []int64{3, 4, 5}),
	}
	reader, err := array.NewRecordReader(schema, records)
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	defer reader.Release()

	var stream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &stream); err != nil {
		t.Fatalf("ExportRecordReader: %v", err)
	}
	streamReader, err := NewStreamReader(&stream)
	if err != nil {
		t.Fatalf("NewStreamReader: %v", err)
	}
	if stream.Release != 0 {
		t.Fatal("NewStreamReader left the caller's stream release callback set")
	}
	if !streamReader.Schema().Equal(schema) {
		t.Fatalf("schema = %v, want %v", streamReader.Schema(), schema)
	}

	var got []int64
	var batches []arrow.RecordBatch
	for {
		batch, ok, err := streamReader.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if !ok {
			break
		}
		batches = append(batches, batch)
		column := batch.Column(0).(*array.Int64)
		for i := 0; i < column.Len(); i++ {
			got = append(got, column.Value(i))
		}
	}
	if want := []int64{1, 2, 3, 4, 5}; len(got) != len(want) {
		t.Fatalf("values = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("values = %v, want %v", got, want)
			}
		}
	}
	if _, ok, err := streamReader.Next(); ok || err != nil {
		t.Fatalf("Next after end = %v, %v; want false, nil", ok, err)
	}

	streamReader.Close()
	streamReader.Close()
	if _, _, err := streamReader.Next(); err == nil {
		t.Fatal("Next after Close succeeded")
	}
	column := batches[0].Column(0).(*array.Int64)
	if column.Value(1) != 2 {
		t.Fatalf("released batch value = %d, want 2", column.Value(1))
	}
	for _, batch := range batches {
		batch.Release()
	}
}

func TestStreamReaderRejectsInvalidStream(t *testing.T) {
	if _, err := NewStreamReader(nil); err == nil {
		t.Fatal("nil stream was accepted")
	}
	var stream cdata.ArrowArrayStream
	if _, err := NewStreamReader(&stream); err == nil {
		t.Fatal("stream without callbacks was accepted")
	}
}

func TestStreamReaderPropagatesReaderError(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)
	source := &failingRecordReader{
		schema: schema,
		batch:  newFFITestRecord(t, schema, []int64{1}),
	}
	var stream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(source, &stream); err != nil {
		t.Fatalf("ExportRecordReader: %v", err)
	}
	reader, err := NewStreamReader(&stream)
	if err != nil {
		t.Fatalf("NewStreamReader: %v", err)
	}
	defer reader.Close()

	if batch, ok, err := reader.Next(); err != nil || !ok {
		t.Fatalf("first Next = %v, %v", ok, err)
	} else {
		batch.Release()
	}
	_, ok, err := reader.Next()
	if ok || err == nil {
		t.Fatalf("second Next = %v, %v; want error", ok, err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want source error text", err)
	}
	if _, _, again := reader.Next(); again == nil || again.Error() != err.Error() {
		t.Fatalf("repeated error = %v, want %v", again, err)
	}
}

func newFFITestRecord(t *testing.T, schema *arrow.Schema, values []int64) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	column := builder.Field(0).(*array.Int64Builder)
	for _, value := range values {
		column.Append(value)
	}
	return builder.NewRecordBatch()
}

type failingRecordReader struct {
	schema  *arrow.Schema
	batch   arrow.RecordBatch
	refs    atomic.Int64
	pending bool
	err     error
}

func (r *failingRecordReader) Retain() { r.refs.Add(1) }

func (r *failingRecordReader) Release() {
	if r.refs.Add(-1) == 0 && r.batch != nil {
		r.batch.Release()
		r.batch = nil
	}
}

func (r *failingRecordReader) Schema() *arrow.Schema { return r.schema }

func (r *failingRecordReader) Next() bool {
	if r.err == nil && !r.pending {
		r.pending = true
		return true
	}
	r.err = errors.New("boom")
	return false
}

func (r *failingRecordReader) RecordBatch() arrow.RecordBatch { return r.batch }

func (r *failingRecordReader) Record() arrow.Record { return r.batch }

func (r *failingRecordReader) Err() error { return r.err }
