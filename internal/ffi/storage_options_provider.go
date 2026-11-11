package ffi

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/handle"
)

// StorageOptionsFetchFunc resolves storage options for a native accessor.
// Returning a nil map means native None; a non-nil empty map means an
// explicitly empty option set.
type StorageOptionsFetchFunc func(ctx context.Context, force bool) (map[string]string, error)

// storageOptionsProviderSpec mirrors LanceStorageOptionsProviderSpec.
type storageOptionsProviderSpec struct {
	Fetch       uintptr
	ForceFetch  uintptr
	Release     uintptr
	ProviderCtx uintptr
	ProviderID  *byte
}

// storageOptionsFetchResult mirrors LanceStorageOptionsFetchResult. SnapshotCtx
// is an integer handle rather than a Go pointer: native code only stores and
// passes it back to the release callback.
type storageOptionsFetchResult struct {
	HasOptions      int32
	Keys            **byte
	Values          **byte
	Count           uint64
	SnapshotCtx     uintptr
	ReleaseSnapshot uintptr
}

// datasetOpenOptions mirrors LanceDatasetOpenOptions.
type datasetOpenOptions struct {
	Version                uint64
	StorageOptionsAccessor uintptr
}

// StorageOptionsProvider owns a native storage-options provider. The provider
// context is released when the final native owner (accessor, dataset, or
// object-store registry entry) is dropped.
type StorageOptionsProvider struct {
	b      *bindings
	handle uintptr
	id     string
}

// ValidateStorageOptions checks the presence-independent part of a storage
// options map before any native or callback context is allocated.
func ValidateStorageOptions(values map[string]string) error {
	for key, value := range values {
		if key == "" {
			return &Error{Code: CodeInvalidArgument, Message: "storage option key must not be empty"}
		}
		if err := validateCString(key); err != nil {
			return &Error{Code: CodeInvalidArgument, Message: fmt.Sprintf("storage option key: %v", err)}
		}
		if err := validateCString(value); err != nil {
			return &Error{Code: CodeInvalidArgument, Message: fmt.Sprintf("storage option value: %v", err)}
		}
	}
	return nil
}

// NewStorageOptionsProvider creates a provider backed by fetch. force may be
// nil, in which case native force-fetch falls back to fetch. The ID is copied
// by native code and defines native provider equality and registry cache
// identity.
func NewStorageOptionsProvider(id string, fetch, force StorageOptionsFetchFunc) (*StorageOptionsProvider, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return newStorageOptionsProvider(b, id, fetch, force)
}

func newStorageOptionsProvider(b *bindings, id string, fetch, force StorageOptionsFetchFunc) (*StorageOptionsProvider, error) {
	if err := validateCString(id); err != nil {
		return nil, fmt.Errorf("ffi: storage options provider id: %w", err)
	}
	if id == "" {
		return nil, fmt.Errorf("ffi: storage options provider id must not be empty")
	}
	if fetch == nil {
		return nil, fmt.Errorf("ffi: storage options fetch callback must not be nil")
	}

	initStorageOptionsProviderCallbacks()
	adapter := &storageOptionsProviderAdapter{fetch: fetch, force: force}
	ctx := handle.New(adapter)
	idBytes := append([]byte(id), 0)
	var idPinner runtime.Pinner
	idPinner.Pin(&idBytes[0])

	spec := storageOptionsProviderSpec{
		Fetch:       callbackStorageOptionsFetch,
		Release:     callbackContextRelease,
		ProviderCtx: uintptr(ctx),
		ProviderID:  &idBytes[0],
	}
	if force != nil {
		spec.ForceFetch = callbackStorageOptionsForceFetch
	}

	runtime.LockOSThread()
	provider := b.storageProviderNew(&spec)
	idPinner.Unpin()
	runtime.KeepAlive(idBytes)
	if provider == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		handle.Take(ctx)
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_storage_options_provider_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &StorageOptionsProvider{b: b, handle: provider, id: id}, nil
}

// ID returns the provider identity copied into native code.
func (p *StorageOptionsProvider) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}

// NativeID reads the identity back from native code. It is primarily useful
// for diagnostics and tests that assert the native copy is stable.
func (p *StorageOptionsProvider) NativeID() (string, error) {
	if p == nil || p.handle == 0 {
		return "", ErrClosed
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ptr := p.b.storageProviderID(p.handle)
	if ptr == nil {
		if nativeErr := p.b.takeError(); nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_storage_options_provider_id returned NULL without setting an error")
	}
	id := goString(ptr)
	p.b.freeString(ptr)
	return id, nil
}

// Close releases the caller's native provider handle. Native owners retain
// their own references, so datasets and accessors may outlive this call.
func (p *StorageOptionsProvider) Close() {
	if p == nil || p.handle == 0 {
		return
	}
	handle := p.handle
	p.handle = 0
	p.b.storageProviderClose(handle)
}

type storageOptionsProviderAdapter struct {
	fetch StorageOptionsFetchFunc
	force StorageOptionsFetchFunc
}

func (a *storageOptionsProviderAdapter) releaseContext() {}

func (a *storageOptionsProviderAdapter) fetchOptions(ctx context.Context, force bool) (map[string]string, error) {
	if force && a.force != nil {
		return a.force(ctx, true)
	}
	return a.fetch(ctx, false)
}

var (
	storageOptionsProviderCallbacksOnce sync.Once
	callbackStorageOptionsFetch         uintptr
	callbackStorageOptionsForceFetch    uintptr
)

func initStorageOptionsProviderCallbacks() {
	storageOptionsProviderCallbacksOnce.Do(func() {
		initOwnedReleaseCallback()
		initContextReleaseCallback()
		callbackStorageOptionsFetch = purego.NewCallback(storageOptionsFetchCallback)
		callbackStorageOptionsForceFetch = purego.NewCallback(storageOptionsForceFetchCallback)
	})
}

// storageOptionsForceFetchCallback is a distinct trampoline so native can tell
// an explicit force-fetch implementation from the default fallback. Both
// trampolines return uintptr for Windows syscall.NewCallback; C consumes the
// low int32 status, which is always zero or one.
func storageOptionsForceFetchCallback(ctx uintptr, out *storageOptionsFetchResult, outError *callbackError) uintptr {
	return uintptr(dispatchStatus3(ctx, true, unsafe.Pointer(out), outError))
}

func storageOptionsFetchCallback(ctx uintptr, out *storageOptionsFetchResult, outError *callbackError) uintptr {
	return uintptr(dispatchStatus3(ctx, false, unsafe.Pointer(out), outError))
}

func (a *storageOptionsProviderAdapter) dispatchStatus3(force bool, payload unsafe.Pointer, outError *callbackError) (ret int32) {
	out := (*storageOptionsFetchResult)(payload)
	if out != nil {
		*out = storageOptionsFetchResult{}
	}
	clearCallbackError(outError)
	published := false
	defer func() {
		if recovered := recover(); recovered != nil {
			if out != nil && !published {
				*out = storageOptionsFetchResult{}
			}
			setCallbackError(outError, fmt.Errorf("storage options provider panic"))
			ret = 1
		}
	}()

	if out == nil {
		setCallbackError(outError, fmt.Errorf("storage options fetch result pointer is NULL"))
		return 1
	}
	if a == nil || a.fetch == nil {
		setCallbackError(outError, fmt.Errorf("storage options provider context has the wrong type"))
		return 1
	}

	options, err := a.fetchOptions(context.Background(), force)
	if err != nil {
		setCallbackError(outError, err)
		return 1
	}
	if options == nil {
		return 0
	}

	var snapshot *fetchedStorageOptions
	defer func() {
		if snapshot != nil && !published {
			snapshot.releaseContext()
		}
	}()
	snapshot, err = newFetchedStorageOptions(options)
	if err != nil {
		setCallbackError(outError, err)
		return 1
	}
	ctxID := handle.New(snapshot)
	*out = storageOptionsFetchResult{
		HasOptions:      1,
		Keys:            snapshot.keysPtr(),
		Values:          snapshot.valuesPtr(),
		Count:           uint64(len(snapshot.keyPtrs)),
		SnapshotCtx:     uintptr(ctxID),
		ReleaseSnapshot: callbackContextRelease,
	}
	published = true
	return 0
}

type fetchedStorageOptions struct {
	keyBufs   [][]byte
	valueBufs [][]byte
	keyPtrs   []*byte
	valuePtrs []*byte
	pinner    runtime.Pinner
}

func newFetchedStorageOptions(options map[string]string) (*fetchedStorageOptions, error) {
	if err := ValidateStorageOptions(options); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	snapshot := &fetchedStorageOptions{
		keyBufs:   make([][]byte, len(keys)),
		valueBufs: make([][]byte, len(keys)),
		keyPtrs:   make([]*byte, len(keys)),
		valuePtrs: make([]*byte, len(keys)),
	}
	for i, key := range keys {
		snapshot.keyBufs[i] = append([]byte(key), 0)
		snapshot.valueBufs[i] = append([]byte(options[key]), 0)
		snapshot.pinner.Pin(&snapshot.keyBufs[i][0])
		snapshot.pinner.Pin(&snapshot.valueBufs[i][0])
		snapshot.keyPtrs[i] = &snapshot.keyBufs[i][0]
		snapshot.valuePtrs[i] = &snapshot.valueBufs[i][0]
	}
	if len(keys) > 0 {
		snapshot.pinner.Pin(&snapshot.keyPtrs[0])
		snapshot.pinner.Pin(&snapshot.valuePtrs[0])
	}
	return snapshot, nil
}

func (s *fetchedStorageOptions) keysPtr() **byte {
	if len(s.keyPtrs) == 0 {
		return nil
	}
	return &s.keyPtrs[0]
}

func (s *fetchedStorageOptions) valuesPtr() **byte {
	if len(s.valuePtrs) == 0 {
		return nil
	}
	return &s.valuePtrs[0]
}

func (s *fetchedStorageOptions) releaseContext() {
	s.pinner.Unpin()
	runtime.KeepAlive(s)
}

// NewStorageOptionsAccessorWithPresence creates a native accessor. A nil
// initial map with hasInitial false becomes native None; a non-nil map,
// including an empty one, becomes native Some(initial).
func NewStorageOptionsAccessorWithPresence(initial map[string]string, hasInitial bool, provider *StorageOptionsProvider) (*StorageOptionsAccessor, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	var providerHandle uintptr
	if provider != nil {
		providerHandle = provider.handle
		if providerHandle == 0 {
			return nil, ErrClosed
		}
	}
	return newStorageOptionsAccessorWithPresence(b, initial, hasInitial, providerHandle)
}

func newStorageOptionsAccessorWithPresence(b *bindings, initial map[string]string, hasInitial bool, provider uintptr) (*StorageOptionsAccessor, error) {
	if !hasInitial && initial != nil {
		return nil, fmt.Errorf("ffi: storage options are non-nil but presence is false")
	}
	if hasInitial {
		if err := ValidateStorageOptions(initial); err != nil {
			return nil, err
		}
	}

	var raw storageOptionsInput
	var keyList, valueList *cStringList
	if hasInitial {
		keys := make([]string, 0, len(initial))
		for key := range initial {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		keyValues := make([]string, len(keys))
		optionValues := make([]string, len(keys))
		for i, key := range keys {
			keyValues[i] = key
			optionValues[i] = initial[key]
		}
		var err error
		keyList, err = newCStringList(keyValues)
		if err != nil {
			return nil, err
		}
		valueList, err = newCStringList(optionValues)
		if err != nil {
			return nil, err
		}
		raw.Count = uint64(len(keys))
		if len(keyList.ptrs) > 0 {
			raw.Keys = (**byte)(unsafe.Pointer(&keyList.ptrs[0]))
			raw.Values = (**byte)(unsafe.Pointer(&valueList.ptrs[0]))
		}
	}

	var initialPtr *storageOptionsInput
	if hasInitial {
		initialPtr = &raw
	}

	runtime.LockOSThread()
	handle := b.storageAccessorNew(initialPtr, provider)
	if keyList != nil {
		keyList.keepAlive()
	}
	if valueList != nil {
		valueList.keepAlive()
	}
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_storage_options_accessor_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &StorageOptionsAccessor{b: b, handle: handle}, nil
}

// Snapshot returns the accessor's current resolved options. A nil map with
// hasOptions false means native None; a non-nil empty map with true means
// native Some(empty). Native caching, expiration and fallback are preserved.
func (a *StorageOptionsAccessor) Snapshot() (map[string]string, bool, error) {
	if a == nil || a.handle == 0 {
		return nil, false, ErrClosed
	}

	runtime.LockOSThread()
	snapshot := a.b.storageAccessorSnapshot(a.handle)
	if snapshot == 0 {
		nativeErr := a.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, false, nativeErr
		}
		return nil, false, fmt.Errorf("ffi: lance_storage_options_accessor_snapshot returned NULL without setting an error")
	}
	defer func() {
		a.b.storageSnapshotClose(snapshot)
		runtime.UnlockOSThread()
	}()

	hasOptions := a.b.storageSnapshotHasOptions(snapshot)
	if hasOptions < 0 {
		if nativeErr := a.b.takeError(); nativeErr != nil {
			return nil, false, nativeErr
		}
		return nil, false, fmt.Errorf("ffi: storage options snapshot has_options failed without setting an error")
	}
	if hasOptions == 0 {
		return nil, false, nil
	}

	count := a.b.storageSnapshotCount(snapshot)
	values := make(map[string]string, count)
	for i := uint64(0); i < count; i++ {
		key := a.b.storageSnapshotKeyAt(snapshot, i)
		if key == nil {
			if nativeErr := a.b.takeError(); nativeErr != nil {
				return nil, false, nativeErr
			}
			return nil, false, fmt.Errorf("ffi: storage options snapshot key %d is NULL without setting an error", i)
		}
		value := a.b.storageSnapshotValueAt(snapshot, i)
		if value == nil {
			if nativeErr := a.b.takeError(); nativeErr != nil {
				return nil, false, nativeErr
			}
			return nil, false, fmt.Errorf("ffi: storage options snapshot value %d is NULL without setting an error", i)
		}
		values[goString(key)] = goString(value)
	}
	return values, true, nil
}

// NewStorageOptionsProviderFromNamespace creates a native namespace-backed
// provider. The native provider retains the namespace and fetches vended
// credentials with typed table-id serialization.
func NewStorageOptionsProviderFromNamespace(namespace *Namespace, tableIDJSON string) (*StorageOptionsProvider, error) {
	if namespace == nil || namespace.handle == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(tableIDJSON); err != nil {
		return nil, err
	}
	if strings.TrimSpace(tableIDJSON) == "" {
		return nil, fmt.Errorf("ffi: namespace table id must not be empty")
	}
	if namespace.b.namespaceStorageProviderNew == nil {
		return nil, fmt.Errorf("ffi: lance_namespace_storage_options_provider_new is unavailable")
	}
	runtime.LockOSThread()
	handle := namespace.b.namespaceStorageProviderNew(namespace.handle, tableIDJSON)
	if handle == 0 {
		nativeErr := namespace.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_namespace_storage_options_provider_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	provider := &StorageOptionsProvider{b: namespace.b, handle: handle}
	id, err := provider.NativeID()
	if err != nil {
		provider.Close()
		return nil, err
	}
	provider.id = id
	return provider, nil
}
