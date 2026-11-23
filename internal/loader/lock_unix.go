//go:build !lance_external_lib && (linux || darwin)

package loader

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// lockShared blocks until this process holds a shared advisory lock, letting
// concurrent processes verify the same cache entry at the same time.
func lockShared(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_SH)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
}

// lockExclusive blocks until this process holds the exclusive advisory lock
// for a cache entry. Closing the file releases the lock, including on error
// paths and process exit.
func lockExclusive(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
}

// unlock releases the advisory lock held on f.
func unlock(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_UN)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
}
