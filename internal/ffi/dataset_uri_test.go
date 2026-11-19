package ffi

import (
	"errors"
	"runtime"
	"testing"
)

func TestDatasetURICopiesAndFreesNativeString(t *testing.T) {
	buf := append([]byte("file:///tmp/data.lance"), 0)
	ptr := &buf[0]
	freed := false
	b := &bindings{
		datasetURI: func(handle uintptr) *byte {
			if handle != 0x1234 {
				t.Errorf("datasetURI received handle %#x, want 0x1234", handle)
			}
			return ptr
		},
		freeString: func(got *byte) {
			freed = true
			if got != ptr {
				t.Errorf("freeString received %p, want %p", got, ptr)
			}
			// Scribble over the fake allocation. A URI copied after the free
			// would observe these bytes instead.
			for i := range buf {
				buf[i] = 'X'
			}
		},
	}
	d := &Dataset{b: b}
	d.handle.Store(0x1234)

	got, err := d.URI()
	if err != nil {
		t.Fatalf("URI: %v", err)
	}
	if want := "file:///tmp/data.lance"; got != want {
		t.Fatalf("URI = %q, want %q", got, want)
	}
	if !freed {
		t.Fatal("URI did not free the native string")
	}
	runtime.KeepAlive(buf)
}

func TestDatasetURIReportsNativeNullError(t *testing.T) {
	message := append([]byte("dataset is NULL"), 0)
	b := &bindings{
		datasetURI:       func(uintptr) *byte { return nil },
		lastErrorCode:    func() int32 { return int32(CodeInvalidArgument) },
		lastErrorMessage: func() *byte { return &message[0] },
		freeString:       func(*byte) {},
	}
	d := &Dataset{b: b}
	d.handle.Store(0x1234)

	_, err := d.URI()
	var native *Error
	if !errors.As(err, &native) {
		t.Fatalf("URI error is %T, want *Error: %v", err, err)
	}
	if native.Code != CodeInvalidArgument || native.Message != "dataset is NULL" {
		t.Fatalf("URI error = %+v, want invalid argument with native message", native)
	}
	runtime.KeepAlive(message)
}

func TestDatasetURINullWithoutNativeError(t *testing.T) {
	b := &bindings{
		datasetURI:    func(uintptr) *byte { return nil },
		lastErrorCode: func() int32 { return int32(CodeOK) },
	}
	d := &Dataset{b: b}
	d.handle.Store(0x1234)

	_, err := d.URI()
	if err == nil {
		t.Fatal("URI returned nil error for a NULL native string")
	}
	var native *Error
	if errors.As(err, &native) {
		t.Fatalf("synthetic NULL-string error must not masquerade as a native *Error: %v", err)
	}
}
