package lifetime

import (
	"sync"
	"sync/atomic"
	"testing"
)

type waitNotifier struct {
	sync.Locker
	once    sync.Once
	waiting chan struct{}
}

func (l *waitNotifier) Unlock() {
	l.Locker.Unlock()
	l.once.Do(func() { close(l.waiting) })
}

func TestCloseWaitsForAcquiredOperation(t *testing.T) {
	var closes atomic.Int32
	released := make(chan struct{})
	resource := New(func() {
		closes.Add(1)
		close(released)
	})
	if !resource.Acquire() {
		t.Fatal("Acquire on a new resource failed")
	}

	waiting := make(chan struct{})
	resource.Cond().L = &waitNotifier{Locker: resource.Cond().L, waiting: waiting}
	done := make(chan struct{})
	go func() {
		resource.Close()
		close(done)
	}()
	<-waiting
	if got := closes.Load(); got != 0 {
		t.Fatalf("close function ran %d times before Release", got)
	}

	resource.Release()
	<-released
	<-done
	if got := closes.Load(); got != 1 {
		t.Fatalf("close function ran %d times, want 1", got)
	}
}

func TestCloseIsIdempotentAndWaitsForConcurrentCallers(t *testing.T) {
	var closes atomic.Int32
	resource := New(func() { closes.Add(1) })
	if !resource.Acquire() {
		t.Fatal("Acquire on a new resource failed")
	}

	done := make(chan struct{}, 2)
	go func() {
		resource.Close()
		done <- struct{}{}
	}()
	go func() {
		resource.Close()
		done <- struct{}{}
	}()
	resource.Release()
	<-done
	<-done
	resource.Close()

	if got := closes.Load(); got != 1 {
		t.Fatalf("close function ran %d times, want 1", got)
	}
	if resource.Acquire() {
		t.Fatal("Acquire after Close succeeded")
	}
}
