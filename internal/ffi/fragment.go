package ffi

import (
	"fmt"
	"runtime"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
)

// Fragment is an owned snapshot of native Lance fragment metadata. A bound
// fragment reads the dataset version it was opened from and remains valid
// after the source dataset is closed or mutated. A detached fragment supports
// metadata inspection and operation construction only.
type Fragment struct {
	b      *bindings
	handle atomic.Uintptr
}

func wrapFragment(b *bindings, handle uintptr) *Fragment {
	f := &Fragment{b: b}
	f.handle.Store(handle)
	return f
}

// FragmentCount returns the number of fragments in the dataset snapshot.
func (d *Dataset) FragmentCount() (uint64, error) {
	handleValue := d.handle.Load()
	if handleValue == 0 {
		return 0, ErrClosed
	}

	runtime.LockOSThread()
	count := d.b.datasetFragmentCount(handleValue)
	nativeErr := d.b.takeError()
	runtime.UnlockOSThread()
	if nativeErr != nil {
		return 0, nativeErr
	}
	return count, nil
}

// FragmentIDs returns the fragment IDs in manifest order.
func (d *Dataset) FragmentIDs() ([]uint64, error) {
	count, err := d.FragmentCount()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	if count > uint64(int(^uint(0)>>1)) {
		return nil, fmt.Errorf("ffi: fragment count %d exceeds addressable slice length", count)
	}

	ids := make([]uint64, int(count))
	handleValue := d.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	runtime.LockOSThread()
	rc := d.b.datasetFragmentIDs(handleValue, ids)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_fragment_ids failed without setting an error")
	}
	runtime.UnlockOSThread()
	return ids, nil
}

// OpenFragment resolves a bound fragment snapshot by ID.
func (d *Dataset) OpenFragment(id uint64) (*Fragment, error) {
	handleValue := d.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}

	runtime.LockOSThread()
	out := d.b.datasetOpenFragment(handleValue, id)
	if out == 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_open_fragment returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapFragment(d.b, out), nil
}

// Close releases the fragment handle. It is idempotent.
func (f *Fragment) Close() {
	handleValue := f.handle.Swap(0)
	if handleValue == 0 {
		return
	}
	runtime.LockOSThread()
	f.b.fragmentClose(handleValue)
	runtime.UnlockOSThread()
}

// ID returns the native fragment ID. Zero is valid.
func (f *Fragment) ID() uint64 {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0
	}
	runtime.LockOSThread()
	id := f.b.fragmentID(handleValue)
	runtime.UnlockOSThread()
	return id
}

// DataFileCount returns the number of native base data files. Overlay and
// deletion files are not included.
func (f *Fragment) DataFileCount() (uint64, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0, ErrClosed
	}

	runtime.LockOSThread()
	count := f.b.fragmentDataFileCount(handleValue)
	nativeErr := f.b.takeError()
	runtime.UnlockOSThread()
	if nativeErr != nil {
		return 0, nativeErr
	}
	return count, nil
}

// IsBound reports whether dataset-dependent reads are available.
func (f *Fragment) IsBound() (bool, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return false, ErrClosed
	}

	var bound bool
	runtime.LockOSThread()
	rc := f.b.fragmentIsBound(handleValue, &bound)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return false, nativeErr
		}
		return false, fmt.Errorf("ffi: lance_fragment_is_bound failed without setting an error")
	}
	runtime.UnlockOSThread()
	return bound, nil
}

// DataFileAt clones one native base data file.
func (f *Fragment) DataFileAt(index uint64) (*DataFile, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}

	var out uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentDataFileAt(handleValue, index, &out)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_data_file_at failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_fragment_data_file_at returned NULL without setting an error")
	}
	return wrapDataFile(f.b, out), nil
}

// DataFileForField finds the base data file storing fieldID. It returns
// (nil, nil) when the field is not present in any base data file.
func (f *Fragment) DataFileForField(fieldID int32) (*DataFile, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	if fieldID < 0 {
		return nil, fmt.Errorf("ffi: field ID must not be negative")
	}

	var out uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentDataFileForField(handleValue, fieldID, &out)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_data_file_for_field failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, nil
	}
	return wrapDataFile(f.b, out), nil
}

// MetadataJSON returns the native Fragment JSON serialization.
func (f *Fragment) MetadataJSON() ([]byte, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}

	runtime.LockOSThread()
	ptr := f.b.fragmentMetadataJSON(handleValue)
	if ptr == nil {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_metadata_json returned NULL without setting an error")
	}
	value := goString(ptr)
	f.b.freeString(ptr)
	runtime.UnlockOSThread()
	return []byte(value), nil
}

// Schema exports the fragment's lossy Arrow logical schema.
func (f *Fragment) Schema() (*arrow.Schema, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}

	var raw cdata.ArrowSchema
	runtime.LockOSThread()
	rc := f.b.fragmentSchema(handleValue, &raw)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_schema failed without setting an error")
	}
	runtime.UnlockOSThread()

	defer cdata.ReleaseSchema(&raw)
	schema, err := cdata.ImportSchema(&raw)
	if err != nil {
		return nil, fmt.Errorf("ffi: import fragment schema: %w", err)
	}
	return schema, nil
}

// NativeSchema clones the fragment's native logical schema.
func (f *Fragment) NativeSchema() (*Schema, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}

	var out uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentSchemaClone(handleValue, &out)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_schema_clone failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_fragment_schema_clone returned NULL without setting an error")
	}
	return wrapSchema(f.b, out), nil
}

// PhysicalRows returns the physical row count, including deleted rows.
func (f *Fragment) PhysicalRows() (uint64, error) {
	return f.rowCount("lance_fragment_physical_rows", f.b.fragmentPhysicalRows)
}

// DeletedRows returns the deleted-row count.
func (f *Fragment) DeletedRows() (uint64, error) {
	return f.rowCount("lance_fragment_deleted_rows", f.b.fragmentDeletedRows)
}

// LogicalRows returns the logical row count.
func (f *Fragment) LogicalRows() (uint64, error) {
	return f.rowCount("lance_fragment_logical_rows", f.b.fragmentLogicalRows)
}

func (f *Fragment) rowCount(name string, call func(uintptr, *uint64) int32) (uint64, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0, ErrClosed
	}

	var rows uint64
	runtime.LockOSThread()
	rc := call(handleValue, &rows)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: %s failed without setting an error", name)
	}
	runtime.UnlockOSThread()
	return rows, nil
}

// Scan starts a scanner over the fragment's bound snapshot.
//
// Native fragment scans do not accept initial column selection or a SQL filter;
// callers must use projection expressions or filter through the returned
// scanner's supported setters.
func (f *Fragment) Scan(opts ScannerOptions, initialize func(*Scanner) error) (*Scanner, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	if len(opts.Columns) > 0 {
		return nil, fmt.Errorf("ffi: fragment scan does not support initial column selection")
	}
	if opts.Filter != nil {
		return nil, fmt.Errorf("ffi: fragment scan does not support an initial SQL filter")
	}

	runtime.LockOSThread()
	scannerHandle := f.b.fragmentScan(handleValue)
	if scannerHandle == 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_scan returned NULL without setting an error")
	}
	runtime.UnlockOSThread()

	return newScanner(f.b, scannerHandle, opts, initialize)
}

// Take returns fragment-local logical rows by index. A nil columns slice
// selects the full logical schema; a non-nil empty slice requests an explicit
// empty projection.
func (f *Fragment) Take(indices []uint32, columns []string) ([]arrow.RecordBatch, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	columnList, err := fragmentColumnList(columns)
	if err != nil {
		return nil, err
	}

	indexPtr := indices
	if len(indexPtr) == 0 {
		indexPtr = []uint32{0}
	}

	var stream cdata.ArrowArrayStream
	runtime.LockOSThread()
	rc := f.b.fragmentTake(handleValue, indexPtr, uint64(len(indices)), columnList.ptrs, &stream)
	runtime.KeepAlive(indexPtr)
	columnList.keepAlive()
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_take failed without setting an error")
	}
	runtime.UnlockOSThread()

	return importArrowStream(&stream)
}

func fragmentColumnList(columns []string) (*cStringList, error) {
	if columns == nil {
		return &cStringList{}, nil
	}
	if len(columns) > 0 {
		return newCStringList(columns)
	}
	// An explicit empty projection needs a non-NULL pointer whose first entry
	// is NULL so native take does not treat it as the full schema.
	return &cStringList{ptrs: []uintptr{0}}, nil
}

// CountRows counts fragment rows, optionally applying a native SQL filter.
// A nil filter selects no filter; a non-nil empty string is passed as Some("").
func (f *Fragment) CountRows(filter *string) (uint64, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0, ErrClosed
	}
	var filterPtr uintptr
	var filterList *cStringList
	if filter != nil {
		list, err := newCStringList([]string{*filter})
		if err != nil {
			return 0, err
		}
		filterList = list
		filterPtr = list.ptrs[0]
	}

	var rows uint64
	runtime.LockOSThread()
	rc := f.b.fragmentCountRows(handleValue, filterPtr, &rows)
	if filterList != nil {
		filterList.keepAlive()
	}
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_fragment_count_rows failed without setting an error")
	}
	runtime.UnlockOSThread()
	return rows, nil
}

// Delete stages a deletion on this fragment snapshot and returns the updated
// metadata. It returns (nil, nil) when the predicate deletes every row.
func (f *Fragment) Delete(predicate string) (*Fragment, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(predicate); err != nil {
		return nil, err
	}

	var out uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentDelete(handleValue, predicate, &out)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_delete failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, nil
	}
	return wrapFragment(f.b, out), nil
}

func (f *Fragment) String() string {
	if f == nil {
		return "<nil>"
	}
	bound, err := f.IsBound()
	if err != nil {
		return fmt.Sprintf("Fragment{closed}")
	}
	return fmt.Sprintf("Fragment{id=%d bound=%t}", f.ID(), bound)
}
