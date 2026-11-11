package ffi

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/handle"
)

// CustomNamespaceDispatcher implements the single native dispatch callback
// used by a custom namespace. The operation string identifies which typed
// request model requestJSON contains. The returned JSON must be a response
// model for that operation.
type CustomNamespaceDispatcher interface {
	DispatchNamespace(operation string, requestJSON []byte) ([]byte, error)
}

// namespaceCustomDispatchSpec is the C ABI layout for
// LanceNamespaceCustomClientDispatchSpec. namespaceIDBuffer is not part of the
// C struct; it keeps the namespace ID bytes alive while the pointer is in use.
type namespaceCustomDispatchSpec struct {
	Dispatch          uintptr
	Release           uintptr
	ClientCtx         uintptr
	NamespaceID       *byte
	namespaceIDBuffer []byte
}

type namespaceCustomAdapter struct {
	mu     sync.RWMutex
	client CustomNamespaceDispatcher
}

func (a *namespaceCustomAdapter) dispatcher() CustomNamespaceDispatcher {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.client
}

// releaseContext is called by the shared generic release callback exactly once
// after native code drops the final owner of this context.
func (a *namespaceCustomAdapter) releaseContext() {
	a.mu.Lock()
	a.client = nil
	a.mu.Unlock()
}

var (
	namespaceCustomCallbacksOnce    sync.Once
	callbackNamespaceCustomDispatch uintptr
)

func initNamespaceCustomCallbacks() {
	namespaceCustomCallbacksOnce.Do(func() {
		initNamespaceContextCallbacks()
		callbackNamespaceCustomDispatch = purego.NewCallback(namespaceCustomDispatchCallback)
	})
}

// newNamespaceCustomDispatchSpec transfers ownership of the client context to
// native code. The caller must pass the spec to exactly one native constructor
// invocation and KeepAlive it until that call returns; native releases the
// context even when construction fails.
func newNamespaceCustomDispatchSpec(client CustomNamespaceDispatcher, namespaceID string) (*namespaceCustomDispatchSpec, error) {
	if client == nil {
		return nil, fmt.Errorf("ffi: custom namespace client is nil")
	}
	idBuffer, idPtr, err := newCStringPtr(namespaceID)
	if err != nil {
		return nil, err
	}
	initNamespaceCustomCallbacks()
	adapter := &namespaceCustomAdapter{client: client}
	return &namespaceCustomDispatchSpec{
		Dispatch:          callbackNamespaceCustomDispatch,
		Release:           callbackNamespaceRelease,
		ClientCtx:         uintptr(handle.New(adapter)),
		NamespaceID:       idPtr,
		namespaceIDBuffer: idBuffer,
	}, nil
}

// The uintptr result satisfies Windows syscall.NewCallback; C reads its low
// int32 status, which is always zero or one.
func namespaceCustomDispatchCallback(ctx uintptr, operation, request *byte, outResponse *ownedString, outError *callbackError) (ret uintptr) {
	if outResponse != nil {
		*outResponse = ownedString{}
	}
	clearCallbackError(outError)
	defer func() {
		if recovered := recover(); recovered != nil {
			if outResponse != nil {
				*outResponse = ownedString{}
			}
			setCallbackError(outError, fmt.Errorf("custom namespace dispatch panic: %v", recovered))
			ret = 1
		}
	}()

	if ctx == 0 {
		setCallbackError(outError, fmt.Errorf("custom namespace dispatch context is NULL"))
		return 1
	}
	value, ok := handle.Get(uint64(ctx))
	if !ok {
		setCallbackError(outError, fmt.Errorf("custom namespace dispatch context is no longer registered"))
		return 1
	}
	adapter, ok := value.(*namespaceCustomAdapter)
	if !ok {
		setCallbackError(outError, fmt.Errorf("custom namespace dispatch context has the wrong type"))
		return 1
	}
	client := adapter.dispatcher()
	if client == nil {
		setCallbackError(outError, fmt.Errorf("custom namespace dispatch context was released"))
		return 1
	}
	if outResponse == nil {
		setCallbackError(outError, fmt.Errorf("custom namespace dispatch response pointer is NULL"))
		return 1
	}

	response, err := client.DispatchNamespace(goString(operation), []byte(goString(request)))
	if err != nil {
		setCallbackError(outError, err)
		return 1
	}
	if len(response) == 0 {
		setCallbackError(outError, fmt.Errorf("custom namespace dispatch returned an empty response"))
		return 1
	}
	*outResponse = newOwnedString(string(response))
	return 0
}

// NewCustomNamespace creates a native custom namespace backed by dispatcher.
// The namespace ID is optional; an empty value asks native code to generate a
// lifetime-stable identity. Native code owns the callback context and releases
// it exactly once, including when this constructor fails.
func NewCustomNamespace(dispatcher CustomNamespaceDispatcher, namespaceID string) (*Namespace, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	spec, err := newNamespaceCustomDispatchSpec(dispatcher, namespaceID)
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	handle := b.namespaceCustomDispatchNew(spec)
	runtime.KeepAlive(spec)
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: custom namespace constructor returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &Namespace{b: b, handle: handle}, nil
}
