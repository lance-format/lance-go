package file

import (
	"context"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/ffi"
	"github.com/lance-format/lance-go/internal/lifetime"
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
	// Session shares an existing Lance session's caches with a standalone
	// writer. Nil uses no shared cache. Session.OpenWriter rejects a non-nil
	// value because session writers use the file session's store.
	Session *lance.Session
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

func writerSession(opts *WriterOptions) *lance.Session {
	if opts == nil {
		return nil
	}
	return opts.Session
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
	// opMu serializes native calls on this writer; writers mutate shared native
	// state. Close coordination stays in res, so teardown remains single and
	// still waits for the operation holding opMu.
	opMu   sync.Mutex
	res    *lifetime.Resource
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

	session, releaseSession, err := acquireLanceSession("create file", uri, writerSession(opts))
	if err != nil {
		return nil, err
	}
	defer releaseSession()
	native, err := ffi.OpenFileWriter(uri, schema, rawOptions, accessor, session)
	if err != nil {
		return nil, wrapError("create file", uri, err)
	}
	return newWriter(native, uri), nil
}

func newWriter(native *ffi.FileWriter, uri string) *Writer {
	writer := &Writer{native: native, uri: uri}
	writer.res = lifetime.New(native.Close)
	return writer
}

func (w *Writer) acquire(op string) (*ffi.FileWriter, func(), error) {
	w.opMu.Lock()
	if w.res == nil || !w.res.Acquire() {
		w.opMu.Unlock()
		return nil, nil, closedError(op, w.uri)
	}
	return w.native, func() {
		w.res.Release()
		w.opMu.Unlock()
	}, nil
}

// Write writes one borrowed batch. The writer does not retain the batch after
// Write returns.
func (w *Writer) Write(ctx context.Context, batch arrow.RecordBatch) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	native, unlock, err := w.acquire("writer write")
	if err != nil {
		return err
	}
	defer unlock()
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
	native, unlock, err := w.acquire("writer schema metadata")
	if err != nil {
		return err
	}
	defer unlock()
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
	native, unlock, err := w.acquire("writer global buffer")
	if err != nil {
		return 0, err
	}
	defer unlock()
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
	native, unlock, err := w.acquire("writer finish")
	if err != nil {
		return WriteSummary{}, err
	}
	defer unlock()
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
	native, unlock, err := w.acquire("writer abort")
	if err != nil {
		return err
	}
	defer unlock()
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
	if w == nil || w.res == nil {
		return nil
	}
	w.res.Close()
	return nil
}
