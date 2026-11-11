// Package accessor owns a reusable native storage-options accessor and the
// dynamic Go provider behind it.
//
// The native accessor caches, expires and refreshes options. Go callbacks only
// translate one fetch request; they do not run a refresh loop or keep a second
// credential cache.
package accessor

import (
	"context"
	"fmt"

	"github.com/lance-format/lance-go/internal/ffi"
	"github.com/lance-format/lance-go/internal/lifetime"
)

// Provider supplies dynamic storage options.
//
// StorageOptionsProviderID identifies the provider to native code. Providers
// with the same ID share native equality and object-store registry identity;
// the ID is security-scoped, so different credential scopes must use different
// IDs. FetchStorageOptions returns nil for native None and a non-nil empty map
// for an explicitly empty option set. The context is valid only for the
// callback and is currently background-scoped; native refresh policy owns
// expiration and call timing. Implementations must be safe for concurrent
// calls from native worker threads.
type Provider interface {
	StorageOptionsProviderID() string
	FetchStorageOptions(ctx context.Context) (map[string]string, error)
}

// ForceProvider optionally distinguishes an explicit force refresh from a
// normal fetch. Native falls back to Provider.FetchStorageOptions when this
// interface is not implemented.
type ForceProvider interface {
	ForceFetchStorageOptions(ctx context.Context) (map[string]string, error)
}

// Accessor is a reusable, owned native storage-options accessor. It may be
// shared by datasets, file readers, file writers and file sessions. Close
// waits for local snapshots and native calls that acquired it, but not for
// native datasets or stores that retained their own reference.
type Accessor struct {
	native     *ffi.StorageOptionsAccessor
	life       *lifetime.Resource
	providerID string
}

// New creates an accessor from optional initial options and an optional
// provider. A nil initial map means native None; a non-nil map, including an
// empty one, means Some(initial). With neither initial options nor a provider
// the accessor is native None.
func New(initial map[string]string, provider Provider) (*Accessor, error) {
	if provider == nil {
		return NewStaticPresence(initial, initial != nil)
	}
	if err := ffi.ValidateStorageOptions(initial); err != nil {
		return nil, err
	}
	id, err := providerID(provider)
	if err != nil {
		return nil, err
	}

	fetch := func(ctx context.Context, force bool) (map[string]string, error) {
		if force {
			if forced, ok := provider.(ForceProvider); ok {
				return forced.ForceFetchStorageOptions(ctx)
			}
		}
		return provider.FetchStorageOptions(ctx)
	}
	var force ffi.StorageOptionsFetchFunc
	if _, ok := provider.(ForceProvider); ok {
		force = func(ctx context.Context, _ bool) (map[string]string, error) {
			return provider.(ForceProvider).ForceFetchStorageOptions(ctx)
		}
	}

	nativeProvider, err := ffi.NewStorageOptionsProvider(id, fetch, force)
	if err != nil {
		return nil, err
	}
	nativeAccessor, err := ffi.NewStorageOptionsAccessorWithPresence(initial, initial != nil, nativeProvider)
	nativeProvider.Close()
	if err != nil {
		return nil, err
	}
	return newOwned(nativeAccessor, id), nil
}

// NewStaticPresence creates a static accessor with explicit nil/empty
// presence. A nil initial map with hasInitial false means native None.
func NewStaticPresence(initial map[string]string, hasInitial bool) (*Accessor, error) {
	native, err := ffi.NewStorageOptionsAccessorWithPresence(initial, hasInitial, nil)
	if err != nil {
		return nil, err
	}
	return newOwned(native, ""), nil
}

// NewStatic creates a static accessor. A nil or empty map means Some(empty),
// matching the historical file StorageOptions behavior; use a nil accessor for
// native None.
func NewStatic(values map[string]string) (*Accessor, error) {
	return NewStaticPresence(values, true)
}

// NewFromProvider adopts a provider created by native namespace bindings. The
// temporary provider handle is closed after the accessor retains its native
// Arc. Initial options are not accepted here; the namespace provider supplies
// them from its own describe-table response.
func NewFromProvider(provider *ffi.StorageOptionsProvider) (*Accessor, error) {
	if provider == nil {
		return nil, fmt.Errorf("storage options provider must not be nil")
	}
	defer provider.Close()
	id := provider.ID()
	if id == "" {
		nativeID, err := provider.NativeID()
		if err != nil {
			return nil, err
		}
		id = nativeID
	}
	if id == "" {
		return nil, fmt.Errorf("storage options provider identity must not be empty")
	}
	native, err := ffi.NewStorageOptionsAccessorWithPresence(nil, false, provider)
	if err != nil {
		return nil, err
	}
	return newOwned(native, id), nil
}

func newOwned(native *ffi.StorageOptionsAccessor, providerID string) *Accessor {
	return &Accessor{
		native:     native,
		life:       lifetime.New(native.Close),
		providerID: providerID,
	}
}

// ProviderID returns the native identity of the provider, or an empty string
// for a purely static accessor.
func (a *Accessor) ProviderID() string {
	if a == nil {
		return ""
	}
	return a.providerID
}

// Snapshot returns the currently resolved options. A nil map with hasOptions
// false means native None; a non-nil empty map with true means Some(empty).
// The map is an independent copy and may be mutated by the caller.
func (a *Accessor) Snapshot() (map[string]string, bool, error) {
	native, release, ok := Acquire(a)
	if !ok {
		return nil, false, ffi.ErrClosed
	}
	defer release()
	return native.Snapshot()
}

// Close releases the caller's accessor handle and waits for in-flight local
// acquisitions. Native datasets and stores retain their own references, so
// they do not block Close and remain usable afterwards.
func (a *Accessor) Close() {
	if a == nil || a.life == nil {
		return
	}
	a.life.Close()
}

// Acquire pins the accessor for one native call. The release function must be
// called after the native call returns. It reports false after Close starts.
func Acquire(a *Accessor) (*ffi.StorageOptionsAccessor, func(), bool) {
	if a == nil || a.native == nil || a.life == nil {
		return nil, nil, false
	}
	if !a.life.Acquire() {
		return nil, nil, false
	}
	return a.native, a.life.Release, true
}

func providerID(provider Provider) (id string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			id = ""
			err = fmt.Errorf("storage options provider ID panic")
		}
	}()
	id = provider.StorageOptionsProviderID()
	return id, nil
}
