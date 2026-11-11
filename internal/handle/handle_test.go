package handle

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestNewGetTakeRemove(t *testing.T) {
	before := Len()
	value := &struct{ n int }{n: 7}
	id := New(value)
	if id == 0 {
		t.Fatal("New returned zero")
	}
	if Len() != before+1 {
		t.Fatalf("Len = %d, want %d", Len(), before+1)
	}

	got, ok := Get(id)
	if !ok || got != value {
		t.Fatalf("Get(%d) = %v, %v; want original value, true", id, got, ok)
	}
	got, ok = Take(id)
	if !ok || got != value {
		t.Fatalf("Take(%d) = %v, %v; want original value, true", id, got, ok)
	}
	if _, ok := Take(id); ok {
		t.Fatalf("second Take(%d) succeeded", id)
	}
	if _, ok := Get(id); ok {
		t.Fatalf("Get(%d) succeeded after Take", id)
	}
	if Remove(id) {
		t.Fatalf("Remove(%d) succeeded after Take", id)
	}
	if Len() != before {
		t.Fatalf("Len = %d, want %d", Len(), before)
	}
}

func TestZeroAndUnknownIDs(t *testing.T) {
	if _, ok := Get(0); ok {
		t.Fatal("Get(0) succeeded")
	}
	if _, ok := Take(0); ok {
		t.Fatal("Take(0) succeeded")
	}
	if Remove(0) {
		t.Fatal("Remove(0) succeeded")
	}
	if _, ok := Get(^uint64(0)); ok {
		t.Fatal("Get(max) succeeded")
	}
}

func TestIDsAreNotReused(t *testing.T) {
	const n = 1000
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = New(i)
	}
	for i := 1; i < n; i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("ids are not increasing: %d then %d", ids[i-1], ids[i])
		}
	}
	for _, id := range ids {
		Remove(id)
	}
	next := New("after")
	if next <= ids[n-1] {
		t.Fatalf("New reused or moved backwards: got %d, previous %d", next, ids[n-1])
	}
	Remove(next)
}

func TestTakeExactlyOnceConcurrently(t *testing.T) {
	for iteration := 0; iteration < 50; iteration++ {
		id := New(iteration)
		var successes atomic.Int64
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, ok := Take(id); ok {
					successes.Add(1)
				}
			}()
		}
		close(start)
		wg.Wait()
		if got := successes.Load(); got != 1 {
			t.Fatalf("iteration %d: successful Takes = %d, want 1", iteration, got)
		}
	}
}

func TestNilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New(nil) did not panic")
		}
	}()
	New(nil)
}
