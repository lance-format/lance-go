//go:build !lance_external_lib && (linux || darwin)

package loader

import (
	"errors"
	"os"
	"syscall"
)

// lockExclusive blocks until this process holds the exclusive advisory lock
// for a cache entry. Closing the file releases the lock, including on error
// paths and process exit.
func lockExclusive(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}
