package ffi

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
)

// FileReader is an opaque native raw Lance-file reader.
type FileReader struct {
	b      *bindings
	handle uintptr
}

// OpenFileReader opens uri as a raw Lance file. A nil storage accessor selects
// native None. Scheduler nil selects native defaults.
func OpenFileReader(uri string, storage *StorageOptionsAccessor, options *FileReaderOptions, scheduler *FileSchedulerConfig) (*FileReader, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return openFileReader(b, uri, storage, options, scheduler)
}

func openFileReader(b *bindings, uri string, storage *StorageOptionsAccessor, options *FileReaderOptions, scheduler *FileSchedulerConfig) (*FileReader, error) {
	if err := validateCString(uri); err != nil {
		return nil, err
	}
	rawOptions, keep, err := buildFileReaderOptions(options)
	if err != nil {
		return nil, err
	}
	rawScheduler, err := buildFileSchedulerConfig(scheduler)
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	handle := b.fileReaderOpen(uri, storage.Handle(), rawOptions, 0, rawScheduler)
	keep.keepAlive()
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_reader_open returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &FileReader{b: b, handle: handle}, nil
}

// Close releases the reader allocation. Streams already returned stay usable.
func (r *FileReader) Close() {
	if r == nil || r.handle == 0 {
		return
	}
	handle := r.handle
	r.handle = 0
	r.b.fileReaderClose(handle)
}

// NumRows returns the file row count.
func (r *FileReader) NumRows() (uint64, error) {
	handle := r.handle
	if handle == 0 {
		return 0, ErrClosed
	}
	var rows uint64
	runtime.LockOSThread()
	rc := r.b.fileReaderNumRows(handle, &rows)
	if rc != 0 {
		nativeErr := r.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_file_reader_num_rows failed without setting an error")
	}
	runtime.UnlockOSThread()
	return rows, nil
}

// Metadata returns an independent metadata snapshot.
func (r *FileReader) Metadata() (*FileMetadata, error) {
	handle := r.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	var nativeHandle uintptr
	runtime.LockOSThread()
	rc := r.b.fileReaderGetMetadata(handle, &nativeHandle)
	if rc != 0 {
		nativeErr := r.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_reader_get_metadata failed without setting an error")
	}
	runtime.UnlockOSThread()
	if nativeHandle == 0 {
		return nil, fmt.Errorf("ffi: lance_file_reader_get_metadata returned NULL without setting an error")
	}
	return &FileMetadata{b: r.b, handle: nativeHandle}, nil
}

// Statistics returns an independent statistics snapshot.
func (r *FileReader) Statistics() (*FileStatistics, error) {
	handle := r.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	var nativeHandle uintptr
	runtime.LockOSThread()
	rc := r.b.fileReaderGetStatistics(handle, &nativeHandle)
	if rc != 0 {
		nativeErr := r.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_reader_get_statistics failed without setting an error")
	}
	runtime.UnlockOSThread()
	if nativeHandle == 0 {
		return nil, fmt.Errorf("ffi: lance_file_reader_get_statistics returned NULL without setting an error")
	}
	return &FileStatistics{b: r.b, handle: nativeHandle}, nil
}

// ReadGlobalBuffer reads global buffer index. Index 0 is the serialized
// schema; writer-added buffers start at 1.
func (r *FileReader) ReadGlobalBuffer(index uint32) ([]byte, error) {
	handle := r.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	runtime.LockOSThread()
	buffer := r.b.fileReaderReadGlobalBuffer(handle, index)
	if buffer == 0 {
		nativeErr := r.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_reader_read_global_buffer returned NULL without setting an error")
	}
	data, err := copyNativeByteBuffer(r.b, buffer)
	runtime.UnlockOSThread()
	return data, err
}

// ReadAll streams all rows. A zero batchSize or batchReadahead selects the
// native default for that parameter.
func (r *FileReader) ReadAll(batchSize, batchReadahead uint32) (*StreamReader, error) {
	handle := r.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	var raw cdata.ArrowArrayStream
	runtime.LockOSThread()
	rc := r.b.fileReaderReadAll(handle, batchSize, batchReadahead, &raw)
	if rc != 0 {
		nativeErr := r.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_reader_read_all failed without setting an error")
	}
	runtime.UnlockOSThread()
	return NewStreamReader(&raw)
}

// ReadRange streams numRows rows starting at offset.
func (r *FileReader) ReadRange(offset, numRows uint64, batchSize, batchReadahead uint32) (*StreamReader, error) {
	handle := r.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	var raw cdata.ArrowArrayStream
	runtime.LockOSThread()
	rc := r.b.fileReaderReadRange(handle, offset, numRows, batchSize, batchReadahead, &raw)
	if rc != 0 {
		nativeErr := r.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_reader_read_range failed without setting an error")
	}
	runtime.UnlockOSThread()
	return NewStreamReader(&raw)
}

// ReadIndices streams rows in the given order. An empty slice reads no rows.
func (r *FileReader) ReadIndices(indices []uint32, batchSize, batchReadahead uint32) (*StreamReader, error) {
	handle := r.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	var ptr *uint32
	if len(indices) > 0 {
		ptr = &indices[0]
	}
	count := uint64(len(indices))
	if uint64(int(count)) != count {
		return nil, fmt.Errorf("ffi: too many indices: %d", count)
	}

	var raw cdata.ArrowArrayStream
	runtime.LockOSThread()
	rc := r.b.fileReaderReadIndices(handle, ptr, count, batchSize, batchReadahead, &raw)
	if rc != 0 {
		nativeErr := r.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_reader_read_indices failed without setting an error")
	}
	runtime.UnlockOSThread()
	return NewStreamReader(&raw)
}

// IOStats returns cumulative native IO counters.
func (r *FileReader) IOStats() (FileScanStats, error) {
	handle := r.handle
	if handle == 0 {
		return FileScanStats{}, ErrClosed
	}
	var raw fileScanStats
	runtime.LockOSThread()
	rc := r.b.fileReaderGetIOStats(handle, &raw)
	if rc != 0 {
		nativeErr := r.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return FileScanStats{}, nativeErr
		}
		return FileScanStats{}, fmt.Errorf("ffi: lance_file_reader_get_io_stats failed without setting an error")
	}
	runtime.UnlockOSThread()
	return FileScanStats{IOPS: raw.IOPS, Requests: raw.Requests, BytesRead: raw.BytesRead}, nil
}

// FileMetadata is an owned native metadata snapshot.
type FileMetadata struct {
	b      *bindings
	handle uintptr
}

// Close releases the metadata snapshot.
func (m *FileMetadata) Close() {
	if m == nil || m.handle == 0 {
		return
	}
	handle := m.handle
	m.handle = 0
	m.b.fileMetadataClose(handle)
}

// Info copies scalar metadata.
func (m *FileMetadata) Info() (FileMetadataInfo, error) {
	handle := m.handle
	if handle == 0 {
		return FileMetadataInfo{}, ErrClosed
	}
	var raw fileMetadataInfo
	runtime.LockOSThread()
	rc := m.b.fileMetadataGetInfo(handle, &raw)
	if rc != 0 {
		nativeErr := m.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return FileMetadataInfo{}, nativeErr
		}
		return FileMetadataInfo{}, fmt.Errorf("ffi: lance_file_metadata_get_info failed without setting an error")
	}
	runtime.UnlockOSThread()
	return FileMetadataInfo{
		FormatVersion:          raw.FormatVersion,
		MajorVersion:           raw.MajorVersion,
		MinorVersion:           raw.MinorVersion,
		NumRows:                raw.NumRows,
		NumDataBytes:           raw.NumDataBytes,
		NumColumnMetadataBytes: raw.NumColumnMetadataBytes,
		NumGlobalBufferBytes:   raw.NumGlobalBufferBytes,
		NumFooterBytes:         raw.NumFooterBytes,
		FileSizeBytes:          raw.FileSizeBytes,
		NumColumns:             raw.NumColumns,
		NumPhysicalColumns:     raw.NumPhysicalColumns,
		NumGlobalBuffers:       raw.NumGlobalBuffers,
	}, nil
}

// Schema copies the file's complete Arrow schema.
func (m *FileMetadata) Schema() (*arrow.Schema, error) {
	handle := m.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	raw := new(cdata.ArrowSchema)
	runtime.LockOSThread()
	rc := m.b.fileMetadataGetSchema(handle, raw)
	if rc != 0 {
		nativeErr := m.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_metadata_get_schema failed without setting an error")
	}
	runtime.UnlockOSThread()
	defer cdata.ReleaseSchema(raw)

	schema, err := cdata.ImportSchema(raw)
	if err != nil {
		return nil, fmt.Errorf("ffi: import file schema: %w", err)
	}
	return schema, nil
}

// GlobalBuffers copies global buffer descriptors (not contents).
func (m *FileMetadata) GlobalBuffers() ([]FileBufferDescriptor, error) {
	handle := m.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	var buffers *fileBufferDescriptor
	var count uint64
	runtime.LockOSThread()
	rc := m.b.fileMetadataGetGlobalBuffers(handle, &buffers, &count)
	if rc != 0 {
		nativeErr := m.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_metadata_get_global_buffers failed without setting an error")
	}
	runtime.UnlockOSThread()
	if count == 0 {
		return []FileBufferDescriptor{}, nil
	}
	if buffers == nil {
		return nil, fmt.Errorf("ffi: metadata returned %d global buffers with NULL data", count)
	}
	if count > uint64(maxIntValue()) {
		return nil, fmt.Errorf("ffi: too many global buffers: %d", count)
	}
	raw := unsafe.Slice(buffers, int(count))
	result := make([]FileBufferDescriptor, len(raw))
	for i, buffer := range raw {
		result[i] = FileBufferDescriptor{Position: buffer.Position, Size: buffer.Size}
	}
	return result, nil
}

// Columns copies per-column and per-page descriptors.
func (m *FileMetadata) Columns() ([]FileColumnMetadata, error) {
	handle := m.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	var columns *fileColumnMetadata
	var count uint64
	runtime.LockOSThread()
	rc := m.b.fileMetadataGetColumns(handle, &columns, &count)
	if rc != 0 {
		nativeErr := m.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_metadata_get_columns failed without setting an error")
	}
	runtime.UnlockOSThread()
	if count == 0 {
		return []FileColumnMetadata{}, nil
	}
	if columns == nil {
		return nil, fmt.Errorf("ffi: metadata returned %d columns with NULL data", count)
	}
	if count > uint64(maxIntValue()) {
		return nil, fmt.Errorf("ffi: too many columns: %d", count)
	}

	raw := unsafe.Slice(columns, int(count))
	result := make([]FileColumnMetadata, len(raw))
	for i, column := range raw {
		buffers, err := copyBufferDescriptors(column.Buffers, column.BufferCount)
		if err != nil {
			return nil, err
		}
		if column.PageCount > uint64(maxIntValue()) {
			return nil, fmt.Errorf("ffi: column %d has too many pages: %d", i, column.PageCount)
		}
		pages := make([]FilePageMetadata, 0, int(column.PageCount))
		if column.PageCount > 0 {
			if column.Pages == nil {
				return nil, fmt.Errorf("ffi: column %d has %d pages with NULL data", i, column.PageCount)
			}
			rawPages := unsafe.Slice(column.Pages, int(column.PageCount))
			for j, page := range rawPages {
				pageBuffers, err := copyBufferDescriptors(page.Buffers, page.BufferCount)
				if err != nil {
					return nil, err
				}
				encoding, err := copyEncoding(page.Encoding)
				if err != nil {
					return nil, fmt.Errorf("ffi: column %d page %d: %w", i, j, err)
				}
				description := ""
				if page.EncodingDescription != nil {
					description = goString(page.EncodingDescription)
				}
				pages = append(pages, FilePageMetadata{
					Buffers:             pageBuffers,
					NumRows:             page.NumRows,
					Priority:            page.Priority,
					Encoding:            encoding,
					EncodingDescription: description,
				})
			}
		}
		encoding, err := copyEncoding(column.Encoding)
		if err != nil {
			return nil, fmt.Errorf("ffi: column %d: %w", i, err)
		}
		result[i] = FileColumnMetadata{
			Buffers:  buffers,
			Pages:    pages,
			Encoding: encoding,
		}
	}
	return result, nil
}

func copyBufferDescriptors(buffers *fileBufferDescriptor, count uint64) ([]FileBufferDescriptor, error) {
	if count == 0 {
		return []FileBufferDescriptor{}, nil
	}
	if buffers == nil {
		return nil, fmt.Errorf("ffi: metadata returned %d buffers with NULL data", count)
	}
	if count > uint64(maxIntValue()) {
		return nil, fmt.Errorf("ffi: too many buffers: %d", count)
	}
	raw := unsafe.Slice(buffers, int(count))
	result := make([]FileBufferDescriptor, len(raw))
	for i, buffer := range raw {
		result[i] = FileBufferDescriptor{Position: buffer.Position, Size: buffer.Size}
	}
	return result, nil
}

func copyEncoding(encoding fileEncodingDescriptor) (FileEncodingDescriptor, error) {
	result := FileEncodingDescriptor{
		Kind:           FileEncodingKind(encoding.Kind),
		BufferLocation: encoding.BufferLocation,
		BufferLength:   encoding.BufferLength,
	}
	if encoding.DirectBytesLen > 0 {
		if encoding.DirectBytes == nil {
			return FileEncodingDescriptor{}, fmt.Errorf("ffi: direct bytes length %d with NULL data", encoding.DirectBytesLen)
		}
		if encoding.DirectBytesLen > uint64(maxIntValue()) {
			return FileEncodingDescriptor{}, fmt.Errorf("ffi: too many direct bytes: %d", encoding.DirectBytesLen)
		}
		result.DirectBytes = append([]byte(nil), unsafe.Slice(encoding.DirectBytes, int(encoding.DirectBytesLen))...)
	}
	return result, nil
}

// FileStatistics is an owned native statistics snapshot.
type FileStatistics struct {
	b      *bindings
	handle uintptr
}

// Close releases the statistics snapshot.
func (s *FileStatistics) Close() {
	if s == nil || s.handle == 0 {
		return
	}
	handle := s.handle
	s.handle = 0
	s.b.fileStatisticsClose(handle)
}

// Columns copies per-column statistics.
func (s *FileStatistics) Columns() ([]FileColumnStatistics, error) {
	handle := s.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	var columns *fileColumnStatistics
	var count uint64
	runtime.LockOSThread()
	rc := s.b.fileStatisticsGetColumns(handle, &columns, &count)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_statistics_get_columns failed without setting an error")
	}
	runtime.UnlockOSThread()
	if count == 0 {
		return []FileColumnStatistics{}, nil
	}
	if columns == nil {
		return nil, fmt.Errorf("ffi: statistics returned %d columns with NULL data", count)
	}
	if count > uint64(maxIntValue()) {
		return nil, fmt.Errorf("ffi: too many statistics columns: %d", count)
	}
	raw := unsafe.Slice(columns, int(count))
	result := make([]FileColumnStatistics, len(raw))
	for i, column := range raw {
		result[i] = FileColumnStatistics{NumPages: column.NumPages, SizeBytes: column.SizeBytes}
	}
	return result, nil
}
