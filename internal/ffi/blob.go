package ffi

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
)

// BlobRange is a byte range within one blob.
type BlobRange struct {
	Offset uint64
	Length uint64
}

// BlobRangeRequest asks for a byte range from one row selected by a blob read.
type BlobRangeRequest struct {
	Row    uint64
	Offset uint64
	Length uint64
}

// BlobReadOptions mirrors LanceBlobReadOptions. A nil pointer selects native
// defaults; an explicit zero IOBufferSize is forwarded and rejected natively.
type BlobReadOptions struct {
	IOBufferSize *uint64
	Order        int32
}

type blobRange struct {
	Offset uint64
	Length uint64
}

type blobRangeRequest struct {
	Row    uint64
	Offset uint64
	Length uint64
}

type blobFieldOptions struct {
	HasInlineSizeThreshold    bool
	InlineSizeThreshold       uint64
	HasDedicatedSizeThreshold bool
	DedicatedSizeThreshold    uint64
}

type blobReadOptions struct {
	HasIOBufferSize bool
	IOBufferSize    uint64
	Order           int32
}

// BlobFile is a native blob handle. It is independent of the dataset that
// produced it and owns the state needed to read the blob after dataset close.
// Callers serialize access through the public BlobFile.
type BlobFile struct {
	b      *bindings
	handle uintptr
}

func newBlobFile(b *bindings, handle uintptr) *BlobFile {
	return &BlobFile{b: b, handle: handle}
}

func (f *BlobFile) closed() bool {
	return f == nil || f.handle == 0
}

// Close frees the native handle allocation. It does not perform a native
// close; the public wrapper only calls it after no operation is in flight.
func (f *BlobFile) Close() {
	if f == nil {
		return
	}
	handle := f.handle
	f.handle = 0
	if handle != 0 {
		f.b.blobFileClose(handle)
	}
}

// Size returns the blob length. A zero size is a valid empty blob; a native
// error is returned separately.
func (f *BlobFile) Size() (uint64, error) {
	if f.closed() {
		return 0, ErrClosed
	}
	runtime.LockOSThread()
	// Size has no status return and zero is also a valid empty blob. Drop any
	// stale thread-local error before the call, then read only the error this
	// call may have set.
	_ = f.b.takeError()
	size := f.b.blobFileSize(f.handle)
	err := f.b.takeError()
	runtime.UnlockOSThread()
	if err != nil {
		return 0, err
	}
	return size, nil
}

// ReadUpTo reads at most len(dst) bytes from the cursor and returns the number
// of bytes read. A read at or past the end returns zero without an error.
func (f *BlobFile) ReadUpTo(dst []byte) (uint64, error) {
	if f.closed() {
		return 0, ErrClosed
	}
	var ptr *byte
	if len(dst) > 0 {
		ptr = &dst[0]
	}
	var n uint64
	runtime.LockOSThread()
	rc := f.b.blobFileReadUpTo(f.handle, ptr, uint64(len(dst)), &n)
	var err error
	if rc != 0 {
		err = f.b.takeError()
		if err == nil {
			err = fmt.Errorf("ffi: lance_blob_file_read_up_to failed without setting an error")
		}
	}
	runtime.UnlockOSThread()
	runtime.KeepAlive(dst)
	if err != nil {
		return 0, err
	}
	if n > uint64(len(dst)) {
		return 0, fmt.Errorf("ffi: lance_blob_file_read_up_to reported %d bytes for a %d-byte buffer", n, len(dst))
	}
	return n, nil
}

// ReadRange reads exactly len(dst) bytes at offset without moving the cursor.
func (f *BlobFile) ReadRange(offset uint64, dst []byte) error {
	if f.closed() {
		return ErrClosed
	}
	var ptr *byte
	if len(dst) > 0 {
		ptr = &dst[0]
	}
	runtime.LockOSThread()
	rc := f.b.blobFileReadRange(f.handle, offset, ptr, uint64(len(dst)))
	err := f.status("lance_blob_file_read_range", rc)
	runtime.UnlockOSThread()
	runtime.KeepAlive(dst)
	return err
}

// ReadRanges reads several ranges without moving the cursor. The native call
// validates the complete request before writing any destination.
func (f *BlobFile) ReadRanges(ranges []BlobRange, destinations [][]byte) error {
	if f.closed() {
		return ErrClosed
	}
	if len(ranges) != len(destinations) {
		return fmt.Errorf("ffi: blob range count %d does not match destination count %d", len(ranges), len(destinations))
	}
	rawRanges := make([]blobRange, len(ranges))
	ptrs := make([]unsafe.Pointer, len(destinations))
	lens := make([]uintptr, len(destinations))
	for i, r := range ranges {
		rawRanges[i] = blobRange{Offset: r.Offset, Length: r.Length}
		if len(destinations[i]) > 0 {
			ptrs[i] = unsafe.Pointer(&destinations[i][0])
		}
		lens[i] = uintptr(len(destinations[i]))
	}

	runtime.LockOSThread()
	rc := f.b.blobFileReadRanges(f.handle, rawRanges, uint64(len(rawRanges)), ptrs, lens)
	err := f.status("lance_blob_file_read_ranges", rc)
	runtime.UnlockOSThread()
	runtime.KeepAlive(ranges)
	runtime.KeepAlive(destinations)
	runtime.KeepAlive(rawRanges)
	runtime.KeepAlive(ptrs)
	runtime.KeepAlive(lens)
	return err
}

// Seek moves the cursor to pos. Positions past the end are allowed.
func (f *BlobFile) Seek(pos uint64) error {
	if f.closed() {
		return ErrClosed
	}
	runtime.LockOSThread()
	rc := f.b.blobFileSeek(f.handle, pos)
	err := f.status("lance_blob_file_seek", rc)
	runtime.UnlockOSThread()
	return err
}

// Tell returns the cursor position.
func (f *BlobFile) Tell() (uint64, error) {
	if f.closed() {
		return 0, ErrClosed
	}
	var pos uint64
	runtime.LockOSThread()
	rc := f.b.blobFileTell(f.handle, &pos)
	err := f.status("lance_blob_file_tell", rc)
	runtime.UnlockOSThread()
	if err != nil {
		return 0, err
	}
	return pos, nil
}

// SetBufferSize sets the native read-ahead buffer size. Zero disables
// read-ahead; the setting never changes the visible bytes or cursor.
func (f *BlobFile) SetBufferSize(size uint64) error {
	if f.closed() {
		return ErrClosed
	}
	runtime.LockOSThread()
	rc := f.b.blobFileSetBufferSize(f.handle, size)
	err := f.status("lance_blob_file_set_buffer_size", rc)
	runtime.UnlockOSThread()
	return err
}

func (f *BlobFile) status(operation string, rc int32) error {
	if rc == 0 {
		return nil
	}
	if nativeErr := f.b.takeError(); nativeErr != nil {
		return nativeErr
	}
	return fmt.Errorf("ffi: %s failed without setting an error", operation)
}

func (d *Dataset) TakeBlobs(rowIDs []uint64, column string) ([]*BlobFile, error) {
	if err := validateCString(column); err != nil {
		return nil, err
	}
	files, err := d.takeBlobs("lance_dataset_take_blobs", len(rowIDs), column, func(handle uintptr, out *uintptr) int32 {
		return d.b.datasetTakeBlobs(handle, rowIDs, uint64(len(rowIDs)), column, out)
	})
	runtime.KeepAlive(rowIDs)
	return files, err
}

func (d *Dataset) TakeBlobsByIndices(indices []uint64, column string) ([]*BlobFile, error) {
	if err := validateCString(column); err != nil {
		return nil, err
	}
	files, err := d.takeBlobs("lance_dataset_take_blobs_by_indices", len(indices), column, func(handle uintptr, out *uintptr) int32 {
		return d.b.datasetTakeBlobsIndices(handle, indices, uint64(len(indices)), column, out)
	})
	runtime.KeepAlive(indices)
	return files, err
}

func (d *Dataset) TakeBlobsByAddresses(rowAddresses []uint64, column string) ([]*BlobFile, error) {
	if err := validateCString(column); err != nil {
		return nil, err
	}
	files, err := d.takeBlobs("lance_dataset_take_blobs_by_addresses", len(rowAddresses), column, func(handle uintptr, out *uintptr) int32 {
		return d.b.datasetTakeBlobsAddrs(handle, rowAddresses, uint64(len(rowAddresses)), column, out)
	})
	runtime.KeepAlive(rowAddresses)
	return files, err
}

func (d *Dataset) takeBlobs(operation string, count int, column string, call func(uintptr, *uintptr) int32) ([]*BlobFile, error) {
	handleValue := d.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	handles := make([]uintptr, count)
	var out *uintptr
	if count == 0 {
		// Native validates the column pointer; keep it non-NULL even when the
		// result array is empty.
		var dummy uintptr
		out = &dummy
	} else {
		out = &handles[0]
	}
	runtime.LockOSThread()
	rc := call(handleValue, out)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: %s failed without setting an error", operation)
	}
	runtime.UnlockOSThread()

	files := make([]*BlobFile, len(handles))
	for i, handle := range handles {
		if handle != 0 {
			files[i] = newBlobFile(d.b, handle)
		}
	}
	return files, nil
}

// BlobField exports the native blob v2 field with default thresholds.
func BlobField(name string, nullable bool) (arrow.Field, error) {
	b, err := load()
	if err != nil {
		return arrow.Field{}, err
	}
	return b.exportBlobField(name, nullable, nil)
}

// BlobFieldWithOptions exports the native blob v2 field with explicit
// threshold options. Nil thresholds select native defaults.
func BlobFieldWithOptions(name string, nullable bool, options BlobFieldOptions) (arrow.Field, error) {
	b, err := load()
	if err != nil {
		return arrow.Field{}, err
	}
	raw := &blobFieldOptions{
		InlineSizeThreshold:       options.InlineSizeThreshold,
		HasInlineSizeThreshold:    options.HasInlineSizeThreshold,
		DedicatedSizeThreshold:    options.DedicatedSizeThreshold,
		HasDedicatedSizeThreshold: options.HasDedicatedSizeThreshold,
	}
	return b.exportBlobField(name, nullable, raw)
}

// BlobFieldOptions selects native blob storage thresholds. A nil threshold
// leaves the native default in place; a present zero inline threshold is
// valid, while a present zero dedicated threshold is rejected natively.
type BlobFieldOptions struct {
	InlineSizeThreshold       uint64
	HasInlineSizeThreshold    bool
	DedicatedSizeThreshold    uint64
	HasDedicatedSizeThreshold bool
}

func (b *bindings) exportBlobField(name string, nullable bool, options *blobFieldOptions) (arrow.Field, error) {
	if err := validateCString(name); err != nil {
		return arrow.Field{}, err
	}
	var raw cdata.ArrowSchema
	runtime.LockOSThread()
	var rc int32
	if options == nil {
		rc = b.blobField(name, nullable, &raw)
	} else {
		rc = b.blobFieldWithOptions(name, nullable, options, &raw)
	}
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return arrow.Field{}, nativeErr
		}
		return arrow.Field{}, fmt.Errorf("ffi: lance_blob_field failed without setting an error")
	}
	runtime.UnlockOSThread()
	defer cdata.ReleaseSchema(&raw)

	field, err := cdata.ImportField(&raw)
	if err != nil {
		return arrow.Field{}, fmt.Errorf("ffi: import blob field: %w", err)
	}
	return field, nil
}

// BlobDescriptorBuilder builds one native prepared blob descriptor column.
type BlobDescriptorBuilder struct {
	b      *bindings
	handle uintptr
}

// NewBlobDescriptorBuilder creates a native descriptor builder for column.
func NewBlobDescriptorBuilder(column string) (*BlobDescriptorBuilder, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	if err := validateCString(column); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	handle := b.blobDescriptorNew(column)
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_blob_descriptor_array_builder_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &BlobDescriptorBuilder{b: b, handle: handle}, nil
}

// Field exports the prepared field owned by the builder. It remains available
// after Finish until Close releases the builder.
func (bld *BlobDescriptorBuilder) Field() (arrow.Field, error) {
	if bld == nil || bld.handle == 0 {
		return arrow.Field{}, ErrClosed
	}
	var raw cdata.ArrowSchema
	runtime.LockOSThread()
	rc := bld.b.blobDescriptorField(bld.handle, &raw)
	if rc != 0 {
		nativeErr := bld.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return arrow.Field{}, nativeErr
		}
		return arrow.Field{}, fmt.Errorf("ffi: lance_blob_descriptor_array_builder_field failed without setting an error")
	}
	runtime.UnlockOSThread()
	defer cdata.ReleaseSchema(&raw)
	field, err := cdata.ImportField(&raw)
	if err != nil {
		return arrow.Field{}, fmt.Errorf("ffi: import blob descriptor field: %w", err)
	}
	return field, nil
}

// AppendInline appends one owned inline descriptor.
func (bld *BlobDescriptorBuilder) AppendInline(data []byte) error {
	if bld == nil || bld.handle == 0 {
		return ErrClosed
	}
	var ptr *byte
	if len(data) > 0 {
		ptr = &data[0]
	}
	runtime.LockOSThread()
	rc := bld.b.blobDescriptorInline(bld.handle, ptr, uint64(len(data)))
	err := bld.status("lance_blob_descriptor_array_builder_append_inline", rc)
	runtime.UnlockOSThread()
	runtime.KeepAlive(data)
	return err
}

// AppendNull appends one null blob descriptor.
func (bld *BlobDescriptorBuilder) AppendNull() error {
	return bld.call("lance_blob_descriptor_array_builder_append_null", func() int32 {
		return bld.b.blobDescriptorNull(bld.handle)
	})
}

// AppendPacked appends one packed descriptor.
func (bld *BlobDescriptorBuilder) AppendPacked(blobID uint32, offset, length uint64) error {
	return bld.call("lance_blob_descriptor_array_builder_append_packed", func() int32 {
		return bld.b.blobDescriptorPacked(bld.handle, blobID, offset, length)
	})
}

// ExtendPacked appends packed descriptors in order. Native keeps the already
// appended prefix if a later range fails.
func (bld *BlobDescriptorBuilder) ExtendPacked(blobID uint32, ranges []BlobRange) error {
	raw := make([]blobRange, len(ranges))
	for i, r := range ranges {
		raw[i] = blobRange{Offset: r.Offset, Length: r.Length}
	}
	err := bld.call("lance_blob_descriptor_array_builder_extend_packed", func() int32 {
		return bld.b.blobDescriptorExtend(bld.handle, blobID, raw, uint64(len(raw)))
	})
	runtime.KeepAlive(raw)
	return err
}

// AppendDedicated appends one dedicated descriptor.
func (bld *BlobDescriptorBuilder) AppendDedicated(blobID uint32, length uint64) error {
	return bld.call("lance_blob_descriptor_array_builder_append_dedicated", func() int32 {
		return bld.b.blobDescriptorDedicated(bld.handle, blobID, length)
	})
}

// AppendManaged appends one managed descriptor. A nil baseID selects the
// native None variant; a pointer selects Some, including Some(0).
func (bld *BlobDescriptorBuilder) AppendManaged(uri string, offset, length uint64, baseID *uint32) error {
	if err := validateCString(uri); err != nil {
		return err
	}
	hasBaseID := baseID != nil
	var base uint32
	if hasBaseID {
		base = *baseID
	}
	return bld.call("lance_blob_descriptor_array_builder_append_managed", func() int32 {
		return bld.b.blobDescriptorManaged(bld.handle, uri, offset, length, hasBaseID, base)
	})
}

// AppendExternal appends one external descriptor with an explicit base ID.
func (bld *BlobDescriptorBuilder) AppendExternal(baseID uint32, uri string, offset, length uint64) error {
	if err := validateCString(uri); err != nil {
		return err
	}
	return bld.call("lance_blob_descriptor_array_builder_append_external", func() int32 {
		return bld.b.blobDescriptorExtern(bld.handle, baseID, uri, offset, length)
	})
}

// PushExternal appends an external descriptor through native push_external.
// A false hasRange stores offset 0 and length 0.
func (bld *BlobDescriptorBuilder) PushExternal(uri string, hasRange bool, offset, length uint64) error {
	if err := validateCString(uri); err != nil {
		return err
	}
	return bld.call("lance_blob_descriptor_array_builder_push_external", func() int32 {
		return bld.b.blobDescriptorPush(bld.handle, uri, hasRange, offset, length)
	})
}

// Finish consumes the native builder and returns the owned prepared array and
// field. The caller must release the array.
func (bld *BlobDescriptorBuilder) Finish() (arrow.Array, arrow.Field, error) {
	if bld == nil || bld.handle == 0 {
		return nil, arrow.Field{}, ErrClosed
	}
	var rawArray cdata.ArrowArray
	var rawSchema cdata.ArrowSchema
	runtime.LockOSThread()
	rc := bld.b.blobDescriptorFinish(bld.handle, &rawArray, &rawSchema)
	if rc != 0 {
		nativeErr := bld.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, arrow.Field{}, nativeErr
		}
		return nil, arrow.Field{}, fmt.Errorf("ffi: lance_blob_descriptor_array_builder_finish failed without setting an error")
	}
	runtime.UnlockOSThread()

	field, err := cdata.ImportField(&rawSchema)
	cdata.ReleaseSchema(&rawSchema)
	if err != nil {
		if rawArray.Release != 0 {
			cdata.ReleaseArray(&rawArray)
		}
		return nil, arrow.Field{}, fmt.Errorf("ffi: import blob descriptor schema: %w", err)
	}
	array, err := cdata.ImportArray(&rawArray, field.Type)
	if err != nil {
		return nil, arrow.Field{}, fmt.Errorf("ffi: import blob descriptor array: %w", err)
	}
	return array, field, nil
}

// Close frees the native builder handle. It is safe after Finish.
func (bld *BlobDescriptorBuilder) Close() {
	if bld == nil {
		return
	}
	handle := bld.handle
	bld.handle = 0
	if handle != 0 {
		bld.b.blobDescriptorClose(handle)
	}
}

func (bld *BlobDescriptorBuilder) call(operation string, fn func() int32) error {
	if bld == nil || bld.handle == 0 {
		return ErrClosed
	}
	runtime.LockOSThread()
	rc := fn()
	err := bld.status(operation, rc)
	runtime.UnlockOSThread()
	return err
}

func (bld *BlobDescriptorBuilder) status(operation string, rc int32) error {
	if rc == 0 {
		return nil
	}
	if nativeErr := bld.b.takeError(); nativeErr != nil {
		return nativeErr
	}
	return fmt.Errorf("ffi: %s failed without setting an error", operation)
}

// ReadBlobs plans a whole-blob read and returns a stream reader. The caller
// owns the reader and must release it.
func (d *Dataset) ReadBlobs(column string, selector int32, rows []uint64, options *BlobReadOptions) (*StreamReader, error) {
	if err := validateCString(column); err != nil {
		return nil, err
	}
	handleValue := d.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	rawOptions := buildBlobReadOptions(options)
	var stream cdata.ArrowArrayStream
	runtime.LockOSThread()
	rc := d.b.datasetReadBlobs(handleValue, column, selector, rows, uint64(len(rows)), rawOptions, &stream)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_read_blobs failed without setting an error")
	}
	runtime.UnlockOSThread()
	runtime.KeepAlive(rows)
	return NewStreamReader(&stream)
}

// ReadBlobRanges plans a blob-range read and returns a stream reader. The
// caller owns the reader and must release it.
func (d *Dataset) ReadBlobRanges(column string, selector int32, requests []BlobRangeRequest, options *BlobReadOptions) (*StreamReader, error) {
	if err := validateCString(column); err != nil {
		return nil, err
	}
	handleValue := d.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	rawRequests := make([]blobRangeRequest, len(requests))
	for i, request := range requests {
		rawRequests[i] = blobRangeRequest{Row: request.Row, Offset: request.Offset, Length: request.Length}
	}
	rawOptions := buildBlobReadOptions(options)
	var stream cdata.ArrowArrayStream
	runtime.LockOSThread()
	rc := d.b.datasetReadBlobRanges(handleValue, column, selector, rawRequests, uint64(len(rawRequests)), rawOptions, &stream)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_read_blob_ranges failed without setting an error")
	}
	runtime.UnlockOSThread()
	runtime.KeepAlive(rawRequests)
	return NewStreamReader(&stream)
}

func buildBlobReadOptions(options *BlobReadOptions) *blobReadOptions {
	if options == nil {
		return nil
	}
	raw := &blobReadOptions{Order: options.Order}
	if options.IOBufferSize != nil {
		raw.HasIOBufferSize = true
		raw.IOBufferSize = *options.IOBufferSize
	}
	return raw
}
