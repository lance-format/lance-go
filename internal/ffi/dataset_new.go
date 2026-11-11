package ffi

import (
	"fmt"
	"runtime"
	"sort"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
)

// OpenOptions controls dataset opening and metadata selection.
type OpenOptions struct {
	// StorageOptions are passed to lance-c as NULL-terminated key/value pairs.
	StorageOptions map[string]string
	// Version selects a dataset version. Zero selects the latest version.
	Version uint64
	// Session shares caches with other datasets. It may be nil.
	Session *Session
}

// OpenDatasetWithOptions opens a dataset with storage options, a version and
// an optional shared session.
func OpenDatasetWithOptions(uri string, opts OpenOptions) (*Dataset, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return openDatasetWithOptions(b, uri, opts)
}

func openDatasetWithOptions(b *bindings, uri string, opts OpenOptions) (*Dataset, error) {
	if err := validateCString(uri); err != nil {
		return nil, err
	}
	storageList, err := storageOptionList(opts.StorageOptions)
	if err != nil {
		return nil, err
	}
	var sessionHandle uintptr
	if opts.Session != nil {
		sessionHandle = opts.Session.nativeHandle()
		if sessionHandle == 0 {
			return nil, fmt.Errorf("ffi: session is closed")
		}
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var handle uintptr
	if opts.Session != nil {
		handle = b.datasetOpenWithSession(uri, storageList.ptrs, opts.Version, sessionHandle)
	} else {
		handle = b.datasetOpen(uri, storageList.ptrs, opts.Version)
	}
	storageList.keepAlive()
	if handle == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_open returned NULL without setting an error")
	}

	ds := &Dataset{b: b}
	ds.handle.Store(handle)
	return ds, nil
}

func storageOptionList(options map[string]string) (*cStringList, error) {
	if len(options) == 0 {
		return &cStringList{}, nil
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	values := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		if key == "" {
			return nil, fmt.Errorf("ffi: storage option key must not be empty")
		}
		values = append(values, key, options[key])
	}
	return newCStringList(values)
}

// Version returns the checked-out dataset version.
func (d *Dataset) Version() (uint64, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	version := d.b.datasetVersion(handle)
	if version == 0 {
		if nativeErr := d.b.takeError(); nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_dataset_version returned 0 without setting an error")
	}
	return version, nil
}

// CountRows returns the dataset row count from the manifest.
func (d *Dataset) CountRows() (uint64, error) {
	return d.countRows("", false)
}

// CountRowsFiltered returns the row count matching a native SQL filter.
func (d *Dataset) CountRowsFiltered(filter string) (uint64, error) {
	return d.countRows(filter, true)
}

func (d *Dataset) countRows(filter string, filtered bool) (uint64, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var rows uint64
	if filtered {
		rows = d.b.datasetCountRowsFiltered(handle, filter)
	} else {
		rows = d.b.datasetCountRows(handle)
	}
	if nativeErr := d.b.takeError(); nativeErr != nil {
		return 0, nativeErr
	}
	return rows, nil
}

// LatestVersion returns the latest committed dataset version.
func (d *Dataset) LatestVersion() (uint64, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	version := d.b.datasetLatestVersion(handle)
	if nativeErr := d.b.takeError(); nativeErr != nil {
		return 0, nativeErr
	}
	if version == 0 {
		return 0, fmt.Errorf("ffi: lance_dataset_latest_version returned 0 without setting an error")
	}
	return version, nil
}

// Validate checks the dataset for consistency.
func (d *Dataset) Validate() error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if rc := d.b.datasetValidate(handle); rc != 0 {
		if nativeErr := d.b.takeError(); nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_validate failed without setting an error")
	}
	return nil
}

// Schema returns the dataset schema.
func (d *Dataset) Schema() (schema *arrow.Schema, err error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}

	var raw cdata.ArrowSchema
	runtime.LockOSThread()
	rc := d.b.datasetSchema(handle, &raw)
	if rc != 0 {
		if nativeErr := d.b.takeError(); nativeErr != nil {
			runtime.UnlockOSThread()
			return nil, nativeErr
		}
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("ffi: lance_dataset_schema failed without setting an error")
	}
	runtime.UnlockOSThread()

	defer cdata.ReleaseSchema(&raw)
	schema, err = cdata.ImportSchema(&raw)
	if err != nil {
		return nil, fmt.Errorf("ffi: import dataset schema: %w", err)
	}
	return schema, nil
}

// FieldStatistics reports compressed on-disk bytes for one schema field.
type FieldStatistics struct {
	FieldID     uint32
	BytesOnDisk uint64
}

// DataStatistics computes per-field compressed on-disk statistics.
func (d *Dataset) DataStatistics() ([]FieldStatistics, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}

	runtime.LockOSThread()
	stats := d.b.datasetDataStats(handle)
	if stats == 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_calculate_data_stats returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	defer d.b.dataStatsClose(stats)

	runtime.LockOSThread()
	count := d.b.dataStatsCount(stats)
	countErr := d.b.takeError()
	runtime.UnlockOSThread()
	if countErr != nil {
		return nil, countErr
	}

	result := make([]FieldStatistics, 0, count)
	for i := uint64(0); i < count; i++ {
		runtime.LockOSThread()
		id := d.b.dataStatsFieldIDAt(stats, i)
		idErr := d.b.takeError()
		bytes := d.b.dataStatsBytesAt(stats, i)
		bytesErr := d.b.takeError()
		runtime.UnlockOSThread()
		if idErr != nil {
			return nil, idErr
		}
		if bytesErr != nil {
			return nil, bytesErr
		}
		result = append(result, FieldStatistics{FieldID: id, BytesOnDisk: bytes})
	}
	return result, nil
}

// Take reads rows by zero-based offsets. The returned batches are owned by the
// caller and must each be released.
func (d *Dataset) Take(indices []uint64, columns []string) ([]arrow.RecordBatch, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	columnList, err := newCStringList(columns)
	if err != nil {
		return nil, err
	}

	// lance-c rejects a NULL indices pointer even when the length is zero.
	indexPtr := indices
	if len(indexPtr) == 0 {
		indexPtr = []uint64{0}
	}

	var stream cdata.ArrowArrayStream
	runtime.LockOSThread()
	rc := d.b.datasetTake(handle, indexPtr, uint64(len(indices)), columnList.ptrs, &stream)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_take failed without setting an error")
	}
	runtime.UnlockOSThread()
	columnList.keepAlive()

	return importArrowStream(&stream)
}

// TakeRows reads rows by dataset row IDs. Missing or deleted row IDs may be
// omitted by native Lance. The returned batches are owned by the caller and
// must each be released.
func (d *Dataset) TakeRows(rowIDs []uint64, columns []string) ([]arrow.RecordBatch, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	columnList, err := newCStringList(columns)
	if err != nil {
		return nil, err
	}

	var stream cdata.ArrowArrayStream
	runtime.LockOSThread()
	rc := d.b.datasetTakeRows(handle, rowIDs, uint64(len(rowIDs)), columnList.ptrs, &stream)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_take_rows failed without setting an error")
	}
	runtime.UnlockOSThread()
	columnList.keepAlive()

	return importArrowStream(&stream)
}
