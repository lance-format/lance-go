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
	res         *resource
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
	s.res = newResource(native.Close)
	return s
}

// Acquire pins s for the duration of one native call. The returned release
// function must be called after the native call, including on failure. It
// returns false when s is nil or already closed.
func Acquire(s *Session) (native *ffi.Session, release func(), ok bool) {
	if s == nil || !s.res.acquire() {
		return nil, nil, false
	}
	return s.native, s.res.release, true
}

// CacheStats returns a snapshot of the session caches.
func (s *Session) CacheStats() (CacheStats, error) {
	if !s.res.acquire() {
		return CacheStats{}, s.closedError("session cache stats", "")
	}
	defer s.res.release()
	stats, err := s.native.CacheStats()
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
	if !s.res.acquire() {
		return 0, s.closedError("session size", "")
	}
	defer s.res.release()
	value, err := s.native.SizeBytes()
	if err != nil {
		return 0, s.wrapError("session size", "", err)
	}
	return value, nil
}

// ApproxNumItems returns the approximate number of cached items.
func (s *Session) ApproxNumItems() (uint64, error) {
	if !s.res.acquire() {
		return 0, s.closedError("session items", "")
	}
	defer s.res.release()
	value, err := s.native.ApproxNumItems()
	if err != nil {
		return 0, s.wrapError("session items", "", err)
	}
	return value, nil
}

// Close releases the native session. It is idempotent.
func (s *Session) Close() error {
	s.res.close()
	return nil
}

// resource tracks in-flight operations so the native handle is not released
// while an operation is still using it. It also makes Close idempotent and
// lets concurrent callers wait for the actual release.
type resource struct {
	mu      sync.Mutex
	cond    *sync.Cond
	ops     int
	closed  bool
	done    chan struct{}
	closeFn func()
}

func newResource(closeFn func()) *resource {
	r := &resource{
		done:    make(chan struct{}),
		closeFn: closeFn,
	}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *resource) acquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.ops++
	return true
}

func (r *resource) release() {
	r.mu.Lock()
	r.ops--
	if r.ops == 0 {
		r.cond.Broadcast()
	}
	r.mu.Unlock()
}

func (r *resource) close() {
	r.mu.Lock()
	if r.closed {
		done := r.done
		r.mu.Unlock()
		<-done
		return
	}
	r.closed = true
	for r.ops > 0 {
		r.cond.Wait()
	}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		close(r.done)
		r.mu.Unlock()
	}()
	r.closeFn()
}
