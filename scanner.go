package lance

import (
	"context"
	"errors"
	"iter"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
)

// ScanMetric is one native scanner metric.
type ScanMetric struct {
	Name  string
	Kind  int32
	Value uint64
}

// ScanStatistics is a snapshot of native scan statistics.
type ScanStatistics struct {
	IOPS                  uint64
	Requests              uint64
	BytesRead             uint64
	IndicesLoaded         uint64
	IndexPartitionsLoaded uint64
	IndexComparisons      uint64
	Metrics               []ScanMetric
}

// Scanner is a single-use native scan. Records and ToArrowReader may each be
// called at most once, and not both. Close is safe concurrently with scanning.
type Scanner struct {
	native        *ffi.Scanner
	uri           string
	mu            sync.Mutex
	used          bool
	statsCallback func(ScanStatistics)
	statsOnce     sync.Once
}

// Scan creates a scanner for the dataset.
func (d *Dataset) Scan(opts ...ScanOption) (*Scanner, error) {
	if err := d.acquire("scan"); err != nil {
		return nil, err
	}
	defer d.res.release()

	config := scanConfig{}
	for _, option := range opts {
		if option == nil {
			return nil, invalidError("scan", d.uri, "nil ScanOption")
		}
		if err := option(&config); err != nil {
			return nil, invalidError("scan", d.uri, "%v", err)
		}
	}
	if len(config.columns) > 0 && len(config.projection) > 0 {
		return nil, invalidError("scan", d.uri, "columns and expression projection cannot both be set")
	}

	projection := make([]ffi.ProjectionColumn, len(config.projection))
	for i, column := range config.projection {
		projection[i] = ffi.ProjectionColumn{OutputName: column.name, Expression: column.expression}
	}
	orderBy := make([]ffi.ColumnOrdering, len(config.orderBy))
	for i, ordering := range config.orderBy {
		orderBy[i] = ffi.ColumnOrdering{
			Column:    ordering.column,
			Direction: int32(ordering.direction),
			NullOrder: int32(ordering.nullOrder),
		}
	}

	native, err := ffi.NewScanner(d.native, ffi.ScannerOptions{
		Columns:              config.columns,
		Filter:               config.filter,
		Limit:                config.limit,
		Offset:               config.offset,
		BatchSize:            config.batchSize,
		BatchSizeBytes:       config.batchSizeBytes,
		Projection:           projection,
		OrderBy:              orderBy,
		RowID:                config.rowID,
		RowAddress:           config.rowAddress,
		SubstraitFilter:      config.substraitFilter,
		SubstraitAggregate:   config.substraitAggregate,
		AdditionalSQLFilters: config.additionalSQLFilters,
	})
	if err != nil {
		return nil, wrapError("scan", d.uri, err)
	}
	return &Scanner{
		native:        native,
		uri:           d.uri,
		statsCallback: config.statistics,
	}, nil
}

// Schema returns the scanner output schema.
func (s *Scanner) Schema() *arrow.Schema {
	return s.native.Schema()
}

// Statistics returns a snapshot of native scan statistics.
func (s *Scanner) Statistics() ScanStatistics {
	stats := s.native.Statistics()
	result := ScanStatistics{
		IOPS:                  stats.IOPS,
		Requests:              stats.Requests,
		BytesRead:             stats.BytesRead,
		IndicesLoaded:         stats.IndicesLoaded,
		IndexPartitionsLoaded: stats.IndexPartitionsLoaded,
		IndexComparisons:      stats.IndexComparisons,
		Metrics:               make([]ScanMetric, len(stats.Metrics)),
	}
	for i, metric := range stats.Metrics {
		result.Metrics[i] = ScanMetric{Name: metric.Name, Kind: metric.Kind, Value: metric.Value}
	}
	return result
}

// Close releases the scanner. It is idempotent.
func (s *Scanner) Close() error {
	return s.native.Close()
}

// Records returns an iterator over scanned batches. Each yielded batch is
// borrowed and is released when iteration advances; call Retain to keep it.
// Breaking out of the loop closes the scanner.
func (s *Scanner) Records(ctx context.Context) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		if err := s.begin(ctx); err != nil {
			_ = s.Close()
			yield(nil, err)
			return
		}
		defer func() { _ = s.Close() }()

		for {
			batch, done, err := s.native.Next(ctx)
			if err != nil {
				yield(nil, s.scanError(err))
				return
			}
			if done {
				s.notifyStatistics()
				return
			}

			keep := func() (keep bool) {
				defer batch.Release()
				return yield(batch, nil)
			}()
			if !keep {
				return
			}
		}
	}
}

// ToArrowReader returns an Arrow RecordReader over the scan. The reader owns
// the scanner and closes it when Release reaches zero.
func (s *Scanner) ToArrowReader(ctx context.Context) (array.RecordReader, error) {
	if err := s.begin(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	reader := &scannerReader{
		scanner: s,
		ctx:     ctx,
		schema:  s.Schema(),
	}
	reader.refs.Store(1)
	return reader, nil
}

func (s *Scanner) begin(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used {
		return invalidError("scan", s.uri, "scanner is single-use")
	}
	s.used = true
	return nil
}

func (s *Scanner) notifyStatistics() {
	callback := s.statsCallback
	if callback == nil {
		return
	}
	s.statsOnce.Do(func() {
		callback(s.Statistics())
	})
}

func (s *Scanner) scanError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return wrapError("scan", s.uri, err)
}

type scannerReader struct {
	scanner *Scanner
	ctx     context.Context
	schema  *arrow.Schema
	refs    atomic.Int64

	mu      sync.Mutex
	current arrow.RecordBatch
	err     error
	done    bool
}

func (r *scannerReader) Retain() {
	r.refs.Add(1)
}

func (r *scannerReader) Release() {
	if r.refs.Add(-1) != 0 {
		return
	}
	r.mu.Lock()
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	r.mu.Unlock()
	_ = r.scanner.Close()
}

func (r *scannerReader) Schema() *arrow.Schema {
	return r.schema
}

func (r *scannerReader) Next() bool {
	if r.refs.Load() <= 0 {
		return false
	}
	r.mu.Lock()
	if r.done || r.err != nil {
		r.mu.Unlock()
		return false
	}
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}

	batch, done, err := r.scanner.native.Next(r.ctx)
	if err != nil {
		r.err = r.scanner.scanError(err)
		r.done = true
		r.mu.Unlock()
		return false
	}
	if done {
		r.done = true
		r.mu.Unlock()
		r.scanner.notifyStatistics()
		return false
	}
	r.current = batch
	r.mu.Unlock()
	return true
}

func (r *scannerReader) RecordBatch() arrow.RecordBatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

func (r *scannerReader) Record() arrow.Record {
	return r.RecordBatch()
}

func (r *scannerReader) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

var _ array.RecordReader = (*scannerReader)(nil)
