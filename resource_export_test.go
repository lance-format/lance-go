package lance

import "sync"

// ObserveDatasetCloseWait reports when Close releases its mutex inside
// Cond.Wait. Install it before starting concurrent operations on the dataset.
// This test-only adapter distinguishes a waiting Close from an unscheduled one.
func ObserveDatasetCloseWait(d *Dataset) <-chan struct{} {
	waiting := make(chan struct{})
	d.res.cond.L = &observedCloseWaitLocker{Locker: d.res.cond.L, waiting: waiting}
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
