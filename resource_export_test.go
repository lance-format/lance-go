package lance

import "sync"

// ObserveDatasetCloseWait reports when Close releases its mutex inside
// Cond.Wait. Install it before starting concurrent operations on the dataset.
// This test-only adapter distinguishes a waiting Close from an unscheduled one.
func ObserveDatasetCloseWait(d *Dataset) <-chan struct{} {
	return observeResourceCloseWait(d.res)
}

func observeResourceCloseWait(r *resource) <-chan struct{} {
	waiting := make(chan struct{})
	r.cond.L = &observedCloseWaitLocker{Locker: r.cond.L, waiting: waiting}
	return waiting
}

type observedCloseWaitLocker struct {
	sync.Locker
	waiting chan struct{}
	once    sync.Once
}

func (l *observedCloseWaitLocker) Unlock() {
	l.Locker.Unlock()
	l.once.Do(func() { close(l.waiting) })
}
