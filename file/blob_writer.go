package file

import (
	"context"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
)

// PackedBlobWriter writes one packed blob sidecar. It is safe for concurrent
// use. Close waits for in-flight calls and is idempotent.
type PackedBlobWriter struct {
	mu     sync.Mutex
	closed bool
	native *ffi.PackedBlobWriter
	path   string
}

// OpenPackedBlobWriter opens a packed blob writer for dataFilePath and blobID.
func (s *Session) OpenPackedBlobWriter(ctx context.Context, dataFilePath string, blobID uint32) (*PackedBlobWriter, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	native, unlock, err := s.acquire("session open packed blob writer")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	writer, err := native.OpenPackedBlobWriter(dataFilePath, blobID)
	if err != nil {
		return nil, wrapError("session open packed blob writer", dataFilePath, err)
	}
	return &PackedBlobWriter{native: writer, path: dataFilePath}, nil
}

func (w *PackedBlobWriter) lock(op string) (*ffi.PackedBlobWriter, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, closedError(op, w.path)
	}
	return w.native, nil
}

// BlobID returns the native blob id.
func (w *PackedBlobWriter) BlobID() uint32 {
	native, err := w.lock("packed blob writer blob id")
	if err != nil {
		return 0
	}
	defer w.mu.Unlock()
	return native.BlobID()
}

// Path returns the native sidecar path.
func (w *PackedBlobWriter) Path() (string, error) {
	native, err := w.lock("packed blob writer path")
	if err != nil {
		return "", err
	}
	defer w.mu.Unlock()

	path, err := native.Path()
	if err != nil {
		return "", wrapError("packed blob writer path", w.path, err)
	}
	return path, nil
}

// Write appends one payload.
func (w *PackedBlobWriter) Write(ctx context.Context, data []byte) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	native, err := w.lock("packed blob writer write")
	if err != nil {
		return err
	}
	defer w.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}

	if err := native.Write(data); err != nil {
		return wrapError("packed blob writer write", w.path, err)
	}
	return nil
}

// WriteBlobs appends one Arrow array of Binary, LargeBinary, or
// FixedSizeBinary payloads. Bytes from payloads are copied before it returns,
// so the array remains owned and usable by the caller. Nulls and non-null
// empty payloads keep their distinct meanings in the finished descriptors.
func (w *PackedBlobWriter) WriteBlobs(ctx context.Context, payloads arrow.Array) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	native, err := w.lock("packed blob writer write blobs")
	if err != nil {
		return err
	}
	defer w.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}

	if err := native.WriteBlobs(payloads); err != nil {
		return wrapError("packed blob writer write blobs", w.path, err)
	}
	return nil
}

// FinishArray consumes the native writer and returns its prepared descriptor.
// The returned array owns its buffers. Call Close to release the allocation.
func (w *PackedBlobWriter) FinishArray(ctx context.Context, fieldName string) (arrow.Field, arrow.Array, error) {
	if err := checkContext(ctx); err != nil {
		return arrow.Field{}, nil, err
	}
	native, err := w.lock("packed blob writer finish")
	if err != nil {
		return arrow.Field{}, nil, err
	}
	defer w.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return arrow.Field{}, nil, err
	}

	field, array, err := native.FinishArray(fieldName)
	if err != nil {
		return arrow.Field{}, nil, wrapError("packed blob writer finish", w.path, err)
	}
	return field, array, nil
}

// Close frees the native allocation and waits for in-flight calls.
func (w *PackedBlobWriter) Close() error {
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

// DedicatedBlobWriter writes one dedicated blob sidecar. It is safe for
// concurrent use. Close waits for in-flight calls and is idempotent.
type DedicatedBlobWriter struct {
	mu     sync.Mutex
	closed bool
	native *ffi.DedicatedBlobWriter
	path   string
}

// OpenDedicatedBlobWriter opens a dedicated blob writer for dataFilePath and
// blobID.
func (s *Session) OpenDedicatedBlobWriter(ctx context.Context, dataFilePath string, blobID uint32) (*DedicatedBlobWriter, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	native, unlock, err := s.acquire("session open dedicated blob writer")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	writer, err := native.OpenDedicatedBlobWriter(dataFilePath, blobID)
	if err != nil {
		return nil, wrapError("session open dedicated blob writer", dataFilePath, err)
	}
	return &DedicatedBlobWriter{native: writer, path: dataFilePath}, nil
}

func (w *DedicatedBlobWriter) lock(op string) (*ffi.DedicatedBlobWriter, error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, closedError(op, w.path)
	}
	return w.native, nil
}

// BlobID returns the native blob id.
func (w *DedicatedBlobWriter) BlobID() uint32 {
	native, err := w.lock("dedicated blob writer blob id")
	if err != nil {
		return 0
	}
	defer w.mu.Unlock()
	return native.BlobID()
}

// Path returns the native sidecar path.
func (w *DedicatedBlobWriter) Path() (string, error) {
	native, err := w.lock("dedicated blob writer path")
	if err != nil {
		return "", err
	}
	defer w.mu.Unlock()

	path, err := native.Path()
	if err != nil {
		return "", wrapError("dedicated blob writer path", w.path, err)
	}
	return path, nil
}

// Write appends bytes to the dedicated sidecar.
func (w *DedicatedBlobWriter) Write(ctx context.Context, data []byte) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	native, err := w.lock("dedicated blob writer write")
	if err != nil {
		return err
	}
	defer w.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}

	if err := native.Write(data); err != nil {
		return wrapError("dedicated blob writer write", w.path, err)
	}
	return nil
}

// FinishArray consumes the native writer and returns its prepared descriptor.
// The returned array owns its buffers. Call Close to release the allocation.
func (w *DedicatedBlobWriter) FinishArray(ctx context.Context, fieldName string) (arrow.Field, arrow.Array, error) {
	if err := checkContext(ctx); err != nil {
		return arrow.Field{}, nil, err
	}
	native, err := w.lock("dedicated blob writer finish")
	if err != nil {
		return arrow.Field{}, nil, err
	}
	defer w.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return arrow.Field{}, nil, err
	}

	field, array, err := native.FinishArray(fieldName)
	if err != nil {
		return arrow.Field{}, nil, wrapError("dedicated blob writer finish", w.path, err)
	}
	return field, array, nil
}

// Close frees the native allocation and waits for in-flight calls.
func (w *DedicatedBlobWriter) Close() error {
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
