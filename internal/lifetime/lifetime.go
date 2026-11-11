// Package lifetime tracks in-flight operations on an owned native handle.
//
// Acquire and Release delimit an operation that borrows the native handle.
// Close marks the handle closed, waits for every acquired operation to
// release it, and then runs the close function exactly once. Concurrent
// callers of Close wait for the same release to finish.
package lifetime

import "sync"

// Resource owns one native handle and its in-flight operation count.
type Resource struct {
	mu      sync.Mutex
	cond    *sync.Cond
	ops     int
	closed  bool
	done    chan struct{}
	closeFn func()
}

// New returns a resource that calls closeFn after the last operation releases
// and Close has been requested. closeFn is called exactly once.
func New(closeFn func()) *Resource {
	r := &Resource{
		done:    make(chan struct{}),
		closeFn: closeFn,
	}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// Cond returns the condition variable used to wait for in-flight operations.
// It exists so package-level tests can observe the transition into Wait
// without timing assumptions. Callers must not signal or broadcast it.
func (r *Resource) Cond() *sync.Cond {
	return r.cond
}

// Acquire registers one in-flight operation. It reports false when the
// resource is already closed or closing.
func (r *Resource) Acquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.ops++
	return true
}

// Release ends one operation registered by Acquire.
func (r *Resource) Release() {
	r.mu.Lock()
	r.ops--
	if r.ops == 0 {
		r.cond.Broadcast()
	}
	r.mu.Unlock()
}

// Close marks the resource closed and waits for in-flight operations before
// calling the close function. It is idempotent; concurrent callers wait for
// the first caller to finish.
func (r *Resource) Close() {
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
