package lance_test

import (
	"testing"

	"github.com/lance-format/lance-go/internal/loader"
)

// BenchmarkLoaderWarmLoad measures repeated in-process Load calls after the
// library has been opened once. This is the fast path every FFI call reaches:
// an atomic pointer load, with the mutex taken only around the first open.
//
// Cache extraction and digest verification are a different phase. They run in
// internal/loader's BenchmarkBundleWarmCacheVerify, which measures a warm cache
// hit under the shared lock, so hash and lock costs do not blur together.
func BenchmarkLoaderWarmLoad(b *testing.B) {
	requireNative(b)
	b.ReportAllocs()

	for b.Loop() {
		if _, err := loader.Load(); err != nil {
			b.Fatalf("Load: %v", err)
		}
	}
}
