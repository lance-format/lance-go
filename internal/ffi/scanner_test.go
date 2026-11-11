package ffi

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/lance-format/lance-go/internal/handle"
)

func TestScannerCloseWhilePollPending(t *testing.T) {
	entered := make(chan struct{})
	closed := make(chan uintptr, 1)
	b := &bindings{
		scannerPollNext: func(_ uintptr, _ uintptr, _ uintptr, out *uintptr) int32 {
			*out = 0
			close(entered)
			return pollPending
		},
		scannerClose: func(handleValue uintptr) {
			closed <- handleValue
		},
	}
	s := &Scanner{
		b:         b,
		closeCh:   make(chan struct{}),
		closeDone: make(chan struct{}),
		waiter:    newPollWaiter(),
		stats:     &statsState{},
	}
	s.handle.Store(0x77)
	s.cond = sync.NewCond(&s.mu)
	s.waiterHandle = handle.New(s.waiter)
	s.statsHandle = handle.New(s.stats)

	done := make(chan error, 1)
	go func() {
		_, _, err := s.Next(context.Background())
		done <- err
	}()

	<-entered
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case handleValue := <-closed:
		if handleValue != 0x77 {
			t.Fatalf("native close handle = %#x, want 0x77", handleValue)
		}
	default:
		t.Fatal("native scanner close was not called")
	}
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatalf("Next error = %v, want ErrClosed", err)
	}
	if _, _, err := s.Next(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Next after Close error = %v, want ErrClosed", err)
	}
}
