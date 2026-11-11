package ffi

import (
	"fmt"
	"runtime"
)

// Clone returns an independent handle to the same native snapshot as d.
//
// The clone shares the underlying Arc<Dataset> but has its own lock, so
// mutating or closing either handle does not change the other handle's
// snapshot. Cloning is an in-memory operation: it performs no storage I/O and
// does not reopen the dataset by URI.
func (d *Dataset) Clone() (*Dataset, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cloned := b.datasetCloneHandle(handle)
	if cloned == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_clone_handle returned NULL without setting an error")
	}

	out := &Dataset{b: b}
	out.handle.Store(cloned)
	return out, nil
}
