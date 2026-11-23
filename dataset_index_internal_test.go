package lance

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestDecodeNativeJSONErrorContract covers the decode failure branch shared by
// ListIndices, DescribeIndices and IndexSegments. Native metadata JSON cannot
// be made invalid through the public API, so the helper is exercised directly.
func TestDecodeNativeJSONErrorContract(t *testing.T) {
	cases := []struct {
		label  string
		raw    []byte
		verify func(*testing.T, error)
	}{
		{
			label: "syntax",
			raw:   []byte("{"),
			verify: func(t *testing.T, err error) {
				var syntaxErr *json.SyntaxError
				if !errors.As(err, &syntaxErr) {
					t.Fatalf("error = %v, want preserved *json.SyntaxError", err)
				}
			},
		},
		{
			label: "type",
			raw:   []byte(`{"name":"not-a-list"}`),
			verify: func(t *testing.T, err error) {
				var typeErr *json.UnmarshalTypeError
				if !errors.As(err, &typeErr) {
					t.Fatalf("error = %v, want preserved *json.UnmarshalTypeError", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			var out []struct{ Name string }
			err := decodeNativeJSON("list indices", "/tmp/data.lance", tc.raw, &out)
			if err == nil {
				t.Fatal("malformed native JSON was accepted")
			}
			var lanceErr *Error
			if !errors.As(err, &lanceErr) {
				t.Fatalf("error = %T (%v), want *lance.Error", err, err)
			}
			if lanceErr.Code != CodeInternal {
				t.Fatalf("error code = %v, want %v", lanceErr.Code, CodeInternal)
			}
			if !errors.Is(err, ErrInternal) {
				t.Fatalf("error = %v, want ErrInternal", err)
			}
			if strings.Contains(err.Error(), "ffi:") {
				t.Fatalf("error leaks internal ffi prefix: %v", err)
			}
			if !strings.Contains(err.Error(), "lance: list indices /tmp/data.lance: decode native JSON: ") {
				t.Fatalf("error = %q, want operation, URI and decode context", err)
			}
			tc.verify(t, err)
		})
	}
}
