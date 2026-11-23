package lance

import (
	"github.com/lance-format/lance-go/internal/ffi"
	sharesession "github.com/lance-format/lance-go/internal/session"
)

// CacheSpecKind selects the configuration used by one session cache.
type CacheSpecKind int32

const (
	CacheSpecDefault CacheSpecKind = 0
	CacheSpecSize    CacheSpecKind = 1
	CacheSpecURI     CacheSpecKind = 2
	CacheSpecConfig  CacheSpecKind = 3
)

// CacheSpec configures an index or metadata cache for NewSessionWithCacheSpecs.
// Only fields selected by Kind are used.
type CacheSpec struct {
	Kind        CacheSpecKind
	SizeBytes   uint64
	URI         string
	BackendKind string
	Options     map[string]string
}

// CacheStats is a snapshot of both session caches.
//
// CacheStats has the following fields:
//
//	IndexCacheHits         uint64
//	IndexCacheMisses       uint64
//	IndexCacheEntries      uint64
//	IndexCacheSizeBytes    uint64
//	MetadataCacheHits      uint64
//	MetadataCacheMisses    uint64
//	MetadataCacheEntries   uint64
//	MetadataCacheSizeBytes uint64
//
// The hit and miss counters describe lookups. Entries and size bytes describe
// the current cache contents. Session.CacheStats returns a copied snapshot.
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
//
// A Session has the following methods:
//
//	func (s *Session) CacheStats() (CacheStats, error)
//	func (s *Session) SizeBytes() (uint64, error)
//	func (s *Session) ApproxNumItems() (uint64, error)
//	func (s *Session) Close() error
//
// SizeBytes reports approximate cache memory use, and ApproxNumItems reports
// the approximate number of cached items. Close is idempotent, waits for
// in-flight calls, and releases the caller's session handle; objects already
// opened from the session remain usable.
type coreSession = sharesession.Session

type Session struct {
	*coreSession
}

// CacheStats returns a snapshot of the session caches.
func (s *Session) CacheStats() (CacheStats, error) {
	if s == nil || s.coreSession == nil {
		return CacheStats{}, closedError("session cache stats", "")
	}
	stats, err := s.coreSession.CacheStats()
	if err != nil {
		return CacheStats{}, err
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
	if s == nil || s.coreSession == nil {
		return 0, closedError("session size", "")
	}
	return s.coreSession.SizeBytes()
}

// ApproxNumItems returns the approximate number of cached items.
func (s *Session) ApproxNumItems() (uint64, error) {
	if s == nil || s.coreSession == nil {
		return 0, closedError("session items", "")
	}
	return s.coreSession.ApproxNumItems()
}

// Close releases the native session after in-flight calls complete. It is idempotent.
func (s *Session) Close() error {
	if s == nil || s.coreSession == nil {
		return nil
	}
	return s.coreSession.Close()
}

// NewSession creates a session with fixed-size caches.
func NewSession(indexCacheSize, metadataCacheSize uint64) (*Session, error) {
	native, err := ffi.NewSession(indexCacheSize, metadataCacheSize)
	if err != nil {
		return nil, wrapError("new session", "", err)
	}
	return newSession(native), nil
}

// NewSessionWithCacheSpecs creates a session with configured caches.
func NewSessionWithCacheSpecs(indexCache, metadataCache CacheSpec) (*Session, error) {
	indexNative, err := toFFICacheSpec(indexCache)
	if err != nil {
		return nil, err
	}
	metadataNative, err := toFFICacheSpec(metadataCache)
	if err != nil {
		return nil, err
	}
	native, err := ffi.NewSessionWithCacheSpecs(indexNative, metadataNative)
	if err != nil {
		return nil, wrapError("new session with cache specs", "", err)
	}
	return newSession(native), nil
}

func newSession(native *ffi.Session) *Session {
	return &Session{coreSession: sharesession.New(native, wrapError, closedError)}
}

func toFFICacheSpec(spec CacheSpec) (ffi.CacheSpec, error) {
	if err := validateCString(spec.URI); err != nil {
		return ffi.CacheSpec{}, invalidError("new session", "", "cache spec URI: %v", err)
	}
	if err := validateCString(spec.BackendKind); err != nil {
		return ffi.CacheSpec{}, invalidError("new session", "", "cache spec backend kind: %v", err)
	}
	native := ffi.CacheSpec{
		Kind:        ffi.CacheSpecKind(spec.Kind),
		SizeBytes:   spec.SizeBytes,
		URI:         spec.URI,
		BackendKind: spec.BackendKind,
	}
	if spec.Options != nil {
		native.Options = make(map[string]string, len(spec.Options))
		for key, value := range spec.Options {
			if err := validateCString(key); err != nil {
				return ffi.CacheSpec{}, invalidError("new session", "", "cache spec option key: %v", err)
			}
			if err := validateCString(value); err != nil {
				return ffi.CacheSpec{}, invalidError("new session", "", "cache spec option value: %v", err)
			}
			native.Options[key] = value
		}
	}
	if native.Kind < ffi.CacheSpecDefault || native.Kind > ffi.CacheSpecConfig {
		return ffi.CacheSpec{}, invalidError("new session", "", "unknown cache spec kind %d", spec.Kind)
	}
	return native, nil
}

// acquireSession pins s for one native call. The caller must call release
// after the native call, including when the call fails.
func acquireSession(op, uri string, s *Session) (*ffi.Session, func(), error) {
	if s == nil || s.coreSession == nil {
		return nil, func() {}, nil
	}
	native, release, ok := sharesession.Acquire(s.coreSession)
	if !ok {
		return nil, nil, closedError(op, uri)
	}
	return native, release, nil
}
