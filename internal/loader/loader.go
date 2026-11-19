//go:build linux || darwin || windows

// Package loader discovers and loads the official lance-c shared library
// without cgo.
//
// Discovery order:
//
//  1. LANCE_LIB_PATH, which must name an existing regular file. When it is
//     set it is the only candidate; a bad value fails loudly instead of
//     silently falling back to another library.
//  2. In the default (untagged) build, a compiled-in artifact bundle
//     registered by a generated platform selector. The bundle is verified and
//     unpacked into a private user cache before it is loaded. Once a bundle is
//     registered for the running target, cache, extraction, or loading errors
//     fail loudly; the loader does not fall back to the system copy.
//  3. The platform library name (liblance_c.so on Linux, liblance_c.dylib on
//     macOS, lance_c.dll on Windows), resolved by the platform loader. Unix
//     honors LD_LIBRARY_PATH, DYLD_LIBRARY_PATH, ldconfig caches and the usual
//     system directories. Windows applies the process's DLL search policy,
//     which can include the application, system and current directories and PATH.
//
// Builds tagged lance_external_lib omit bundle, cache, and decompression code
// entirely and use only the environment and system-loader steps.
//
// The loader package does not embed a bundle itself: generated per-platform
// artifact modules and the selector files that register them are supplied by
// the module that imports this package. This module ships its platform
// selectors in internal/bundled, which every public entry point blank-imports;
// without a selector for the running target, the default build uses the system
// loader.
//
// The library is loaded at most once per process and is never unloaded.
// lance-c owns a Tokio runtime, thread-local error state and handles that may
// outlive a dataset, so unloading the library while native threads or
// callbacks may still run is not safe. Callers that need a different library must start a
// new process; changing LANCE_LIB_PATH after a successful Load has no effect.
//
// Platform loading uses purego's dlopen/dlsym bindings on Linux and macOS and
// LoadLibrary/GetProcAddress on Windows. purego v0.10 does not expose the
// Windows loading entry points, so the Windows implementation uses
// golang.org/x/sys/windows directly.
package loader

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// EnvLibPath names the environment variable that points at the liblance_c
// shared library file.
const EnvLibPath = "LANCE_LIB_PATH"

// EnvCacheDir names the environment variable that overrides the root of the
// private bundled-library cache. An empty value falls back to
// os.UserCacheDir. It is ignored when EnvLibPath is set.
const EnvCacheDir = "LANCE_CACHE_DIR"

// ErrNotFound reports that no lance-c library was configured or found by the
// platform loader. Errors caused by an explicitly configured but unusable
// LANCE_LIB_PATH do not wrap ErrNotFound.
var ErrNotFound = errors.New("lance-c shared library not found")

// Source identifies how a successfully opened library was selected.
type Source uint8

const (
	// SourceUnknown means this process has not successfully opened a library.
	SourceUnknown Source = iota
	// SourceEnv means LANCE_LIB_PATH named the opened file.
	SourceEnv
	// SourceBundle means the compiled-in artifact was extracted and opened.
	SourceBundle
	// SourceSystem means the platform loader resolved the library name.
	SourceSystem
)

// String returns the diagnostic name of the source.
func (s Source) String() string {
	switch s {
	case SourceEnv:
		return EnvLibPath
	case SourceBundle:
		return "bundle"
	case SourceSystem:
		return "system"
	default:
		return "unknown"
	}
}

// BundleInfo is the packaging metadata of a compiled-in artifact. The upstream
// commit and header hash are claims recorded when the artifact was built; they
// are not read from or verified against the loaded binary.
type BundleInfo struct {
	Module         string
	Target         string
	UpstreamCommit string
	HeaderSHA256   string
}

// State is a side-effect-free snapshot of the loaded library.
type State struct {
	// Loaded reports whether this process has successfully opened a library.
	Loaded bool
	// Source and Path describe that successful open. Path is the explicit
	// path, the extracted bundle cache path, or the platform library name for
	// a system lookup.
	Source Source
	Path   string
	// Bundle is non-nil only when Source is SourceBundle.
	Bundle *BundleInfo
}

var (
	mu     sync.Mutex
	cached *Library
)

// Library is a loaded lance-c shared library.
type Library struct {
	path   string
	handle uintptr
	source Source
	bundle *BundleInfo
}

// Load returns the process-wide lance-c library, opening it on first use.
// Failed attempts are not cached; a later call retries discovery.
func Load() (*Library, error) {
	mu.Lock()
	defer mu.Unlock()

	if cached != nil {
		return cached, nil
	}

	lib, err := open()
	if err != nil {
		return nil, err
	}
	cached = lib
	return lib, nil
}

// Path returns the configured path when LANCE_LIB_PATH was used, the cache
// path when a compiled-in bundle was used, or the platform library name when
// the system loader resolved the library.
func (l *Library) Path() string {
	return l.path
}

// Observe returns the current process state without loading a library. It does
// not inspect the environment, extract a bundle, resolve a symbol, or retry a
// previous failure.
func Observe() State {
	mu.Lock()
	defer mu.Unlock()
	return cached.state()
}

func (l *Library) state() State {
	if l == nil {
		return State{}
	}
	state := State{Loaded: true, Source: l.source, Path: l.path}
	if l.bundle != nil {
		bundle := *l.bundle
		state.Bundle = &bundle
	}
	return state
}

// Lookup resolves an exported C symbol.
func (l *Library) Lookup(name string) (uintptr, error) {
	addr, err := lookupSymbol(l.handle, name)
	if err != nil {
		return 0, fmt.Errorf("loader: resolve %q in %s: %w", name, l.path, err)
	}
	if addr == 0 {
		return 0, fmt.Errorf("loader: symbol %q is missing from %s", name, l.path)
	}
	return addr, nil
}

func openFromEnv(path string) (*Library, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("loader: %s=%q: %w", EnvLibPath, path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("loader: %s=%q is not a regular file (mode %s)", EnvLibPath, path, info.Mode())
	}

	handle, err := openLibrary(path)
	if err != nil {
		return nil, fmt.Errorf("loader: load %s=%q: %w", EnvLibPath, path, err)
	}
	return &Library{path: path, handle: handle, source: SourceEnv}, nil
}

func openFromSystemPath() (*Library, error) {
	handle, err := openLibrary(libraryName)
	if err != nil {
		return nil, fmt.Errorf("loader: %w: the platform loader could not open %q: %v%s",
			ErrNotFound, libraryName, err, strategyNote())
	}
	return &Library{path: libraryName, handle: handle, source: SourceSystem}, nil
}

// resetForTest clears the cached handle without unloading it. Tests use it to
// exercise discovery more than once in a single process.
func resetForTest() {
	mu.Lock()
	cached = nil
	mu.Unlock()
}
