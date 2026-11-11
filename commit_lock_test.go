package lance_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lance "github.com/lance-format/lance-go"
)

type commitLockCounters struct {
	acquire      atomic.Int64
	releaseTrue  atomic.Int64
	releaseFalse atomic.Int64
	destroy      atomic.Int64
	firstAcquire atomic.Bool
	acquireHook  func(version uint64) error
	releaseErr   error
}

func newCommitLock(t *testing.T, counters *commitLockCounters) *lance.CommitLock {
	t.Helper()
	lock, err := lance.NewCommitLock(lance.CommitLockCallbacks{
		Acquire: func(version uint64) (lance.CommitLease, error) {
			counters.acquire.Add(1)
			if counters.acquireHook != nil {
				if err := counters.acquireHook(version); err != nil {
					return nil, err
				}
			}
			return "lease", nil
		},
		Release: func(lease lance.CommitLease, success bool) error {
			if success {
				counters.releaseTrue.Add(1)
			} else {
				counters.releaseFalse.Add(1)
			}
			return counters.releaseErr
		},
		Destroy: func(lance.CommitLease) {
			counters.destroy.Add(1)
		},
	})
	if err != nil {
		t.Fatalf("NewCommitLock: %v", err)
	}
	t.Cleanup(func() { lock.Close() })
	return lock
}

func TestCommitLockWriteRetainsDatasetAfterOwnerClose(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	counters := &commitLockCounters{}
	lock := newCommitLock(t, counters)

	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})), lance.WithWriteCommitLock(lock))
	if err != nil {
		t.Fatalf("Create with commit lock: %v", err)
	}
	defer dataset.Close()
	if got := mutationVersion(t, dataset); got != 1 {
		t.Fatalf("version = %d, want 1", got)
	}
	requireMutationRows(t, dataset, []int64{1, 2}, []string{"a", "b"})
	opened, err := lance.Open(ctx, uri, lance.WithOpenCommitLock(lock))
	if err != nil {
		t.Fatalf("Open with commit lock: %v", err)
	}
	requireMutationRows(t, opened, []int64{1, 2}, []string{"a", "b"})
	opened.Close()

	if err := lock.Close(); err != nil {
		t.Fatalf("Close lock: %v", err)
	}
	requireMutationRows(t, dataset, []int64{1, 2}, []string{"a", "b"})
	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after lock close: %v", err)
	}
	requireMutationRows(t, reopened, []int64{1, 2}, []string{"a", "b"})
	reopened.Close()

	if got := counters.acquire.Load(); got == 0 {
		t.Fatal("commit lock was not acquired")
	}
	if got := counters.releaseTrue.Load(); got != counters.acquire.Load() {
		t.Fatalf("successful releases = %d, want %d", got, counters.acquire.Load())
	}
	dataset.Close()
	if got := counters.destroy.Load(); got != counters.acquire.Load() {
		t.Fatalf("destroy calls = %d, want %d", got, counters.acquire.Load())
	}
}

func TestCommitLockStagedAndBatchCommit(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	fixture, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})))
	if err != nil {
		t.Fatalf("Create fixture: %v", err)
	}
	fixture.Close()
	counters := &commitLockCounters{}
	lock := newCommitLock(t, counters)

	staged, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{2}, []string{"b"}, []bool{true})),
		lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteCommitLock(lock))
	if err != nil {
		t.Fatalf("WriteUncommitted: %v", err)
	}
	first, err := staged.Commit(ctx, uri, lance.WithCommitLock(lock))
	if err != nil {
		t.Fatalf("staged commit: %v", err)
	}
	if got := mutationVersion(t, first); got != 2 {
		t.Fatalf("staged commit version = %d, want 2", got)
	}
	requireMutationRows(t, first, []int64{1, 2}, []string{"a", "b"})
	first.Close()
	staged.Close()

	tx1, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{3}, []string{"c"}, []bool{true})),
		lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteCommitLock(lock))
	if err != nil {
		t.Fatalf("WriteUncommitted tx1: %v", err)
	}
	tx2, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{4}, []string{"d"}, []bool{true})),
		lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteCommitLock(lock))
	if err != nil {
		tx1.Close()
		t.Fatalf("WriteUncommitted tx2: %v", err)
	}
	batched, batchedTx, err := lance.CommitTransactionBatch(ctx, uri, []*lance.Transaction{tx1, tx2}, lance.WithCommitLock(lock))
	if err != nil {
		tx1.Close()
		tx2.Close()
		t.Fatalf("CommitTransactionBatch: %v", err)
	}
	if got := mutationVersion(t, batched); got != 3 {
		t.Fatalf("batch version = %d, want 3", got)
	}
	requireMutationRows(t, batched, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	batched.Close()
	batchedTx.Close()
	tx1.Close()
	tx2.Close()

	if err := lock.Close(); err != nil {
		t.Fatalf("Close lock: %v", err)
	}
	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after commit lock close: %v", err)
	}
	if got := mutationVersion(t, reopened); got != 3 {
		t.Fatalf("reopened version = %d, want 3", got)
	}
	requireMutationRows(t, reopened, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	reopened.Close()
}

func TestCommitLockConflictReportsFailedRelease(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	fixture, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})))
	if err != nil {
		t.Fatalf("Create fixture: %v", err)
	}
	fixture.Close()

	bumpReader := makeReader(t, schema, makeRecord(t, schema, []int64{2}, []string{"b"}, []bool{true}))
	counters := &commitLockCounters{}
	counters.acquireHook = func(uint64) error {
		if counters.firstAcquire.CompareAndSwap(false, true) {
			bumped, err := lance.Append(ctx, uri, bumpReader)
			if err != nil {
				return err
			}
			return bumped.Close()
		}
		return nil
	}
	lock := newCommitLock(t, counters)
	staged, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{3}, []string{"c"}, []bool{true})),
		lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteCommitLock(lock))
	if err != nil {
		t.Fatalf("WriteUncommitted: %v", err)
	}
	dataset, err := staged.Commit(ctx, uri, lance.WithCommitLock(lock), lance.WithCommitMaxRetries(4))
	if err != nil {
		t.Fatalf("commit after conflict: %v", err)
	}
	if got := mutationVersion(t, dataset); got != 3 {
		t.Fatalf("version = %d, want 3", got)
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3}, []string{"a", "b", "c"})
	if counters.releaseFalse.Load() == 0 {
		t.Fatal("conflict did not report a failed release")
	}
	if counters.releaseTrue.Load() == 0 {
		t.Fatal("successful release was not reported")
	}
	dataset.Close()
	staged.Close()
}

func TestCommitLockAcquireErrorsAreContained(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	schema := testSchema()

	t.Run("partial lease", func(t *testing.T) {
		var acquireCalls, releaseCalls, destroyCalls atomic.Int64
		lock, err := lance.NewCommitLock(lance.CommitLockCallbacks{
			Acquire: func(uint64) (lance.CommitLease, error) {
				acquireCalls.Add(1)
				return "partial", errors.New("injected acquire failure")
			},
			Release: func(lance.CommitLease, bool) error {
				releaseCalls.Add(1)
				return nil
			},
			Destroy: func(lease lance.CommitLease) {
				destroyCalls.Add(1)
				if lease != "partial" {
					t.Errorf("destroyed lease = %v, want partial", lease)
				}
			},
		})
		if err != nil {
			t.Fatalf("NewCommitLock: %v", err)
		}
		defer lock.Close()
		uri := filepath.Join(t.TempDir(), "data.lance")
		_, err = lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})), lance.WithWriteCommitLock(lock))
		if err == nil {
			t.Fatal("Create succeeded despite acquire error")
		}
		if acquireCalls.Load() != 1 || releaseCalls.Load() != 0 || destroyCalls.Load() != 1 {
			t.Fatalf("acquire=%d release=%d destroy=%d, want 1/0/1", acquireCalls.Load(), releaseCalls.Load(), destroyCalls.Load())
		}
	})

	t.Run("acquire panic", func(t *testing.T) {
		var acquireCalls, releaseCalls, destroyCalls atomic.Int64
		lock, err := lance.NewCommitLock(lance.CommitLockCallbacks{
			Acquire: func(uint64) (lance.CommitLease, error) {
				acquireCalls.Add(1)
				panic("injected acquire panic")
			},
			Release: func(lance.CommitLease, bool) error {
				releaseCalls.Add(1)
				return nil
			},
			Destroy: func(lance.CommitLease) {
				destroyCalls.Add(1)
			},
		})
		if err != nil {
			t.Fatalf("NewCommitLock: %v", err)
		}
		defer lock.Close()
		uri := filepath.Join(t.TempDir(), "data.lance")
		_, err = lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})), lance.WithWriteCommitLock(lock))
		if err == nil {
			t.Fatal("Create succeeded despite acquire panic")
		}
		if acquireCalls.Load() != 1 || releaseCalls.Load() != 0 || destroyCalls.Load() != 0 {
			t.Fatalf("acquire=%d release=%d destroy=%d, want 1/0/0", acquireCalls.Load(), releaseCalls.Load(), destroyCalls.Load())
		}
	})
}

func TestCommitLockReleaseErrorAfterPublication(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	counters := &commitLockCounters{releaseErr: errors.New("injected release failure")}
	lock := newCommitLock(t, counters)

	_, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})), lance.WithWriteCommitLock(lock))
	if err == nil {
		t.Fatal("Create succeeded despite release error")
	}
	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open published dataset: %v", err)
	}
	requireMutationRows(t, reopened, []int64{1}, []string{"a"})
	reopened.Close()
	if counters.releaseTrue.Load() != 1 || counters.destroy.Load() != 1 {
		t.Fatalf("release=%d destroy=%d, want 1/1", counters.releaseTrue.Load(), counters.destroy.Load())
	}
}

func TestCommitLockRejectedByFragmentCreation(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	schema := testSchema()
	lock := newCommitLock(t, &commitLockCounters{})
	uri := filepath.Join(t.TempDir(), "data.lance")
	_, err := lance.CreateFragment(ctx, uri, 1, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})), lance.WithWriteCommitLock(lock))
	if !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CreateFragment with commit lock = %v, want ErrInvalidArgument", err)
	}
}

func TestCommitLockRejectedByNamespaceOperations(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	schema := testSchema()
	lock := newCommitLock(t, &commitLockCounters{})

	if _, err := lance.OpenFromNamespace(ctx, nil, []string{"table"}, lance.WithOpenCommitLock(lock)); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("OpenFromNamespace with commit lock = %v, want ErrInvalidArgument", err)
	}
	reader := makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true}))
	if _, err := lance.WriteToNamespace(ctx, nil, []string{"table"}, reader, lance.WithWriteCommitLock(lock)); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("WriteToNamespace with commit lock = %v, want ErrInvalidArgument", err)
	}

	uri := filepath.Join(t.TempDir(), "data.lance")
	fixture, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})))
	if err != nil {
		t.Fatalf("Create fixture: %v", err)
	}
	fixture.Close()
	tx, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{2}, []string{"b"}, []bool{true})), lance.WithWriteMode(lance.WriteModeAppend))
	if err != nil {
		t.Fatalf("WriteUncommitted fixture: %v", err)
	}
	defer tx.Close()
	if _, err := lance.CommitTransactionToNamespace(ctx, nil, []string{"table"}, uri, tx, lance.WithCommitLock(lock)); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CommitTransactionToNamespace with commit lock = %v, want ErrInvalidArgument", err)
	}
}

func TestCommitLockCloseWaitsForInflightWrite(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	entered := make(chan struct{})
	allow := make(chan struct{})
	var enteredOnce sync.Once
	var closeDone sync.WaitGroup
	lock, err := lance.NewCommitLock(lance.CommitLockCallbacks{
		Acquire: func(uint64) (lance.CommitLease, error) {
			enteredOnce.Do(func() { close(entered) })
			<-allow
			return "in-flight", nil
		},
		Release: func(lance.CommitLease, bool) error { return nil },
		Destroy: func(lance.CommitLease) {},
	})
	if err != nil {
		t.Fatalf("NewCommitLock: %v", err)
	}
	defer lock.Close()

	reader := makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true}))
	wrote := make(chan *lance.Dataset, 1)
	writeErr := make(chan error, 1)
	go func() {
		ds, err := lance.Create(ctx, uri, reader, lance.WithWriteCommitLock(lock))
		if err != nil {
			writeErr <- err
			return
		}
		wrote <- ds
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("write did not enter acquire")
	}

	closeDone.Add(1)
	go func() {
		defer closeDone.Done()
		if err := lock.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	select {
	case <-closeDone:
		close(allow)
		t.Fatal("Close returned while write callback was in flight")
	case <-time.After(100 * time.Millisecond):
	}
	close(allow)

	select {
	case err := <-writeErr:
		t.Fatalf("write failed: %v", err)
	case ds := <-wrote:
		requireMutationRows(t, ds, []int64{1}, []string{"a"})
		ds.Close()
	case <-time.After(10 * time.Second):
		t.Fatal("write did not finish")
	}
	select {
	case <-closeDone:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return after write finished")
	}
}
