//go:build !lance_external_lib && windows

package loader

import "os"

// protectCachedLibrary leaves the cached DLL writable. Windows does not expose
// Unix permission bits through os.Chmod; access is inherited from the
// per-user LocalAppData cache directory, and a writable file can still be
// replaced after verification fails.
func protectCachedLibrary(*os.File) error {
	return nil
}
