package file

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
	"github.com/lance-format/lance-go/internal/lifetime"
)

// PackedBlobWriter writes one packed blob sidecar. It is safe for concurrent
// use. Close waits for in-flight calls and is idempotent.
type PackedBlobWriter struct {
	res    *lifetime.Resource
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
	packed := newPackedBlobWriter(writer, dataFilePath)
	return packed, nil
}

func newPackedBlobWriter(native *ffi.PackedBlobWriter, path string) *PackedBlobWriter {
	packed := &PackedBlobWriter{native: native, path: path}
	packed.res = lifetime.New(native.Close)
	return packed
}

func (w *PackedBlobWriter) acquire(op string) (*ffi.PackedBlobWriter, func(), error) {
	if w.res == nil || !w.res.Acquire() {
		return nil, nil, closedError(op, w.path)
	}
	return w.native, w.res.Release, nil
}

// BlobID returns the native blob id.
func (w *PackedBlobWriter) BlobID() uint32 {
	native, unlock, err := w.acquire("packed blob writer blob id")
	if err != nil {
		return 0
	}
	defer unlock()
	return native.BlobID()
}

// Path returns the native sidecar path.
func (w *PackedBlobWriter) Path() (string, error) {
	native, unlock, err := w.acquire("packed blob writer path")
	if err != nil {
		return "", err
	}
	defer unlock()

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
	native, unlock, err := w.acquire("packed blob writer write")
	if err != nil {
		return err
	}
	defer unlock()
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
	native, unlock, err := w.acquire("packed blob writer write blobs")
	if err != nil {
		return err
	}
	defer unlock()
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
	native, unlock, err := w.acquire("packed blob writer finish")
	if err != nil {
		return arrow.Field{}, nil, err
	}
	defer unlock()
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
	if w == nil || w.res == nil {
		return nil
	}
	w.res.Close()
	return nil
}

// DedicatedBlobWriter writes one dedicated blob sidecar. It is safe for
// concurrent use. Close waits for in-flight calls and is idempotent.
type DedicatedBlobWriter struct {
	res    *lifetime.Resource
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
	dedicated := newDedicatedBlobWriter(writer, dataFilePath)
	return dedicated, nil
}

func newDedicatedBlobWriter(native *ffi.DedicatedBlobWriter, path string) *DedicatedBlobWriter {
	dedicated := &DedicatedBlobWriter{native: native, path: path}
	dedicated.res = lifetime.New(native.Close)
	return dedicated
}

func (w *DedicatedBlobWriter) acquire(op string) (*ffi.DedicatedBlobWriter, func(), error) {
	if w.res == nil || !w.res.Acquire() {
		return nil, nil, closedError(op, w.path)
	}
	return w.native, w.res.Release, nil
}

// BlobID returns the native blob id.
func (w *DedicatedBlobWriter) BlobID() uint32 {
	native, unlock, err := w.acquire("dedicated blob writer blob id")
	if err != nil {
		return 0
	}
	defer unlock()
	return native.BlobID()
}

// Path returns the native sidecar path.
func (w *DedicatedBlobWriter) Path() (string, error) {
	native, unlock, err := w.acquire("dedicated blob writer path")
	if err != nil {
		return "", err
	}
	defer unlock()

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
	native, unlock, err := w.acquire("dedicated blob writer write")
	if err != nil {
		return err
	}
	defer unlock()
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
	native, unlock, err := w.acquire("dedicated blob writer finish")
	if err != nil {
		return arrow.Field{}, nil, err
	}
	defer unlock()
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
	if w == nil || w.res == nil {
		return nil
	}
	w.res.Close()
	return nil
}
