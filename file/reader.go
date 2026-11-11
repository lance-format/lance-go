package file

import (
	"context"
	"fmt"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
)

// ReaderOptions configures a raw file reader. Pointer fields distinguish an
// unset option from an explicit zero value.
type ReaderOptions struct {
	// Columns projects the file. A nil slice leaves projection unset; a
	// non-nil empty slice requests an explicit empty projection.
	Columns []string
	// CacheMode defaults to CacheNone.
	CacheMode CacheMode

	ReadChunkSize        *uint64
	BatchSizeBytes       *uint64
	CacheRepetitionIndex *bool
	ValidateOnDecode     *bool
	InlineScheduling     *bool

	// Scheduler is nil for native defaults. Session.OpenReader rejects a
	// non-nil value because session readers inherit the session scheduler.
	Scheduler *SchedulerConfig
	// StorageOptions is nil for native defaults. Session.OpenReader rejects a
	// non-nil value because session readers use the session store.
	StorageOptions *StorageOptions
}

func (o *ReaderOptions) ffiOptions() (*ffi.FileReaderOptions, *ffi.FileSchedulerConfig, error) {
	if o == nil {
		return nil, nil, nil
	}
	raw := &ffi.FileReaderOptions{
		Columns:              o.Columns,
		CacheMode:            ffi.FileCacheMode(o.CacheMode),
		ReadChunkSize:        o.ReadChunkSize,
		BatchSizeBytes:       o.BatchSizeBytes,
		CacheRepetitionIndex: o.CacheRepetitionIndex,
		ValidateOnDecode:     o.ValidateOnDecode,
		InlineScheduling:     o.InlineScheduling,
	}
	var scheduler *ffi.FileSchedulerConfig
	if o.Scheduler != nil {
		scheduler = &ffi.FileSchedulerConfig{
			IOBufferSizeBytes: o.Scheduler.IOBufferSizeBytes,
			LiteMode:          int32(o.Scheduler.LiteMode),
		}
	}
	return raw, scheduler, nil
}

// Reader reads one raw Lance file. It is safe for concurrent use. Close waits
// for in-flight calls and is idempotent. Record readers and metadata snapshots
// already returned remain usable after Close.
type Reader struct {
	mu     sync.RWMutex
	closed bool
	native *ffi.FileReader
	uri    string
}

// Open opens uri as a raw Lance file.
func Open(ctx context.Context, uri string, opts *ReaderOptions) (*Reader, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if uri == "" {
		return nil, invalidError("open file", uri, "uri must not be empty")
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	rawOptions, scheduler, err := opts.ffiOptions()
	if err != nil {
		return nil, wrapError("open file", uri, err)
	}
	unlockStorage, accessor, err := lockStorage(storageOptions(opts))
	if err != nil {
		return nil, err
	}
	defer unlockStorage()

	native, err := ffi.OpenFileReader(uri, accessor, rawOptions, scheduler)
	if err != nil {
		return nil, wrapError("open file", uri, err)
	}
	return &Reader{native: native, uri: uri}, nil
}

func storageOptions(opts *ReaderOptions) *StorageOptions {
	if opts == nil {
		return nil
	}
	return opts.StorageOptions
}

func (r *Reader) acquire(op string) (*ffi.FileReader, func(), error) {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return nil, nil, closedError(op, r.uri)
	}
	return r.native, r.mu.RUnlock, nil
}

// Close releases the reader allocation and waits for in-flight calls. Streams
// already returned by ReadAll, ReadRange, or ReadIndices remain usable.
func (r *Reader) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	native := r.native
	r.native = nil
	r.mu.Unlock()
	native.Close()
	return nil
}

// NumRows returns the file row count.
func (r *Reader) NumRows() (uint64, error) {
	native, unlock, err := r.acquire("reader num rows")
	if err != nil {
		return 0, err
	}
	defer unlock()

	rows, err := native.NumRows()
	if err != nil {
		return 0, wrapError("reader num rows", r.uri, err)
	}
	return rows, nil
}

// Metadata returns an independent metadata snapshot.
func (r *Reader) Metadata() (*Metadata, error) {
	native, unlock, err := r.acquire("reader metadata")
	if err != nil {
		return nil, err
	}
	defer unlock()

	metadata, err := native.Metadata()
	if err != nil {
		return nil, wrapError("reader metadata", r.uri, err)
	}
	return &Metadata{native: metadata, uri: r.uri}, nil
}

// Schema returns the complete file schema.
func (r *Reader) Schema() (*arrow.Schema, error) {
	metadata, err := r.Metadata()
	if err != nil {
		return nil, err
	}
	defer metadata.Close()
	return metadata.Schema()
}

// Statistics returns an independent statistics snapshot.
func (r *Reader) Statistics() (*Statistics, error) {
	native, unlock, err := r.acquire("reader statistics")
	if err != nil {
		return nil, err
	}
	defer unlock()

	statistics, err := native.Statistics()
	if err != nil {
		return nil, wrapError("reader statistics", r.uri, err)
	}
	return &Statistics{native: statistics, uri: r.uri}, nil
}

// ReadGlobalBuffer reads a global buffer. Index 0 is the serialized schema;
// writer-added buffers start at 1.
func (r *Reader) ReadGlobalBuffer(ctx context.Context, index uint32) ([]byte, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	native, unlock, err := r.acquire("reader global buffer")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	data, err := native.ReadGlobalBuffer(index)
	if err != nil {
		return nil, wrapError("reader global buffer", r.uri, err)
	}
	return data, nil
}

// ReadAll streams all rows. The returned reader is independent of this Reader
// and remains valid after Close.
func (r *Reader) ReadAll(ctx context.Context, opts ReadOptions) (array.RecordReader, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	native, unlock, err := r.acquire("read all")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	stream, err := native.ReadAll(opts.BatchSize, opts.BatchReadahead)
	if err != nil {
		return nil, wrapError("read all", r.uri, err)
	}
	return newRecordReader(ctx, stream), nil
}

// ReadRange streams numRows rows starting at offset.
func (r *Reader) ReadRange(ctx context.Context, offset, numRows uint64, opts ReadOptions) (array.RecordReader, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if numRows > ^uint64(0)-offset {
		return nil, invalidError("read range", r.uri, "offset+numRows overflows uint64")
	}
	native, unlock, err := r.acquire("read range")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	stream, err := native.ReadRange(offset, numRows, opts.BatchSize, opts.BatchReadahead)
	if err != nil {
		return nil, wrapError("read range", r.uri, err)
	}
	return newRecordReader(ctx, stream), nil
}

// ReadIndices streams rows in the given order.
func (r *Reader) ReadIndices(ctx context.Context, indices []uint32, opts ReadOptions) (array.RecordReader, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	native, unlock, err := r.acquire("read indices")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	stream, err := native.ReadIndices(indices, opts.BatchSize, opts.BatchReadahead)
	if err != nil {
		return nil, wrapError("read indices", r.uri, err)
	}
	return newRecordReader(ctx, stream), nil
}

// IOStats returns cumulative native IO counters.
func (r *Reader) IOStats() (ScanStats, error) {
	native, unlock, err := r.acquire("reader io stats")
	if err != nil {
		return ScanStats{}, err
	}
	defer unlock()

	stats, err := native.IOStats()
	if err != nil {
		return ScanStats{}, wrapError("reader io stats", r.uri, err)
	}
	return ScanStats{IOPS: stats.IOPS, Requests: stats.Requests, BytesRead: stats.BytesRead}, nil
}

// ScanStats is a native scan counter snapshot.
type ScanStats struct {
	IOPS      uint64
	Requests  uint64
	BytesRead uint64
}

func (r *Reader) String() string {
	if r == nil {
		return "file.Reader(nil)"
	}
	return fmt.Sprintf("file.Reader(%s)", r.uri)
}
