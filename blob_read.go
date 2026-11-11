package lance

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
)

// BlobRowSelector selects how read rows are interpreted.
type BlobRowSelector int32

const (
	// BlobRowSelectorNone passes no row selection. Native reports an error
	// when rows or requests are non-empty, and when no selection is supplied.
	BlobRowSelectorNone BlobRowSelector = 0
	// BlobRowSelectorIDs selects by Lance row ID.
	BlobRowSelectorIDs BlobRowSelector = 1
	// BlobRowSelectorAddresses selects by row address.
	BlobRowSelectorAddresses BlobRowSelector = 2
	// BlobRowSelectorIndices selects by zero-based row offset.
	BlobRowSelectorIndices BlobRowSelector = 3
)

// BlobReadOrder controls whether planned reads preserve input order.
type BlobReadOrder int32

const (
	BlobOrderPreserve  BlobReadOrder = 0
	BlobOrderUnordered BlobReadOrder = 1
)

// BlobReadOptions configures planned blob reads. IOBufferSize is forwarded
// only when non-nil; an explicit zero is rejected natively. A nil
// BlobReadOptions selects native defaults.
type BlobReadOptions struct {
	IOBufferSize *uint64
	Order        BlobReadOrder
}

// BlobRangeRequest asks for a byte range from one selected row. Row is
// interpreted according to the BlobRowSelector.
type BlobRangeRequest struct {
	Row    uint64
	Offset uint64
	Length uint64
}

// ReadBlobs plans a whole-blob read and returns an incremental Arrow record
// reader. The schema is row_address: UInt64 (non-null), data: LargeBinary
// (nullable). A null blob produces a null data value; an empty blob produces a
// non-null empty value. Batches own their buffers and stay valid after the
// reader is released and after the dataset is closed.
//
// The context is checked before planning and between batches. Native planning
// and payload I/O cannot be interrupted once a call is in flight.
func (d *Dataset) ReadBlobs(ctx context.Context, column string, selector BlobRowSelector, rows []uint64, options *BlobReadOptions) (array.RecordReader, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateCString(column); err != nil {
		return nil, invalidError("read blobs", d.uri, "%v", err)
	}
	if err := d.acquire("read blobs"); err != nil {
		return nil, err
	}
	defer d.res.release()

	stream, err := d.native.ReadBlobs(column, int32(selector), rows, toFFIBlobReadOptions(options))
	if err != nil {
		return nil, wrapError("read blobs", d.uri, err)
	}
	if err := checkContext(ctx); err != nil {
		stream.Close()
		return nil, err
	}
	return newBlobRecordReader(ctx, stream), nil
}

// ReadBlobRanges plans per-row blob-range reads and returns an incremental
// Arrow record reader. The schema is request_index: UInt64 (non-null),
// row_address: UInt64 (non-null), offset: UInt64 (non-null), length: UInt64
// (non-null), data: LargeBinary (nullable). request_index preserves the
// zero-based input position for duplicate and unordered requests.
//
// The context is checked before planning and between batches. Native planning
// and payload I/O cannot be interrupted once a call is in flight.
func (d *Dataset) ReadBlobRanges(ctx context.Context, column string, selector BlobRowSelector, requests []BlobRangeRequest, options *BlobReadOptions) (array.RecordReader, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateCString(column); err != nil {
		return nil, invalidError("read blob ranges", d.uri, "%v", err)
	}
	if err := d.acquire("read blob ranges"); err != nil {
		return nil, err
	}
	defer d.res.release()

	nativeRequests := make([]ffi.BlobRangeRequest, len(requests))
	for i, request := range requests {
		nativeRequests[i] = ffi.BlobRangeRequest{
			Row:    request.Row,
			Offset: request.Offset,
			Length: request.Length,
		}
	}
	stream, err := d.native.ReadBlobRanges(column, int32(selector), nativeRequests, toFFIBlobReadOptions(options))
	if err != nil {
		return nil, wrapError("read blob ranges", d.uri, err)
	}
	if err := checkContext(ctx); err != nil {
		stream.Close()
		return nil, err
	}
	return newBlobRecordReader(ctx, stream), nil
}

func toFFIBlobReadOptions(options *BlobReadOptions) *ffi.BlobReadOptions {
	if options == nil {
		return nil
	}
	return &ffi.BlobReadOptions{
		IOBufferSize: options.IOBufferSize,
		Order:        int32(options.Order),
	}
}

type blobRecordReader struct {
	ctx    context.Context
	stream *ffi.StreamReader
	schema *arrow.Schema
	refs   atomic.Int64

	mu      sync.Mutex
	current arrow.RecordBatch
	err     error
	done    bool
}

func newBlobRecordReader(ctx context.Context, stream *ffi.StreamReader) *blobRecordReader {
	if ctx == nil {
		ctx = context.Background()
	}
	reader := &blobRecordReader{
		ctx:    ctx,
		stream: stream,
		schema: stream.Schema(),
	}
	reader.refs.Store(1)
	return reader
}

func (r *blobRecordReader) Retain() {
	r.refs.Add(1)
}

func (r *blobRecordReader) Release() {
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

func (r *blobRecordReader) Schema() *arrow.Schema {
	return r.schema
}

func (r *blobRecordReader) Next() bool {
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

func (r *blobRecordReader) RecordBatch() arrow.RecordBatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

func (r *blobRecordReader) Record() arrow.Record {
	return r.RecordBatch()
}

func (r *blobRecordReader) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

var _ array.RecordReader = (*blobRecordReader)(nil)
