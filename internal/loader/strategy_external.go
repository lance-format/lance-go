//go:build lance_external_lib

package loader

// strategyNote explains the current build strategy in discovery failures.
func strategyNote() string {
	return "; the lance_external_lib build never embeds artifacts, so install liblance_c in the system search path or set LANCE_LIB_PATH"
}
