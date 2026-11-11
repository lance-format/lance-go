// Package handle stores Go objects behind integer identifiers that can be
// passed through foreign-function boundaries.
//
// Identifiers are monotonically increasing, never reused, and zero is never
// assigned. Take removes an object atomically, which makes it suitable for
// exactly-once release callbacks.
package handle

import (
	"sync"
	"sync/atomic"
)

const (
	shardBits  = 6
	shardCount = 1 << shardBits
	shardMask  = shardCount - 1
)

type shard struct {
	mu     sync.RWMutex
	values map[uint64]any
}

var (
	nextID atomic.Uint64
	shards [shardCount]shard
)

func init() {
	for i := range shards {
		shards[i].values = make(map[uint64]any)
	}
}

// New stores v and returns a non-zero identifier that names it.
//
// New panics if v is nil or if the identifier space is exhausted. Identifiers
// are never reused, including after Take or Remove.
func New(v any) uint64 {
	if v == nil {
		panic("handle: cannot register a nil value")
	}
	for {
		id := nextID.Add(1)
		if id == 0 {
			panic("handle: identifier space exhausted")
		}
		s := &shards[id&shardMask]
		s.mu.Lock()
		if _, exists := s.values[id]; exists {
			// Unreachable while nextID is monotonic, but preserves the
			// never-reuse invariant if the allocator changes later.
			s.mu.Unlock()
			continue
		}
		s.values[id] = v
		s.mu.Unlock()
		return id
	}
}

// Get returns the value named by id. It does not remove the value.
func Get(id uint64) (any, bool) {
	if id == 0 {
		return nil, false
	}
	s := &shards[id&shardMask]
	s.mu.RLock()
	v, ok := s.values[id]
	s.mu.RUnlock()
	return v, ok
}

// Take removes and returns the value named by id. Only one concurrent caller
// can observe a successful Take for a given identifier.
func Take(id uint64) (any, bool) {
	if id == 0 {
		return nil, false
	}
	s := &shards[id&shardMask]
	s.mu.Lock()
	v, ok := s.values[id]
	if ok {
		delete(s.values, id)
	}
	s.mu.Unlock()
	return v, ok
}

// Remove deletes the value named by id without returning it. It reports
// whether the identifier was present.
func Remove(id uint64) bool {
	_, ok := Take(id)
	return ok
}

// Len returns the number of currently registered values.
func Len() int {
	n := 0
	for i := range shards {
		s := &shards[i]
		s.mu.RLock()
		n += len(s.values)
		s.mu.RUnlock()
	}
	return n
}
