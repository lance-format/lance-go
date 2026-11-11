package ffi

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

// Dataset is an opaque native LanceDataset handle.
//
// A Dataset owns one native handle. Close releases it and is idempotent, so
// only the first call reaches native code even when several goroutines call
// Close.
type Dataset struct {
	b      *bindings
	handle atomic.Uintptr
}

// OpenDataset opens the dataset at uri. A zero version selects the latest
// version. storage_opts is always NULL in this slice; storage options arrive
// with the public configuration API.
//
// The goroutine is locked to its OS thread across the native call and error
// extraction, because lance-c reports failures through thread-local storage.
func OpenDataset(uri string, version uint64) (*Dataset, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}

	return openDataset(b, uri, version)
}

// openDataset is OpenDataset with the bindings injected, so unit tests can
// exercise handle ownership without a native library.
func openDataset(b *bindings, uri string, version uint64) (*Dataset, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	handle := b.datasetOpen(uri, 0, version)
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

// Close releases the native dataset handle. It is idempotent.
func (d *Dataset) Close() {
	handle := d.handle.Swap(0)
	if handle == 0 {
		return
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	d.b.datasetClose(handle)
}
