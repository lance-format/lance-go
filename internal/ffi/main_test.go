package ffi

import (
	"fmt"
	"os"
	"testing"
)

// TestMain verifies that no string-list pin survived the package tests.
// Missing releases are reported with the accounting count instead of a fatal
// runtime.Pinner panic from a finalizer.
func TestMain(m *testing.M) {
	before := acquiredLists.Load()
	code := m.Run()
	if after := acquiredLists.Load(); after != before {
		fmt.Fprintf(os.Stderr, "string-list ownership accounting: %d live pinned lists after tests, want %d\n", after, before)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
