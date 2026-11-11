package ffi

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

// DataFile is an owned snapshot of native Lance DataFile metadata. It remains
// valid after the dataset, fragment or operation that produced it is closed.
type DataFile struct {
	b      *bindings
	handle atomic.Uintptr
}

func wrapDataFile(b *bindings, handle uintptr) *DataFile {
	f := &DataFile{b: b}
	f.handle.Store(handle)
	return f
}

// CreateDataFile creates native data-file metadata from an existing Lance file.
func (d *Dataset) CreateDataFile(path string, baseID *uint32) (*DataFile, error) {
	handleValue := d.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(path); err != nil {
		return nil, err
	}

	var out uintptr
	runtime.LockOSThread()
	rc := d.b.datasetCreateDataFile(handleValue, path, baseID, &out)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_create_data_file failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_dataset_create_data_file returned NULL without setting an error")
	}
	return wrapDataFile(d.b, out), nil
}

// Close releases the data-file handle. It is idempotent.
func (f *DataFile) Close() {
	handle := f.handle.Swap(0)
	if handle == 0 {
		return
	}
	runtime.LockOSThread()
	f.b.dataFileClose(handle)
	runtime.UnlockOSThread()
}

// Clone returns an independently owned metadata snapshot.
func (f *DataFile) Clone() (*DataFile, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}

	runtime.LockOSThread()
	out := f.b.dataFileClone(handleValue)
	if out == 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_data_file_clone returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapDataFile(f.b, out), nil
}

// Path returns the native data-file path.
func (f *DataFile) Path() (string, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return "", ErrClosed
	}

	runtime.LockOSThread()
	ptr := f.b.dataFilePath(handleValue)
	if ptr == nil {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_data_file_path returned NULL without setting an error")
	}
	value := goString(ptr)
	runtime.UnlockOSThread()
	return value, nil
}

// FieldIDCount returns the number of native field IDs.
func (f *DataFile) FieldIDCount() (uint64, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0, ErrClosed
	}

	runtime.LockOSThread()
	count := f.b.dataFileFieldCount(handleValue)
	nativeErr := f.b.takeError()
	runtime.UnlockOSThread()
	if nativeErr != nil {
		return 0, nativeErr
	}
	return count, nil
}

// FieldIDAt returns one native field ID.
func (f *DataFile) FieldIDAt(index uint64) (int32, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0, ErrClosed
	}

	var fieldID int32
	runtime.LockOSThread()
	rc := f.b.dataFileFieldIDAt(handleValue, index, &fieldID)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_data_file_field_id_at failed without setting an error")
	}
	runtime.UnlockOSThread()
	return fieldID, nil
}

// ColumnIndexCount returns the number of native column indices.
func (f *DataFile) ColumnIndexCount() (uint64, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0, ErrClosed
	}

	runtime.LockOSThread()
	count := f.b.dataFileColumnIndexCount(handleValue)
	nativeErr := f.b.takeError()
	runtime.UnlockOSThread()
	if nativeErr != nil {
		return 0, nativeErr
	}
	return count, nil
}

// ColumnIndexAt returns one native column index, including the native -1 marker.
func (f *DataFile) ColumnIndexAt(index uint64) (int32, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0, ErrClosed
	}

	var columnIndex int32
	runtime.LockOSThread()
	rc := f.b.dataFileColumnIndexAt(handleValue, index, &columnIndex)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_data_file_column_index_at failed without setting an error")
	}
	runtime.UnlockOSThread()
	return columnIndex, nil
}

// FileVersion returns the native data-file format version.
func (f *DataFile) FileVersion() (uint32, uint32, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0, 0, ErrClosed
	}

	var major, minor uint32
	runtime.LockOSThread()
	rc := f.b.dataFileFileVersion(handleValue, &major, &minor)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, 0, nativeErr
		}
		return 0, 0, fmt.Errorf("ffi: lance_data_file_file_version failed without setting an error")
	}
	runtime.UnlockOSThread()
	return major, minor, nil
}

// FileSize returns the optional native data-file size.
func (f *DataFile) FileSize() (uint64, bool, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0, false, ErrClosed
	}

	var isSet bool
	var size uint64
	runtime.LockOSThread()
	rc := f.b.dataFileFileSize(handleValue, &isSet, &size)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, false, nativeErr
		}
		return 0, false, fmt.Errorf("ffi: lance_data_file_file_size failed without setting an error")
	}
	runtime.UnlockOSThread()
	return size, isSet, nil
}

// BaseID returns the optional native base ID.
func (f *DataFile) BaseID() (uint32, bool, error) {
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return 0, false, ErrClosed
	}

	var isSet bool
	var baseID uint32
	runtime.LockOSThread()
	rc := f.b.dataFileBaseID(handleValue, &isSet, &baseID)
	if rc != 0 {
		nativeErr := f.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, false, nativeErr
		}
		return 0, false, fmt.Errorf("ffi: lance_data_file_base_id failed without setting an error")
	}
	runtime.UnlockOSThread()
	return baseID, isSet, nil
}
