package ffi

import (
	"errors"
	"runtime"
	"testing"
)

func TestTakeErrorReadsCodeThenMessageThenFrees(t *testing.T) {
	buf := append([]byte("boom"), 0)
	msgPtr := &buf[0]

	var calls []string
	b := &bindings{
		lastErrorCode: func() int32 {
			calls = append(calls, "code")
			return int32(CodeIO)
		},
		lastErrorMessage: func() *byte {
			calls = append(calls, "message")
			return msgPtr
		},
		freeString: func(ptr *byte) {
			calls = append(calls, "free")
			if ptr != msgPtr {
				t.Errorf("freeString received %p, want %p", ptr, msgPtr)
			}
			// Scribble over the fake C allocation. A message copied after the
			// free would observe this instead of "boom".
			for i := range buf {
				buf[i] = 'X'
			}
		},
	}
	defer runtime.KeepAlive(buf)

	err := b.takeError()
	if err == nil {
		t.Fatal("takeError returned nil for a non-OK code")
	}
	if err.Code != CodeIO {
		t.Fatalf("Code = %v, want %v", err.Code, CodeIO)
	}
	if err.Message != "boom" {
		t.Fatalf("Message = %q, want %q (copied before free?)", err.Message, "boom")
	}
	want := []string{"code", "message", "free"}
	if len(calls) != len(want) {
		t.Fatalf("native calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("native calls = %v, want %v", calls, want)
		}
	}
}

func TestTakeErrorIgnoresMessageWhenCodeIsOK(t *testing.T) {
	b := &bindings{
		lastErrorCode: func() int32 { return int32(CodeOK) },
		lastErrorMessage: func() *byte {
			t.Error("lance_last_error_message called for LANCE_OK")
			return nil
		},
		freeString: func(*byte) {
			t.Error("lance_free_string called for LANCE_OK")
		},
	}

	if err := b.takeError(); err != nil {
		t.Fatalf("takeError returned %v for LANCE_OK", err)
	}
}

func TestErrorFormatting(t *testing.T) {
	err := &Error{Code: CodeNotFound, Message: "dataset /tmp/missing does not exist"}
	if got, want := err.Error(), "lance-c: LANCE_ERR_NOT_FOUND: dataset /tmp/missing does not exist"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}

	empty := &Error{Code: CodePanic}
	if got, want := empty.Error(), "lance-c: LANCE_ERR_PANIC"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}

	unknown := &Error{Code: Code(42), Message: "future"}
	if got, want := unknown.Error(), "lance-c: LanceErrorCode(42): future"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestCodeString(t *testing.T) {
	tests := []struct {
		code Code
		want string
	}{
		{CodeOK, "LANCE_OK"},
		{CodeInvalidArgument, "LANCE_ERR_INVALID_ARGUMENT"},
		{CodeIO, "LANCE_ERR_IO"},
		{CodeNotFound, "LANCE_ERR_NOT_FOUND"},
		{CodeDatasetAlreadyExists, "LANCE_ERR_DATASET_ALREADY_EXISTS"},
		{CodeIndex, "LANCE_ERR_INDEX"},
		{CodeInternal, "LANCE_ERR_INTERNAL"},
		{CodeNotSupported, "LANCE_ERR_NOT_SUPPORTED"},
		{CodeCommitConflict, "LANCE_ERR_COMMIT_CONFLICT"},
		{CodePanic, "LANCE_ERR_PANIC"},
		{Code(42), "LanceErrorCode(42)"},
	}
	for _, tt := range tests {
		if got := tt.code.String(); got != tt.want {
			t.Errorf("Code(%d).String() = %q, want %q", int32(tt.code), got, tt.want)
		}
	}
}

func TestOpenDatasetTransfersHandleAndCloseIsIdempotent(t *testing.T) {
	var gotURI string
	var gotVersion uint64
	var closes []uintptr

	b := &bindings{
		datasetOpen: func(uri string, storageOpts uintptr, version uint64) uintptr {
			gotURI = uri
			gotVersion = version
			if storageOpts != 0 {
				t.Errorf("storageOpts = %#x, want NULL", storageOpts)
			}
			return 0x1234
		},
		datasetClose: func(handle uintptr) {
			closes = append(closes, handle)
		},
	}

	ds, err := openDataset(b, "file:///tmp/example", 7)
	if err != nil {
		t.Fatalf("openDataset: %v", err)
	}
	if ds == nil {
		t.Fatal("openDataset returned a nil dataset without an error")
	}
	if gotURI != "file:///tmp/example" || gotVersion != 7 {
		t.Fatalf("native open received uri=%q version=%d", gotURI, gotVersion)
	}

	ds.Close()
	ds.Close()
	if len(closes) != 1 {
		t.Fatalf("datasetClose called %d times, want 1", len(closes))
	}
	if closes[0] != 0x1234 {
		t.Fatalf("datasetClose received %#x, want 0x1234", closes[0])
	}
}

func TestOpenDatasetNullHandleWithoutError(t *testing.T) {
	b := &bindings{
		lastErrorCode: func() int32 { return int32(CodeOK) },
		datasetOpen:   func(string, uintptr, uint64) uintptr { return 0 },
	}

	ds, err := openDataset(b, "file:///tmp/missing", 0)
	if err == nil {
		t.Fatal("openDataset returned nil error for a NULL handle")
	}
	if ds != nil {
		t.Fatal("openDataset returned a dataset together with an error")
	}
	var native *Error
	if errors.As(err, &native) {
		t.Fatalf("synthetic NULL-handle error must not masquerade as a native *Error: %v", err)
	}
}
