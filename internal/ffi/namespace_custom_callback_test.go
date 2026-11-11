package ffi

import (
	"github.com/lance-format/lance-go/internal/handle"
	"testing"
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
