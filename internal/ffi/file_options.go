package ffi

import (
	"fmt"
	"runtime"
	"sort"
	"unsafe"
)

// FileCacheMode mirrors LanceFileCacheMode.
type FileCacheMode int32

const (
	FileCacheNone    FileCacheMode = 0
	FileCacheSession FileCacheMode = 1
)

// FileReaderOptions mirrors LanceFileReaderOptions. Pointer fields preserve
// the difference between an unset option and an explicit zero value. A nil
// Columns slice leaves projection unset; a non-nil empty slice forwards an
// explicit empty projection.
type FileReaderOptions struct {
	Columns              []string
	CacheMode            FileCacheMode
	ReadChunkSize        *uint64
	BatchSizeBytes       *uint64
	CacheRepetitionIndex *bool
	ValidateOnDecode     *bool
	InlineScheduling     *bool
}

type fileReaderOptions struct {
	HasColumns              int32
	Columns                 **byte
	ColumnCount             uint64
	CacheMode               int32
	HasReadChunkSize        int32
	ReadChunkSize           uint64
	HasBatchSizeBytes       int32
	BatchSizeBytes          uint64
	HasCacheRepetitionIndex int32
	CacheRepetitionIndex    int32
	HasValidateOnDecode     int32
	ValidateOnDecode        int32
	HasInlineScheduling     int32
	InlineScheduling        int32
}

type fileReaderOptionsKeepAlive struct {
	columns *cStringList
}

func (k fileReaderOptionsKeepAlive) keepAlive() {
	k.columns.keepAlive()
}

func buildFileReaderOptions(options *FileReaderOptions) (*fileReaderOptions, fileReaderOptionsKeepAlive, error) {
	var keep fileReaderOptionsKeepAlive
	if options == nil {
		return nil, keep, nil
	}
	if options.CacheMode != FileCacheNone && options.CacheMode != FileCacheSession {
		return nil, keep, fmt.Errorf("ffi: invalid file cache mode %d", options.CacheMode)
	}

	raw := &fileReaderOptions{CacheMode: int32(options.CacheMode)}
	if options.Columns != nil {
		columns, err := newCStringList(options.Columns)
		if err != nil {
			return nil, keep, err
		}
		keep.columns = columns
		raw.HasColumns = 1
		raw.ColumnCount = uint64(len(options.Columns))
		if len(columns.ptrs) > 0 {
			raw.Columns = (**byte)(unsafe.Pointer(&columns.ptrs[0]))
		}
	}
	if options.ReadChunkSize != nil {
		raw.HasReadChunkSize = 1
		raw.ReadChunkSize = *options.ReadChunkSize
	}
	if options.BatchSizeBytes != nil {
		raw.HasBatchSizeBytes = 1
		raw.BatchSizeBytes = *options.BatchSizeBytes
	}
	if options.CacheRepetitionIndex != nil {
		raw.HasCacheRepetitionIndex = 1
		raw.CacheRepetitionIndex = boolToInt32(*options.CacheRepetitionIndex)
	}
	if options.ValidateOnDecode != nil {
		raw.HasValidateOnDecode = 1
		raw.ValidateOnDecode = boolToInt32(*options.ValidateOnDecode)
	}
	if options.InlineScheduling != nil {
		raw.HasInlineScheduling = 1
		raw.InlineScheduling = boolToInt32(*options.InlineScheduling)
	}
	return raw, keep, nil
}

// FileWriterOptions mirrors LanceFileWriterOptions. Pointer fields preserve
// unset versus explicit zero. Version is forwarded only when non-empty.
type FileWriterOptions struct {
	DataCacheBytes    *uint64
	MaxPageBytes      *uint64
	KeepOriginalArray *bool
	Version           string
}

type fileWriterOptions struct {
	HasDataCacheBytes    int32
	DataCacheBytes       uint64
	HasMaxPageBytes      int32
	MaxPageBytes         uint64
	HasKeepOriginalArray int32
	KeepOriginalArray    int32
	Version              uintptr
}

type fileWriterOptionsKeepAlive struct {
	version *cStringList
}

func (k fileWriterOptionsKeepAlive) keepAlive() {
	k.version.keepAlive()
}

func buildFileWriterOptions(options *FileWriterOptions) (*fileWriterOptions, fileWriterOptionsKeepAlive, error) {
	var keep fileWriterOptionsKeepAlive
	if options == nil {
		return nil, keep, nil
	}
	raw := &fileWriterOptions{}
	if options.DataCacheBytes != nil {
		raw.HasDataCacheBytes = 1
		raw.DataCacheBytes = *options.DataCacheBytes
	}
	if options.MaxPageBytes != nil {
		raw.HasMaxPageBytes = 1
		raw.MaxPageBytes = *options.MaxPageBytes
	}
	if options.KeepOriginalArray != nil {
		raw.HasKeepOriginalArray = 1
		raw.KeepOriginalArray = boolToInt32(*options.KeepOriginalArray)
	}
	if options.Version != "" {
		version, err := newCStringList([]string{options.Version})
		if err != nil {
			return nil, keep, err
		}
		keep.version = version
		raw.Version = version.ptrs[0]
	}
	return raw, keep, nil
}

// FileSchedulerConfig mirrors LanceSchedulerConfig.
type FileSchedulerConfig struct {
	IOBufferSizeBytes uint64
	LiteMode          int32
}

const (
	FileSchedulerAuto     int32 = 0
	FileSchedulerStandard int32 = 1
	FileSchedulerLite     int32 = 2
)

type fileSchedulerConfig struct {
	IOBufferSizeBytes uint64
	UseLiteScheduler  int32
}

func buildFileSchedulerConfig(config *FileSchedulerConfig) (*fileSchedulerConfig, error) {
	if config == nil {
		return nil, nil
	}
	if config.LiteMode < FileSchedulerAuto || config.LiteMode > FileSchedulerLite {
		return nil, fmt.Errorf("ffi: invalid scheduler lite mode %d", config.LiteMode)
	}
	return &fileSchedulerConfig{
		IOBufferSizeBytes: config.IOBufferSizeBytes,
		UseLiteScheduler:  config.LiteMode,
	}, nil
}

// FileScanStats mirrors LanceScanStats.
type FileScanStats struct {
	IOPS      uint64
	Requests  uint64
	BytesRead uint64
}

type fileScanStats struct {
	IOPS      uint64
	Requests  uint64
	BytesRead uint64
}

// FileRegistryStats mirrors LanceObjectStoreRegistryStats.
type FileRegistryStats struct {
	Hits         uint64
	Misses       uint64
	ActiveStores uint64
}

type fileRegistryStats struct {
	Hits         uint64
	Misses       uint64
	ActiveStores uint64
}

// FileStoreIOStats mirrors LanceObjectStoreIoStats.
type FileStoreIOStats struct {
	ReadIOPS     uint64
	ReadBytes    uint64
	WriteIOPS    uint64
	WrittenBytes uint64
}

type fileStoreIOStats struct {
	ReadIOPS     uint64
	ReadBytes    uint64
	WriteIOPS    uint64
	WrittenBytes uint64
}

// FileStoreInfo mirrors LanceObjectStoreInfo.
type FileStoreInfo struct {
	BlockSize            uint64
	MaxIOPSize           uint64
	IOParallelism        uint64
	IsLocal              bool
	IsCloud              bool
	PrefersLiteScheduler bool
}

type fileStoreInfo struct {
	BlockSize            uint64
	MaxIOPSize           uint64
	IOParallelism        uint64
	IsLocal              int32
	IsCloud              int32
	PrefersLiteScheduler int32
}

// FileBufferDescriptor mirrors LanceBufferDescriptor.
type FileBufferDescriptor struct {
	Position uint64
	Size     uint64
}

type fileBufferDescriptor struct {
	Position uint64
	Size     uint64
}

// FileEncodingKind mirrors LanceFileEncodingKind.
type FileEncodingKind int32

const (
	FileEncodingAbsent   FileEncodingKind = 0
	FileEncodingNone     FileEncodingKind = 1
	FileEncodingDirect   FileEncodingKind = 2
	FileEncodingIndirect FileEncodingKind = 3
)

// FileEncodingDescriptor is a copied encoding descriptor. DirectBytes is
// copied out of the native metadata snapshot.
type FileEncodingDescriptor struct {
	Kind           FileEncodingKind
	BufferLocation uint64
	BufferLength   uint64
	DirectBytes    []byte
}

type fileEncodingDescriptor struct {
	Kind           int32
	BufferLocation uint64
	BufferLength   uint64
	DirectBytes    *byte
	DirectBytesLen uint64
}

// FilePageMetadata is a copied page descriptor.
type FilePageMetadata struct {
	Buffers             []FileBufferDescriptor
	NumRows             uint64
	Priority            uint64
	Encoding            FileEncodingDescriptor
	EncodingDescription string
}

type filePageMetadata struct {
	Buffers             *fileBufferDescriptor
	BufferCount         uint64
	NumRows             uint64
	Priority            uint64
	Encoding            fileEncodingDescriptor
	EncodingDescription *byte
}

// FileColumnMetadata is a copied column descriptor.
type FileColumnMetadata struct {
	Buffers  []FileBufferDescriptor
	Pages    []FilePageMetadata
	Encoding FileEncodingDescriptor
}

type fileColumnMetadata struct {
	Buffers     *fileBufferDescriptor
	BufferCount uint64
	Pages       *filePageMetadata
	PageCount   uint64
	Encoding    fileEncodingDescriptor
}

// FileMetadataInfo mirrors LanceFileMetadataInfo.
type FileMetadataInfo struct {
	FormatVersion          int32
	MajorVersion           uint16
	MinorVersion           uint16
	NumRows                uint64
	NumDataBytes           uint64
	NumColumnMetadataBytes uint64
	NumGlobalBufferBytes   uint64
	NumFooterBytes         uint64
	FileSizeBytes          uint64
	NumColumns             uint64
	NumPhysicalColumns     uint64
	NumGlobalBuffers       uint64
}

type fileMetadataInfo struct {
	FormatVersion          int32
	MajorVersion           uint16
	MinorVersion           uint16
	NumRows                uint64
	NumDataBytes           uint64
	NumColumnMetadataBytes uint64
	NumGlobalBufferBytes   uint64
	NumFooterBytes         uint64
	FileSizeBytes          uint64
	NumColumns             uint64
	NumPhysicalColumns     uint64
	NumGlobalBuffers       uint64
}

// FileColumnStatistics mirrors LanceColumnStatistics.
type FileColumnStatistics struct {
	NumPages  uint64
	SizeBytes uint64
}

type fileColumnStatistics struct {
	NumPages  uint64
	SizeBytes uint64
}

// FileWriteSummary mirrors LanceFileWriteSummary.
type FileWriteSummary struct {
	NumRows   uint64
	SizeBytes uint64
}

type fileWriteSummary struct {
	NumRows   uint64
	SizeBytes uint64
}

// DelimitedList is a copied native list_with_delimiter result.
type DelimitedList struct {
	CommonPrefixes []string
	Objects        []string
}

type storageOptionsInput struct {
	Keys   **byte
	Values **byte
	Count  uint64
}

// StorageOptionsAccessor owns a static native storage-options accessor.
// Callers serialize Close against every native functions that consumes it.
type StorageOptionsAccessor struct {
	b      *bindings
	handle uintptr
}

// NewStorageOptionsAccessor creates an accessor with fixed options. A nil or
// empty map creates Some(empty), not None; use a nil accessor for native None.
func NewStorageOptionsAccessor(values map[string]string) (*StorageOptionsAccessor, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return newStorageOptionsAccessor(b, values)
}

func newStorageOptionsAccessor(b *bindings, values map[string]string) (*StorageOptionsAccessor, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	keyValues := make([]string, len(keys))
	optionValues := make([]string, len(keys))
	for i, key := range keys {
		if key == "" {
			return nil, fmt.Errorf("ffi: storage option key must not be empty")
		}
		keyValues[i] = key
		optionValues[i] = values[key]
	}
	keyList, err := newCStringList(keyValues)
	if err != nil {
		return nil, err
	}
	valueList, err := newCStringList(optionValues)
	if err != nil {
		return nil, err
	}

	raw := storageOptionsInput{Count: uint64(len(keys))}
	if len(keyList.ptrs) > 0 {
		raw.Keys = (**byte)(unsafe.Pointer(&keyList.ptrs[0]))
		raw.Values = (**byte)(unsafe.Pointer(&valueList.ptrs[0]))
	}

	runtime.LockOSThread()
	handle := b.storageAccessorNew(&raw, 0)
	keyList.keepAlive()
	valueList.keepAlive()
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_storage_options_accessor_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &StorageOptionsAccessor{b: b, handle: handle}, nil
}

// Handle returns the native accessor pointer. The caller must serialize Close.
func (a *StorageOptionsAccessor) Handle() uintptr {
	if a == nil {
		return 0
	}
	return a.handle
}

// Close releases the native accessor. It is idempotent.
func (a *StorageOptionsAccessor) Close() {
	if a == nil || a.handle == 0 {
		return
	}
	handle := a.handle
	a.handle = 0
	a.b.storageAccessorClose(handle)
}

func boolToInt32(value bool) int32 {
	if value {
		return 1
	}
	return 0
}
