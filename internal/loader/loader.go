//go:build linux || darwin

// Package loader discovers and loads the official lance-c shared library
// without cgo.
//
// Discovery order:
//
//  1. LANCE_LIB_PATH, which must name an existing regular file. When it is
//     set it is the only candidate; a bad value fails loudly instead of
//     silently falling back to another library.
//  2. The platform soname (liblance_c.so on Linux, liblance_c.dylib on
//     macOS), resolved by the operating system dynamic loader. This honors
//     LD_LIBRARY_PATH, DYLD_LIBRARY_PATH, ldconfig caches and the usual
//     system directories.
//
// Builds tagged lance_external_lib skip embedded artifact modules and always
// use the discovery order above. This foundation slice does not ship
// embedded artifacts yet, so the untagged build currently uses the same
// discovery order, but the tag is the switch that later artifact modules key
// off.
//
// The library is loaded at most once per process and is never unloaded.
// lance-c owns a Tokio runtime, thread-local error state and handles that may
// outlive a dataset, so calling Dlclose while native threads or callbacks may
// still run is not safe. Callers that need a different library must start a
// new process; changing LANCE_LIB_PATH after a successful Load has no effect.
//
// This package supports Linux and macOS only. Windows needs a LoadLibrary
// implementation because purego v0.10 does not provide Dlopen there.
package loader

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/ebitengine/purego"
)

// EnvLibPath names the environment variable that points at the liblance_c
// shared library file.
const EnvLibPath = "LANCE_LIB_PATH"

// ErrNotFound reports that no lance-c library was configured or found by the
// system dynamic loader. Errors caused by an explicitly configured but
// unusable LANCE_LIB_PATH do not wrap ErrNotFound.
var ErrNotFound = errors.New("lance-c shared library not found")

const dlopenFlags = purego.RTLD_NOW | purego.RTLD_LOCAL

var (
	mu     sync.Mutex
	cached *Library
)

// Library is a loaded lance-c shared library.
type Library struct {
	path   string
	handle uintptr
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

// Path returns the configured path when LANCE_LIB_PATH was used, or the
// platform soname when the system dynamic loader resolved the library.
func (l *Library) Path() string {
	return l.path
}

// Lookup resolves an exported C symbol.
func (l *Library) Lookup(name string) (uintptr, error) {
	addr, err := purego.Dlsym(l.handle, name)
	if err != nil {
		return 0, fmt.Errorf("loader: resolve %q in %s: %w", name, l.path, err)
	}
	if addr == 0 {
		return 0, fmt.Errorf("loader: symbol %q is missing from %s", name, l.path)
	}
	return addr, nil
}

func open() (*Library, error) {
	if path := os.Getenv(EnvLibPath); path != "" {
		return openFromEnv(path)
	}
	return openFromSystemPath()
}

func openFromEnv(path string) (*Library, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("loader: %s=%q: %w", EnvLibPath, path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("loader: %s=%q is not a regular file (mode %s)", EnvLibPath, path, info.Mode())
	}

	handle, err := purego.Dlopen(path, dlopenFlags)
	if err != nil {
		return nil, fmt.Errorf("loader: dlopen %s=%q: %w", EnvLibPath, path, err)
	}
	return &Library{path: path, handle: handle}, nil
}

func openFromSystemPath() (*Library, error) {
	handle, err := purego.Dlopen(libraryName, dlopenFlags)
	if err != nil {
		return nil, fmt.Errorf("loader: %w: the system dynamic loader could not open %q: %v%s",
			ErrNotFound, libraryName, err, strategyNote())
	}
	return &Library{path: libraryName, handle: handle}, nil
}

// resetForTest clears the cached handle without unloading it. Tests use it to
// exercise discovery more than once in a single process.
func resetForTest() {
	mu.Lock()
	cached = nil
	mu.Unlock()
}
