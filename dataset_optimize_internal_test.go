package lance

import (
	"errors"
	"strings"
	"testing"
)

func TestEncodeOwnedSegmentMetadataNilError(t *testing.T) {
	_, err := encodeOwnedSegmentMetadata(nil)
	if err == nil {
		t.Fatal("encodeOwnedSegmentMetadata(nil) succeeded")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
	var lanceErr *Error
	if !errors.As(err, &lanceErr) {
		t.Fatalf("error = %T, want *Error", err)
	}
	if lanceErr.Code != CodeInvalidArgument {
		t.Fatalf("error code = %v, want %v", lanceErr.Code, CodeInvalidArgument)
	}
	if strings.Contains(err.Error(), "ffi:") {
		t.Fatalf("error leaks internal ffi prefix: %v", err)
	}
	if !strings.Contains(err.Error(), "segment metadata is nil") {
		t.Fatalf("error = %q, want segment metadata detail", err)
	}
}
