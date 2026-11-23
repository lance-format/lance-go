package lance

import (
	"context"

	"github.com/lance-format/lance-go/internal/accessor"
	"github.com/lance-format/lance-go/internal/ffi"
	"github.com/lance-format/lance-go/namespace"
)

// StorageOptionsProvider supplies dynamic storage options to Lance.
//
// StorageOptionsProviderID defines native provider identity, equality and
// object-store registry cache scope. Providers that expose different
// credential scopes must use different IDs; sharing an ID is an explicit
// decision to share the native store and its cached credentials.
//
// FetchStorageOptions may be called concurrently from native worker threads.
// It returns nil for native absence and a non-nil empty map for an explicitly
// empty option set. The context is background-scoped and valid only for the
// callback; it cannot cancel native I/O. Native code owns expiration, caching
// and fallback; a provider must not start a refresh goroutine or retain fetched
// credentials in a second Go cache.
//
// A StorageOptionsProvider has the following methods:
//
//	StorageOptionsProviderID() string
//	FetchStorageOptions(ctx context.Context) (map[string]string, error)
//
// For example, this provider returns a fixed option map; a real provider
// should fetch current credentials and endpoint settings:
//
//	type tenantProvider struct{}
//
//	func (tenantProvider) StorageOptionsProviderID() string { return "tenant-a/read" }
//
//	func (tenantProvider) FetchStorageOptions(ctx context.Context) (map[string]string, error) {
//		return map[string]string{"allow_http": "true"}, nil
//	}
//
//	accessor, err := NewStorageOptionsAccessor(nil, tenantProvider{})
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer accessor.Close()
type StorageOptionsProvider interface {
	StorageOptionsProviderID() string
	FetchStorageOptions(ctx context.Context) (map[string]string, error)
}

// ForceStorageOptionsProvider optionally handles an explicit force refresh.
// Without it, native force fetch falls back to StorageOptionsProvider.
//
// ForceStorageOptionsProvider has the following method:
//
//	ForceFetchStorageOptions(ctx context.Context) (map[string]string, error)
//
// It has the same nil-versus-empty and concurrency contract as
// StorageOptionsProvider.FetchStorageOptions.
type ForceStorageOptionsProvider interface {
	ForceFetchStorageOptions(ctx context.Context) (map[string]string, error)
}

// StorageOptionsAccessor is a reusable owned accessor for dynamic storage
// options. It may be supplied to Open, Write, WriteUncommitted,
// CommitTransaction and CommitTransactionBatch, and to raw-file readers,
// writers and sessions.
//
// The accessor contains its initial static options and optional provider.
// A nil initial map means native None; a non-nil map, including an empty one,
// means an explicit initial set. Valid initial options are used without
// fetching. After they expire, provider options replace the initial set; a nil
// provider result falls back to the initial set.
//
// A StorageOptionsAccessor has the following methods:
//
//	func (a *StorageOptionsAccessor) ProviderID() string
//	func (a *StorageOptionsAccessor) Snapshot() (map[string]string, bool, error)
//	func (a *StorageOptionsAccessor) Close()
//
// ProviderID returns the native provider identity, or an empty string for a
// purely static accessor. Snapshot returns the currently resolved options and
// whether native has an option set: a nil map with false means native None,
// while a non-nil empty map with true means Some(empty). The map is an
// independent copy. Close is idempotent and waits for in-flight local snapshot
// calls, but does not wait for native datasets or stores that retained their
// own reference. Callers own the provider value passed to
// NewStorageOptionsAccessor; the accessor does not call a Go provider Close
// method.
type StorageOptionsAccessor struct {
	core *accessor.Accessor
}

// init installs the private implementation lookup used by package file.
// The public type must stay in this package for go doc indexing, and file
// cannot read its unexported field directly.
func init() {
	accessor.SetResolver(func(owner any) (*accessor.Accessor, bool) {
		owned, ok := owner.(*StorageOptionsAccessor)
		if !ok || owned == nil || owned.core == nil {
			return nil, false
		}
		return owned.core, true
	})
}

// ProviderID returns the native identity of the provider, or an empty string
// for a purely static accessor.
func (a *StorageOptionsAccessor) ProviderID() string {
	if a == nil || a.core == nil {
		return ""
	}
	return a.core.ProviderID()
}

// Snapshot returns the currently resolved options. A nil map with hasOptions
// false means native None; a non-nil empty map with true means Some(empty).
// The map is an independent copy and may be mutated by the caller. Native
// wraps provider fetch failures as IO errors while retaining their messages.
func (a *StorageOptionsAccessor) Snapshot() (map[string]string, bool, error) {
	if a == nil || a.core == nil {
		return nil, false, ffi.ErrClosed
	}
	return a.core.Snapshot()
}

// Close releases the caller's accessor handle and waits for in-flight local
// acquisitions. Native datasets and stores retain their own references, so
// they do not block Close and remain usable afterwards.
func (a *StorageOptionsAccessor) Close() {
	if a == nil || a.core == nil {
		return
	}
	a.core.Close()
}

// NewStorageOptionsAccessor creates a reusable accessor. A nil initial map
// means native None; a non-nil map, including an empty one, means Some(initial).
// Native uses valid initial options without fetching. After expiration, provider
// options replace the initial set; a nil result falls back to the initial set.
func NewStorageOptionsAccessor(initial map[string]string, provider StorageOptionsProvider) (*StorageOptionsAccessor, error) {
	core, err := accessor.New(initial, provider, wrapError)
	if err != nil {
		return nil, wrapError("create storage options accessor", "", err)
	}
	return &StorageOptionsAccessor{core: core}, nil
}

// NewNamespaceStorageOptionsAccessor creates a table-scoped accessor backed by
// native namespace credential vending. It is intended for raw-file readers,
// writers and sessions that need the same credentials as namespace-managed
// tables.
//
// The provider retains the native namespace. Closing the public namespace
// handle does not invalidate the accessor or datasets and stores that already
// retained it; the native namespace is released by its final native owner.
// tableID uses the same typed string IDs as OpenFromNamespace and friends.
func NewNamespaceStorageOptionsAccessor(ns namespace.Namespace, tableID []string) (*StorageOptionsAccessor, error) {
	if len(tableID) == 0 {
		return nil, invalidError("create namespace storage options accessor", "", "table id must not be empty")
	}
	lease, err := acquireNamespace(ns, "create namespace storage options accessor")
	if err != nil {
		return nil, err
	}
	defer lease.Release()

	provider, err := lease.NewStorageOptionsProvider(tableID)
	if err != nil {
		return nil, wrapError("create namespace storage options provider", "", err)
	}
	core, err := accessor.NewFromProvider(provider, wrapError)
	if err != nil {
		return nil, wrapError("create namespace storage options accessor", "", err)
	}
	return &StorageOptionsAccessor{core: core}, nil
}

// acquireStorageOptionsAccessor pins a caller-owned accessor for one native
// call. A nil accessor is native None.
func acquireStorageOptionsAccessor(op, uri string, options *StorageOptionsAccessor) (*ffi.StorageOptionsAccessor, func(), error) {
	if options == nil {
		return nil, func() {}, nil
	}
	if options.core == nil {
		return nil, nil, closedError(op, uri)
	}
	native, release, ok := accessor.Acquire(options.core)
	if !ok {
		return nil, nil, closedError(op, uri)
	}
	return native, release, nil
}

// resolveStorageOptions applies the static/accessor exclusivity rule once, so
// option order cannot change the result.
func resolveStorageOptions(op, uri string, staticOptions map[string]string, staticOptionsSet bool, options *StorageOptionsAccessor) (*ffi.StorageOptionsAccessor, func(), error) {
	if (staticOptionsSet || staticOptions != nil) && options != nil {
		return nil, nil, invalidError(op, uri, "storage options and storage options accessor are mutually exclusive")
	}
	return acquireStorageOptionsAccessor(op, uri, options)
}

// resolveLockStorageOptions supplies the accessor-only storage interface used
// by commit-lock overloads. Non-empty static options are bridged to a transient
// accessor that native code retains; nil and empty static options remain native
// None, matching the older static entry points.
func resolveLockStorageOptions(op, uri string, staticOptions map[string]string, staticOptionsSet bool, options *StorageOptionsAccessor) (*ffi.StorageOptionsAccessor, func(), error) {
	if (staticOptionsSet || staticOptions != nil) && options != nil {
		return nil, nil, invalidError(op, uri, "storage options and storage options accessor are mutually exclusive")
	}
	if options != nil {
		return acquireStorageOptionsAccessor(op, uri, options)
	}
	if len(staticOptions) == 0 {
		return nil, func() {}, nil
	}
	native, err := ffi.NewStorageOptionsAccessorWithPresence(staticOptions, true, nil)
	if err != nil {
		return nil, nil, invalidError(op, uri, "%v", err)
	}
	return native, native.Close, nil
}
