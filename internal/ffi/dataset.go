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
// version. This compatibility wrapper uses no storage options or session.
func OpenDataset(uri string, version uint64) (*Dataset, error) {
	return OpenDatasetWithOptions(uri, OpenOptions{Version: version})
}

func openDataset(b *bindings, uri string, version uint64) (*Dataset, error) {
	return openDatasetWithOptions(b, uri, OpenOptions{Version: version})
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

// URI returns a copy of the snapshot's fully qualified native URI.
//
// The value is stable for the lifetime of the native snapshot and is copied
// into Go memory before return, so it remains valid after the dataset closes.
func (d *Dataset) URI() (string, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return "", ErrClosed
	}

	b := d.b
	runtime.LockOSThread()
	ptr := b.datasetURI(handle)
	if ptr == nil {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_dataset_uri returned NULL without setting an error")
	}
	uri := goString(ptr)
	b.freeString(ptr)
	runtime.UnlockOSThread()
	return uri, nil
}

func (d *Dataset) jsonString(op string, call func(handle uintptr) *byte) (string, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return "", ErrClosed
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ptr := call(handle)
	if ptr == nil {
		return "", nativeNullError(b, op)
	}
	text := goString(ptr)
	b.freeString(ptr)
	return text, nil
}

func (d *Dataset) jsonBytes(op string, call func(uintptr) *byte) ([]byte, error) {
	text, err := d.jsonString(op, call)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}
