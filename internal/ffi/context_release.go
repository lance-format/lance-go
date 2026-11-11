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

// initContextReleaseCallback installs the single release trampoline shared by
// namespace context providers, storage options providers and fetched storage
// option snapshots. Keeping one trampoline bounds the process-wide callback
// budget and gives every adapter the same exactly-once release semantics.
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
	if value, ok := handle.Take(uint64(ctx)); ok {
		if releaser, ok := value.(contextReleaser); ok {
			releaser.releaseContext()
		}
	}
	return 0
}
