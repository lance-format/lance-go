package file

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/cdata"
	"github.com/lance-format/lance-go/internal/ffi"
)

// TestRecordReaderWrapsStreamErrors verifies that the raw-file record reader
// maps an Arrow stream failure to the public error contract. A raw-file stream
// cannot be made to fail deterministically through the public API, so the
// stream comes from a failing RecordReader; the recordReader.Next path and its
// wrap closure are the same ones used by ReadAll, ReadRange and ReadIndices.
func TestRecordReaderWrapsStreamErrors(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)
	source := &failingStreamReader{
		schema: schema,
		batch:  newStreamTestBatch(t, schema, []int64{1}),
	}
	var raw cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(source, &raw); err != nil {
		t.Fatalf("ExportRecordReader: %v", err)
	}
	stream, err := ffi.NewStreamReader(&raw)
	if err != nil {
		t.Fatalf("NewStreamReader: %v", err)
	}

	const uri = "/tmp/data.lance"
	records := newRecordReader(context.Background(), stream, func(err error) error {
		return wrapError("read all", uri, err)
	})
	defer records.Release()

	if !records.Next() {
		t.Fatalf("first Next failed: %v", records.Err())
	}
	for records.Next() {
	}
	err = records.Err()
	if err == nil {
		t.Fatal("stream failure was not reported")
	}
	var lanceErr *lance.Error
	if !errors.As(err, &lanceErr) {
		t.Fatalf("error = %T (%v), want *lance.Error", err, err)
	}
	if lanceErr.Code != lance.CodeIO {
		t.Fatalf("error code = %v, want %v", lanceErr.Code, lance.CodeIO)
	}
	if !errors.Is(err, lance.ErrIO) {
		t.Fatalf("error = %v, want ErrIO", err)
	}
	if strings.Contains(err.Error(), "ffi:") {
		t.Fatalf("error leaks internal ffi prefix: %v", err)
	}
	if !strings.Contains(err.Error(), "Arrow stream get_next") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %q, want stream operation and native message", err)
	}
}

func newStreamTestBatch(t *testing.T, schema *arrow.Schema, values []int64) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	column := builder.Field(0).(*array.Int64Builder)
	for _, value := range values {
		column.Append(value)
	}
	return builder.NewRecordBatch()
}

// failingStreamReader yields one batch and then reports a native stream error.
type failingStreamReader struct {
	schema  *arrow.Schema
	batch   arrow.RecordBatch
	refs    atomic.Int64
	pending bool
	err     error
}

func (r *failingStreamReader) Retain() { r.refs.Add(1) }

func (r *failingStreamReader) Release() {
	if r.refs.Add(-1) == 0 && r.batch != nil {
		r.batch.Release()
		r.batch = nil
	}
}

func (r *failingStreamReader) Schema() *arrow.Schema { return r.schema }

func (r *failingStreamReader) Next() bool {
	if r.err == nil && !r.pending {
		r.pending = true
		return true
	}
	r.err = errors.New("boom")
	return false
}

func (r *failingStreamReader) RecordBatch() arrow.RecordBatch { return r.batch }

func (r *failingStreamReader) Record() arrow.Record { return r.batch }

func (r *failingStreamReader) Err() error { return r.err }
