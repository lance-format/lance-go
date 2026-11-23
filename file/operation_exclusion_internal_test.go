package file

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/lifetime"
)

// writerHandle builds one writer around res and exposes its operation acquire
// path without creating a native writer.
type writerHandle struct {
	name   string
	attach func(*lifetime.Resource) (acquire func() (func(), error), closeFn func() error)
}

func writerHandles() []writerHandle {
	return []writerHandle{
		{"Writer", func(res *lifetime.Resource) (func() (func(), error), func() error) {
			handle := &Writer{uri: "file:///example"}
			handle.res = res
			return func() (func(), error) {
				_, release, err := handle.acquire("writer test")
				return release, err
			}, handle.Close
		}},
		{"PackedBlobWriter", func(res *lifetime.Resource) (func() (func(), error), func() error) {
			handle := &PackedBlobWriter{path: "file:///example"}
			handle.res = res
			return func() (func(), error) {
				_, release, err := handle.acquire("packed blob writer test")
				return release, err
			}, handle.Close
		}},
		{"DedicatedBlobWriter", func(res *lifetime.Resource) (func() (func(), error), func() error) {
			handle := &DedicatedBlobWriter{path: "file:///example"}
			handle.res = res
			return func() (func(), error) {
				_, release, err := handle.acquire("dedicated blob writer test")
				return release, err
			}, handle.Close
		}},
	}
}

func TestWriterOperationsExcludeEachOther(t *testing.T) {
	for _, handle := range writerHandles() {
		t.Run(handle.name, func(t *testing.T) {
			acquire, _ := handle.attach(lifetime.New(func() {}))
			release, err := acquire()
			if err != nil {
				t.Fatalf("first acquire: %v", err)
			}
			if release == nil {
				t.Fatal("first acquire returned no release function")
			}

			started := make(chan struct{})
			acquired := make(chan func(), 1)
			failed := make(chan error, 1)
			go func() {
				close(started)
				second, err := acquire()
				if err != nil {
					failed <- err
					return
				}
				acquired <- second
			}()
			<-started

			select {
			case second := <-acquired:
				second()
				release()
				t.Fatal("second operation acquired the writer before the first released it")
			case err := <-failed:
				release()
				t.Fatalf("second acquire: %v", err)
			case <-time.After(100 * time.Millisecond):
			}

			release()
			select {
			case second := <-acquired:
				second()
			case err := <-failed:
				t.Fatalf("second acquire after release: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("second operation did not proceed after the first released the writer")
			}
		})
	}
}

// closeWaitNotifier reports when Close releases its mutex inside Cond.Wait, so
// the test can distinguish a waiting Close from an unscheduled one.
type closeWaitNotifier struct {
	sync.Locker
	once    sync.Once
	waiting chan struct{}
}

func (n *closeWaitNotifier) Unlock() {
	n.Locker.Unlock()
	n.once.Do(func() { close(n.waiting) })
}

func TestWriterCloseWaitsForInflightOperation(t *testing.T) {
	for _, handle := range writerHandles() {
		t.Run(handle.name, func(t *testing.T) {
			resource := lifetime.New(func() {})
			acquire, closeFn := handle.attach(resource)
			release, err := acquire()
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}

			waiting := make(chan struct{})
			resource.Cond().L = &closeWaitNotifier{Locker: resource.Cond().L, waiting: waiting}
			closed := make(chan error, 1)
			go func() { closed <- closeFn() }()
			select {
			case <-waiting:
			case err := <-closed:
				release()
				t.Fatalf("Close returned %v before the in-flight operation released", err)
			case <-time.After(5 * time.Second):
				release()
				t.Fatal("Close did not wait for the in-flight operation")
			}

			select {
			case err := <-closed:
				release()
				t.Fatalf("Close returned %v before the in-flight operation released", err)
			default:
			}

			release()
			select {
			case err := <-closed:
				if err != nil {
					t.Fatalf("Close: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Close did not return after the in-flight operation released")
			}
		})
	}
}

func TestWriterAcquireAfterCloseReportsClosed(t *testing.T) {
	for _, handle := range writerHandles() {
		t.Run(handle.name, func(t *testing.T) {
			acquire, closeFn := handle.attach(lifetime.New(func() {}))
			if err := closeFn(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			for i := 0; i < 2; i++ {
				release, err := acquire()
				if !errors.Is(err, lance.ErrClosed) {
					t.Fatalf("acquire %d after Close = %v, want ErrClosed", i+1, err)
				}
				if release != nil {
					release()
					t.Fatalf("acquire %d after Close returned a release function", i+1)
				}
			}
		})
	}
}
