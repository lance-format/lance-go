package ffi

import "testing"

func TestInspectRuntimeSeparatesLoadedAndBoundState(t *testing.T) {
	previous := bound.Load()
	bound.Store(nil)
	t.Cleanup(func() {
		bound.Store(previous)
	})

	state := InspectRuntime()
	if state.FFIReady {
		t.Fatalf("InspectRuntime reported FFI readiness without bindings: %+v", state)
	}
	if state.Expected != Pinned() {
		t.Fatalf("Expected = %+v, want %+v", state.Expected, Pinned())
	}
	if state.Loaded != state.Library.Loaded {
		t.Fatalf("Loaded = %v, Library.Loaded = %v", state.Loaded, state.Library.Loaded)
	}

	bound.Store(&bindings{})

	state = InspectRuntime()
	if state.FFIReady && !state.Loaded {
		t.Fatalf("InspectRuntime reported FFI readiness without a loaded library: %+v", state)
	}
	if state.FFIReady != state.Loaded {
		t.Fatalf("InspectRuntime bound state is inconsistent with loader state: %+v", state)
	}
}

func TestPinnedMatchesUpstreamConstants(t *testing.T) {
	pinned := Pinned()
	if pinned.Repository != upstreamRepo || pinned.Release != upstreamRelease ||
		pinned.Commit != upstreamCommit || pinned.HeaderSHA256 != upstreamHeaderSHA256 {
		t.Fatalf("Pinned() = %+v, does not match upstream constants", pinned)
	}
}
