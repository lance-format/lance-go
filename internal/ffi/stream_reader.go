package ffi

import (
	"fmt"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
)

// StreamReader incrementally drains a native ArrowArrayStream. It owns the
// stream and releases it exactly once. Each batch returned by Next is owned by
// the caller. Next and Close are serialized; batches remain valid after Close.
type StreamReader struct {
	stream *cdata.ArrowArrayStream
	schema *arrow.Schema

	mu       sync.Mutex
	released bool
	done     bool
	err      error
}

// NewStreamReader takes ownership of stream and reads its schema immediately.
// On error the stream has already been released. On success stream.Release is
// cleared so the caller cannot release it a second time.
func NewStreamReader(stream *cdata.ArrowArrayStream) (*StreamReader, error) {
	if stream == nil {
		return nil, fmt.Errorf("ffi: native Arrow stream must not be nil")
	}
	if stream.Release == 0 || stream.GetSchema == 0 || stream.GetNext == 0 {
		cdata.ReleaseStream(stream)
		stream.Release = 0
		return nil, fmt.Errorf("ffi: native Arrow stream is missing required callbacks")
	}

	owned := new(cdata.ArrowArrayStream)
	*owned = *stream
	stream.Release = 0
	r := &StreamReader{stream: owned}
	if err := r.loadSchema(); err != nil {
		r.closeLocked()
		return nil, err
	}
	return r, nil
}

func (r *StreamReader) loadSchema() error {
	var raw cdata.ArrowSchema
	if rc := callStreamGetSchema(r.stream, &raw); rc != 0 {
		if raw.Release != 0 {
			cdata.ReleaseSchema(&raw)
		}
		err := streamError(r.stream, "get_schema")
		if err == nil {
			err = fmt.Errorf("ffi: Arrow stream get_schema failed without setting an error")
		}
		return err
	}
	defer cdata.ReleaseSchema(&raw)
	schema, err := cdata.ImportSchema(&raw)
	if err != nil {
		return fmt.Errorf("ffi: import Arrow stream schema: %w", err)
	}
	r.schema = schema
	return nil
}

// Schema returns the immutable stream schema.
func (r *StreamReader) Schema() *arrow.Schema {
	return r.schema
}

// Next returns the next owned batch. A false return with a nil error marks the
// end of the stream. After an error every later call returns the same error.
func (r *StreamReader) Next() (arrow.RecordBatch, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.released {
		return nil, false, fmt.Errorf("ffi: Arrow stream reader is released")
	}
	if r.err != nil {
		return nil, false, r.err
	}
	if r.done {
		return nil, false, nil
	}

	var raw cdata.ArrowArray
	if rc := callStreamGetNext(r.stream, &raw); rc != 0 {
		if raw.Release != 0 {
			cdata.ReleaseArray(&raw)
		}
		r.err = streamError(r.stream, "get_next")
		if r.err == nil {
			r.err = fmt.Errorf("ffi: Arrow stream get_next failed without setting an error")
		}
		return nil, false, r.err
	}
	if raw.Release == 0 {
		r.done = true
		return nil, false, nil
	}
	batch, err := cdata.ImportRecordBatch(&raw, r.schema)
	if err != nil {
		r.err = fmt.Errorf("ffi: import Arrow stream batch: %w", err)
		return nil, false, r.err
	}
	return batch, true, nil
}

// Close releases the native stream. It is idempotent and does not invalidate
// batches that were already returned.
func (r *StreamReader) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
}

func (r *StreamReader) closeLocked() {
	if r == nil || r.released {
		return
	}
	r.released = true
	cdata.ReleaseStream(r.stream)
	r.stream = nil
}
