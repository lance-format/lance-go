package ffi

import (
	"strings"
	"testing"
)

// FuzzValidateCString checks that the C-string validator accepts exactly the Go
// strings without a NUL byte, using strings.IndexByte as an independent
// predicate. The deterministic list, list and nullable cases stay in
// cstring_test.go; this target covers the per-string contract only.
func FuzzValidateCString(f *testing.F) {
	f.Add("")
	f.Add("plain")
	f.Add("with space")
	f.Add("bad\x00value")
	f.Add("üñïcode")
	f.Add(strings.Repeat("x", 64))

	f.Fuzz(func(t *testing.T, value string) {
		want := strings.IndexByte(value, 0) < 0
		if got := validateCString(value) == nil; got != want {
			t.Fatalf("validateCString(%q) accepted = %t, want %t", value, got, want)
		}
	})
}
