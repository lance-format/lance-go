package lance

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestCommitTransactionBatchNilReleasesAcquiredTransactions(t *testing.T) {
	var closes atomic.Int32
	acquired := &Transaction{
		res: newResource(func() { closes.Add(1) }),
	}

	_, _, err := CommitTransactionBatch(context.Background(), "memory://dataset", []*Transaction{acquired, nil})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("CommitTransactionBatch error = %v, want ErrInvalidArgument", err)
	}
	requireResourceCloseCompletes(t, acquired.res, acquired.Close, &closes, "transaction")
}

func TestNewDataReplacementOperationNilReleasesAcquiredGroups(t *testing.T) {
	var closes atomic.Int32
	acquired := &DataReplacementGroup{
		res: newResource(func() { closes.Add(1) }),
	}

	_, err := NewDataReplacementOperation(acquired, nil)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("NewDataReplacementOperation error = %v, want ErrInvalidArgument", err)
	}
	requireResourceCloseCompletes(t, acquired.res, acquired.Close, &closes, "data replacement group")
}

func requireResourceCloseCompletes(t *testing.T, res *resource, closeFn func() error, closes *atomic.Int32, kind string) {
	t.Helper()
	waiting := observeResourceCloseWait(res)
	done := make(chan struct{})
	go func() {
		_ = closeFn()
		close(done)
	}()
	select {
	case <-done:
	case <-waiting:
		res.release()
		<-done
		t.Fatalf("earlier %s Close blocked on an acquisition leaked by the invalid argument path", kind)
	}
	if got := closes.Load(); got != 1 {
		t.Fatalf("%s close hook ran %d times, want 1", kind, got)
	}
}
