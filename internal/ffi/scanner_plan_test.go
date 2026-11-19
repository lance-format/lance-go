package ffi

import (
	"errors"
	"testing"
)

func TestScannerPlanStringFreesNativeString(t *testing.T) {
	buf := append([]byte("native plan"), 0)
	freed := 0
	s := &Scanner{
		b: &bindings{
			freeString: func(ptr *byte) {
				if ptr != &buf[0] {
					t.Fatalf("freeString pointer = %p, want %p", ptr, &buf[0])
				}
				freed++
			},
		},
	}
	s.handle.Store(0x51)

	plan, err := s.planString("test plan", func(handle uintptr) *byte {
		if handle != 0x51 {
			t.Fatalf("native plan handle = %#x, want 0x51", handle)
		}
		return &buf[0]
	})
	if err != nil {
		t.Fatalf("planString: %v", err)
	}
	if plan != "native plan" {
		t.Fatalf("plan = %q, want %q", plan, "native plan")
	}
	if freed != 1 {
		t.Fatalf("freeString calls = %d, want 1", freed)
	}
}

func TestScannerPlanStringCapturesNativeError(t *testing.T) {
	message := append([]byte("native failure"), 0)
	freed := 0
	s := &Scanner{
		b: &bindings{
			lastErrorCode: func() int32 { return int32(CodeInvalidArgument) },
			lastErrorMessage: func() *byte {
				return &message[0]
			},
			freeString: func(ptr *byte) {
				if ptr != &message[0] {
					t.Fatalf("freeString pointer = %p, want %p", ptr, &message[0])
				}
				freed++
			},
		},
	}
	s.handle.Store(0x52)

	_, err := s.planString("test plan", func(uintptr) *byte { return nil })
	var nativeErr *Error
	if !errors.As(err, &nativeErr) {
		t.Fatalf("planString error = %T, want *Error: %v", err, err)
	}
	if nativeErr.Code != CodeInvalidArgument {
		t.Fatalf("error code = %v, want %v", nativeErr.Code, CodeInvalidArgument)
	}
	if nativeErr.Message != "native failure" {
		t.Fatalf("error message = %q, want %q", nativeErr.Message, "native failure")
	}
	if freed != 1 {
		t.Fatalf("freeString calls = %d, want 1", freed)
	}
}

func TestScannerCountRowsCapturesZeroResultError(t *testing.T) {
	message := append([]byte("count failed"), 0)
	b := &bindings{
		scannerCountRows: func(handle uintptr) uint64 {
			if handle != 0x53 {
				t.Fatalf("native count handle = %#x, want 0x53", handle)
			}
			return 0
		},
		lastErrorCode: func() int32 { return int32(CodeOK) },
	}
	s := &Scanner{b: b}
	s.handle.Store(0x53)

	rows, err := s.CountRows()
	if err != nil || rows != 0 {
		t.Fatalf("CountRows zero match = %d, %v; want 0, nil", rows, err)
	}

	b.lastErrorCode = func() int32 { return int32(CodeIO) }
	b.lastErrorMessage = func() *byte { return &message[0] }
	b.freeString = func(ptr *byte) {
		if ptr != &message[0] {
			t.Fatalf("freeString pointer = %p, want %p", ptr, &message[0])
		}
	}
	rows, err = s.CountRows()
	var nativeErr *Error
	if !errors.As(err, &nativeErr) {
		t.Fatalf("CountRows error = %T, want *Error: %v", err, err)
	}
	if rows != 0 || nativeErr.Code != CodeIO || nativeErr.Message != "count failed" {
		t.Fatalf("CountRows error = %d, %v; want 0, LANCE_ERR_IO with message", rows, nativeErr)
	}
}
