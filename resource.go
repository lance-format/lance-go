package lance

import (
	"sync"

	"github.com/lance-format/lance-go/internal/lifetime"
)

// resource tracks in-flight operations so native handles are not released
// while an operation is still using them. It also makes Close idempotent and
// lets concurrent callers wait for the actual release.
//
// The implementation is shared with namespace-owned handles through
// internal/lifetime.
type resource struct {
	*lifetime.Resource
	cond *sync.Cond
}

func newResource(closeFn func()) *resource {
	r := &resource{Resource: lifetime.New(closeFn)}
	r.cond = r.Resource.Cond()
	return r
}

func (r *resource) acquire() bool {
	return r.Acquire()
}

func (r *resource) release() {
	r.Release()
}

func (r *resource) close() {
	r.Close()
}
