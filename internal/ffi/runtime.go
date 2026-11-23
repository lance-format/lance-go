package ffi

import "github.com/lance-format/lance-go/internal/loader"

// PinnedLibrary is the lance-c build this package was compiled against. The
// values are build-time metadata; lance_c_version() reports only the binding
// release, so a loaded library cannot be verified against them.
type PinnedLibrary struct {
	Repository   string
	Release      string
	Commit       string
	HeaderSHA256 string
}

// Pinned returns the compile-time native pin.
func Pinned() PinnedLibrary {
	return PinnedLibrary{
		Repository:   upstreamRepo,
		Release:      upstreamRelease,
		Commit:       upstreamCommit,
		HeaderSHA256: upstreamHeaderSHA256,
	}
}

// RuntimeState is a side-effect-free snapshot of the native loader and FFI
// binding state.
type RuntimeState struct {
	// Loaded reports whether a native library has been opened successfully.
	Loaded bool
	// FFIReady reports whether the mandatory C symbols are bound and native
	// calls are ready. It is false when Loaded is false.
	FFIReady bool
	// Library describes the opened library. It is the zero value when Loaded
	// is false.
	Library loader.State
	// Expected is the build-time lance-c pin.
	Expected PinnedLibrary
}

// InspectRuntime observes the loader and binding state without discovering,
// extracting, loading, binding, retrying, or otherwise changing it.
func InspectRuntime() RuntimeState {
	mu.Lock()
	defer mu.Unlock()

	library := loader.Observe()
	return RuntimeState{
		Loaded:   library.Loaded,
		FFIReady: bound != nil && library.Loaded,
		Library:  library,
		Expected: Pinned(),
	}
}
