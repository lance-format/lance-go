package lance_test

import (
	"errors"
	"strings"
	"testing"

	lance "github.com/lance-format/lance-go"
)

func requireInvalidArgumentError(t *testing.T, err error, wantText string) *lance.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
	var lanceErr *lance.Error
	if !errors.As(err, &lanceErr) {
		t.Fatalf("error = %T, want *lance.Error", err)
	}
	if lanceErr.Code != lance.CodeInvalidArgument {
		t.Fatalf("error code = %v, want %v", lanceErr.Code, lance.CodeInvalidArgument)
	}
	if strings.Contains(err.Error(), "ffi:") {
		t.Fatalf("error leaks internal ffi prefix: %v", err)
	}
	if wantText != "" && !strings.Contains(err.Error(), wantText) {
		t.Fatalf("error = %q, want substring %q", err, wantText)
	}
	return lanceErr
}

func requireClosedError(t *testing.T, err error, wantText string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("error = %v, want ErrClosed", err)
	}
	if strings.Contains(err.Error(), "ffi:") {
		t.Fatalf("error leaks internal ffi prefix: %v", err)
	}
	if wantText != "" && !strings.Contains(err.Error(), wantText) {
		t.Fatalf("error = %q, want substring %q", err, wantText)
	}
}
