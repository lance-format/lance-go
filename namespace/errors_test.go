package namespace

import (
	"errors"
	"testing"

	"github.com/lance-format/lance-go/internal/ffi"
)

func TestWrapNativeErrorKeepsUnsupportedCode(t *testing.T) {
	wrapped := wrapNativeError("namespace_exists", &ffi.NamespaceError{Code: 0, Message: "unsupported"})
	var namespaceErr *NamespaceError
	if !errors.As(wrapped, &namespaceErr) {
		t.Fatalf("wrapped error type = %T, want *NamespaceError", wrapped)
	}
	if namespaceErr.Code != ErrorCodeUnsupported {
		t.Fatalf("wrapped code = %d, want %d", namespaceErr.Code, ErrorCodeUnsupported)
	}
	if namespaceErr.Operation != "namespace_exists" {
		t.Fatalf("wrapped operation = %q", namespaceErr.Operation)
	}
}

func TestWrapNativeErrorClosed(t *testing.T) {
	wrapped := wrapNativeError("list_tables", ffi.ErrClosed)
	if !errors.Is(wrapped, ErrClosed) {
		t.Fatalf("wrapped closed error = %v, want ErrClosed", wrapped)
	}
}

func TestIsNotFound(t *testing.T) {
	notFound := []NamespaceErrorCode{
		ErrorCodeNamespaceNotFound,
		ErrorCodeTableNotFound,
		ErrorCodeTableIndexNotFound,
		ErrorCodeTableTagNotFound,
		ErrorCodeTransactionNotFound,
		ErrorCodeTableVersionNotFound,
		ErrorCodeTableBranchNotFound,
	}
	for _, code := range notFound {
		if !IsNotFound(&NamespaceError{Code: code}) {
			t.Fatalf("code %d was not treated as not found", code)
		}
	}
	if IsNotFound(&NamespaceError{Code: ErrorCodeUnsupported}) {
		t.Fatal("Unsupported was treated as not found")
	}
	if IsNotFound(errors.New("other")) {
		t.Fatal("unrelated error was treated as not found")
	}
}
