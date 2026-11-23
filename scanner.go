package lance

import (
	"context"
	"errors"
	"iter"
	"runtime"
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
	if err := config.validateVector(); err != nil {
		return nil, invalidError("scan", d.uri, "%v", err)
	}

	native, err := ffi.NewScanner(d.native, nativeScannerOptions(config), func(native *ffi.Scanner) error {
		return applyFTSScan(native, &config, "scan", d.uri)
	})
	// Native query setters copy their input before returning.
	runtime.KeepAlive(&config)
	if err != nil {
		var lanceErr *Error
		if errors.As(err, &lanceErr) {
			return nil, err
		}
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

// ExplainPlan returns the native query plan for this scanner without starting
// the scan. verbose selects the native DataFusion tree formatting. The scanner
// remains usable for iteration or count diagnostics afterward.
func (s *Scanner) ExplainPlan(ctx context.Context, verbose bool) (string, error) {
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	plan, err := s.native.ExplainPlan(verbose)
	if err != nil {
		return "", wrapError("explain plan", s.uri, err)
	}
	return plan, nil
}

// AnalyzePlan executes the native query plan and returns its runtime analysis
// without starting or advancing iteration. The scanner remains usable
// afterward.
func (s *Scanner) AnalyzePlan(ctx context.Context) (string, error) {
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	plan, err := s.native.AnalyzePlan()
	if err != nil {
		return "", wrapError("analyze plan", s.uri, err)
	}
	return plan, nil
}

// AnalyzeCountPlan returns the runtime analysis of the native COUNT(*) plan
// used by CountRows. It obeys the same count-plan restrictions and does not
// start or advance iteration.
func (s *Scanner) AnalyzeCountPlan(ctx context.Context) (string, error) {
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	plan, err := s.native.AnalyzeCountPlan()
	if err != nil {
		return "", wrapError("analyze count plan", s.uri, err)
	}
	return plan, nil
}

// CountRows counts rows matched by the configured scanner plan. It applies the
// native COUNT(*) aggregate instead of consuming scan batches; limit, offset,
// ordering and MinHash queries are rejected by native planning. Use
// Dataset.CountRows or CountRowsFiltered for dataset-wide counts.
func (s *Scanner) CountRows(ctx context.Context) (uint64, error) {
	if err := checkContext(ctx); err != nil {
		return 0, err
	}
	rows, err := s.native.CountRows()
	if err != nil {
		return 0, wrapError("count rows", s.uri, err)
	}
	return rows, nil
}

// Close releases the scanner. It is idempotent, waits for an in-flight poll to
// leave native code, and is a no-op for a nil or zero-value scanner.
func (s *Scanner) Close() error {
	if s == nil || s.native == nil {
		return nil
	}
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
// the scanner and closes it when Release reaches zero. Release is safe
// concurrently with Next: it closes the scanner, which wakes a blocked native
// poll, and discards a batch that arrives after release.
func (s *Scanner) ToArrowReader(ctx context.Context) (array.RecordReader, error) {
	if err := s.begin(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return newScannerReader(s, s.native, ctx, s.Schema()), nil
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

// scannerSource is the native scanner a scannerReader polls. *ffi.Scanner
// implements it. The interface keeps the reader's cancellation behavior
// testable without a native library.
type scannerSource interface {
	Next(ctx context.Context) (arrow.RecordBatch, bool, error)
	Close() error
}

// scannerReader adapts a Scanner to array.RecordReader.
//
// Next is serialized by nextMu rather than by mu so Release can close the
// scanner while a native poll is in flight. Closing the scanner wakes that
// poll through the native close channel; the reader then discards whatever the
// poll returns because released is set. Holding mu across the poll would let a
// blocked Next prevent Release from ever reaching Close.
type scannerReader struct {
	scanner *Scanner
	source  scannerSource
	ctx     context.Context
	schema  *arrow.Schema
	refs    atomic.Int64

	nextMu sync.Mutex

	mu       sync.Mutex
	current  arrow.RecordBatch
	err      error
	done     bool
	released bool
}

func newScannerReader(scanner *Scanner, source scannerSource, ctx context.Context, schema *arrow.Schema) *scannerReader {
	reader := &scannerReader{
		scanner: scanner,
		source:  source,
		ctx:     ctx,
		schema:  schema,
	}
	reader.refs.Store(1)
	return reader
}

func (r *scannerReader) Retain() {
	r.refs.Add(1)
}

func (r *scannerReader) Release() {
	if r.refs.Add(-1) != 0 {
		return
	}
	r.mu.Lock()
	r.released = true
	current := r.current
	r.current = nil
	r.mu.Unlock()
	if current != nil {
		current.Release()
	}
	_ = r.source.Close()
}

func (r *scannerReader) Schema() *arrow.Schema {
	return r.schema
}

func (r *scannerReader) Next() bool {
	if r.refs.Load() <= 0 {
		return false
	}
	r.nextMu.Lock()
	batch, done, err := r.pollNext()
	r.nextMu.Unlock()

	if err != nil {
		return false
	}
	if done {
		// Statistics callbacks run here, after end-of-stream state and both
		// reader locks are released, so a callback may use this reader again.
		r.scanner.notifyStatistics()
		return false
	}
	return batch != nil
}

// pollNext performs one native poll. The caller owns nextMu. This method
// acquires mu only for state transitions and never holds it across the native
// call, so Release can close the scanner and wake a blocked poll. It reports
// done instead of notifying statistics so the caller can run user callbacks
// with nextMu released as well as mu.
func (r *scannerReader) pollNext() (batch arrow.RecordBatch, done bool, err error) {
	r.mu.Lock()
	if r.released || r.done || r.err != nil {
		r.mu.Unlock()
		return nil, false, nil
	}
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	r.mu.Unlock()

	batch, done, err = r.source.Next(r.ctx)

	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.err = r.scanner.scanError(err)
		r.done = true
		return nil, false, r.err
	}
	if r.released {
		if batch != nil {
			batch.Release()
		}
		return nil, false, nil
	}
	if done {
		r.done = true
		return nil, true, nil
	}
	r.current = batch
	return batch, false, nil
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

func nativeScannerOptions(config scanConfig) ffi.ScannerOptions {
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
	var blobHandling *int32
	if config.blobHandling != nil {
		value := int32(*config.blobHandling)
		blobHandling = &value
	}
	var materializationStyle *int32
	if config.materializationStyle != nil {
		value := int32(*config.materializationStyle)
		materializationStyle = &value
	}
	var scalarIndexSegment []byte
	if config.scalarIndexSegment != nil {
		scalarIndexSegment = append([]byte(nil), config.scalarIndexSegment[:]...)
	}
	return ffi.ScannerOptions{
		Columns:              config.columns,
		Filter:               config.filter,
		Limit:                config.limit,
		Offset:               config.offset,
		BatchSize:            config.batchSize,
		BatchSizeBytes:       config.batchSizeBytes,
		IOBufferSize:         config.ioBufferSize,
		BatchReadahead:       config.batchReadahead,
		FragmentReadahead:    config.fragmentReadahead,
		TargetParallelism:    config.targetParallelism,
		ScanInOrder:          config.scanInOrder,
		UseScalarIndex:       config.useScalarIndex,
		StrictBatchSize:      config.strictBatchSize,
		UseStats:             config.useStats,
		IncludeDeletedRows:   config.includeDeletedRows,
		BlobHandling:         blobHandling,
		MaterializationStyle: materializationStyle,
		MaterializationEarly: config.materializationEarly,
		FragmentIDs:          config.fragmentIDs,
		FragmentIDsSet:       config.fragmentIDsSet,
		DisableScoring:       config.disableScoring,
		ScalarIndexSegment:   scalarIndexSegment,
		Projection:           projection,
		OrderBy:              orderBy,
		RowID:                config.rowID,
		RowAddress:           config.rowAddress,
		SubstraitFilter:      config.substraitFilter,
		SubstraitAggregate:   config.substraitAggregate,
		AdditionalSQLFilters: config.additionalSQLFilters,
		Vector:               config.ffiVectorOptions(),
	}
}
