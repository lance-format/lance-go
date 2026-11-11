package file

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
)

type recordReader struct {
	ctx    context.Context
	stream *ffi.StreamReader
	schema *arrow.Schema
	refs   atomic.Int64

	mu      sync.Mutex
	current arrow.RecordBatch
	err     error
	done    bool
}

func newRecordReader(ctx context.Context, stream *ffi.StreamReader) *recordReader {
	if ctx == nil {
		ctx = context.Background()
	}
	reader := &recordReader{
		ctx:    ctx,
		stream: stream,
		schema: stream.Schema(),
	}
	reader.refs.Store(1)
	return reader
}

func (r *recordReader) Retain() {
	r.refs.Add(1)
}

func (r *recordReader) Release() {
	if r.refs.Add(-1) != 0 {
		return
	}
	r.mu.Lock()
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	r.mu.Unlock()
	r.stream.Close()
}

func (r *recordReader) Schema() *arrow.Schema {
	return r.schema
}

func (r *recordReader) Next() bool {
	if r.refs.Load() <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done || r.err != nil {
		return false
	}
	if err := r.ctx.Err(); err != nil {
		r.err = err
		r.done = true
		return false
	}
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	batch, ok, err := r.stream.Next()
	if err != nil {
		r.err = err
		r.done = true
		return false
	}
	if !ok {
		r.done = true
		return false
	}
	r.current = batch
	return true
}

func (r *recordReader) RecordBatch() arrow.RecordBatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

func (r *recordReader) Record() arrow.Record {
	return r.RecordBatch()
}

func (r *recordReader) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

var _ array.RecordReader = (*recordReader)(nil)
