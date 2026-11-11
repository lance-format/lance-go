package lance

import "sync"

// resource tracks in-flight operations so native handles are not released
// while an operation is still using them. It also makes Close idempotent and
// lets concurrent callers wait for the actual release.
type resource struct {
	mu      sync.Mutex
	cond    *sync.Cond
	ops     int
	closed  bool
	done    chan struct{}
	closeFn func()
}

func newResource(closeFn func()) *resource {
	r := &resource{
		done:    make(chan struct{}),
		closeFn: closeFn,
	}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *resource) acquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.ops++
	return true
}

func (r *resource) release() {
	r.mu.Lock()
	r.ops--
	if r.ops == 0 {
		r.cond.Broadcast()
	}
	r.mu.Unlock()
}

func (r *resource) close() {
	r.mu.Lock()
	if r.closed {
		done := r.done
		r.mu.Unlock()
		<-done
		return
	}
	r.closed = true
	for r.ops > 0 {
		r.cond.Wait()
	}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		close(r.done)
		r.mu.Unlock()
	}()
	r.closeFn()
}
