package ffi

import (
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
