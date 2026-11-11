package ffi

import (
	"sync"

	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/handle"
)

// contextReleaser is implemented by Go values whose only native ownership
// contract is a single release callback. Native code invokes the callback once
// when its final owner is dropped.
type contextReleaser interface {
	releaseContext()
}

var (
	contextReleaseOnce     sync.Once
	callbackContextRelease uintptr
)

// initContextReleaseCallback installs the single one-word trampoline shared by
// namespace context providers, storage options providers, fetched storage
// option snapshots and scanner poll wakeups. Keeping one trampoline bounds the
// process-wide callback budget and gives every adapter the same exactly-once
// release semantics. Scanner waiters are woken without being removed because
// their handle is released separately during scanner cleanup.
func initContextReleaseCallback() {
	contextReleaseOnce.Do(func() {
		callbackContextRelease = purego.NewCallback(contextReleaseCallback)
	})
}

func contextReleaseCallback(ctx uintptr) (ret uintptr) {
	if ctx == 0 {
		return 0
	}
	defer func() {
		_ = recover()
	}()
	value, ok := handle.Get(uint64(ctx))
	if !ok {
		return 0
	}
	switch target := value.(type) {
	case *pollWaiter:
		target.wake()
	case contextReleaser:
		removed, removedOK := handle.Take(uint64(ctx))
		if !removedOK {
			return 0
		}
		if releaser, ok := removed.(contextReleaser); ok {
			releaser.releaseContext()
		}
	default:
		handle.Remove(uint64(ctx))
	}
	return 0
}
