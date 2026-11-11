//go:build linux || darwin || windows

package ffi

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lance-format/lance-go/internal/loader"
)

// requireLibrary loads the real lance-c library or skips the test. A
// configured but broken library fails instead of skipping, because that is a
// setup error rather than a machine without the native dependency.
func requireLibrary(t *testing.T) *bindings {
	t.Helper()

	b, err := load()
	if errors.Is(err, loader.ErrNotFound) {
		t.Skip("lance-c is not available; set LANCE_LIB_PATH to a built lance-c library")
	}
	if err != nil {
		t.Fatalf("load and bind lance-c: %v", err)
	}
	return b
}

// TestNativeErrorSemantics verifies the upstream thread-local contract this
// package depends on, on one locked OS thread: a failed call sets a non-OK
// code, reading the message consumes the whole error, and freeing the message
// is safe.
func TestNativeErrorSemantics(t *testing.T) {
	b := requireLibrary(t)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	missing := filepath.Join(t.TempDir(), "does-not-exist")

	handle := b.datasetOpen(missing, nil, 0)
	if handle != 0 {
		b.datasetClose(handle)
		t.Fatalf("lance_dataset_open(%q) unexpectedly succeeded", missing)
	}

	code := b.lastErrorCode()
	if code == int32(CodeOK) {
		t.Fatal("failed native call left LANCE_OK in thread-local storage")
	}

	msgPtr := b.lastErrorMessage()
	if msgPtr == nil {
		t.Fatal("lance_last_error_message returned NULL after a failed call")
	}
	message := goString(msgPtr)
	b.freeString(msgPtr)
	if message == "" {
		t.Fatal("native error message is empty")
	}

	if after := b.lastErrorCode(); after != int32(CodeOK) {
		t.Fatalf("error code still %d after reading the message; message did not consume the error", after)
	}

	t.Logf("native error %d: %s", code, message)
}

// TestOpenDatasetReportsNativeErrors checks the full path: loader, bindings,
// OS-thread lock, error extraction and the NULL-handle contract.
func TestOpenDatasetReportsNativeErrors(t *testing.T) {
	requireLibrary(t)

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	for attempt := 1; attempt <= 2; attempt++ {
		ds, err := OpenDataset(missing, 0)
		if err == nil {
			if ds != nil {
				ds.Close()
			}
			t.Fatalf("attempt %d: OpenDataset(%q) succeeded, want an error", attempt, missing)
		}
		if ds != nil {
			t.Fatalf("attempt %d: OpenDataset returned a dataset together with an error", attempt)
		}

		var native *Error
		if !errors.As(err, &native) {
			t.Fatalf("attempt %d: error is %T, want *ffi.Error from thread-local storage: %v", attempt, err, err)
		}
		if native.Code == CodeOK {
			t.Fatalf("attempt %d: native error has CodeOK", attempt)
		}
		if native.Message == "" {
			t.Fatalf("attempt %d: native error has an empty message", attempt)
		}
	}
}
