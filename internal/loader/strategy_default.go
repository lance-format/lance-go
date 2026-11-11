//go:build !lance_external_lib

package loader

// strategyNote explains the current build strategy in discovery failures.
// This build will prefer embedded artifacts once the artifact modules exist;
// until then it falls back to the same discovery order as the external build.
func strategyNote() string {
	return "; this default build carries no embedded artifacts yet, so install liblance_c in the system search path or set LANCE_LIB_PATH"
}
