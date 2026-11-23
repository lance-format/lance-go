//go:build !lance_external_lib && (linux || darwin || windows)

package loader

import (
	"path/filepath"
	"testing"
)

// benchmarkCachePayloadBytes keeps the digest work large enough to measure
// without turning the cache-extraction setup into the dominant cost. It is a
// smaller proxy for the real library; SetBytes reports verification throughput
// so runs with other payload sizes stay comparable.
const benchmarkCachePayloadBytes = 1 << 20

// BenchmarkBundleWarmCacheVerify measures one warm cache hit: taking the
// entry's shared lock and verifying the extracted library by size and SHA-256.
// This is the phase landed in #117; it is deliberately separate from the
// in-process Load fast path benchmarked in the root package.
//
// The benchmark uses a temporary cache root, so it neither reads nor writes the
// user's cache.
func BenchmarkBundleWarmCacheVerify(b *testing.B) {
	cacheBase := testCacheBase(b)
	payload := make([]byte, benchmarkCachePayloadBytes)
	for i := range payload {
		payload[i] = byte(i * 31)
	}
	bundle := testBundle(b, payload)

	path, err := bundle.cachedLibrary()
	if err != nil {
		b.Fatalf("prime cache: %v", err)
	}
	wantPath := filepath.Join(cacheBase, bundle.UncompressedSHA256, libraryName)
	if path != wantPath {
		b.Fatalf("cache path = %q, want %q", path, wantPath)
	}

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		got, err := bundle.cachedLibrary()
		if err != nil {
			b.Fatalf("warm cache hit: %v", err)
		}
		if got != path {
			b.Fatalf("cache path = %q, want %q", got, path)
		}
	}
}
