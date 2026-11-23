// Package streamreader adapts a native ArrowArrayStream to an Arrow
// RecordReader with reference-counted ownership.
//
// The root blob readers and the raw-file readers both consume
// ffi.StreamReader. They differ only in how a stream failure is classified for
// their public package, so the lifetime, context and batch-borrowing rules
// live here once and the caller supplies the error mapping.
package streamreader

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Source is the native Arrow stream a Reader drains. *ffi.StreamReader
// implements it. Close releases the native stream exactly once; batches that
// were already returned stay valid.
type Source interface {
	// Next returns the next owned batch. A false return with a nil error marks
	// the end of the stream.
	Next() (arrow.RecordBatch, bool, error)
	// Close releases the native stream.
	Close()
}

// Reader is an array.RecordReader over a Source. A batch returned by Next is
// borrowed: the next Next call or Release releases it, so call Retain to keep
// it. The context passed to NewReader is checked before every read.
//
// wrap classifies source failures for the public package. It is called once
// per source error and stored as Err. Context errors are returned unchanged.
type Reader struct {
	ctx    context.Context
	source Source
	schema *arrow.Schema
	wrap   func(error) error
	refs   atomic.Int64

	mu      sync.Mutex
	current arrow.RecordBatch
	err     error
	done    bool
}

// NewReader takes ownership of source. The caller must not use source after
// the returned reader is released. A nil ctx uses context.Background; a nil
// wrap returns source errors unchanged.
func NewReader(ctx context.Context, source Source, schema *arrow.Schema, wrap func(error) error) *Reader {
	if ctx == nil {
		ctx = context.Background()
	}
	if wrap == nil {
		wrap = func(err error) error { return err }
	}
	reader := &Reader{
		ctx:    ctx,
		source: source,
		schema: schema,
		wrap:   wrap,
	}
	reader.refs.Store(1)
	return reader
}

// Retain increments the reader reference count.
func (r *Reader) Retain() {
	r.refs.Add(1)
}

// Release drops one reference and closes the source when the last reference is
// released.
func (r *Reader) Release() {
	if r.refs.Add(-1) != 0 {
		return
	}
	r.mu.Lock()
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	r.mu.Unlock()
	r.source.Close()
}

// Schema returns the stream schema.
func (r *Reader) Schema() *arrow.Schema {
	return r.schema
}

// Next advances to the next batch. It reports false at the end of the stream,
// after a context or source error, or after the last reference was released.
func (r *Reader) Next() bool {
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
	batch, ok, err := r.source.Next()
	if err != nil {
		r.err = r.wrap(err)
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

// RecordBatch returns the current borrowed batch.
func (r *Reader) RecordBatch() arrow.RecordBatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

// Record returns the current borrowed batch.
func (r *Reader) Record() arrow.Record {
	return r.RecordBatch()
}

// Err returns the context or wrapped source error that ended iteration.
func (r *Reader) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

var _ array.RecordReader = (*Reader)(nil)
