//go:build linux || darwin

package ffi

import (
	"runtime"
	"strings"
	"testing"

	"github.com/lance-format/lance-go/internal/handle"
)

type testCustomDispatcher struct {
	response []byte
	err      error
}

func (d *testCustomDispatcher) DispatchNamespace(string, []byte) ([]byte, error) {
	return d.response, d.err
}

func TestNamespaceCustomReleaseCallbackReleasesOnce(t *testing.T) {
	adapter := &namespaceCustomAdapter{client: &testCustomDispatcher{response: []byte("{}")}}
	ctx := handle.New(adapter)

	namespaceContextReleaseCallback(uintptr(ctx))
	if adapter.dispatcher() != nil {
		t.Fatal("release callback did not drop the dispatcher")
	}
	if _, ok := handle.Get(ctx); ok {
		t.Fatal("release callback left the context registered")
	}

	// A second native release would violate the ownership contract, but it
	// must not panic or touch a different context.
	namespaceContextReleaseCallback(uintptr(ctx))
}

func TestNamespaceCustomFinalOwnerReleasesContext(t *testing.T) {
	b := requireLibrary(t)

	dispatcher := &testCustomDispatcher{response: []byte("{}")}
	spec, err := newNamespaceCustomDispatchSpec(dispatcher, "custom-final-owner")
	if err != nil {
		t.Fatalf("new spec: %v", err)
	}
	value, ok := handle.Get(uint64(spec.ClientCtx))
	if !ok {
		t.Fatal("custom context is not registered before construction")
	}
	adapter, ok := value.(*namespaceCustomAdapter)
	if !ok {
		t.Fatalf("custom context type = %T, want *namespaceCustomAdapter", value)
	}

	runtime.LockOSThread()
	nativeHandle := b.namespaceCustomDispatchNew(spec)
	runtime.KeepAlive(spec)
	var nativeErr error
	if nativeHandle == 0 {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()
	if nativeErr != nil {
		t.Fatalf("custom namespace constructor: %v", nativeErr)
	}
	if nativeHandle == 0 {
		t.Fatal("custom namespace constructor returned NULL without setting an error")
	}

	b.namespaceClose(nativeHandle)
	if adapter.dispatcher() != nil {
		t.Fatal("final native owner did not release the dispatcher context")
	}
	if _, ok := handle.Get(uint64(spec.ClientCtx)); ok {
		t.Fatal("final native owner left the context registered")
	}
}

func TestNamespaceCustomConstructorFailureReleasesContext(t *testing.T) {
	b := requireLibrary(t)

	dispatcher := &testCustomDispatcher{response: []byte("{}")}
	spec, err := newNamespaceCustomDispatchSpec(dispatcher, "custom-constructor-failure")
	if err != nil {
		t.Fatalf("new spec: %v", err)
	}
	value, ok := handle.Get(uint64(spec.ClientCtx))
	if !ok {
		t.Fatal("custom context is not registered before construction")
	}
	adapter, ok := value.(*namespaceCustomAdapter)
	if !ok {
		t.Fatalf("custom context type = %T, want *namespaceCustomAdapter", value)
	}

	// Native creates OwnedCallbackContext before validating dispatch, so this
	// NULL callback deterministically fails construction after taking
	// ownership and must release it exactly once.
	spec.Dispatch = 0
	runtime.LockOSThread()
	nativeHandle := b.namespaceCustomDispatchNew(spec)
	runtime.KeepAlive(spec)
	var nativeErr error
	if nativeHandle == 0 {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()

	if nativeHandle != 0 {
		b.namespaceClose(nativeHandle)
		t.Fatal("constructor accepted a NULL dispatch callback")
	}
	if nativeErr == nil {
		t.Fatal("constructor returned NULL without setting an error")
	}
	if !strings.Contains(nativeErr.Error(), "dispatch must not be NULL") {
		t.Fatalf("constructor error = %q, want NULL dispatch validation", nativeErr)
	}
	if adapter.dispatcher() != nil {
		t.Fatal("constructor failure did not release the dispatcher context")
	}
	if _, ok := handle.Get(uint64(spec.ClientCtx)); ok {
		t.Fatal("constructor failure left the context registered")
	}
}
