package lance

import (
	"github.com/lance-format/lance-go/internal/ffi"
	"github.com/lance-format/lance-go/internal/loader"
)

// RuntimeSource identifies how a successfully opened native library was
// selected.
type RuntimeSource uint8

const (
	// RuntimeSourceUnknown means no native library has been opened in this
	// process.
	RuntimeSourceUnknown RuntimeSource = iota
	// RuntimeSourceLibPath means LANCE_LIB_PATH named the opened file.
	RuntimeSourceLibPath
	// RuntimeSourceBundle means the compiled-in artifact was extracted and
	// opened.
	RuntimeSourceBundle
	// RuntimeSourceSystem means the platform loader resolved the library name.
	RuntimeSourceSystem
)

// String returns the diagnostic name of the source.
func (s RuntimeSource) String() string {
	switch s {
	case RuntimeSourceLibPath:
		return "LANCE_LIB_PATH"
	case RuntimeSourceBundle:
		return "bundle"
	case RuntimeSourceSystem:
		return "system"
	default:
		return "unknown"
	}
}

// PinnedLibrary is the lance-c build this Go module was compiled against.
// These values are build-time metadata, not a version read from the loaded
// library.
type PinnedLibrary struct {
	Repository   string
	Release      string
	Commit       string
	HeaderSHA256 string
}

// LoadedBundle describes the packaging metadata of a compiled-in artifact.
// UpstreamCommit and HeaderSHA256 are claims recorded when the artifact was
// built; they are not verified against the loaded binary.
type LoadedBundle struct {
	Module         string
	Target         string
	UpstreamCommit string
	HeaderSHA256   string
}

// LoadedLibrary describes the library successfully opened by this process.
type LoadedLibrary struct {
	// Source identifies the successful discovery path. Path is the explicit
	// file, the extracted bundle cache path, or the bare platform library name
	// for a system lookup.
	Source RuntimeSource
	Path   string
	// Bundle is non-nil only when Source is RuntimeSourceBundle.
	Bundle *LoadedBundle
}

// RuntimeStatus is a side-effect-free snapshot of the native runtime.
type RuntimeStatus struct {
	// Loaded reports whether a native library has been opened successfully.
	Loaded bool
	// FFIReady reports whether the mandatory C symbols are bound and native
	// calls are ready. It is false when Loaded is false.
	FFIReady bool
	// Library describes the opened library. It is the zero value when Loaded
	// is false.
	Library LoadedLibrary
	// Expected is the lance-c build this Go module was compiled against.
	Expected PinnedLibrary
}

// RuntimeInfo returns the native runtime state observed in this process. It
// does not discover, extract, load, bind, or retry a native library, so a call
// before the first native operation reports Loaded and FFIReady as false.
//
// Loaded and Library describe a successful library open. FFIReady is a
// separate state: a library can open and still fail mandatory symbol binding.
// Expected is the build-time lance-c pin. The SDK does not bind
// lance_c_version(), which reports only the binding release, so a loaded
// liblance_c cannot be verified against that pin. Bundle
// metadata is available only for a compiled-in artifact, and it is a
// packaging-time claim rather than a value read from the loaded binary.
func RuntimeInfo() RuntimeStatus {
	state := ffi.InspectRuntime()
	status := RuntimeStatus{
		Loaded:   state.Loaded,
		FFIReady: state.FFIReady,
		Library: LoadedLibrary{
			Source: runtimeSource(state.Library.Source),
			Path:   state.Library.Path,
		},
		Expected: PinnedLibrary{
			Repository:   state.Expected.Repository,
			Release:      state.Expected.Release,
			Commit:       state.Expected.Commit,
			HeaderSHA256: state.Expected.HeaderSHA256,
		},
	}
	if state.Library.Bundle != nil {
		status.Library.Bundle = &LoadedBundle{
			Module:         state.Library.Bundle.Module,
			Target:         state.Library.Bundle.Target,
			UpstreamCommit: state.Library.Bundle.UpstreamCommit,
			HeaderSHA256:   state.Library.Bundle.HeaderSHA256,
		}
	}
	return status
}

func runtimeSource(source loader.Source) RuntimeSource {
	switch source {
	case loader.SourceEnv:
		return RuntimeSourceLibPath
	case loader.SourceBundle:
		return RuntimeSourceBundle
	case loader.SourceSystem:
		return RuntimeSourceSystem
	default:
		return RuntimeSourceUnknown
	}
}
