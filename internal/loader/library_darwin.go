//go:build darwin

package loader

// libraryName is the install name searched through the system dynamic loader
// when LANCE_LIB_PATH is unset.
const libraryName = "liblance_c.dylib"
