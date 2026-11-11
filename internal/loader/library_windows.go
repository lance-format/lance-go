//go:build windows

package loader

// libraryName is the DLL name resolved through the standard Windows search
// order when LANCE_LIB_PATH is unset.
const libraryName = "lance_c.dll"
