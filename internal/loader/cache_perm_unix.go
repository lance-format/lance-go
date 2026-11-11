//go:build !lance_external_lib && (linux || darwin)

package loader

import "os"

// protectCachedLibrary makes a published library read-only. The containing
// cache directory is private to the user, so removing a stale entry is still
// possible after the file is verified.
func protectCachedLibrary(f *os.File) error {
	return f.Chmod(0o500)
}
