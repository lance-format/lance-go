package ffi

import (
	"fmt"
	"runtime"
	"sort"
	"sync/atomic"
)

// CacheSpecKind selects one field group in a native session cache spec.
type CacheSpecKind int32

const (
	CacheSpecDefault CacheSpecKind = 0
	CacheSpecSize    CacheSpecKind = 1
	CacheSpecURI     CacheSpecKind = 2
	CacheSpecConfig  CacheSpecKind = 3
)

// CacheSpec describes one session cache. Only the field group selected by Kind
// is passed to native code.
type CacheSpec struct {
	Kind        CacheSpecKind
	SizeBytes   uint64
	URI         string
	BackendKind string
	Options     map[string]string
}

type sessionCacheSpec struct {
	Kind         int32
	SizeBytes    uint64
	BackendURI   uintptr
	BackendKind  uintptr
	OptionKeys   uintptr
	OptionValues uintptr
	OptionCount  uint64
}

type sessionCacheStats struct {
	IndexCacheHits         uint64
	IndexCacheMisses       uint64
	IndexCacheEntries      uint64
	IndexCacheSizeBytes    uint64
	MetadataCacheHits      uint64
	MetadataCacheMisses    uint64
	MetadataCacheEntries   uint64
	MetadataCacheSizeBytes uint64
}

// Session is an opaque native LanceSession handle. It owns shared index and
// metadata caches.
type Session struct {
	b      *bindings
	handle atomic.Uintptr
}

// NewSession creates a session with fixed-size index and metadata caches.
func NewSession(indexCacheSize, metadataCacheSize uint64) (*Session, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return newSession(b, indexCacheSize, metadataCacheSize)
}

func newSession(b *bindings, indexCacheSize, metadataCacheSize uint64) (*Session, error) {
	runtime.LockOSThread()
	handle := b.sessionNew(indexCacheSize, metadataCacheSize)
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_session_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()

	s := &Session{b: b}
	s.handle.Store(handle)
	return s, nil
}

// NewSessionWithCacheSpecs creates a session with independently configured
// index and metadata caches. Both specs are required.
func NewSessionWithCacheSpecs(indexCache, metadataCache CacheSpec) (*Session, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return newSessionWithCacheSpecs(b, indexCache, metadataCache)
}

func newSessionWithCacheSpecs(b *bindings, indexCache, metadataCache CacheSpec) (*Session, error) {
	indexRaw, indexKeep, err := buildSessionCacheSpec(indexCache)
	if err != nil {
		return nil, err
	}
	metadataRaw, metadataKeep, err := buildSessionCacheSpec(metadataCache)
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	handle := b.sessionNewWithCacheSpecs(&indexRaw, &metadataRaw)
	indexKeep.keepAlive()
	metadataKeep.keepAlive()
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_session_new_with_cache_specs returned NULL without setting an error")
	}
	runtime.UnlockOSThread()

	s := &Session{b: b}
	s.handle.Store(handle)
	return s, nil
}

type cacheSpecKeepAlive struct {
	values []*cStringList
}

func (k cacheSpecKeepAlive) keepAlive() {
	for _, value := range k.values {
		value.keepAlive()
	}
}

func buildSessionCacheSpec(spec CacheSpec) (sessionCacheSpec, cacheSpecKeepAlive, error) {
	var raw sessionCacheSpec
	raw.Kind = int32(spec.Kind)
	var keep cacheSpecKeepAlive

	switch spec.Kind {
	case CacheSpecDefault:
	case CacheSpecSize:
		raw.SizeBytes = spec.SizeBytes
	case CacheSpecURI:
		if spec.URI == "" {
			return sessionCacheSpec{}, keep, fmt.Errorf("ffi: cache spec URI must not be empty")
		}
		list, err := newCStringList([]string{spec.URI})
		if err != nil {
			return sessionCacheSpec{}, keep, err
		}
		raw.BackendURI = list.ptrs[0]
		keep.values = append(keep.values, list)
	case CacheSpecConfig:
		if spec.BackendKind == "" {
			return sessionCacheSpec{}, keep, fmt.Errorf("ffi: cache spec backend kind must not be empty")
		}
		kindList, err := newCStringList([]string{spec.BackendKind})
		if err != nil {
			return sessionCacheSpec{}, keep, err
		}
		raw.BackendKind = kindList.ptrs[0]
		keep.values = append(keep.values, kindList)

		if len(spec.Options) > 0 {
			keys := make([]string, 0, len(spec.Options))
			for key := range spec.Options {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			values := make([]string, 0, len(keys))
			for _, key := range keys {
				if key == "" {
					return sessionCacheSpec{}, keep, fmt.Errorf("ffi: cache spec option key must not be empty")
				}
				values = append(values, spec.Options[key])
			}

			keyList, err := newCStringList(keys)
			if err != nil {
				return sessionCacheSpec{}, keep, err
			}
			valueList, err := newCStringList(values)
			if err != nil {
				return sessionCacheSpec{}, keep, err
			}
			raw.OptionKeys = keyList.ptrs[0]
			raw.OptionValues = valueList.ptrs[0]
			raw.OptionCount = uint64(len(keys))
			keep.values = append(keep.values, keyList, valueList)
		}
	default:
		return sessionCacheSpec{}, keep, fmt.Errorf("ffi: unknown cache spec kind %d", spec.Kind)
	}
	return raw, keep, nil
}

// CacheStats is a snapshot of both native session caches.
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

// CacheStats returns a snapshot of the session cache counters.
func (s *Session) CacheStats() (CacheStats, error) {
	handle := s.handle.Load()
	if handle == 0 {
		return CacheStats{}, ErrClosed
	}

	var raw sessionCacheStats
	runtime.LockOSThread()
	rc := s.b.sessionGetCacheStats(handle, &raw)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return CacheStats{}, nativeErr
		}
		return CacheStats{}, fmt.Errorf("ffi: lance_session_get_cache_stats failed without setting an error")
	}
	runtime.UnlockOSThread()

	return CacheStats{
		IndexCacheHits:         raw.IndexCacheHits,
		IndexCacheMisses:       raw.IndexCacheMisses,
		IndexCacheEntries:      raw.IndexCacheEntries,
		IndexCacheSizeBytes:    raw.IndexCacheSizeBytes,
		MetadataCacheHits:      raw.MetadataCacheHits,
		MetadataCacheMisses:    raw.MetadataCacheMisses,
		MetadataCacheEntries:   raw.MetadataCacheEntries,
		MetadataCacheSizeBytes: raw.MetadataCacheSizeBytes,
	}, nil
}

// SizeBytes returns the approximate memory size of both caches.
func (s *Session) SizeBytes() (uint64, error) {
	return s.readUint64(s.b.sessionGetSizeBytes, "lance_session_get_size_bytes")
}

// ApproxNumItems returns the approximate number of cached items.
func (s *Session) ApproxNumItems() (uint64, error) {
	return s.readUint64(s.b.sessionGetApproxNumItems, "lance_session_get_approx_num_items")
}

func (s *Session) readUint64(read func(uintptr, *uint64) int32, name string) (uint64, error) {
	handle := s.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}

	var value uint64
	runtime.LockOSThread()
	rc := read(handle, &value)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: %s failed without setting an error", name)
	}
	runtime.UnlockOSThread()
	return value, nil
}

// NativeHandle returns the native session handle for internal call sites that
// hold an independent session lease. The caller must keep the Session open for
// the duration of the native call.
func (s *Session) NativeHandle() uintptr {
	return s.nativeHandle()
}

func (s *Session) nativeHandle() uintptr {
	return s.handle.Load()
}

// Close releases the native session handle. Datasets opened with the session
// keep their shared cache state and remain valid.
func (s *Session) Close() {
	handle := s.handle.Swap(0)
	if handle == 0 {
		return
	}
	s.b.sessionClose(handle)
}
