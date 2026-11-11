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
	if opts.Params.DataStorageVersion != "" {
		if err := validateCString(opts.Params.DataStorageVersion); err != nil {
			return nil, err
		}
	}
	storageList, err := storageOptionList(storageOptions)
	if err != nil {
		return nil, err
	}

	versionList := &cStringList{}
	if opts.Params.DataStorageVersion != "" {
		versionList, err = newCStringList([]string{opts.Params.DataStorageVersion})
		if err != nil {
			return nil, err
		}
	}

	var rawOptions writeOptions
	rawOptions.Params.MaxRowsPerFile = opts.Params.MaxRowsPerFile
	rawOptions.Params.MaxRowsPerGroup = opts.Params.MaxRowsPerGroup
	rawOptions.Params.MaxBytesPerFile = opts.Params.MaxBytesPerFile
	rawOptions.Params.EnableStableRowIDs = opts.Params.EnableStableRowIDs
	if len(versionList.ptrs) > 0 {
		rawOptions.Params.DataStorageVersion = &versionList.bufs[0][0]
	}
	if opts.AutoCleanup != nil {
		if opts.AutoCleanup.OlderThanNanos < -999999999 || opts.AutoCleanup.OlderThanNanos > 999999999 {
			return nil, fmt.Errorf("ffi: auto-cleanup nanoseconds must be in [-999999999, 999999999]")
		}
		cleanup := &autoCleanupParams{
			Interval:         opts.AutoCleanup.Interval,
			OlderThanSeconds: opts.AutoCleanup.OlderThanSeconds,
			OlderThanNanos:   opts.AutoCleanup.OlderThanNanos,
		}
		rawOptions.AutoCleanup = cleanup
	}
	rawOptions.SkipAutoCleanup = opts.SkipAutoCleanup

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
