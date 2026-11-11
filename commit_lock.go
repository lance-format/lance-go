package lance

import (
	"fmt"
	"sync"

	"github.com/lance-format/lance-go/internal/ffi"
)

// CommitLease is the opaque value returned by CommitLockCallbacks.Acquire.
// A nil lease represents stateless coordination.
type CommitLease any

// CommitLockCallbacks implements externally coordinated commit serialization.
//
// Acquire and Release run synchronously on native threads. They may be called
// concurrently but must not re-enter the dataset or transaction currently
// being committed, or call Close. Acquire may return a nil lease for stateless
// coordination. If it returns both a lease and an error, native code destroys
// that partial lease once without calling Release.
//
// Callbacks do not receive a context.Context; native code waits for them to
// return and cannot cancel an in-flight callback. Release may be called more
// than once for the same lease; implementations must be idempotent. A release
// error does not undo a commit that native code has already published. Destroy
// is optional and runs exactly once for every lease returned by Acquire,
// including a nil stateless lease.
type CommitLockCallbacks struct {
	Acquire func(version uint64) (CommitLease, error)
	Release func(lease CommitLease, success bool) error
	Destroy func(lease CommitLease)
}

// CommitLock owns an external commit lock. Close only drops this handle's
// owner; datasets that already retained the lock remain usable.
type CommitLock struct {
	mu     sync.RWMutex
	closed bool
	native *ffi.CommitLock
}

// NewCommitLock constructs an external commit lock. Construction transfers the
// callback set only on success; a failed construction invokes no callback.
func NewCommitLock(callbacks CommitLockCallbacks) (*CommitLock, error) {
	if callbacks.Acquire == nil {
		return nil, invalidError("create commit lock", "", "acquire callback must not be nil")
	}
	if callbacks.Release == nil {
		return nil, invalidError("create commit lock", "", "release callback must not be nil")
	}
	nativeCallbacks := ffi.CommitLockCallbacks{
		Acquire: func(version uint64) (any, error) {
			return callbacks.Acquire(version)
		},
		Release: func(lease any, success bool) error {
			return callbacks.Release(lease, success)
		},
	}
	if callbacks.Destroy != nil {
		nativeCallbacks.Destroy = func(lease any) {
			callbacks.Destroy(lease)
		}
	}
	native, err := ffi.NewCommitLock(nativeCallbacks)
	if err != nil {
		return nil, wrapError("commit lock", "", err)
	}
	return &CommitLock{native: native}, nil
}

// Close releases the caller's native commit-lock owner. It is idempotent and
// waits for native calls that already pinned the lock. Callback state must
// outlive native datasets that retain the lock. Close returns an error only for
// API symmetry; native close has no failure status.
func (l *CommitLock) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	native := l.native
	l.mu.Unlock()
	if native != nil {
		native.Close()
	}
	return nil
}

func (l *CommitLock) pin(op, uri string) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	l.mu.RLock()
	if l.closed || l.native == nil || l.native.Handle() == 0 {
		l.mu.RUnlock()
		return nil, closedError(op, uri)
	}
	return l.mu.RUnlock, nil
}

// WithOpenCommitLock routes Open through an external commit lock.
func WithOpenCommitLock(lock *CommitLock) OpenOption {
	return func(config *openConfig) error {
		if lock == nil {
			return fmt.Errorf("commit lock must not be nil")
		}
		config.commitLock = lock
		return nil
	}
}

// WithWriteCommitLock routes ordinary and uncommitted writes through an
// external commit lock. It is not supported by namespace writes or fragment
// creation.
func WithWriteCommitLock(lock *CommitLock) WriteOption {
	return func(config *writeConfig) error {
		if lock == nil {
			return fmt.Errorf("commit lock must not be nil")
		}
		config.commitLock = lock
		return nil
	}
}

// WithCommitLock routes CommitTransaction and CommitTransactionBatch through an
// external commit lock.
func WithCommitLock(lock *CommitLock) CommitOption {
	return func(config *commitConfig) error {
		if lock == nil {
			return fmt.Errorf("commit lock must not be nil")
		}
		config.commitLock = lock
		return nil
	}
}
