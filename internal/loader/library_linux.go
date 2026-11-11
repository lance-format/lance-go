//go:build linux

package loader

// libraryName is the runtime soname searched through the system dynamic
// loader when LANCE_LIB_PATH is unset.
const libraryName = "liblance_c.so"
