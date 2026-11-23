package lance

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCommitLockCloseWaitsForConcurrentCallers pauses the lock's native
// teardown and verifies that a second Close cannot return until the single
// release finishes.
func TestCommitLockCloseWaitsForConcurrentCallers(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	var releaseOnce sync.Once
	var closes atomic.Int32
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()

	lock := &CommitLock{}
	lock.res = newResource(func() {
		closes.Add(1)
		enteredOnce.Do(func() { close(entered) })
		<-release
	})

	first := make(chan error, 1)
	go func() { first <- lock.Close() }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first Close did not enter native teardown")
	}

	second := make(chan error, 1)
	go func() { second <- lock.Close() }()
	select {
	case err := <-second:
		t.Fatalf("second Close returned %v while native teardown was still in progress", err)
	case <-time.After(100 * time.Millisecond):
	}

	unblock()
	for i, done := range []chan error{first, second} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Close caller %d: %v", i+1, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Close caller %d did not return after teardown completed", i+1)
		}
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("Close after teardown: %v", err)
	}
	if got := closes.Load(); got != 1 {
		t.Fatalf("native teardown ran %d times, want 1", got)
	}
}

func TestCommitLockCloseNilAndZeroValue(t *testing.T) {
	if err := (*CommitLock)(nil).Close(); err != nil {
		t.Fatalf("nil CommitLock.Close: %v", err)
	}
	if err := (&CommitLock{}).Close(); err != nil {
		t.Fatalf("zero-value CommitLock.Close: %v", err)
	}

	release, err := (*CommitLock)(nil).pin("test", "file:///example")
	if err != nil {
		t.Fatalf("nil CommitLock.pin: %v", err)
	}
	release()

	if _, err := (&CommitLock{}).pin("test", "file:///example"); !errors.Is(err, ErrClosed) {
		t.Fatalf("zero-value CommitLock.pin = %v, want ErrClosed", err)
	}
}
