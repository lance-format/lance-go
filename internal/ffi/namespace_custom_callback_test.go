package ffi

import (
	"testing"
	"unsafe"

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
	// Register the real trampolines on every supported OS, including Windows.
	initNamespaceCustomCallbacks()
	adapter := &namespaceCustomAdapter{client: &testCustomDispatcher{response: []byte("{}")}}
	ctx := handle.New(adapter)

	contextReleaseCallback(uintptr(ctx))
	if adapter.dispatcher() != nil {
		t.Fatal("release callback did not drop the dispatcher")
	}
	if _, ok := handle.Get(ctx); ok {
		t.Fatal("release callback left the context registered")
	}

	// A second native release would violate the ownership contract, but it
	// must not panic or touch a different context.
	contextReleaseCallback(uintptr(ctx))
}

func TestNamespaceCustomDispatchSpecLayout(t *testing.T) {
	// LanceNamespaceCustomClientDispatchSpec in the pinned header contains
	// dispatch, release, client_ctx and namespace_id, in that order.
	var spec namespaceCustomDispatchSpec
	pointerSize := unsafe.Sizeof(uintptr(0))
	offsets := []uintptr{
		unsafe.Offsetof(spec.Dispatch), unsafe.Offsetof(spec.Release),
		unsafe.Offsetof(spec.ClientCtx), unsafe.Offsetof(spec.NamespaceID),
		unsafe.Offsetof(spec.namespaceIDBuffer),
	}
	for i, offset := range offsets {
		if want := uintptr(i) * pointerSize; offset != want {
			t.Fatalf("dispatch spec field %d offset = %d, want %d", i, offset, want)
		}
	}
}
