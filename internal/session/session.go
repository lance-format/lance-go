// Package session owns shared native Lance session lifetimes. The root lance
// package exposes this type through an alias, and the file package borrows the
// native handle without exposing it in the public API.
package session

import (
	"sync"

	"github.com/lance-format/lance-go/internal/ffi"
)

// CacheStats is a snapshot of both session caches.
type CacheStats struct {
	IndexCacheHits         uint64
	IndexCacheMisses       uint64
	IndexCacheEntries      uint64
	IndexCacheSizeBytes    uint64
	MetadataCacheHits      uint64
	MetadataCacheMisses    uint64
	MetadataCacheEntries   uint64
	MetadataCacheSizeBytes uint64
}

// Session owns shared index and metadata caches. Datasets and raw file
// objects opened with a session keep their cache state after the session is
// closed.
type Session struct {
	native      *ffi.Session
	mu          sync.RWMutex
	closed      bool
	wrapError   func(op, uri string, err error) error
	closedError func(op, uri string) error
}

// New adopts native and uses the supplied error conversion so callers observe
// the root package error identities.
func New(native *ffi.Session, wrapError func(op, uri string, err error) error, closedError func(op, uri string) error) *Session {
	s := &Session{
		native:      native,
		wrapError:   wrapError,
		closedError: closedError,
	}
	return s
}

// Acquire pins s for the duration of one native call. The returned release
// function must be called after the native call, including on failure. It
// returns false when s is nil or already closed.
func Acquire(s *Session) (native *ffi.Session, release func(), ok bool) {
	if s == nil {
		return nil, nil, false
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, nil, false
	}
	return s.native, s.mu.RUnlock, true
}

// CacheStats returns a snapshot of the session caches.
func (s *Session) CacheStats() (CacheStats, error) {
	native, release, ok := Acquire(s)
	if !ok {
		return CacheStats{}, s.closedError("session cache stats", "")
	}
	defer release()
	stats, err := native.CacheStats()
	if err != nil {
		return CacheStats{}, s.wrapError("session cache stats", "", err)
	}
	return CacheStats{
		IndexCacheHits:         stats.IndexCacheHits,
		IndexCacheMisses:       stats.IndexCacheMisses,
		IndexCacheEntries:      stats.IndexCacheEntries,
		IndexCacheSizeBytes:    stats.IndexCacheSizeBytes,
		MetadataCacheHits:      stats.MetadataCacheHits,
		MetadataCacheMisses:    stats.MetadataCacheMisses,
		MetadataCacheEntries:   stats.MetadataCacheEntries,
		MetadataCacheSizeBytes: stats.MetadataCacheSizeBytes,
	}, nil
}

// SizeBytes returns the approximate cache memory size.
func (s *Session) SizeBytes() (uint64, error) {
	native, release, ok := Acquire(s)
	if !ok {
		return 0, s.closedError("session size", "")
	}
	defer release()
	value, err := native.SizeBytes()
	if err != nil {
		return 0, s.wrapError("session size", "", err)
	}
	return value, nil
}

// ApproxNumItems returns the approximate number of cached items.
func (s *Session) ApproxNumItems() (uint64, error) {
	native, release, ok := Acquire(s)
	if !ok {
		return 0, s.closedError("session items", "")
	}
	defer release()
	value, err := native.ApproxNumItems()
	if err != nil {
		return 0, s.wrapError("session items", "", err)
	}
	return value, nil
}

// Close releases the native session after in-flight calls complete. It is idempotent.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.native.Close()
	return nil
}
