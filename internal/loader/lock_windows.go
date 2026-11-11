//go:build !lance_external_lib && windows

package loader

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockExclusive blocks until this process holds the exclusive lock for a
// cache entry. Closing the file releases the lock, including on error paths
// and process exit.
func lockExclusive(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,
		^uint32(0),
		^uint32(0),
		&overlapped,
	)
}
