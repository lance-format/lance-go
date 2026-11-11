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

// ReadOptions controls Arrow batch production. Zero values select 1024 rows
// per batch and 16 batches of readahead, matching the native Python reader.
type ReadOptions struct {
	BatchSize      uint32
	BatchReadahead uint32
}

func (o ReadOptions) withDefaults() ReadOptions {
	// The C API forwards these values directly. In particular, zero readahead
	// creates a stream that never polls its input instead of selecting a default.
	if o.BatchSize == 0 {
		o.BatchSize = 1024
	}
	if o.BatchReadahead == 0 {
		o.BatchReadahead = 16
	}
	return o
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
