package ffi

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/handle"
)

// ContextProvider supplies dynamic request context for a namespace. The
// operation name and object identifier are native OperationInfo values and
// must not be retained: the callback copies them only for the call. Returning
// an error or panicking is logged by native code and treated as an empty
// context; the request continues.
type ContextProvider interface {
	ProvideContext(operation, objectID string) (map[string]string, error)
}

type namespaceContextProviderSpec struct {
	ProvideContext uintptr
	Release        uintptr
	ProviderCtx    uintptr
}

type namespaceContextAdapter struct {
	provider ContextProvider
}

// releaseContext is called by the generic release callback. It exists so later
// callback adapters can share the same native release hook.
func (a *namespaceContextAdapter) releaseContext() {}

var (
	namespaceContextCallbacksOnce sync.Once
	callbackNamespaceProvide      uintptr
)

func initNamespaceContextCallbacks() {
	namespaceContextCallbacksOnce.Do(func() {
		initOwnedReleaseCallback()
		initContextReleaseCallback()
		callbackNamespaceProvide = purego.NewCallback(namespaceContextProvideCallback)
	})
}

// newNamespaceContextProviderSpec transfers ownership of the provider context
// to native code. The caller must pass the spec to exactly one native
// constructor invocation; native releases it even when construction fails.
func newNamespaceContextProviderSpec(provider ContextProvider) *namespaceContextProviderSpec {
	if provider == nil {
		return nil
	}
	initNamespaceContextCallbacks()
	adapter := &namespaceContextAdapter{provider: provider}
	return &namespaceContextProviderSpec{
		ProvideContext: callbackNamespaceProvide,
		Release:        callbackContextRelease,
		ProviderCtx:    uintptr(handle.New(adapter)),
	}
}

func namespaceContextProvideCallback(ctx uintptr, operation, objectID *byte, outResult *ownedString, outError *callbackError) (ret uintptr) {
	if outResult != nil {
		*outResult = ownedString{}
	}
	clearCallbackError(outError)
	defer func() {
		if recovered := recover(); recovered != nil {
			if outResult != nil {
				*outResult = ownedString{}
			}
			setCallbackError(outError, fmt.Errorf("namespace context provider panic"))
			ret = 1
		}
	}()

	if ctx == 0 {
		setCallbackError(outError, fmt.Errorf("namespace context provider context is NULL"))
		return 1
	}
	value, ok := handle.Get(uint64(ctx))
	if !ok {
		setCallbackError(outError, fmt.Errorf("namespace context provider context is no longer registered"))
		return 1
	}
	adapter, ok := value.(*namespaceContextAdapter)
	if !ok || adapter.provider == nil {
		setCallbackError(outError, fmt.Errorf("namespace context provider context has the wrong type"))
		return 1
	}

	values, err := adapter.provider.ProvideContext(goString(operation), goString(objectID))
	if err != nil {
		setCallbackError(outError, err)
		return 1
	}
	if values == nil {
		values = map[string]string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		setCallbackError(outError, fmt.Errorf("encode namespace context: %w", err))
		return 1
	}
	if outResult == nil {
		setCallbackError(outError, fmt.Errorf("namespace context result pointer is NULL"))
		return 1
	}
	*outResult = newOwnedString(string(encoded))
	return 0
}
