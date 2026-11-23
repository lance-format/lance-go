package file

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lance-format/lance-go/internal/lifetime"
)

// controlledTeardown replaces one handle's native close function so tests can
// pause teardown, observe concurrent Close callers, and count releases.
type controlledTeardown struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once
	closes      atomic.Int32
}

func newControlledTeardown() *controlledTeardown {
	return &controlledTeardown{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

// closeFn blocks until unblock is called, then returns.
func (c *controlledTeardown) closeFn() func() {
	return func() {
		c.closes.Add(1)
		c.enteredOnce.Do(func() { close(c.entered) })
		<-c.release
	}
}

func (c *controlledTeardown) unblock() {
	c.releaseOnce.Do(func() { close(c.release) })
}

// assertConcurrentCloseWaits verifies that a second Close call cannot return
// while the first call is still running native teardown, and that teardown
// runs exactly once.
func assertConcurrentCloseWaits(t *testing.T, closeFn func() error, teardown *controlledTeardown) {
	t.Helper()
	t.Cleanup(teardown.unblock)

	first := make(chan error, 1)
	go func() { first <- closeFn() }()
	select {
	case <-teardown.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first Close did not enter native teardown")
	}

	second := make(chan error, 1)
	go func() { second <- closeFn() }()
	select {
	case err := <-second:
		t.Fatalf("second Close returned %v while native teardown was still in progress", err)
	case <-time.After(100 * time.Millisecond):
	}

	teardown.unblock()
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
	if err := closeFn(); err != nil {
		t.Fatalf("Close after teardown: %v", err)
	}
	if got := teardown.closes.Load(); got != 1 {
		t.Fatalf("native teardown ran %d times, want 1", got)
	}
}

func TestHandleCloseWaitsForConcurrentCallers(t *testing.T) {
	tests := []struct {
		name   string
		attach func(*lifetime.Resource) func() error
	}{
		{"Reader", func(res *lifetime.Resource) func() error {
			handle := &Reader{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"Session", func(res *lifetime.Resource) func() error {
			handle := &Session{baseURI: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"Writer", func(res *lifetime.Resource) func() error {
			handle := &Writer{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"PackedBlobWriter", func(res *lifetime.Resource) func() error {
			handle := &PackedBlobWriter{path: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"DedicatedBlobWriter", func(res *lifetime.Resource) func() error {
			handle := &DedicatedBlobWriter{path: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"Metadata", func(res *lifetime.Resource) func() error {
			handle := &Metadata{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"Statistics", func(res *lifetime.Resource) func() error {
			handle := &Statistics{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teardown := newControlledTeardown()
			closeFn := tt.attach(lifetime.New(teardown.closeFn()))
			assertConcurrentCloseWaits(t, closeFn, teardown)
		})
	}
}

func TestHandleCloseNilAndZeroValue(t *testing.T) {
	tests := []struct {
		name  string
		close func() error
	}{
		{"Reader/nil", (*Reader)(nil).Close},
		{"Reader/zero", (&Reader{}).Close},
		{"Session/nil", (*Session)(nil).Close},
		{"Session/zero", (&Session{}).Close},
		{"Writer/nil", (*Writer)(nil).Close},
		{"Writer/zero", (&Writer{}).Close},
		{"PackedBlobWriter/nil", (*PackedBlobWriter)(nil).Close},
		{"PackedBlobWriter/zero", (&PackedBlobWriter{}).Close},
		{"DedicatedBlobWriter/nil", (*DedicatedBlobWriter)(nil).Close},
		{"DedicatedBlobWriter/zero", (&DedicatedBlobWriter{}).Close},
		{"Metadata/nil", (*Metadata)(nil).Close},
		{"Metadata/zero", (&Metadata{}).Close},
		{"Statistics/nil", (*Statistics)(nil).Close},
		{"Statistics/zero", (&Statistics{}).Close},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
		})
	}
}
