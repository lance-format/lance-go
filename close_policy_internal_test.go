package lance

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lance-format/lance-go/internal/ffi"
)

// closeTeardown replaces one handle's native close function so tests can pause
// teardown, observe concurrent Close callers, and count releases.
type closeTeardown struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once
	closes      atomic.Int32
}

func newCloseTeardown() *closeTeardown {
	return &closeTeardown{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

// closeFn blocks until unblock is called, then returns.
func (c *closeTeardown) closeFn() func() {
	return func() {
		c.closes.Add(1)
		c.enteredOnce.Do(func() { close(c.entered) })
		<-c.release
	}
}

func (c *closeTeardown) unblock() {
	c.releaseOnce.Do(func() { close(c.release) })
}

// assertCloseWaits verifies that a second Close call cannot return while the
// first call is still running native teardown, and that teardown runs once.
func assertCloseWaits(t *testing.T, closeFn func() error, teardown *closeTeardown) {
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

func TestResourceHandleCloseWaitsForConcurrentCallers(t *testing.T) {
	tests := []struct {
		name   string
		attach func(*resource) func() error
	}{
		{"Dataset", func(res *resource) func() error {
			handle := &Dataset{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"BlobFile", func(res *resource) func() error {
			handle := &BlobFile{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"Fragment", func(res *resource) func() error {
			handle := &Fragment{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"NativeSchema", func(res *resource) func() error {
			handle := &NativeSchema{}
			handle.res = res
			return handle.Close
		}},
		{"DataFile", func(res *resource) func() error {
			handle := &DataFile{}
			handle.res = res
			return handle.Close
		}},
		{"Transaction", func(res *resource) func() error {
			handle := &Transaction{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"Operation", func(res *resource) func() error {
			handle := &Operation{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"UpdatedFragmentOffsets", func(res *resource) func() error {
			handle := &UpdatedFragmentOffsets{}
			handle.res = res
			return handle.Close
		}},
		{"UpdateColumnsResult", func(res *resource) func() error {
			handle := &UpdateColumnsResult{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"DataReplacementGroup", func(res *resource) func() error {
			handle := &DataReplacementGroup{}
			handle.res = res
			return handle.Close
		}},
		{"SegmentBuilder", func(res *resource) func() error {
			handle := &SegmentBuilder{uri: "file:///example"}
			handle.res = res
			return handle.Close
		}},
		{"IndexSegmentMetadata", func(res *resource) func() error {
			handle := &IndexSegmentMetadata{}
			handle.res = res
			return handle.Close
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teardown := newCloseTeardown()
			closeFn := tt.attach(newResource(teardown.closeFn()))
			assertCloseWaits(t, closeFn, teardown)
		})
	}
}

func TestHandleCloseNilZeroAndDuplicate(t *testing.T) {
	tests := []struct {
		name  string
		close func() error
	}{
		{"Dataset/nil", (*Dataset)(nil).Close},
		{"Dataset/zero", (&Dataset{}).Close},
		{"BlobFile/nil", (*BlobFile)(nil).Close},
		{"BlobFile/zero", (&BlobFile{}).Close},
		{"Fragment/nil", (*Fragment)(nil).Close},
		{"Fragment/zero", (&Fragment{}).Close},
		{"NativeSchema/nil", (*NativeSchema)(nil).Close},
		{"NativeSchema/zero", (&NativeSchema{}).Close},
		{"DataFile/nil", (*DataFile)(nil).Close},
		{"DataFile/zero", (&DataFile{}).Close},
		{"Transaction/nil", (*Transaction)(nil).Close},
		{"Transaction/zero", (&Transaction{}).Close},
		{"Operation/nil", (*Operation)(nil).Close},
		{"Operation/zero", (&Operation{}).Close},
		{"UpdatedFragmentOffsets/nil", (*UpdatedFragmentOffsets)(nil).Close},
		{"UpdatedFragmentOffsets/zero", (&UpdatedFragmentOffsets{}).Close},
		{"UpdateColumnsResult/nil", (*UpdateColumnsResult)(nil).Close},
		{"UpdateColumnsResult/zero", (&UpdateColumnsResult{}).Close},
		{"DataReplacementGroup/nil", (*DataReplacementGroup)(nil).Close},
		{"DataReplacementGroup/zero", (&DataReplacementGroup{}).Close},
		{"SegmentBuilder/nil", (*SegmentBuilder)(nil).Close},
		{"SegmentBuilder/zero", (&SegmentBuilder{}).Close},
		{"Scanner/nil", (*Scanner)(nil).Close},
		{"Scanner/zero", (&Scanner{}).Close},
		{"FTSQuery/nil", (*FTSQuery)(nil).Close},
		{"FTSQuery/zero", (&FTSQuery{}).Close},
		{"FTSPreparedQuery/nil", (*FTSPreparedQuery)(nil).Close},
		{"FTSPreparedQuery/zero", (&FTSPreparedQuery{}).Close},
		{"IndexSegmentMetadata/nil", (*IndexSegmentMetadata)(nil).Close},
		{"IndexSegmentMetadata/zero", (&IndexSegmentMetadata{}).Close},
		{"BlobDescriptorBuilder/nil", (*BlobDescriptorBuilder)(nil).Close},
		{"BlobDescriptorBuilder/zero", (&BlobDescriptorBuilder{}).Close},
		{"Session/nil", (*Session)(nil).Close},
		{"Session/zero", (&Session{}).Close},
		{"StorageOptionsAccessor/nil", func() error { (*StorageOptionsAccessor)(nil).Close(); return nil }},
		{"StorageOptionsAccessor/zero", func() error { (&StorageOptionsAccessor{}).Close(); return nil }},
		{"CommitLock/nil", (*CommitLock)(nil).Close},
		{"CommitLock/zero", (&CommitLock{}).Close},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 2; i++ {
				if err := tt.close(); err != nil {
					t.Fatalf("Close call %d: %v", i+1, err)
				}
			}
		})
	}
}

// TestSegmentBuilderCloseNilAndZeroValueReturnsNil pins the v0.3.0 contract
// change: closing an absent builder is a no-op instead of ErrInvalidArgument.
func TestSegmentBuilderCloseNilAndZeroValueReturnsNil(t *testing.T) {
	if err := (*SegmentBuilder)(nil).Close(); err != nil {
		t.Fatalf("nil SegmentBuilder.Close = %v, want nil", err)
	}
	if err := (&SegmentBuilder{}).Close(); err != nil {
		t.Fatalf("zero-value SegmentBuilder.Close = %v, want nil", err)
	}
}

func TestIndexSegmentMetadataCallsAfterCloseReportClosed(t *testing.T) {
	metadata := newIndexSegmentMetadata(&ffi.SegmentMetadata{})
	if err := metadata.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := metadata.UUID(); !errors.Is(err, ErrClosed) {
		t.Fatalf("UUID after Close = %v, want ErrClosed", err)
	}
	if _, err := metadata.Clone(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Clone after Close = %v, want ErrClosed", err)
	}
}
