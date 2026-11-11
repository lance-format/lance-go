package ffi

import (
	"fmt"
	"runtime"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
)

// FileWriter is an opaque native raw Lance-file writer.
type FileWriter struct {
	b      *bindings
	handle uintptr
}

// OpenFileWriter opens a standalone raw Lance-file writer. A nil schema
// creates a lazy writer that adopts the first batch schema. A nil storage
// accessor selects native None.
func OpenFileWriter(uri string, schema *arrow.Schema, options *FileWriterOptions, storage *StorageOptionsAccessor, session *Session) (*FileWriter, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	if err := validateCString(uri); err != nil {
		return nil, err
	}
	rawOptions, keep, err := buildFileWriterOptions(options)
	if err != nil {
		return nil, err
	}
	rawSchema, releaseSchema, err := exportOptionalSchema(schema)
	if err != nil {
		return nil, err
	}
	defer releaseSchema()

	runtime.LockOSThread()
	handle := b.fileWriterOpen(uri, rawSchema, rawOptions, storage.Handle(), SessionHandle(session))
	keep.keepAlive()
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_writer_open returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &FileWriter{b: b, handle: handle}, nil
}

// WriteBatch writes one borrowed Arrow batch.
func (w *FileWriter) WriteBatch(batch arrow.RecordBatch) error {
	handle := w.handle
	if handle == 0 {
		return ErrClosed
	}
	if batch == nil {
		return fmt.Errorf("ffi: nil record batch")
	}

	var rawArray cdata.ArrowArray
	if err := cdata.ExportRecordBatch(batch, &rawArray); err != nil {
		return fmt.Errorf("ffi: export record batch: %w", err)
	}
	var rawSchema cdata.ArrowSchema
	if err := cdata.ExportSchema(batch.Schema(), &rawSchema); err != nil {
		cdata.ReleaseArray(&rawArray)
		return fmt.Errorf("ffi: export schema: %w", err)
	}
	defer cdata.ReleaseSchema(&rawSchema)

	var nativeBatch uintptr
	runtime.LockOSThread()
	rc := w.b.batchFromArrow(&rawArray, &rawSchema, &nativeBatch)
	if rc != 0 {
		nativeErr := w.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_batch_from_arrow failed without setting an error")
	}
	runtime.UnlockOSThread()
	if nativeBatch == 0 {
		return fmt.Errorf("ffi: lance_batch_from_arrow returned NULL without setting an error")
	}
	defer w.b.batchFree(nativeBatch)

	runtime.LockOSThread()
	rc = w.b.fileWriterWriteBatch(handle, nativeBatch)
	if rc != 0 {
		nativeErr := w.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_file_writer_write_batch failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

// AddSchemaMetadata adds one schema metadata entry.
func (w *FileWriter) AddSchemaMetadata(key, value string) error {
	handle := w.handle
	if handle == 0 {
		return ErrClosed
	}
	if err := validateCString(key); err != nil {
		return err
	}
	if err := validateCString(value); err != nil {
		return err
	}

	runtime.LockOSThread()
	rc := w.b.fileWriterAddSchemaMetadata(handle, key, value)
	if rc != 0 {
		nativeErr := w.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_file_writer_add_schema_metadata failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

// AddGlobalBuffer appends a global buffer and returns its one-based index.
func (w *FileWriter) AddGlobalBuffer(data []byte) (uint32, error) {
	handle := w.handle
	if handle == 0 {
		return 0, ErrClosed
	}
	var ptr *byte
	if len(data) > 0 {
		ptr = &data[0]
	}
	var index uint32
	runtime.LockOSThread()
	rc := w.b.fileWriterAddGlobalBuffer(handle, ptr, uint64(len(data)), &index)
	if rc != 0 {
		nativeErr := w.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_file_writer_add_global_buffer failed without setting an error")
	}
	runtime.UnlockOSThread()
	return index, nil
}

// Finish finalizes the file and returns the native write summary.
func (w *FileWriter) Finish() (FileWriteSummary, error) {
	handle := w.handle
	if handle == 0 {
		return FileWriteSummary{}, ErrClosed
	}
	var raw fileWriteSummary
	runtime.LockOSThread()
	rc := w.b.fileWriterFinish(handle, &raw)
	if rc != 0 {
		nativeErr := w.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return FileWriteSummary{}, nativeErr
		}
		return FileWriteSummary{}, fmt.Errorf("ffi: lance_file_writer_finish failed without setting an error")
	}
	runtime.UnlockOSThread()
	return FileWriteSummary{NumRows: raw.NumRows, SizeBytes: raw.SizeBytes}, nil
}

// Abort forwards the native abort call.
func (w *FileWriter) Abort() error {
	handle := w.handle
	if handle == 0 {
		return ErrClosed
	}
	runtime.LockOSThread()
	rc := w.b.fileWriterAbort(handle)
	if rc != 0 {
		nativeErr := w.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_file_writer_abort failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

// Close drops the writer allocation. It is idempotent and does not imply a
// successful finish.
func (w *FileWriter) Close() {
	if w == nil || w.handle == 0 {
		return
	}
	handle := w.handle
	w.handle = 0
	w.b.fileWriterClose(handle)
}

// PackedBlobWriter is an opaque native packed blob sidecar writer.
type PackedBlobWriter struct {
	b      *bindings
	handle uintptr
}

// OpenPackedBlobWriter opens a packed blob writer relative to the session base.
func (s *FileSession) OpenPackedBlobWriter(dataFilePath string, blobID uint32) (*PackedBlobWriter, error) {
	handle := s.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(dataFilePath); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	nativeHandle := s.b.fileSessionOpenPackedBlobWriter(handle, dataFilePath, blobID)
	if nativeHandle == 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_session_open_packed_blob_writer returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &PackedBlobWriter{b: s.b, handle: nativeHandle}, nil
}

// BlobID returns the native blob id.
func (w *PackedBlobWriter) BlobID() uint32 {
	if w == nil || w.handle == 0 {
		return 0
	}
	return w.b.packedBlobWriterBlobID(w.handle)
}

// Path returns the native sidecar path.
func (w *PackedBlobWriter) Path() (string, error) {
	return packedBlobPath(w.b, w.handle)
}

func packedBlobPath(b *bindings, handle uintptr) (string, error) {
	if handle == 0 {
		return "", ErrClosed
	}
	runtime.LockOSThread()
	ptr := b.packedBlobWriterPath(handle)
	if ptr == nil {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_packed_blob_writer_path returned NULL without setting an error")
	}
	path := goString(ptr)
	b.freeString(ptr)
	runtime.UnlockOSThread()
	return path, nil
}

// Write appends one payload.
func (w *PackedBlobWriter) Write(data []byte) error {
	if w == nil || w.handle == 0 {
		return ErrClosed
	}
	var ptr *byte
	if len(data) > 0 {
		ptr = &data[0]
	}
	runtime.LockOSThread()
	rc := w.b.packedBlobWriterWriteBlob(w.handle, ptr, uint64(len(data)))
	if rc != 0 {
		nativeErr := w.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_packed_blob_writer_write_blob failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

// WriteBlobs appends one borrowed Arrow array of Binary, LargeBinary, or
// FixedSizeBinary payloads. The Arrow bridge rejects BinaryView. Native code
// consumes the exported array on every return, while payloads itself remains
// owned by the caller.
func (w *PackedBlobWriter) WriteBlobs(payloads arrow.Array) error {
	if w == nil || w.handle == 0 {
		return ErrClosed
	}
	if payloads == nil {
		return fmt.Errorf("ffi: nil blob payload array")
	}

	var rawPayloads cdata.ArrowArray
	if err := cdata.ExportArray(payloads, &rawPayloads); err != nil {
		return fmt.Errorf("ffi: export blob payload array: %w", err)
	}
	var rawSchema cdata.ArrowSchema
	if err := cdata.ExportField(arrow.Field{Name: "payloads", Type: payloads.DataType(), Nullable: true}, &rawSchema); err != nil {
		cdata.ReleaseArray(&rawPayloads)
		return fmt.Errorf("ffi: export blob payload field: %w", err)
	}
	defer cdata.ReleaseSchema(&rawSchema)

	runtime.LockOSThread()
	rc := w.b.packedBlobWriterWriteBlobs(w.handle, &rawPayloads, &rawSchema)
	if rc != 0 {
		nativeErr := w.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_packed_blob_writer_write_blobs failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

// FinishArray consumes the native writer and returns the prepared descriptor.
func (w *PackedBlobWriter) FinishArray(fieldName string) (arrow.Field, arrow.Array, error) {
	if w == nil || w.handle == 0 {
		return arrow.Field{}, nil, ErrClosed
	}
	if err := validateCString(fieldName); err != nil {
		return arrow.Field{}, nil, err
	}
	return finishBlobArray(w.b, w.handle, fieldName, w.b.packedBlobWriterFinishArray)
}

// Close frees the native allocation. It is idempotent.
func (w *PackedBlobWriter) Close() {
	if w == nil || w.handle == 0 {
		return
	}
	handle := w.handle
	w.handle = 0
	w.b.packedBlobWriterClose(handle)
}

// DedicatedBlobWriter is an opaque native dedicated blob sidecar writer.
type DedicatedBlobWriter struct {
	b      *bindings
	handle uintptr
}

// OpenDedicatedBlobWriter opens a dedicated blob writer relative to the
// session base.
func (s *FileSession) OpenDedicatedBlobWriter(dataFilePath string, blobID uint32) (*DedicatedBlobWriter, error) {
	handle := s.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(dataFilePath); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	nativeHandle := s.b.fileSessionOpenDedicatedBlobWriter(handle, dataFilePath, blobID)
	if nativeHandle == 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_session_open_dedicated_blob_writer returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &DedicatedBlobWriter{b: s.b, handle: nativeHandle}, nil
}

// BlobID returns the native blob id.
func (w *DedicatedBlobWriter) BlobID() uint32 {
	if w == nil || w.handle == 0 {
		return 0
	}
	return w.b.dedicatedBlobWriterBlobID(w.handle)
}

// Path returns the native sidecar path.
func (w *DedicatedBlobWriter) Path() (string, error) {
	if w == nil || w.handle == 0 {
		return "", ErrClosed
	}
	runtime.LockOSThread()
	ptr := w.b.dedicatedBlobWriterPath(w.handle)
	if ptr == nil {
		nativeErr := w.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_dedicated_blob_writer_path returned NULL without setting an error")
	}
	path := goString(ptr)
	w.b.freeString(ptr)
	runtime.UnlockOSThread()
	return path, nil
}

// Write appends bytes to the dedicated sidecar.
func (w *DedicatedBlobWriter) Write(data []byte) error {
	if w == nil || w.handle == 0 {
		return ErrClosed
	}
	var ptr *byte
	if len(data) > 0 {
		ptr = &data[0]
	}
	runtime.LockOSThread()
	rc := w.b.dedicatedBlobWriterWrite(w.handle, ptr, uint64(len(data)))
	if rc != 0 {
		nativeErr := w.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dedicated_blob_writer_write failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

// FinishArray consumes the native writer and returns the prepared descriptor.
func (w *DedicatedBlobWriter) FinishArray(fieldName string) (arrow.Field, arrow.Array, error) {
	if w == nil || w.handle == 0 {
		return arrow.Field{}, nil, ErrClosed
	}
	if err := validateCString(fieldName); err != nil {
		return arrow.Field{}, nil, err
	}
	return finishBlobArray(w.b, w.handle, fieldName, w.b.dedicatedBlobWriterFinishArray)
}

// Close frees the native allocation. It is idempotent.
func (w *DedicatedBlobWriter) Close() {
	if w == nil || w.handle == 0 {
		return
	}
	handle := w.handle
	w.handle = 0
	w.b.dedicatedBlobWriterClose(handle)
}

func finishBlobArray(
	b *bindings,
	handle uintptr,
	fieldName string,
	finish func(writer uintptr, field string, outArray *cdata.ArrowArray, outSchema *cdata.ArrowSchema) int32,
) (arrow.Field, arrow.Array, error) {
	rawArray := new(cdata.ArrowArray)
	rawSchema := new(cdata.ArrowSchema)

	runtime.LockOSThread()
	rc := finish(handle, fieldName, rawArray, rawSchema)
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if rawArray.Release != 0 {
			cdata.ReleaseArray(rawArray)
		}
		if rawSchema.Release != 0 {
			cdata.ReleaseSchema(rawSchema)
		}
		if nativeErr != nil {
			return arrow.Field{}, nil, nativeErr
		}
		return arrow.Field{}, nil, fmt.Errorf("ffi: blob writer finish_array failed without setting an error")
	}
	runtime.UnlockOSThread()

	field, err := cdata.ImportField(rawSchema)
	cdata.ReleaseSchema(rawSchema)
	if err != nil {
		if rawArray.Release != 0 {
			cdata.ReleaseArray(rawArray)
		}
		return arrow.Field{}, nil, fmt.Errorf("ffi: import blob field: %w", err)
	}
	array, err := cdata.ImportArray(rawArray, field.Type)
	if err != nil {
		return arrow.Field{}, nil, fmt.Errorf("ffi: import blob array: %w", err)
	}
	return field, array, nil
}
