package accessor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	_ "github.com/lance-format/lance-go/internal/bundled"
	"github.com/lance-format/lance-go/internal/loader"
)

type blockingProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (p *blockingProvider) StorageOptionsProviderID() string { return "test/close-wait" }
func (p *blockingProvider) FetchStorageOptions(context.Context) (map[string]string, error) {
	close(p.entered)
	<-p.release
	return map[string]string{"allow_http": "true"}, nil
}

type closeWaitLocker struct {
	sync.Locker
	once    sync.Once
	waiting chan struct{}
}

func (l *closeWaitLocker) Unlock() {
	l.Locker.Unlock()
	l.once.Do(func() { close(l.waiting) })
}

func TestStorageOptionsAccessorCloseWaitsForSnapshot(t *testing.T) {
	if _, err := loader.Load(); err != nil {
		if errors.Is(err, loader.ErrNotFound) {
			t.Skip("lance-c is not available")
		}
		t.Fatal(err)
	}
	p := &blockingProvider{entered: make(chan struct{}), release: make(chan struct{})}
	owned, err := New(nil, p, func(_, _ string, err error) error { return err })
	if err != nil {
		t.Fatal(err)
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(p.release) }) }
	t.Cleanup(func() { release(); owned.Close() })
	waiting := make(chan struct{})
	owned.life.Cond().L = &closeWaitLocker{Locker: owned.life.Cond().L, waiting: waiting}
	snapshotDone := make(chan error, 1)
	go func() { _, _, err := owned.Snapshot(); snapshotDone <- err }()
	select {
	case <-p.entered:
	case err := <-snapshotDone:
		t.Fatalf("Snapshot did not enter provider: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	closeDone := make(chan struct{})
	go func() { owned.Close(); close(closeDone) }()
	select {
	case <-waiting:
	case <-closeDone:
		t.Fatal("Close returned without waiting for Snapshot")
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not enter its wait")
	}
	select {
	case <-closeDone:
		t.Fatal("Close returned while Snapshot was blocked")
	default:
	}
	release()
	if err := <-snapshotDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-closeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not finish after Snapshot")
	}
}
