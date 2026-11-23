//go:build !lance_external_lib && windows

package loader

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockShared blocks until this process holds a shared lock, letting concurrent
// processes verify the same cache entry at the same time.
func lockShared(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		0,
		0,
		^uint32(0),
		^uint32(0),
		&overlapped,
	)
}

// lockExclusive blocks until this process holds the exclusive lock for a cache
// entry. Closing the file releases the lock, including on error paths and
// process exit.
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

// unlock releases the lock held on f.
func unlock(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		0,
		^uint32(0),
		^uint32(0),
		&overlapped,
	)
}
