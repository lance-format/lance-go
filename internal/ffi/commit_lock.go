package ffi

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/handle"
)

// CommitLockCallbacks implements the native commit-lock contract. Acquire and
// Release run synchronously on native threads and may be invoked concurrently.
// Acquire owns the returned lease value even when it also returns an error;
// native code destroys such a partial lease exactly once without releasing it.
// Release may be called more than once for the same lease and must be
// idempotent. Destroy runs exactly once for every returned lease, including
// nil for a stateless lease; when nil it only drops internal lease tracking.
type CommitLockCallbacks struct {
	Acquire func(version uint64) (any, error)
	Release func(lease any, success bool) error
	Destroy func(lease any)
}

type commitLockCallbacks struct {
	Lock           uintptr
	Release        uintptr
	DestroyLease   uintptr
	CallbackCtx    uintptr
	ReleaseContext uintptr
}

var (
	commitLockReleaseOnce     sync.Once
	callbackCommitLockRelease uintptr
)

func initCommitLockReleaseCallback() {
	commitLockReleaseOnce.Do(func() {
		callbackCommitLockRelease = purego.NewCallback(commitLockReleaseCallback)
	})
}

func commitLockReleaseCallback(ctx uintptr, token unsafe.Pointer, success bool, cbErr *callbackError) (ret uintptr) {
	clearCallbackError(cbErr)
	adapter, ok := lookupCommitLockContext(ctx)
	if !ok {
		setCallbackError(cbErr, errors.New("commit lock context is no longer available"))
		return 1
	}
	return adapter.release(token, success, cbErr)
}

type commitLockContext struct {
	callbacks CommitLockCallbacks

	mu     sync.Mutex
	leases map[unsafe.Pointer]*commitLeaseState
}

// CommitLock is an owned native commit-lock handle. Native datasets,
// transactions and scanners retain their own native owner after Close, so the
// lock may be closed while those objects remain usable.
type CommitLock struct {
	b      *bindings
	handle atomic.Uintptr
}

// NewCommitLock constructs a native commit lock. The callback context is
// transferred to native code only on success. Construction failure drops the
// Go context without invoking release_context.
func NewCommitLock(callbacks CommitLockCallbacks) (*CommitLock, error) {
	if callbacks.Acquire == nil {
		return nil, errors.New("ffi: commit lock acquire callback must not be nil")
	}
	if callbacks.Release == nil {
		return nil, errors.New("ffi: commit lock release callback must not be nil")
	}

	b, err := load()
	if err != nil {
		return nil, err
	}
	initContextReleaseCallback()
	initBatchUDFCallbacks()
	initCommitLockReleaseCallback()

	ctx := &commitLockContext{
		callbacks: callbacks,
		leases:    make(map[unsafe.Pointer]*commitLeaseState),
	}
	ctxID := handle.New(ctx)
	raw := commitLockCallbacks{
		Lock:           callbackFragmentInsert,
		Release:        callbackCommitLockRelease,
		CallbackCtx:    uintptr(ctxID),
		ReleaseContext: callbackContextRelease,
		DestroyLease:   statisticsCallback(),
	}

	runtime.LockOSThread()
	nativeHandle := b.commitLockNew(&raw)
	runtime.KeepAlive(ctx)
	runtime.KeepAlive(raw)
	if nativeHandle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		handle.Take(ctxID)
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, errors.New("ffi: native commit lock construction failed")
	}

	runtime.UnlockOSThread()
	lock := &CommitLock{b: b}
	lock.handle.Store(nativeHandle)
	return lock, nil
}

// Handle returns the native commit-lock pointer, or zero after Close.
func (l *CommitLock) Handle() uintptr {
	if l == nil {
		return 0
	}
	return l.handle.Load()
}

// Close drops this handle's native owner. It is idempotent; native objects
// that already retained the lock keep their own owner.
func (l *CommitLock) Close() {
	if l == nil {
		return
	}
	nativeHandle := l.handle.Swap(0)
	if nativeHandle == 0 {
		return
	}
	runtime.LockOSThread()
	l.b.commitLockClose(nativeHandle)
	runtime.UnlockOSThread()
}

func lookupCommitLockContext(ctx uintptr) (*commitLockContext, bool) {
	if ctx == 0 {
		return nil, false
	}
	value, ok := handle.Get(uint64(ctx))
	if !ok {
		return nil, false
	}
	adapter, ok := value.(*commitLockContext)
	return adapter, ok
}

func (c *commitLockContext) acquire(version uint64, outLease *unsafe.Pointer, cbErr *callbackError) (ret uintptr) {
	clearCallbackError(cbErr)
	if outLease == nil {
		setCallbackError(cbErr, errors.New("commit lock acquire received a nil lease slot"))
		return 1
	}
	*outLease = nil
	defer func() {
		if r := recover(); r != nil {
			*outLease = nil
			setCallbackError(cbErr, fmt.Errorf("commit lock acquire panic: %v", r))
			ret = 1
		}
	}()

	lease, err := c.callbacks.Acquire(version)
	if lease != nil {
		*outLease = c.storeLease(lease)
	}
	if err != nil {
		setCallbackError(cbErr, err)
		return 1
	}
	return 0
}

func (c *commitLockContext) release(token unsafe.Pointer, success bool, cbErr *callbackError) (ret uintptr) {
	clearCallbackError(cbErr)
	defer func() {
		if r := recover(); r != nil {
			setCallbackError(cbErr, fmt.Errorf("commit lock release panic: %v", r))
			ret = 1
		}
	}()

	c.mu.Lock()
	lease, ok := c.leases[token]
	c.mu.Unlock()
	if !ok && token != nil {
		setCallbackError(cbErr, fmt.Errorf("commit lock lease %p is not available", token))
		return 1
	}
	var value any
	if ok {
		value = lease.value
	}
	if err := c.callbacks.Release(value, success); err != nil {
		setCallbackError(cbErr, err)
		return 1
	}
	return 0
}

func (c *commitLockContext) dispatchVoid2(payload unsafe.Pointer) {
	c.destroy(payload)
}

func (c *commitLockContext) destroy(token unsafe.Pointer) {
	if token == nil {
		c.destroyLease(nil)
		return
	}

	c.mu.Lock()
	lease, ok := c.leases[token]
	if ok {
		delete(c.leases, token)
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	defer lease.pin.Unpin()
	c.destroyLease(lease.value)
}

// destroyLease contains user panics because native's destroy callback cannot
// report an error and must not unwind through the C ABI.
func (c *commitLockContext) destroyLease(lease any) {
	if c.callbacks.Destroy == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	c.callbacks.Destroy(lease)
}

// A lease token is a pinned scalar allocation, so shared pointer callbacks
// never receive a small integer masquerading as a Go pointer. The user value
// stays in the Go registry and is never exposed to native code.
type commitLeaseState struct {
	token *byte
	pin   runtime.Pinner
	value any
}

func (c *commitLockContext) storeLease(value any) unsafe.Pointer {
	lease := &commitLeaseState{token: new(byte), value: value}
	lease.pin.Pin(lease.token)
	token := unsafe.Pointer(lease.token)
	c.mu.Lock()
	c.leases[token] = lease
	c.mu.Unlock()
	return token
}

// releaseContext drops the adapter after the last native owner releases the
// callback context. Native code destroys all retained leases first.
func (c *commitLockContext) releaseContext() {
	c.mu.Lock()
	c.leases = nil
	c.mu.Unlock()
}
