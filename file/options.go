package file

import "context"

// CacheMode selects the reader's page-cache behavior.
type CacheMode int32

const (
	// CacheNone disables the reader cache.
	CacheNone CacheMode = 0
	// CacheSession uses the cache owned by the session the reader was opened
	// from. It is invalid for standalone readers.
	CacheSession CacheMode = 1
)

// SchedulerLiteMode selects the native scheduler implementation.
type SchedulerLiteMode int32

const (
	SchedulerAuto     SchedulerLiteMode = 0
	SchedulerStandard SchedulerLiteMode = 1
	SchedulerLite     SchedulerLiteMode = 2
)

// SchedulerConfig configures the native scan scheduler.
type SchedulerConfig struct {
	IOBufferSizeBytes uint64
	LiteMode          SchedulerLiteMode
}

// ReadOptions controls Arrow batch production. Zero values use native
// defaults for the batch size and readahead.
type ReadOptions struct {
	BatchSize      uint32
	BatchReadahead uint32
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
