package file

import (
	"fmt"
	"sync"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/accessor"
	"github.com/lance-format/lance-go/internal/ffi"
)

// StorageOptions holds reusable object-store configuration. It can carry
// static options, a dynamic root StorageOptionsProvider, or both. A nil
// *StorageOptions means native defaults; NewStorageOptions always supplies an
// explicit option map.
//
// StorageOptions may be closed once every session, reader, and writer opened
// with it has returned from its open call. Native handles clone or retain the
// resolved accessor, so later use of those handles is unaffected. The provider
// value passed to NewStorageOptionsWithProvider remains owned by the caller;
// this type owns only the native accessor and its retained callback context.
type StorageOptions struct {
	mu       sync.RWMutex
	accessor *accessor.Accessor
}

// NewStorageOptions creates static storage options. An empty map produces an
// explicit empty option set, which is different from passing nil.
func NewStorageOptions(values map[string]string) (*StorageOptions, error) {
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	owned, err := accessor.NewStatic(copied)
	if err != nil {
		return nil, wrapError("storage options", "", err)
	}
	return &StorageOptions{accessor: owned}, nil
}

// NewStorageOptionsWithProvider creates storage options with a dynamic
// provider and optional initial options. A nil initial map means native None;
// a non-nil map, including an empty one, means an explicit initial set.
//
// The provider is retained by native datasets, readers, writers and sessions
// for as long as they need it. Callers may close the returned StorageOptions
// after open calls return without closing the provider themselves.
func NewStorageOptionsWithProvider(initial map[string]string, provider lance.StorageOptionsProvider) (*StorageOptions, error) {
	if provider == nil {
		return nil, fmt.Errorf("storage options provider must not be nil")
	}
	owned, err := lance.NewStorageOptionsAccessor(initial, provider)
	if err != nil {
		return nil, wrapError("storage options", "", err)
	}
	return &StorageOptions{accessor: owned}, nil
}

// NewStorageOptionsFromAccessor adopts an already-created root storage-options
// accessor. Ownership transfers to the returned StorageOptions; closing it
// closes the accessor. The accessor must not be closed separately while it is
// in use here.
func NewStorageOptionsFromAccessor(owned *lance.StorageOptionsAccessor) (*StorageOptions, error) {
	if owned == nil {
		return nil, fmt.Errorf("storage options accessor must not be nil")
	}
	return &StorageOptions{accessor: owned}, nil
}

// Close releases the native accessor. It is idempotent and waits for open
// calls currently acquiring this StorageOptions, but not for native handles
// that retained their own accessor reference.
func (o *StorageOptions) Close() error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	owned := o.accessor
	o.accessor = nil
	o.mu.Unlock()
	if owned != nil {
		owned.Close()
	}
	return nil
}

// lockStorage pins the shared accessor for one native open call. The returned
// release function must run after the native call returns.
func lockStorage(opts *StorageOptions) (func(), *ffi.StorageOptionsAccessor, error) {
	if opts == nil {
		return func() {}, nil, nil
	}
	opts.mu.RLock()
	if opts.accessor == nil {
		opts.mu.RUnlock()
		return nil, nil, closedError("storage options", "")
	}
	native, release, ok := accessor.Acquire(opts.accessor)
	if !ok {
		opts.mu.RUnlock()
		return nil, nil, closedError("storage options", "")
	}
	return func() {
		release()
		opts.mu.RUnlock()
	}, native, nil
}
