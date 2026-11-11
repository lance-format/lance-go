package ffi

import (
	"errors"
	"testing"
)

func TestNamespaceResultErrorPreservesUnsupportedCode(t *testing.T) {
	message := []byte("operation is not supported\x00")
	result := lanceNamespaceResult{
		HasNamespaceError:     1,
		NamespaceErrorCode:    0,
		NamespaceErrorMessage: &message[0],
	}
	err := namespaceResultError(&result)
	if err == nil {
		t.Fatal("namespaceResultError returned nil for a namespace error")
	}
	var namespaceErr *NamespaceError
	if !errors.As(err, &namespaceErr) {
		t.Fatalf("error type = %T, want *NamespaceError", err)
	}
	if namespaceErr.Code != 0 {
		t.Fatalf("code = %d, want 0", namespaceErr.Code)
	}
	if namespaceErr.Message != "operation is not supported" {
		t.Fatalf("message = %q", namespaceErr.Message)
	}
}

func TestNamespaceResultErrorIgnoresSuccess(t *testing.T) {
	result := lanceNamespaceResult{HasNamespaceError: 0}
	if err := namespaceResultError(&result); err != nil {
		t.Fatalf("namespaceResultError on success = %v", err)
	}
	if err := namespaceResultError(nil); err != nil {
		t.Fatalf("namespaceResultError(nil) = %v", err)
	}
}
