package lance

import "github.com/lance-format/lance-go/internal/ffi"

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

// Session owns shared index and metadata caches. Datasets opened with a
// session keep their cache state after the session is closed.
type Session struct {
	native *ffi.Session
	res    *resource
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
	s := &Session{native: native}
	s.res = newResource(func() { native.Close() })
	return s
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

// CacheStats returns a snapshot of the session caches.
func (s *Session) CacheStats() (CacheStats, error) {
	if !s.res.acquire() {
		return CacheStats{}, closedError("session cache stats", "")
	}
	defer s.res.release()
	stats, err := s.native.CacheStats()
	if err != nil {
		return CacheStats{}, wrapError("session cache stats", "", err)
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
		return 0, closedError("session size", "")
	}
	defer s.res.release()
	value, err := s.native.SizeBytes()
	if err != nil {
		return 0, wrapError("session size", "", err)
	}
	return value, nil
}

// ApproxNumItems returns the approximate number of cached items.
func (s *Session) ApproxNumItems() (uint64, error) {
	if !s.res.acquire() {
		return 0, closedError("session items", "")
	}
	defer s.res.release()
	value, err := s.native.ApproxNumItems()
	if err != nil {
		return 0, wrapError("session items", "", err)
	}
	return value, nil
}

// Close releases the native session. It is idempotent.
func (s *Session) Close() error {
	s.res.close()
	return nil
}

func (s *Session) acquire() bool {
	if s == nil {
		return false
	}
	return s.res.acquire()
}

func (s *Session) release() {
	s.res.release()
}
