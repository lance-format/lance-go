package ffi

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/lance-format/lance-go/internal/loader"
)

// symbolSource is the part of loader.Library that binding needs. Keeping it
// narrow lets tests exercise the required/optional symbol split without a
// native library.
type symbolSource interface {
	Path() string
	Lookup(name string) (uintptr, error)
}

// abiMismatchError reports a loaded library that is missing mandatory symbols.
// It is separate from *Error because no lance-c call produced it; its message
// leads with the selected library and keeps the complete missing-symbol list
// for investigation.
type abiMismatchError struct {
	Path     string
	Required int
	Missing  []string
}

func (e *abiMismatchError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ffi: selected native library %s has an ABI mismatch with this Go SDK; expected ABI from binding metadata: lance-c %s at source revision %s; %d of %d required symbols are missing\n",
		e.Path, upstreamRelease, upstreamCommit, len(e.Missing), e.Required)
	b.WriteString("recovery:\n")
	fmt.Fprintf(&b, "  bundled: unset %s and build without the lance_external_lib tag; the default build uses the matching companion library for this Go SDK and platform\n", loader.EnvLibPath)
	fmt.Fprintf(&b, "  external: build with CGO_ENABLED=0 and use the official lance-c prebuilt library for %s/%s whose source revision is %s; if the installed SDK version has no matching official asset, use its native companion library. Binding metadata names release %s, so verify that release contains the expected source revision rather than choosing by tag alone (official assets: %s/releases)\n",
		runtime.GOOS, runtime.GOARCH, upstreamCommit, upstreamRelease, upstreamRepo)
	fmt.Fprintf(&b, "missing symbols (%d): %s", len(e.Missing), strings.Join(e.Missing, ", "))
	return b.String()
}
