package ffi

import (
	"errors"
	"testing"
)

func TestCloneRejectsClosedDataset(t *testing.T) {
	dataset := &Dataset{}
	if _, err := dataset.Clone(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Clone error = %v, want ErrClosed", err)
	}
}

func TestCloneUsesSharedBindings(t *testing.T) {
	b := &bindings{}
	dataset := &Dataset{b: b}
	dataset.handle.Store(7)
	b.datasetCloneHandle = func(handle uintptr) uintptr {
		if handle != 7 {
			t.Fatalf("clone handle = %d, want 7", handle)
		}
		return 11
	}
	b.datasetClose = func(uintptr) {}

	clone, err := dataset.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	defer clone.Close()
	if got := clone.handle.Load(); got != 11 {
		t.Fatalf("clone handle = %d, want 11", got)
	}
	if clone.b != b {
		t.Fatalf("clone uses a different bindings table")
	}
}

func TestCloneNativeError(t *testing.T) {
	b := &bindings{}
	dataset := &Dataset{b: b}
	dataset.handle.Store(7)
	b.datasetCloneHandle = func(uintptr) uintptr { return 0 }
	b.lastErrorCode = func() int32 { return int32(CodeInvalidArgument) }
	b.lastErrorMessage = func() *byte { return nil }

	if _, err := dataset.Clone(); err == nil {
		t.Fatal("Clone succeeded without a native handle or error")
	}
}
