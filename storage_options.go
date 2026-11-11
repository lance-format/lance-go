package lance

import (
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
// empty option set. Native code owns expiration, caching and fallback; a
// provider must not start a refresh goroutine or retain fetched credentials in
// a second Go cache.
type StorageOptionsProvider = accessor.Provider

// ForceStorageOptionsProvider optionally handles an explicit force refresh.
// Without it, native force fetch falls back to StorageOptionsProvider.
type ForceStorageOptionsProvider = accessor.ForceProvider

// StorageOptionsAccessor is a reusable owned accessor for dynamic storage
// options. It may be supplied to Open, Write, WriteUncommitted,
// CommitTransaction and CommitTransactionBatch, and to raw-file readers,
// writers and sessions.
//
// The accessor contains its initial static options and optional provider.
// Closing it waits for in-flight local snapshot calls but does not wait for
// native datasets or stores that retained their own reference. Callers own the
// provider value passed to NewStorageOptionsAccessor; the accessor does not
// call a Go provider Close method.
type StorageOptionsAccessor = accessor.Accessor

// NewStorageOptionsAccessor creates a reusable accessor. A nil initial map
// means native None; a non-nil map, including an empty one, means Some(initial).
func NewStorageOptionsAccessor(initial map[string]string, provider StorageOptionsProvider) (*StorageOptionsAccessor, error) {
	created, err := accessor.New(initial, provider, wrapError)
	if err != nil {
		return nil, wrapError("create storage options accessor", "", err)
	}
	return created, nil
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
	created, err := accessor.NewFromProvider(provider, wrapError)
	if err != nil {
		return nil, wrapError("create namespace storage options accessor", "", err)
	}
	return created, nil
}

// acquireStorageOptionsAccessor pins a caller-owned accessor for one native
// call. A nil accessor is native None.
func acquireStorageOptionsAccessor(op, uri string, options *StorageOptionsAccessor) (*ffi.StorageOptionsAccessor, func(), error) {
	if options == nil {
		return nil, func() {}, nil
	}
	native, release, ok := accessor.Acquire(options)
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
