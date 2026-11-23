package lance_test

import (
	"testing"

	"github.com/lance-format/lance-go/internal/loader"
)

// BenchmarkLoaderWarmLoad measures repeated in-process Load calls after the
// library has been opened once. This is the fast path every FFI call reaches:
// one mutex acquisition and a cached pointer.
//
// Cache extraction and digest verification are a different phase of the
// loader, measured separately from warm calls so hash and lock costs do not
// blur together (issue #98).
func BenchmarkLoaderWarmLoad(b *testing.B) {
	requireNative(b)
	b.ReportAllocs()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loader.Load(); err != nil {
			b.Fatalf("Load: %v", err)
		}
	}
}
