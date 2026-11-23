package file_test

import (
	"errors"
	"strings"
	"testing"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/file"
)

// requireFileClassifiedError asserts the public error contract for raw-file
// failures that carry a classified native or argument code.
func requireFileClassifiedError(t *testing.T, err error, wantCode lance.Code, wantSentinel error, wantText string) *lance.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var lanceErr *lance.Error
	if !errors.As(err, &lanceErr) {
		t.Fatalf("error = %T (%v), want *lance.Error", err, err)
	}
	if lanceErr.Code != wantCode {
		t.Fatalf("error code = %v, want %v", lanceErr.Code, wantCode)
	}
	if wantSentinel != nil && !errors.Is(err, wantSentinel) {
		t.Fatalf("error = %v, want %v", err, wantSentinel)
	}
	if strings.Contains(err.Error(), "ffi:") {
		t.Fatalf("error leaks internal ffi prefix: %v", err)
	}
	if wantText != "" && !strings.Contains(err.Error(), wantText) {
		t.Fatalf("error = %q, want substring %q", err, wantText)
	}
	return lanceErr
}

// TestRawFileStorageOptionValidationErrors covers the argument checks that do
// not need native code.
func TestRawFileStorageOptionValidationErrors(t *testing.T) {
	if _, err := file.NewStorageOptionsWithProvider(nil, nil); err != nil {
		requireFileClassifiedError(t, err, lance.CodeInvalidArgument, lance.ErrInvalidArgument, "storage options provider must not be nil")
	} else {
		t.Fatal("nil provider was accepted")
	}

	if _, err := file.NewStorageOptionsFromAccessor(nil); err != nil {
		requireFileClassifiedError(t, err, lance.CodeInvalidArgument, lance.ErrInvalidArgument, "storage options accessor must not be nil")
	} else {
		t.Fatal("nil accessor was accepted")
	}
}
