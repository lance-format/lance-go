//go:build !lance_external_lib && (linux || darwin)

package loader

import (
	"errors"
	"fmt"
	"math"
	"runtime"

	"github.com/ebitengine/purego"
)

// Bundle describes one compressed lance-c payload compiled into a generated
// platform selector. The payload and metadata are data only; importing the
// artifact module does not link, load, or execute native code.
//
// A Bundle is immutable after registration. Its fields are generated from the
// exact library and header used during packaging.
type Bundle struct {
	// Module is the import path of the artifact module that supplied the
	// payload. It is used in diagnostics.
	Module string
	// Target is the Go target the payload was packaged for, as GOOS/GOARCH.
	Target string
	// LibraryName is the platform shared-library basename for Target.
	LibraryName string
	// Compressed is a zstd frame containing the shared library bytes.
	Compressed []byte
	// CompressedSize is len(Compressed) as recorded at packaging time.
	CompressedSize int64
	// CompressedSHA256 is the lowercase SHA-256 of Compressed.
	CompressedSHA256 string
	// UncompressedSize is the exact shared-library size.
	UncompressedSize int64
	// UncompressedSHA256 is the lowercase SHA-256 of the shared library.
	UncompressedSHA256 string
	// UpstreamCommit is the lance-c commit claimed by the packaging process.
	// lance-c exposes no runtime version symbol, so the loader cannot verify
	// this claim from the shared library itself.
	UpstreamCommit string
	// HeaderSHA256 is the SHA-256 of the lance.h used during packaging. It is a
	// packaging-time claim, not a property verified from the binary.
	HeaderSHA256 string
}

// bundle is the descriptor installed by a generated selector. It is guarded by
// mu and is only read while Load holds mu. Registration happens during package
// initialization, before any Load call.
var bundle *Bundle

// RegisterBundle installs the descriptor compiled in by a generated platform
// selector. It must be called during package initialization, before Load.
//
// Linking more than one selector for the same process is a packaging bug, so
// duplicate registration panics instead of silently choosing one payload.
func RegisterBundle(b Bundle) {
	mu.Lock()
	defer mu.Unlock()

	if bundle != nil {
		panic(fmt.Sprintf("loader: bundle %q is already registered; link exactly one generated platform selector", bundle.Module))
	}
	bundle = &b
}

// open is called with mu held. It verifies and materializes the registered
// bundle, then asks the native loader to map it.
func (b *Bundle) open() (*Library, error) {
	path, err := b.cachedLibrary()
	if err != nil {
		return nil, fmt.Errorf("loader: bundled artifact %q for %q: %w", b.Module, b.Target, err)
	}

	handle, err := purego.Dlopen(path, dlopenFlags)
	if err != nil {
		return nil, fmt.Errorf("loader: dlopen bundled artifact %q from %s: %w", b.Module, path, err)
	}
	return &Library{path: path, handle: handle}, nil
}

func (b *Bundle) validate() error {
	if b == nil {
		return errors.New("bundle descriptor is nil")
	}
	if b.Module == "" {
		return errors.New("bundle descriptor has no module")
	}

	wantTarget := runtime.GOOS + "/" + runtime.GOARCH
	if b.Target != wantTarget {
		return fmt.Errorf("bundle descriptor target is %q, want %q", b.Target, wantTarget)
	}
	if b.LibraryName != libraryName {
		return fmt.Errorf("bundle descriptor library name is %q, want %q for %s", b.LibraryName, libraryName, wantTarget)
	}

	if b.CompressedSize <= 0 || b.CompressedSize != int64(len(b.Compressed)) {
		return fmt.Errorf("bundle descriptor compressed size is %d, payload has %d bytes", b.CompressedSize, len(b.Compressed))
	}
	if !isLowerHex(b.CompressedSHA256, 64) {
		return fmt.Errorf("bundle descriptor compressed SHA-256 %q is not 64 lowercase hex characters", b.CompressedSHA256)
	}
	if b.UncompressedSize <= 0 || b.UncompressedSize == math.MaxInt64 {
		return fmt.Errorf("bundle descriptor uncompressed size %d is invalid", b.UncompressedSize)
	}
	if !isLowerHex(b.UncompressedSHA256, 64) {
		return fmt.Errorf("bundle descriptor uncompressed SHA-256 %q is not 64 lowercase hex characters", b.UncompressedSHA256)
	}
	if !isLowerHex(b.UpstreamCommit, 40) {
		return fmt.Errorf("bundle descriptor upstream commit %q is not a 40-character lowercase hex commit", b.UpstreamCommit)
	}
	if !isLowerHex(b.HeaderSHA256, 64) {
		return fmt.Errorf("bundle descriptor header SHA-256 %q is not 64 lowercase hex characters", b.HeaderSHA256)
	}
	return nil
}

func isLowerHex(s string, length int) bool {
	if len(s) != length {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
