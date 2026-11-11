package ffi

import (
	"sync"

	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/handle"
)

// ProgressObservation is one native index-build progress report. Event values
// and their field semantics are defined by the LanceIndexBuildProgressCallback
// header contract; the root package maps them to its public progress type.
type ProgressObservation struct {
	Event     int32
	Stage     string
	Total     uint64
	Unit      string
	Completed uint64
}

// progressState adapts one Go observer to the process-wide native callback. The
// callback runs on native worker threads, so the observer is read under a lock
// and invoked outside of it.
type progressState struct {
	mu sync.RWMutex
	fn func(ProgressObservation)
}

func (s *progressState) observe(observation ProgressObservation) {
	s.mu.RLock()
	fn := s.fn
	s.mu.RUnlock()
	if fn == nil {
		return
	}
	fn(observation)
}

func (s *progressState) set(fn func(ProgressObservation)) {
	s.mu.Lock()
	s.fn = fn
	s.mu.Unlock()
}

// registerProgressObserver installs fn in the handle registry and returns the
// static native callback plus its context. release retires the observer and
// must run after the native operation returns; native drains in-flight calls
// before returning.
func registerProgressObserver(fn func(ProgressObservation)) (callback, ctx uintptr, state *progressState, release func()) {
	if fn == nil {
		return 0, 0, nil, func() {}
	}
	state = &progressState{}
	state.set(fn)
	id := handle.New(state)
	return progressCallback(), uintptr(id), state, func() { handle.Remove(id) }
}

// progressCallback is the single static native callback shared by every index
// segment build and metadata merge. Per-operation observers travel through the
// integer handle registry, so creating a builder never creates a new callback.
var progressCallback = sync.OnceValue(func() uintptr {
	return purego.NewCallback(func(ctx uintptr, event int32, stage *byte, total uint64, unit *byte, completed uint64) (ret uintptr) {
		// Native callers cannot unwind a Go panic, and the callback is advisory,
		// so an observer fault must not take down the build.
		defer func() {
			_ = recover()
		}()
		value, ok := handle.Get(uint64(ctx))
		if !ok {
			return 0
		}
		state, ok := value.(*progressState)
		if !ok {
			return 0
		}
		state.observe(ProgressObservation{
			Event:     event,
			Stage:     goString(stage),
			Total:     total,
			Unit:      goString(unit),
			Completed: completed,
		})
		return 0
	})
})
