package file

import (
	"context"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
)

// WriterOptions configures a raw file writer. Pointer fields distinguish an
// unset option from an explicit zero value.
type WriterOptions struct {
	DataCacheBytes    *uint64
	MaxPageBytes      *uint64
	KeepOriginalArray *bool
	// Version is the native format version selector. Empty uses native default.
	Version string
	// StorageOptions is nil for native defaults. Session.OpenWriter rejects a
	// non-nil value because session writers use the session store.
	StorageOptions *StorageOptions
}

func (o *WriterOptions) ffiOptions() (*ffi.FileWriterOptions, error) {
	if o == nil {
		return nil, nil
	}
	return &ffi.FileWriterOptions{
		DataCacheBytes:    o.DataCacheBytes,
		MaxPageBytes:      o.MaxPageBytes,
		KeepOriginalArray: o.KeepOriginalArray,
		Version:           o.Version,
	}, nil
}

func writerStorageOptions(opts *WriterOptions) *StorageOptions {
	if opts == nil {
		return nil
	}
	return opts.StorageOptions
}

// WriteSummary is the native result of a successful Finish.
type WriteSummary struct {
	NumRows   uint64
	SizeBytes uint64
}

// Writer writes one raw Lance file. It is safe for concurrent use. Close waits
// for in-flight calls and is idempotent. Close does not imply a successful
// Finish.
type Writer struct {
	mu     sync.Mutex
	closed bool
	native *ffi.FileWriter
	uri    string
}

// Create creates uri as a raw Lance file. A nil schema creates a lazy writer
// that adopts the first batch schema.
func Create(ctx context.Context, uri string, schema *arrow.Schema, opts *WriterOptions) (*Writer, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if uri == "" {
		return nil, invalidError("create file", uri, "uri must not be empty")
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	rawOptions, err := opts.ffiOptions()
	if err != nil {
		return nil, wrapError("create file", uri, err)
	}
	unlockStorage, accessor, err := lockStorage(writerStorageOptions(opts))
	if err != nil {
		return nil, err
	}
	defer unlockStorage()

	native, err := ffi.OpenFileWriter(uri, schema, rawOptions, accessor)
	if err != nil {
		return nil, wrapError("create file", uri, err)
	}
	return &Writer{native: native, uri: uri}, nil
}

func (w *Writer) lock(op string) (*ffi.FileWriter, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, closedError(op, w.uri)
	}
	return w.native, nil
}

// Write writes one borrowed batch. The writer does not retain the batch after
// Write returns.
func (w *Writer) Write(ctx context.Context, batch arrow.RecordBatch) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	native, err := w.lock("writer write")
	if err != nil {
		return err
	}
	defer w.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}

	if err := native.WriteBatch(batch); err != nil {
		return wrapError("writer write", w.uri, err)
	}
	return nil
}

// AddSchemaMetadata adds one schema metadata entry.
func (w *Writer) AddSchemaMetadata(ctx context.Context, key, value string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	native, err := w.lock("writer schema metadata")
	if err != nil {
		return err
	}
	defer w.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}

	if err := native.AddSchemaMetadata(key, value); err != nil {
		return wrapError("writer schema metadata", w.uri, err)
	}
	return nil
}

// AddGlobalBuffer appends a global buffer and returns its one-based index.
func (w *Writer) AddGlobalBuffer(ctx context.Context, data []byte) (uint32, error) {
	if err := checkContext(ctx); err != nil {
		return 0, err
	}
	native, err := w.lock("writer global buffer")
	if err != nil {
		return 0, err
	}
	defer w.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return 0, err
	}

	index, err := native.AddGlobalBuffer(data)
	if err != nil {
		return 0, wrapError("writer global buffer", w.uri, err)
	}
	return index, nil
}

// Finish finalizes the file. The writer allocation stays open until Close.
func (w *Writer) Finish(ctx context.Context) (WriteSummary, error) {
	if err := checkContext(ctx); err != nil {
		return WriteSummary{}, err
	}
	native, err := w.lock("writer finish")
	if err != nil {
		return WriteSummary{}, err
	}
	defer w.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return WriteSummary{}, err
	}

	summary, err := native.Finish()
	if err != nil {
		return WriteSummary{}, wrapError("writer finish", w.uri, err)
	}
	return WriteSummary{NumRows: summary.NumRows, SizeBytes: summary.SizeBytes}, nil
}

// Abort forwards the native abort call. The writer allocation stays open
// until Close.
func (w *Writer) Abort(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	native, err := w.lock("writer abort")
	if err != nil {
		return err
	}
	defer w.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}

	if err := native.Abort(); err != nil {
		return wrapError("writer abort", w.uri, err)
	}
	return nil
}

// Close drops the writer allocation and waits for in-flight calls. It does not
// imply a successful finish.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	native := w.native
	w.native = nil
	w.mu.Unlock()
	native.Close()
	return nil
}
