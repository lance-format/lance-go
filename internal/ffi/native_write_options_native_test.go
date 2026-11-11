//go:build linux || darwin

package ffi

import (
	"strings"
	"testing"

	"github.com/lance-format/lance-go/internal/handle"
)

func TestNativeWriteOptionsConstructorFailureDropsProgressContext(t *testing.T) {
	requireLibrary(t)
	before := handle.Len()
	spec := NativeWriteOptionsSpec{
		ExternalBlobMode: 99,
		Progress: &WriteProgress{
			BeginFragment: func(WriteFragmentProgress) error { return nil },
		},
	}
	options, err := NewNativeWriteOptions(spec)
	if err == nil {
		options.Close()
		t.Fatal("NewNativeWriteOptions accepted an invalid external blob mode")
	}
	if !strings.Contains(err.Error(), "external_blob_mode") {
		t.Fatalf("constructor error = %v, want external_blob_mode", err)
	}
	if after := handle.Len(); after != before {
		t.Fatalf("handle count after constructor failure = %d, want %d", after, before)
	}
}

func TestNativeWriteOptionsCloseIsIdempotent(t *testing.T) {
	requireLibrary(t)
	options, err := NewNativeWriteOptions(NativeWriteOptionsSpec{})
	if err != nil {
		t.Fatalf("NewNativeWriteOptions: %v", err)
	}
	if options.Handle() == 0 {
		t.Fatal("native options handle is zero")
	}
	options.Close()
	if options.Handle() != 0 {
		t.Fatalf("handle after Close = %d, want 0", options.Handle())
	}
	options.Close()
}
