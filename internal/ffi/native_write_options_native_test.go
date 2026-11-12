//go:build linux || darwin || windows

package ffi

import (
	"strings"
	"testing"
	"time"

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

func TestNativeWriteOptionsCloseReleasesProgressContextOnce(t *testing.T) {
	requireLibrary(t)
	before := handle.Len()
	options, err := NewNativeWriteOptions(NativeWriteOptionsSpec{Progress: &WriteProgress{
		BeginFragment: func(WriteFragmentProgress) error { return nil },
	}})
	if err != nil {
		t.Fatalf("NewNativeWriteOptions: %v", err)
	}
	if got := handle.Len(); got != before+1 {
		t.Fatalf("handle count after constructor = %d, want %d", got, before+1)
	}
	options.Close()
	deadline := time.Now().Add(5 * time.Second)
	for handle.Len() != before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := handle.Len(); got != before {
		t.Fatalf("handle count after Close = %d, want %d", got, before)
	}
	options.Close()
	time.Sleep(10 * time.Millisecond)
	if got := handle.Len(); got != before {
		t.Fatalf("handle count after second Close = %d, want %d", got, before)
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
