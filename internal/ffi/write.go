package ffi

import (
	"fmt"
	"runtime"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/cdata"
)

// WriteMode mirrors LanceWriteMode.
type WriteMode int32

const (
	WriteModeCreate    WriteMode = 0
	WriteModeAppend    WriteMode = 1
	WriteModeOverwrite WriteMode = 2
)

// WriteParams mirrors LanceWriteParams. Zero values select native defaults,
// except EnableStableRowIDs which is always forwarded explicitly.
type WriteParams struct {
	MaxRowsPerFile     uint64
	MaxRowsPerGroup    uint64
	MaxBytesPerFile    uint64
	DataStorageVersion string
	EnableStableRowIDs bool
}

// AutoCleanupParams mirrors LanceAutoCleanupParams.
type AutoCleanupParams struct {
	Interval         uint64
	OlderThanSeconds int64
	OlderThanNanos   int32
}

// WriteOptions controls a dataset write.
type WriteOptions struct {
	Mode            WriteMode
	Params          WriteParams
	AutoCleanup     *AutoCleanupParams
	SkipAutoCleanup bool
}

type writeParams struct {
	MaxRowsPerFile     uint64
	MaxRowsPerGroup    uint64
	MaxBytesPerFile    uint64
	DataStorageVersion *byte
	EnableStableRowIDs bool
}

type autoCleanupParams struct {
	Interval         uint64
	OlderThanSeconds int64
	OlderThanNanos   int32
}

type writeOptions struct {
	Params          writeParams
	AutoCleanup     *autoCleanupParams
	SkipAutoCleanup bool
}

// Write writes reader to uri. The stream is consumed by native code on every
// return path after export. The caller keeps ownership of reader, but must not
// use it after this call unless it retained an independent reference before.
func Write(uri string, reader array.RecordReader, opts WriteOptions, storageOptions map[string]string) (*Dataset, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return writeDataset(b, uri, reader, opts, storageOptions)
}

func writeDataset(b *bindings, uri string, reader array.RecordReader, opts WriteOptions, storageOptions map[string]string) (*Dataset, error) {
	if reader == nil {
		return nil, fmt.Errorf("ffi: record reader must not be nil")
	}
	schema := reader.Schema()
	if schema == nil {
		return nil, fmt.Errorf("ffi: record reader schema must not be nil")
	}
	if opts.Mode < WriteModeCreate || opts.Mode > WriteModeOverwrite {
		return nil, fmt.Errorf("ffi: invalid write mode %d", opts.Mode)
	}
	if err := validateCString(uri); err != nil {
		return nil, err
	}
	rawOptions, versionList, err := buildWriteOptions(opts.Params, opts.AutoCleanup, opts.SkipAutoCleanup)
	if err != nil {
		return nil, err
	}
	storageList, err := storageOptionList(storageOptions)
	if err != nil {
		return nil, err
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
	var handle uintptr
	rc := b.datasetWriteWithOptions(uri, &rawSchema, &rawStream, int32(opts.Mode), &rawOptions, storageList.ptrs, &handle)
	streamConsumed = true
	versionList.keepAlive()
	storageList.keepAlive()
	runtime.KeepAlive(&rawOptions)
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_write_with_options failed without setting an error")
	}
	runtime.UnlockOSThread()

	if handle == 0 {
		return nil, fmt.Errorf("ffi: lance_dataset_write_with_options returned NULL without setting an error")
	}
	ds := &Dataset{b: b}
	ds.handle.Store(handle)
	return ds, nil
}

// buildWriteOptions converts Go write parameters to the native C layout. The
// returned version list must stay alive until after the native call.
func buildWriteOptions(params WriteParams, autoCleanup *AutoCleanupParams, skipAutoCleanup bool) (writeOptions, *cStringList, error) {
	versionList := &cStringList{}
	if params.DataStorageVersion != "" {
		if err := validateCString(params.DataStorageVersion); err != nil {
			return writeOptions{}, nil, err
		}
		list, err := newCStringList([]string{params.DataStorageVersion})
		if err != nil {
			return writeOptions{}, nil, err
		}
		versionList = list
	}

	rawOptions := writeOptions{
		Params: writeParams{
			MaxRowsPerFile:     params.MaxRowsPerFile,
			MaxRowsPerGroup:    params.MaxRowsPerGroup,
			MaxBytesPerFile:    params.MaxBytesPerFile,
			EnableStableRowIDs: params.EnableStableRowIDs,
		},
		SkipAutoCleanup: skipAutoCleanup,
	}
	if len(versionList.ptrs) > 0 {
		rawOptions.Params.DataStorageVersion = &versionList.bufs[0][0]
	}
	if autoCleanup != nil {
		if autoCleanup.OlderThanNanos < -999999999 || autoCleanup.OlderThanNanos > 999999999 {
			return writeOptions{}, nil, fmt.Errorf("ffi: auto-cleanup nanoseconds must be in [-999999999, 999999999]")
		}
		rawOptions.AutoCleanup = &autoCleanupParams{
			Interval:         autoCleanup.Interval,
			OlderThanSeconds: autoCleanup.OlderThanSeconds,
			OlderThanNanos:   autoCleanup.OlderThanNanos,
		}
	}
	return rawOptions, versionList, nil
}
