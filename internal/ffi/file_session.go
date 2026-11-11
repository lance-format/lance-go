package ffi

import (
	"fmt"
	"runtime"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
)

// FileSession is an opaque native raw Lance-file session.
type FileSession struct {
	b      *bindings
	handle uintptr
}

// OpenFileSession opens a session over baseURI. A nil storage accessor selects
// native None; a non-nil accessor must stay open for the duration of this call.
func OpenFileSession(baseURI string, storage *StorageOptionsAccessor, scheduler *FileSchedulerConfig) (*FileSession, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return openFileSession(b, baseURI, storage, scheduler)
}

func openFileSession(b *bindings, baseURI string, storage *StorageOptionsAccessor, scheduler *FileSchedulerConfig) (*FileSession, error) {
	if err := validateCString(baseURI); err != nil {
		return nil, err
	}
	rawScheduler, err := buildFileSchedulerConfig(scheduler)
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	handle := b.fileSessionNew(baseURI, storage.Handle(), 0, rawScheduler)
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_session_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &FileSession{b: b, handle: handle}, nil
}

// Close releases the session allocation. Readers, writers, streams, and blob
// writers retain their shared native resources.
func (s *FileSession) Close() {
	if s == nil || s.handle == 0 {
		return
	}
	handle := s.handle
	s.handle = 0
	s.b.fileSessionClose(handle)
}

// BasePath returns the native resolved base path.
func (s *FileSession) BasePath() (string, error) {
	handle := s.handle
	if handle == 0 {
		return "", ErrClosed
	}

	runtime.LockOSThread()
	ptr := s.b.fileSessionBasePath(handle)
	if ptr == nil {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_file_session_base_path returned NULL without setting an error")
	}
	path := goString(ptr)
	s.b.freeString(ptr)
	runtime.UnlockOSThread()
	return path, nil
}

// OpenReader opens a raw file reader relative to the session base.
func (s *FileSession) OpenReader(relativePath string, options *FileReaderOptions) (*FileReader, error) {
	handle := s.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(relativePath); err != nil {
		return nil, err
	}
	rawOptions, keep, err := buildFileReaderOptions(options)
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	nativeHandle := s.b.fileSessionOpenReader(handle, relativePath, rawOptions)
	keep.keepAlive()
	if nativeHandle == 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_session_open_reader returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &FileReader{b: s.b, handle: nativeHandle}, nil
}

// OpenWriter opens a raw file writer relative to the session base. A nil
// schema creates a lazy writer that adopts the first batch schema.
func (s *FileSession) OpenWriter(relativePath string, schema *arrow.Schema, options *FileWriterOptions) (*FileWriter, error) {
	handle := s.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(relativePath); err != nil {
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
	nativeHandle := s.b.fileSessionOpenWriter(handle, relativePath, rawSchema, rawOptions)
	keep.keepAlive()
	if nativeHandle == 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_session_open_writer returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &FileWriter{b: s.b, handle: nativeHandle}, nil
}

// Contains reports whether relativePath exists in the session store.
func (s *FileSession) Contains(relativePath string) (bool, error) {
	handle := s.handle
	if handle == 0 {
		return false, ErrClosed
	}
	if err := validateCString(relativePath); err != nil {
		return false, err
	}

	var exists int32
	runtime.LockOSThread()
	rc := s.b.fileSessionContains(handle, relativePath, &exists)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return false, nativeErr
		}
		return false, fmt.Errorf("ffi: lance_file_session_contains failed without setting an error")
	}
	runtime.UnlockOSThread()
	switch exists {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("ffi: lance_file_session_contains returned %d, want 0 or 1", exists)
	}
}

// List returns paths relative to the session base. An empty relativePath lists
// the base itself, which native represents as a NULL path.
func (s *FileSession) List(relativePath string) ([]string, error) {
	handle := s.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	pathList, pathPtr, err := optionalPath(relativePath)
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	nativeHandle := s.b.fileSessionList(handle, pathPtr)
	pathList.keepAlive()
	if nativeHandle == 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_session_list returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	defer s.b.pathListClose(nativeHandle)

	count := s.b.pathListCount(nativeHandle)
	if count > uint64(maxIntValue()) {
		return nil, fmt.Errorf("ffi: path list has too many entries: %d", count)
	}
	paths := make([]string, 0, int(count))
	for i := uint64(0); i < count; i++ {
		ptr := s.b.pathListAt(nativeHandle, i)
		if ptr == nil {
			return nil, fmt.Errorf("ffi: lance_path_list_at returned NULL at index %d", i)
		}
		paths = append(paths, goString(ptr))
	}
	return paths, nil
}

// ListWithDelimiter lists paths and common prefixes relative to the session
// base. An empty relativePath lists the base itself.
func (s *FileSession) ListWithDelimiter(relativePath string) (DelimitedList, error) {
	handle := s.handle
	if handle == 0 {
		return DelimitedList{}, ErrClosed
	}
	pathList, pathPtr, err := optionalPath(relativePath)
	if err != nil {
		return DelimitedList{}, err
	}

	runtime.LockOSThread()
	nativeHandle := s.b.fileSessionListWithDelimiter(handle, pathPtr)
	pathList.keepAlive()
	if nativeHandle == 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return DelimitedList{}, nativeErr
		}
		return DelimitedList{}, fmt.Errorf("ffi: lance_file_session_list_with_delimiter returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	defer s.b.delimitedListClose(nativeHandle)

	prefixes, err := s.copyStringList(
		s.b.delimitedListCommonPrefixCount(nativeHandle),
		func(index uint64) *byte { return s.b.delimitedListCommonPrefixAt(nativeHandle, index) },
		"common prefix",
	)
	if err != nil {
		return DelimitedList{}, err
	}
	objects, err := s.copyStringList(
		s.b.delimitedListObjectCount(nativeHandle),
		func(index uint64) *byte { return s.b.delimitedListObjectAt(nativeHandle, index) },
		"object",
	)
	if err != nil {
		return DelimitedList{}, err
	}
	return DelimitedList{CommonPrefixes: prefixes, Objects: objects}, nil
}

func (s *FileSession) copyStringList(count uint64, at func(uint64) *byte, kind string) ([]string, error) {
	if count > uint64(maxIntValue()) {
		return nil, fmt.Errorf("ffi: %s list has too many entries: %d", kind, count)
	}
	values := make([]string, 0, int(count))
	for i := uint64(0); i < count; i++ {
		ptr := at(i)
		if ptr == nil {
			return nil, fmt.Errorf("ffi: %s list entry %d is NULL", kind, i)
		}
		values = append(values, goString(ptr))
	}
	return values, nil
}

// ReadRange reads length bytes at offset from relativePath.
func (s *FileSession) ReadRange(relativePath string, offset, length uint64) ([]byte, error) {
	handle := s.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(relativePath); err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	buffer := s.b.fileSessionReadRange(handle, relativePath, offset, length)
	if buffer == 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_file_session_read_range returned NULL without setting an error")
	}
	data, err := copyNativeByteBuffer(s.b, buffer)
	runtime.UnlockOSThread()
	return data, err
}

// DeleteFile removes relativePath.
func (s *FileSession) DeleteFile(relativePath string) error {
	handle := s.handle
	if handle == 0 {
		return ErrClosed
	}
	if err := validateCString(relativePath); err != nil {
		return err
	}

	runtime.LockOSThread()
	rc := s.b.fileSessionDeleteFile(handle, relativePath)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_file_session_delete_file failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

// UploadFile uploads localPath to relativeRemotePath.
func (s *FileSession) UploadFile(localPath, relativeRemotePath string) error {
	handle := s.handle
	if handle == 0 {
		return ErrClosed
	}
	if err := validateCString(localPath); err != nil {
		return err
	}
	if err := validateCString(relativeRemotePath); err != nil {
		return err
	}

	runtime.LockOSThread()
	rc := s.b.fileSessionUploadFile(handle, localPath, relativeRemotePath)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_file_session_upload_file failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

// DownloadFile downloads relativeRemotePath to localPath.
func (s *FileSession) DownloadFile(relativeRemotePath, localPath string) error {
	handle := s.handle
	if handle == 0 {
		return ErrClosed
	}
	if err := validateCString(relativeRemotePath); err != nil {
		return err
	}
	if err := validateCString(localPath); err != nil {
		return err
	}

	runtime.LockOSThread()
	rc := s.b.fileSessionDownloadFile(handle, relativeRemotePath, localPath)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_file_session_download_file failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

// SchedulerStats returns a snapshot of scheduler counters.
func (s *FileSession) SchedulerStats() (FileScanStats, error) {
	handle := s.handle
	if handle == 0 {
		return FileScanStats{}, ErrClosed
	}
	var raw fileScanStats
	runtime.LockOSThread()
	rc := s.b.fileSessionGetSchedulerStats(handle, &raw)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return FileScanStats{}, nativeErr
		}
		return FileScanStats{}, fmt.Errorf("ffi: lance_file_session_get_scheduler_stats failed without setting an error")
	}
	runtime.UnlockOSThread()
	return FileScanStats{IOPS: raw.IOPS, Requests: raw.Requests, BytesRead: raw.BytesRead}, nil
}

// RegistryStats returns a snapshot of object-store registry counters.
func (s *FileSession) RegistryStats() (FileRegistryStats, error) {
	handle := s.handle
	if handle == 0 {
		return FileRegistryStats{}, ErrClosed
	}
	var raw fileRegistryStats
	runtime.LockOSThread()
	rc := s.b.fileSessionGetRegistryStats(handle, &raw)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return FileRegistryStats{}, nativeErr
		}
		return FileRegistryStats{}, fmt.Errorf("ffi: lance_file_session_get_registry_stats failed without setting an error")
	}
	runtime.UnlockOSThread()
	return FileRegistryStats{Hits: raw.Hits, Misses: raw.Misses, ActiveStores: raw.ActiveStores}, nil
}

// StoreInfo returns a snapshot of the resolved object store's properties.
func (s *FileSession) StoreInfo() (FileStoreInfo, error) {
	handle := s.handle
	if handle == 0 {
		return FileStoreInfo{}, ErrClosed
	}
	var raw fileStoreInfo
	runtime.LockOSThread()
	rc := s.b.fileSessionGetStoreInfo(handle, &raw)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return FileStoreInfo{}, nativeErr
		}
		return FileStoreInfo{}, fmt.Errorf("ffi: lance_file_session_get_store_info failed without setting an error")
	}
	runtime.UnlockOSThread()
	return FileStoreInfo{
		BlockSize:            raw.BlockSize,
		MaxIOPSize:           raw.MaxIOPSize,
		IOParallelism:        raw.IOParallelism,
		IsLocal:              raw.IsLocal != 0,
		IsCloud:              raw.IsCloud != 0,
		PrefersLiteScheduler: raw.PrefersLiteScheduler != 0,
	}, nil
}

// StoreIOStats returns a snapshot of object-store IO counters.
func (s *FileSession) StoreIOStats() (FileStoreIOStats, error) {
	handle := s.handle
	if handle == 0 {
		return FileStoreIOStats{}, ErrClosed
	}
	var raw fileStoreIOStats
	runtime.LockOSThread()
	rc := s.b.fileSessionGetStoreIOStats(handle, &raw)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return FileStoreIOStats{}, nativeErr
		}
		return FileStoreIOStats{}, fmt.Errorf("ffi: lance_file_session_get_store_io_stats failed without setting an error")
	}
	runtime.UnlockOSThread()
	return FileStoreIOStats{
		ReadIOPS:     raw.ReadIOPS,
		ReadBytes:    raw.ReadBytes,
		WriteIOPS:    raw.WriteIOPS,
		WrittenBytes: raw.WrittenBytes,
	}, nil
}

func optionalPath(path string) (*cStringList, *byte, error) {
	if path == "" {
		return nil, nil, nil
	}
	list, err := newCStringList([]string{path})
	if err != nil {
		return nil, nil, err
	}
	return list, &list.bufs[0][0], nil
}

func exportOptionalSchema(schema *arrow.Schema) (*cdata.ArrowSchema, func(), error) {
	if schema == nil {
		return nil, func() {}, nil
	}
	raw := new(cdata.ArrowSchema)
	if err := cdata.ExportSchema(schema, raw); err != nil {
		return nil, nil, fmt.Errorf("ffi: export schema: %w", err)
	}
	return raw, func() { cdata.ReleaseSchema(raw) }, nil
}

func maxIntValue() int {
	return int(^uint(0) >> 1)
}
