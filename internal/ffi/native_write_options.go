package ffi

import (
	"fmt"
	"runtime"
	"sort"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/cdata"
	"github.com/lance-format/lance-go/internal/handle"
)

// BaseStoreParams mirrors the representable fields of native ObjectStoreParams
// for one registered base path.
type BaseStoreParams struct {
	StorageOptionsAccessor     *StorageOptionsAccessor
	BlockSize                  *uint64
	S3CredentialsRefreshOffset *time.Duration
	UseConstantSizeUploadParts *bool
	ListIsLexicallyOrdered     *bool
}

// WriteFragmentProgress is one native begin- or complete-fragment event.
type WriteFragmentProgress struct {
	FragmentID   uint64
	PhysicalRows *uint64
	FileCount    uint64
}

// WriteStats is a native cumulative write-statistics snapshot.
type WriteStats struct {
	BytesWritten uint64
	RowsWritten  uint64
	FilesWritten uint32
}

// WriteProgress receives native write callbacks. Callbacks run on native
// worker threads and must be safe for concurrent use.
type WriteProgress struct {
	BeginFragment    func(WriteFragmentProgress) error
	CompleteFragment func(WriteFragmentProgress) error
	WriteStats       func(WriteStats)
}

// NativeWriteOptionsSpec mirrors LanceNativeWriteOptionsSpec.
//
// CommitLock is forwarded as NULL when nil. Namespace and fragment-create
// entry points still accept only the older options structure, so callers must
// reject native-only fields there instead of dropping them.
type NativeWriteOptionsSpec struct {
	WriteOptions                  *WriteOptions
	TransactionProperties         map[string]string
	HasTransactionProperties      bool
	EnableV2ManifestPaths         *bool
	InitialBases                  []BasePath
	TargetBases                   []uint32
	TargetBaseNamesOrPaths        []string
	TargetAllBases                *bool
	BaseStoreParams               map[string]BaseStoreParams
	AllowExternalBlobOutsideBases bool
	ExternalBlobMode              int32
	BlobPackFileSizeThreshold     *uint64
	FileWriterOptions             *FileWriterOptions
	Session                       *Session
	StorageOptionsAccessor        *StorageOptionsAccessor
	CommitLock                    *CommitLock
	Progress                      *WriteProgress
}

// NativeWriteOptions is an owned native-backed write-options handle.
type NativeWriteOptions struct {
	b      *bindings
	handle atomic.Uintptr
}

// Handle returns the native handle, or zero after Close.
func (o *NativeWriteOptions) Handle() uintptr {
	if o == nil {
		return 0
	}
	return o.handle.Load()
}

// Close releases the caller's native handle. Native writes clone the values
// they need. Close only after every call borrowing this handle has returned.
func (o *NativeWriteOptions) Close() {
	if o == nil {
		return
	}
	handleValue := o.handle.Swap(0)
	if handleValue == 0 {
		return
	}
	runtime.LockOSThread()
	o.b.nativeWriteOptionsClose(handleValue)
	runtime.UnlockOSThread()
}

type basePathListHeader struct {
	Items *basePath
	Count uint64
}

type uint32ListHeader struct {
	Items *uint32
	Count uint64
}

type stringListHeader struct {
	Items **byte
	Count uint64
}

type baseStoreParams struct {
	BasePath                      *byte
	StorageOptionsAccessor        uintptr
	HasBlockSize                  int32
	BlockSize                     uint64
	HasS3CredentialsRefreshOffset int32
	S3CredentialsRefreshSeconds   uint64
	S3CredentialsRefreshNanos     uint32
	HasUseConstantSizeUploadParts int32
	UseConstantSizeUploadParts    bool
	HasListIsLexicallyOrdered     int32
	ListIsLexicallyOrdered        bool
}

type baseStoreParamsListHeader struct {
	Items *baseStoreParams
	Count uint64
}

type nativeWriteFragmentProgress struct {
	FragmentID      uint64
	HasPhysicalRows int32
	PhysicalRows    uint64
	FileCount       uint64
}

type nativeWriteStats struct {
	BytesWritten uint64
	RowsWritten  uint64
	FilesWritten uint32
}

type nativeWriteProgress struct {
	BeginFragment    uintptr
	CompleteFragment uintptr
	WriteStats       uintptr
	Release          uintptr
	CallbackCtx      uintptr
}

type nativeWriteOptionsSpec struct {
	WriteOptions                  *writeOptions
	TransactionProperties         uintptr
	HasEnableV2ManifestPaths      int32
	EnableV2ManifestPaths         bool
	InitialBases                  *basePathListHeader
	TargetBases                   *uint32ListHeader
	TargetBaseNamesOrPaths        *stringListHeader
	HasTargetAllBases             int32
	TargetAllBases                bool
	BaseStoreParams               *baseStoreParamsListHeader
	AllowExternalBlobOutsideBases bool
	ExternalBlobMode              int32
	HasBlobPackFileSizeThreshold  int32
	BlobPackFileSizeThreshold     uint64
	FileWriterOptions             *fileWriterOptions
	Session                       uintptr
	StorageOptionsAccessor        uintptr
	CommitLock                    uintptr
	Progress                      *nativeWriteProgress
}

type uint32List struct {
	raw []uint32
}

func (l *uint32List) header() *uint32ListHeader {
	if l == nil {
		return nil
	}
	var items *uint32
	if len(l.raw) > 0 {
		items = &l.raw[0]
	}
	return &uint32ListHeader{Items: items, Count: uint64(len(l.raw))}
}

func (l *uint32List) keepAlive() {
	if l != nil {
		runtime.KeepAlive(l.raw)
	}
}

type baseStoreParamsList struct {
	raw  []baseStoreParams
	bufs [][]byte
}

func newBaseStoreParamsList(params map[string]BaseStoreParams) (*baseStoreParamsList, error) {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	list := &baseStoreParamsList{
		raw:  make([]baseStoreParams, len(keys)),
		bufs: make([][]byte, 0, len(keys)),
	}
	for i, key := range keys {
		if err := validateCString(key); err != nil {
			return nil, fmt.Errorf("base store params key: %w", err)
		}
		pathBuf := append([]byte(key), 0)
		list.bufs = append(list.bufs, pathBuf)
		list.raw[i].BasePath = &pathBuf[0]

		value := params[key]
		if value.StorageOptionsAccessor != nil {
			handleValue := value.StorageOptionsAccessor.Handle()
			if handleValue == 0 {
				return nil, ErrClosed
			}
			list.raw[i].StorageOptionsAccessor = handleValue
		}
		if value.BlockSize != nil {
			list.raw[i].HasBlockSize = 1
			list.raw[i].BlockSize = *value.BlockSize
		}
		if value.S3CredentialsRefreshOffset != nil {
			if *value.S3CredentialsRefreshOffset < 0 {
				return nil, fmt.Errorf("base store params %q: S3 credentials refresh offset must not be negative", key)
			}
			seconds, nanos := splitDuration(*value.S3CredentialsRefreshOffset)
			list.raw[i].HasS3CredentialsRefreshOffset = 1
			list.raw[i].S3CredentialsRefreshSeconds = seconds
			list.raw[i].S3CredentialsRefreshNanos = nanos
		}
		if value.UseConstantSizeUploadParts != nil {
			list.raw[i].HasUseConstantSizeUploadParts = 1
			list.raw[i].UseConstantSizeUploadParts = *value.UseConstantSizeUploadParts
		}
		if value.ListIsLexicallyOrdered != nil {
			list.raw[i].HasListIsLexicallyOrdered = 1
			list.raw[i].ListIsLexicallyOrdered = *value.ListIsLexicallyOrdered
		}
	}
	return list, nil
}

func (l *baseStoreParamsList) header() *baseStoreParamsListHeader {
	if l == nil {
		return nil
	}
	var items *baseStoreParams
	if len(l.raw) > 0 {
		items = &l.raw[0]
	}
	return &baseStoreParamsListHeader{Items: items, Count: uint64(len(l.raw))}
}

func (l *baseStoreParamsList) keepAlive() {
	if l != nil {
		runtime.KeepAlive(l.raw)
		runtime.KeepAlive(l.bufs)
	}
}

func cStringListHeaderFor(list *cStringList) *stringListHeader {
	if list == nil {
		return nil
	}
	var items **byte
	if len(list.ptrs) > 0 {
		items = (**byte)(unsafe.Pointer(&list.ptrs[0]))
	}
	count := len(list.ptrs)
	if count > 0 && list.ptrs[count-1] == 0 {
		count--
	}
	return &stringListHeader{Items: items, Count: uint64(count)}
}

type nativeWriteOptionsInputs struct {
	raw               nativeWriteOptionsSpec
	writeOptions      writeOptions
	versionList       *cStringList
	initialBases      *basePathList
	initialHeader     *basePathListHeader
	targetBases       *uint32List
	targetHeader      *uint32ListHeader
	targetNames       *cStringList
	targetNamesHeader *stringListHeader
	baseStoreList     *baseStoreParamsList
	baseStoreHeader   *baseStoreParamsListHeader
	fileWriter        *fileWriterOptions
	fileWriterKeep    fileWriterOptionsKeepAlive
	properties        *transactionProperties
	propertiesKeep    func()
	commitLock        *CommitLock
	progressCtx       uint64
}

func (i *nativeWriteOptionsInputs) keepAlive() {
	runtime.KeepAlive(&i.raw)
	runtime.KeepAlive(&i.writeOptions)
	if i.versionList != nil {
		i.versionList.keepAlive()
	}
	if i.initialBases != nil {
		i.initialBases.keepAlive()
	}
	i.targetBases.keepAlive()
	if i.targetNames != nil {
		i.targetNames.keepAlive()
	}
	i.baseStoreList.keepAlive()
	i.fileWriterKeep.keepAlive()
	if i.propertiesKeep != nil {
		i.propertiesKeep()
	}
	runtime.KeepAlive(i.commitLock)
}

func buildNativeWriteOptions(spec NativeWriteOptionsSpec) (*nativeWriteOptionsInputs, error) {
	inputs := &nativeWriteOptionsInputs{}
	writeOpts := WriteOptions{}
	if spec.WriteOptions != nil {
		writeOpts = *spec.WriteOptions
	}
	rawWrite, versionList, err := buildWriteParameters(writeOpts)
	if err != nil {
		return nil, err
	}
	inputs.writeOptions = rawWrite
	inputs.versionList = versionList
	inputs.raw.WriteOptions = &inputs.writeOptions

	if spec.HasTransactionProperties {
		properties := spec.TransactionProperties
		if properties == nil {
			properties = map[string]string{}
		}
		rawProperties, keepAlive, err := newTransactionProperties(properties)
		if err != nil {
			return nil, err
		}
		inputs.properties = rawProperties
		inputs.propertiesKeep = keepAlive
		inputs.raw.TransactionProperties = uintptr(unsafe.Pointer(rawProperties))
	}

	if spec.EnableV2ManifestPaths != nil {
		inputs.raw.HasEnableV2ManifestPaths = 1
		inputs.raw.EnableV2ManifestPaths = *spec.EnableV2ManifestPaths
	}

	if spec.InitialBases != nil {
		list, err := newBasePathList(spec.InitialBases)
		if err != nil {
			return nil, err
		}
		inputs.initialBases = list
		inputs.initialHeader = &basePathListHeader{Items: list.ptr(), Count: uint64(len(list.raw))}
		inputs.raw.InitialBases = inputs.initialHeader
	}

	if spec.TargetBases != nil {
		inputs.targetBases = &uint32List{raw: append([]uint32(nil), spec.TargetBases...)}
		inputs.targetHeader = inputs.targetBases.header()
		inputs.raw.TargetBases = inputs.targetHeader
	}

	if spec.TargetBaseNamesOrPaths != nil {
		list, err := newCStringList(spec.TargetBaseNamesOrPaths)
		if err != nil {
			return nil, err
		}
		inputs.targetNames = list
		inputs.targetNamesHeader = cStringListHeaderFor(list)
		inputs.raw.TargetBaseNamesOrPaths = inputs.targetNamesHeader
	}

	if spec.TargetAllBases != nil {
		inputs.raw.HasTargetAllBases = 1
		inputs.raw.TargetAllBases = *spec.TargetAllBases
	}

	if spec.BaseStoreParams != nil {
		list, err := newBaseStoreParamsList(spec.BaseStoreParams)
		if err != nil {
			return nil, err
		}
		inputs.baseStoreList = list
		inputs.baseStoreHeader = list.header()
		inputs.raw.BaseStoreParams = inputs.baseStoreHeader
	}

	inputs.raw.AllowExternalBlobOutsideBases = spec.AllowExternalBlobOutsideBases
	inputs.raw.ExternalBlobMode = spec.ExternalBlobMode

	if spec.BlobPackFileSizeThreshold != nil {
		inputs.raw.HasBlobPackFileSizeThreshold = 1
		inputs.raw.BlobPackFileSizeThreshold = *spec.BlobPackFileSizeThreshold
	}

	if spec.FileWriterOptions != nil {
		rawFileWriter, keepAlive, err := buildFileWriterOptions(spec.FileWriterOptions)
		if err != nil {
			return nil, err
		}
		inputs.fileWriter = rawFileWriter
		inputs.fileWriterKeep = keepAlive
		inputs.raw.FileWriterOptions = rawFileWriter
	}

	if spec.Session != nil {
		handleValue := spec.Session.nativeHandle()
		if handleValue == 0 {
			return nil, ErrClosed
		}
		inputs.raw.Session = handleValue
	}
	if spec.StorageOptionsAccessor != nil {
		handleValue := spec.StorageOptionsAccessor.Handle()
		if handleValue == 0 {
			return nil, ErrClosed
		}
		inputs.raw.StorageOptionsAccessor = handleValue
	}
	if spec.CommitLock != nil {
		handleValue := spec.CommitLock.Handle()
		if handleValue == 0 {
			return nil, ErrClosed
		}
		inputs.raw.CommitLock = handleValue
		inputs.commitLock = spec.CommitLock
	}

	if spec.Progress != nil {
		initStorageOptionsProviderCallbacks()
		adapter := &nativeWriteProgressAdapter{progress: *spec.Progress}
		ctxID := handle.New(adapter)
		progress := &nativeWriteProgress{
			Release:     callbackContextRelease,
			CallbackCtx: uintptr(ctxID),
		}
		if spec.Progress.BeginFragment != nil {
			progress.BeginFragment = callbackStorageOptionsFetch
		}
		if spec.Progress.CompleteFragment != nil {
			progress.CompleteFragment = callbackStorageOptionsForceFetch
		}
		if spec.Progress.WriteStats != nil {
			progress.WriteStats = statisticsCallback()
		}
		inputs.raw.Progress = progress
		inputs.progressCtx = ctxID
	}

	return inputs, nil
}

type nativeWriteProgressAdapter struct {
	progress WriteProgress
}

func (a *nativeWriteProgressAdapter) releaseContext() {}

func (a *nativeWriteProgressAdapter) dispatchStatus3(complete bool, payload unsafe.Pointer, outError *callbackError) int32 {
	info := (*nativeWriteFragmentProgress)(payload)
	if info == nil {
		setCallbackError(outError, fmt.Errorf("write fragment progress payload is NULL"))
		return 1
	}
	event := WriteFragmentProgress{
		FragmentID: info.FragmentID,
		FileCount:  info.FileCount,
	}
	if info.HasPhysicalRows != 0 {
		physicalRows := info.PhysicalRows
		event.PhysicalRows = &physicalRows
	}
	callback := a.progress.BeginFragment
	if complete {
		callback = a.progress.CompleteFragment
	}
	if callback == nil {
		return 0
	}
	if err := callback(event); err != nil {
		setCallbackError(outError, err)
		return 1
	}
	return 0
}

func (a *nativeWriteProgressAdapter) dispatchVoid2(payload uintptr) {
	raw := (*nativeWriteStats)(unsafe.Pointer(payload))
	if raw == nil || a.progress.WriteStats == nil {
		return
	}
	a.progress.WriteStats(WriteStats{
		BytesWritten: raw.BytesWritten,
		RowsWritten:  raw.RowsWritten,
		FilesWritten: raw.FilesWritten,
	})
}

// NewNativeWriteOptions creates an owned native-backed write-options handle.
// On success native owns the progress callback context; on failure Go drops it
// without invoking the release callback.
func NewNativeWriteOptions(spec NativeWriteOptionsSpec) (*NativeWriteOptions, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	inputs, err := buildNativeWriteOptions(spec)
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	handleValue := b.nativeWriteOptionsNew(&inputs.raw)
	if handleValue == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if inputs.progressCtx != 0 {
			handle.Take(inputs.progressCtx)
		}
		inputs.keepAlive()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_native_write_options_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	inputs.keepAlive()
	options := &NativeWriteOptions{b: b}
	options.handle.Store(handleValue)
	return options, nil
}

// WriteWithNativeOptions writes reader using an owned native options handle.
func WriteWithNativeOptions(uri string, reader array.RecordReader, mode WriteMode, options *NativeWriteOptions) (*Dataset, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return writeDatasetWithNativeOptions(b, uri, reader, mode, options)
}

func writeDatasetWithNativeOptions(b *bindings, uri string, reader array.RecordReader, mode WriteMode, options *NativeWriteOptions) (*Dataset, error) {
	if mode < WriteModeCreate || mode > WriteModeOverwrite {
		return nil, fmt.Errorf("ffi: invalid write mode %d", mode)
	}
	if reader == nil {
		return nil, fmt.Errorf("ffi: record reader must not be nil")
	}
	schema := reader.Schema()
	if schema == nil {
		return nil, fmt.Errorf("ffi: record reader schema must not be nil")
	}
	if err := validateCString(uri); err != nil {
		return nil, err
	}
	optionsHandle := uintptr(0)
	if options != nil {
		optionsHandle = options.Handle()
		if optionsHandle == 0 {
			return nil, ErrClosed
		}
	}

	var rawSchema cdata.ArrowSchema
	if err := cdata.ExportSchema(schema, &rawSchema); err != nil {
		return nil, fmt.Errorf("ffi: export write schema: %w", err)
	}
	defer cdata.ReleaseSchema(&rawSchema)

	var rawStream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &rawStream); err != nil {
		return nil, fmt.Errorf("ffi: export write stream: %w", err)
	}
	streamConsumed := false
	defer func() {
		if !streamConsumed {
			cdata.ReleaseStream(&rawStream)
		}
	}()

	runtime.LockOSThread()
	var output uintptr
	rc := b.datasetWriteWithNativeOptions(uri, &rawSchema, &rawStream, int32(mode), optionsHandle, &output)
	streamConsumed = true
	runtime.KeepAlive(&rawSchema)
	runtime.KeepAlive(&rawStream)
	runtime.KeepAlive(options)
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_write_with_native_options failed without setting an error")
	}
	runtime.UnlockOSThread()
	if output == 0 {
		return nil, fmt.Errorf("ffi: lance_dataset_write_with_native_options returned NULL without setting an error")
	}
	dataset := &Dataset{b: b}
	dataset.handle.Store(output)
	return dataset, nil
}

// WriteUncommittedWithNativeOptions stages reader using an owned native options
// handle. The returned transaction does not retain the options handle.
func WriteUncommittedWithNativeOptions(uri string, reader array.RecordReader, mode WriteMode, options *NativeWriteOptions) (*Transaction, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return writeUncommittedWithNativeOptions(b, uri, reader, mode, options)
}

func writeUncommittedWithNativeOptions(b *bindings, uri string, reader array.RecordReader, mode WriteMode, options *NativeWriteOptions) (*Transaction, error) {
	if mode < WriteModeCreate || mode > WriteModeOverwrite {
		return nil, fmt.Errorf("ffi: invalid write mode %d", mode)
	}
	if reader == nil {
		return nil, fmt.Errorf("ffi: record reader must not be nil")
	}
	schema := reader.Schema()
	if schema == nil {
		return nil, fmt.Errorf("ffi: record reader schema must not be nil")
	}
	if err := validateCString(uri); err != nil {
		return nil, err
	}
	optionsHandle := uintptr(0)
	if options != nil {
		optionsHandle = options.Handle()
		if optionsHandle == 0 {
			return nil, ErrClosed
		}
	}

	var rawSchema cdata.ArrowSchema
	if err := cdata.ExportSchema(schema, &rawSchema); err != nil {
		return nil, fmt.Errorf("ffi: export write schema: %w", err)
	}
	defer cdata.ReleaseSchema(&rawSchema)

	var rawStream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &rawStream); err != nil {
		return nil, fmt.Errorf("ffi: export write stream: %w", err)
	}
	streamConsumed := false
	defer func() {
		if !streamConsumed {
			cdata.ReleaseStream(&rawStream)
		}
	}()

	runtime.LockOSThread()
	var output uintptr
	rc := b.datasetWriteUncommittedStreamWithNativeOptions(uri, &rawSchema, &rawStream, int32(mode), optionsHandle, &output)
	streamConsumed = true
	runtime.KeepAlive(&rawSchema)
	runtime.KeepAlive(&rawStream)
	runtime.KeepAlive(options)
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_write_uncommitted_stream_with_native_options failed without setting an error")
	}
	runtime.UnlockOSThread()
	if output == 0 {
		return nil, fmt.Errorf("ffi: lance_dataset_write_uncommitted_stream_with_native_options returned NULL without setting an error")
	}
	return wrapTransaction(b, output), nil
}
