package file

import (
	"sync"

	"github.com/lance-format/lance-go/internal/ffi"
)

// StorageOptions holds static object-store configuration. Dynamic Go
// providers are not part of this slice. A nil *StorageOptions means native
// defaults; NewStorageOptions always supplies an explicit option map.
//
// StorageOptions may be closed once every session, reader, and writer opened
// with it has returned from its open call. Native handles clone the resolved
// store, so later use of those handles is unaffected.
type StorageOptions struct {
	mu       sync.RWMutex
	accessor *ffi.StorageOptionsAccessor
}

// NewStorageOptions creates static storage options. An empty map produces an
// explicit empty option set, which is different from passing nil.
func NewStorageOptions(values map[string]string) (*StorageOptions, error) {
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	accessor, err := ffi.NewStorageOptionsAccessor(copied)
	if err != nil {
		return nil, wrapError("storage options", "", err)
	}
	return &StorageOptions{accessor: accessor}, nil
}

// Close releases the native accessor. It is idempotent and waits for native
// open calls currently using the accessor.
func (o *StorageOptions) Close() error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	accessor := o.accessor
	o.accessor = nil
	o.mu.Unlock()
	accessor.Close()
	return nil
}

func lockStorage(opts *StorageOptions) (func(), *ffi.StorageOptionsAccessor, error) {
	if opts == nil {
		return func() {}, nil, nil
	}
	opts.mu.RLock()
	if opts.accessor == nil {
		opts.mu.RUnlock()
		return nil, nil, closedError("storage options", "")
	}
	return opts.mu.RUnlock, opts.accessor, nil
}
