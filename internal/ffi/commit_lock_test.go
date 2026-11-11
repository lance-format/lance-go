package ffi

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/lance-format/lance-go/internal/handle"
)

type commitLockTestState struct {
	acquireCalls int
	releaseCalls int
	releaseFlags []bool
	destroyCalls int
	destroyed    []any
	lease        any
	acquireErr   error
	releaseErr   error
}

func newCommitLockTestContext(t *testing.T, state *commitLockTestState) (uint64, *commitLockContext) {
	t.Helper()
	ctx := &commitLockContext{
		callbacks: CommitLockCallbacks{
			Acquire: func(uint64) (any, error) {
				state.acquireCalls++
				return state.lease, state.acquireErr
			},
			Release: func(lease any, success bool) error {
				state.releaseCalls++
				state.releaseFlags = append(state.releaseFlags, success)
				return state.releaseErr
			},
			Destroy: func(lease any) {
				state.destroyCalls++
				state.destroyed = append(state.destroyed, lease)
			},
		},
		leases: make(map[unsafe.Pointer]*commitLeaseState),
	}
	id := handle.New(ctx)
	t.Cleanup(func() { contextReleaseCallback(uintptr(id)) })
	return id, ctx
}

func TestCommitLockCallbackLeaseLifecycle(t *testing.T) {
	state := &commitLockTestState{lease: "lease"}
	id, _ := newCommitLockTestContext(t, state)

	var acquireErr callbackError
	var lease unsafe.Pointer
	if got := batchUDFInsertFragmentCallback(uintptr(id), 7, (*byte)(unsafe.Pointer(&lease)), &acquireErr); got != 0 {
		t.Fatalf("acquire status = %d, want 0 (error %+v)", got, acquireErr)
	}
	if state.acquireCalls != 1 || lease == nil {
		t.Fatalf("acquire calls = %d lease = %p, want one nonzero lease", state.acquireCalls, lease)
	}

	runtime.GC()
	var releaseErr callbackError
	if got := commitLockReleaseCallback(uintptr(id), lease, true, &releaseErr); got != 0 {
		t.Fatalf("release status = %d, want 0 (error %+v)", got, releaseErr)
	}
	if got := commitLockReleaseCallback(uintptr(id), lease, false, &releaseErr); got != 0 {
		t.Fatalf("repeated release status = %d, want 0 (error %+v)", got, releaseErr)
	}
	if state.releaseCalls != 2 || len(state.releaseFlags) != 2 || !state.releaseFlags[0] || state.releaseFlags[1] {
		t.Fatalf("release calls = %d flags = %v, want [true false]", state.releaseCalls, state.releaseFlags)
	}

	dispatchVoid2(uintptr(id), lease)
	dispatchVoid2(uintptr(id), lease)
	if state.destroyCalls != 1 || len(state.destroyed) != 1 || state.destroyed[0] != "lease" {
		t.Fatalf("destroy calls = %d values = %v, want exactly one lease", state.destroyCalls, state.destroyed)
	}
}

func TestCommitLockCallbackPartialAndPanic(t *testing.T) {
	t.Run("partial lease", func(t *testing.T) {
		state := &commitLockTestState{lease: "partial", acquireErr: errors.New("conflict")}
		id, _ := newCommitLockTestContext(t, state)
		var acquireErr callbackError
		var lease unsafe.Pointer
		if got := batchUDFInsertFragmentCallback(uintptr(id), 1, (*byte)(unsafe.Pointer(&lease)), &acquireErr); got != 1 {
			t.Fatalf("acquire status = %d, want 1", got)
		}
		if lease == nil || acquireErr.Code == 0 {
			t.Fatalf("partial lease = %p error code = %d, want both", lease, acquireErr.Code)
		}
		dispatchVoid2(uintptr(id), lease)
		if state.releaseCalls != 0 || state.destroyCalls != 1 || state.destroyed[0] != "partial" {
			t.Fatalf("release calls = %d destroy calls = %d values = %v, want destroy only", state.releaseCalls, state.destroyCalls, state.destroyed)
		}
	})

	t.Run("acquire panic", func(t *testing.T) {
		state := &commitLockTestState{lease: "unused"}
		id, _ := newCommitLockTestContext(t, state)
		ctx, _ := handle.Get(id)
		ctx.(*commitLockContext).callbacks.Acquire = func(uint64) (any, error) {
			panic("boom")
		}
		var acquireErr callbackError
		var lease unsafe.Pointer
		if got := batchUDFInsertFragmentCallback(uintptr(id), 1, (*byte)(unsafe.Pointer(&lease)), &acquireErr); got != 1 {
			t.Fatalf("acquire status = %d, want 1", got)
		}
		if lease != nil || acquireErr.Code == 0 || !strings.Contains(goString(acquireErr.Message.Data), "boom") {
			t.Fatalf("lease = %p error = %+v, want contained panic", lease, acquireErr)
		}
	})

	t.Run("release error", func(t *testing.T) {
		state := &commitLockTestState{lease: "lease", releaseErr: errors.New("release failed")}
		id, _ := newCommitLockTestContext(t, state)
		var acquireErr callbackError
		var lease unsafe.Pointer
		if got := batchUDFInsertFragmentCallback(uintptr(id), 1, (*byte)(unsafe.Pointer(&lease)), &acquireErr); got != 0 {
			t.Fatalf("acquire status = %d, want 0", got)
		}
		var releaseErr callbackError
		if got := commitLockReleaseCallback(uintptr(id), lease, true, &releaseErr); got != 1 {
			t.Fatalf("release status = %d, want 1", got)
		}
		if releaseErr.Code == 0 || state.destroyCalls != 0 {
			t.Fatalf("release error code = %d destroy calls = %d, want error and no destroy", releaseErr.Code, state.destroyCalls)
		}
		dispatchVoid2(uintptr(id), lease)
	})

	t.Run("destroy panic", func(t *testing.T) {
		state := &commitLockTestState{lease: "lease"}
		id, ctx := newCommitLockTestContext(t, state)
		var acquireErr callbackError
		var lease unsafe.Pointer
		if got := batchUDFInsertFragmentCallback(uintptr(id), 1, (*byte)(unsafe.Pointer(&lease)), &acquireErr); got != 0 {
			t.Fatalf("acquire status = %d, want 0", got)
		}
		ctx.callbacks.Destroy = func(any) {
			state.destroyCalls++
			panic("destroy boom")
		}
		dispatchVoid2(uintptr(id), lease)
		dispatchVoid2(uintptr(id), lease)
		if state.destroyCalls != 1 {
			t.Fatalf("destroy calls = %d, want one contained panic", state.destroyCalls)
		}
	})
}

func TestCommitLockCallbackStatelessDestroy(t *testing.T) {
	state := &commitLockTestState{}
	id, _ := newCommitLockTestContext(t, state)
	var acquireErr callbackError
	var lease unsafe.Pointer
	if got := batchUDFInsertFragmentCallback(uintptr(id), 1, (*byte)(unsafe.Pointer(&lease)), &acquireErr); got != 0 {
		t.Fatalf("acquire status = %d, want 0", got)
	}
	if lease != nil {
		t.Fatalf("stateless lease = %p, want 0", lease)
	}
	dispatchVoid2(uintptr(id), nil)
	if state.destroyCalls != 1 || len(state.destroyed) != 1 || state.destroyed[0] != nil {
		t.Fatalf("destroy calls = %d values = %v, want one nil lease", state.destroyCalls, state.destroyed)
	}
}

func TestCommitLockContextReleaseIsIdempotentForUnexpectedRepeat(t *testing.T) {
	state := &commitLockTestState{}
	id, ctx := newCommitLockTestContext(t, state)
	contextReleaseCallback(uintptr(id))
	if _, ok := handle.Get(id); ok {
		t.Fatal("context release left the handle registered")
	}
	contextReleaseCallback(uintptr(id))
	if ctx.leases != nil {
		t.Fatal("context release retained leases")
	}
}
