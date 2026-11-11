//go:build !lance_external_lib

package loader

// strategyNote explains the current build strategy in discovery failures.
// A generated platform selector can register an embedded artifact for this
// target. This build does not include one unless the packaging process added
// it, so the system-loader fallback remains available.
func strategyNote() string {
	return "; this default build has no compiled-in artifact bundle for this target, so install liblance_c in the system search path or set LANCE_LIB_PATH"
}
