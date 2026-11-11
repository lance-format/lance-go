package ffi

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/cdata"
	"github.com/lance-format/lance-go/internal/handle"
)

const (
	pollReady    = 0
	pollPending  = 1
	pollFinished = 2
	pollError    = -1
)

// ProjectionColumn selects an output column by SQL expression.
type ProjectionColumn struct {
	OutputName string
	Expression string
}

// ColumnOrdering is one native order-by key.
type ColumnOrdering struct {
	Column    string
	Direction int32
	NullOrder int32
}

// ScannerOptions configures a native scanner before the scan starts.
type ScannerOptions struct {
	Columns              []string
	Filter               *string
	Limit                *int64
	Offset               *int64
	BatchSize            *int64
	BatchSizeBytes       *uint64
	IOBufferSize         *uint64
	BatchReadahead       *uint64
	FragmentReadahead    *uint64
	TargetParallelism    *uint64
	ScanInOrder          *bool
	UseScalarIndex       *bool
	StrictBatchSize      *bool
	UseStats             *bool
	IncludeDeletedRows   *bool
	BlobHandling         *int32
	MaterializationStyle *int32
	MaterializationEarly []string
	FragmentIDs          []uint64
	FragmentIDsSet       bool
	DisableScoring       bool
	ScalarIndexSegment   []byte
	Projection           []ProjectionColumn
	OrderBy              []ColumnOrdering
	RowID                *bool
	RowAddress           *bool
	SubstraitFilter      []byte
	SubstraitAggregate   []byte
	AdditionalSQLFilters []string
	Vector               VectorOptions
}

// NearestMode identifies the shape of a nearest-query input.
type NearestMode int32

const (
	// NearestSingle is one query vector of Count elements.
	NearestSingle NearestMode = 0
	// NearestBatch is Count row-major query vectors of Dimension elements.
	NearestBatch NearestMode = 1
	// NearestMultiVector is Count row-major subvectors of Dimension elements.
	NearestMultiVector NearestMode = 2
)

// NearestQuery describes one raw nearest-query input. Data points to
// Count*Dimension elements for batch and multi-vector queries, or to Count
// elements for a single query. The native setter copies the data before it
// returns, but the caller must keep Data alive until then.
type NearestQuery struct {
	Column    string
	Data      unsafe.Pointer
	DataType  int32
	K         uint32
	Mode      NearestMode
	Dimension int
	Count     int
}

// VectorOptions configures vector search and row masks. A nil pointer leaves
// the corresponding native default in place. Row masks use stable _rowid
// values; a set-but-empty mask therefore selects or excludes no rows.
type VectorOptions struct {
	Nearest          *NearestQuery
	Metric           *int32
	NProbes          *uint32
	MinNProbes       *uint32
	MaxNProbes       *uint32
	EF               *uint32
	RefineFactor     *uint32
	SearchEffort     *float64
	ApproxMode       *int32
	QueryParallelism *int32
	UseIndex         *bool
	FastSearch       *bool
	Prefilter        *bool
	LowerDistance    *float32
	UpperDistance    *float32
	RowAllowlist     []uint64
	RowAllowlistSet  bool
	RowBlocklist     []uint64
	RowBlocklistSet  bool
}

// ScanMetric is one native scanner metric.
type ScanMetric struct {
	Name  string
	Kind  int32
	Value uint64
}

// ScanStatistics is a snapshot of native scanner statistics.
type ScanStatistics struct {
	IOPS                  uint64
	Requests              uint64
	BytesRead             uint64
	IndicesLoaded         uint64
	IndexPartitionsLoaded uint64
	IndexComparisons      uint64
	Metrics               []ScanMetric
}

type scanMetric struct {
	Name    *byte
	NameLen uint64
	Kind    int32
	Value   uint64
}

type scanStatistics struct {
	IOPS                  uint64
	Requests              uint64
	BytesRead             uint64
	IndicesLoaded         uint64
	IndexPartitionsLoaded uint64
	IndexComparisons      uint64
	Metrics               *scanMetric
	MetricsLen            uint64
}

type statsState struct {
	mu    sync.Mutex
	value ScanStatistics
}

type projectionColumn struct {
	OutputName uintptr
	Expression uintptr
}

type columnOrdering struct {
	ColumnName uintptr
	Direction  int32
	NullOrder  int32
}

type pollWaiter struct {
	ready chan struct{}
}

func newPollWaiter() *pollWaiter {
	return &pollWaiter{ready: make(chan struct{}, 1)}
}

func (w *pollWaiter) wake() {
	select {
	case w.ready <- struct{}{}:
	default:
	}
}

func (w *pollWaiter) drain() {
	select {
	case <-w.ready:
	default:
	}
}

var pollWaker = sync.OnceValue(func() uintptr {
	return purego.NewCallback(func(ctx uintptr) (ret uintptr) {
		defer func() { _ = recover() }()
		value, ok := handle.Get(uint64(ctx))
		if !ok {
			return
		}
		waiter, ok := value.(*pollWaiter)
		if !ok {
			return
		}
		waiter.wake()
		return
	})
})

var statisticsCallback = sync.OnceValue(func() uintptr {
	return purego.NewCallback(func(ctx uintptr, raw *scanStatistics) (ret uintptr) {
		dispatchVoid2(ctx, unsafe.Pointer(raw))
		return
	})
})

func (s *statsState) dispatchVoid2(payload unsafe.Pointer) {
	s.store((*scanStatistics)(payload))
}

func (s *statsState) store(raw *scanStatistics) {
	if raw == nil {
		return
	}
	value := ScanStatistics{
		IOPS:                  raw.IOPS,
		Requests:              raw.Requests,
		BytesRead:             raw.BytesRead,
		IndicesLoaded:         raw.IndicesLoaded,
		IndexPartitionsLoaded: raw.IndexPartitionsLoaded,
		IndexComparisons:      raw.IndexComparisons,
	}
	if raw.Metrics != nil && raw.MetricsLen > 0 {
		value.Metrics = make([]ScanMetric, 0, raw.MetricsLen)
		metrics := unsafe.Slice(raw.Metrics, int(raw.MetricsLen))
		for _, metric := range metrics {
			name := ""
			if metric.Name != nil && metric.NameLen > 0 {
				name = string(unsafe.Slice(metric.Name, int(metric.NameLen)))
			}
			value.Metrics = append(value.Metrics, ScanMetric{
				Name:  name,
				Kind:  metric.Kind,
				Value: metric.Value,
			})
		}
	}

	s.mu.Lock()
	s.value = value
	s.mu.Unlock()
}

func (s *statsState) snapshot() ScanStatistics {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.value
	value.Metrics = append([]ScanMetric(nil), s.value.Metrics...)
	return value
}

// Scanner is a single-use native scan. Next may be called by one goroutine at
// a time; Close is safe concurrently with Next.
type Scanner struct {
	b      *bindings
	handle atomic.Uintptr

	mu        sync.Mutex
	cond      *sync.Cond
	closeCh   chan struct{}
	closeDone chan struct{}
	closing   bool
	polling   bool
	finished  bool
	aborted   bool

	waiter       *pollWaiter
	waiterHandle uint64
	stats        *statsState
	statsHandle  uint64
	schema       *arrow.Schema
}

// NewScanner creates and configures a native scanner for ds.
// initialize attaches search configuration before the output schema is resolved.
// It runs synchronously during construction and must not publish the scanner.
func NewScanner(ds *Dataset, opts ScannerOptions, initialize func(*Scanner) error) (*Scanner, error) {
	if ds == nil {
		return nil, fmt.Errorf("ffi: dataset must not be nil")
	}
	handleValue := ds.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	b := ds.b

	columnList, err := newCStringList(opts.Columns)
	if err != nil {
		return nil, err
	}
	var filterList *cStringList
	var filterPtr uintptr
	if opts.Filter != nil {
		filterList, err = newCStringList([]string{*opts.Filter})
		if err != nil {
			return nil, err
		}
		filterPtr = filterList.ptrs[0]
	}

	runtime.LockOSThread()
	scannerHandle := b.scannerNew(handleValue, columnList.ptrs, filterPtr)
	columnList.keepAlive()
	if filterList != nil {
		filterList.keepAlive()
	}
	if scannerHandle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_scanner_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()

	return newScanner(b, scannerHandle, opts, initialize)
}

// newScanner takes ownership of a native scanner and configures search before
// resolving its schema. Every failed construction closes the native handle.
func newScanner(b *bindings, scannerHandle uintptr, opts ScannerOptions, initialize func(*Scanner) error) (*Scanner, error) {
	s := &Scanner{
		b:         b,
		closeCh:   make(chan struct{}),
		closeDone: make(chan struct{}),
		waiter:    newPollWaiter(),
		stats:     &statsState{},
	}
	s.handle.Store(scannerHandle)
	s.cond = sync.NewCond(&s.mu)
	s.waiterHandle = handle.New(s.waiter)
	s.statsHandle = handle.New(s.stats)

	if err := s.configure(opts); err != nil {
		s.abort()
		return nil, err
	}
	if err := s.registerStatistics(); err != nil {
		s.abort()
		return nil, err
	}
	if initialize != nil {
		if err := initialize(s); err != nil {
			s.abort()
			return nil, err
		}
	}
	schema, err := s.loadSchema()
	if err != nil {
		s.abort()
		return nil, err
	}
	s.schema = schema
	return s, nil
}

func (s *Scanner) configure(opts ScannerOptions) error {
	defer runtime.KeepAlive(opts)
	if opts.Limit != nil {
		if *opts.Limit < 0 {
			return fmt.Errorf("ffi: scan limit must not be negative")
		}
		if err := s.setter("lance_scanner_set_limit", func(handle uintptr) int32 {
			return s.b.scannerSetLimit(handle, *opts.Limit)
		}); err != nil {
			return err
		}
	}
	if opts.Offset != nil {
		if *opts.Offset < 0 {
			return fmt.Errorf("ffi: scan offset must not be negative")
		}
		if err := s.setter("lance_scanner_set_offset", func(handle uintptr) int32 {
			return s.b.scannerSetOffset(handle, *opts.Offset)
		}); err != nil {
			return err
		}
	}
	if opts.BatchSize != nil {
		if *opts.BatchSize <= 0 {
			return fmt.Errorf("ffi: scan batch size must be positive")
		}
		if err := s.setter("lance_scanner_set_batch_size", func(handle uintptr) int32 {
			return s.b.scannerSetBatchSize(handle, *opts.BatchSize)
		}); err != nil {
			return err
		}
	}
	if opts.BatchSizeBytes != nil {
		if *opts.BatchSizeBytes == 0 {
			return fmt.Errorf("ffi: scan batch size in bytes must be positive")
		}
		if err := s.setter("lance_scanner_set_batch_size_bytes", func(handle uintptr) int32 {
			return s.b.scannerSetBatchSizeBytes(handle, *opts.BatchSizeBytes)
		}); err != nil {
			return err
		}
	}
	if opts.StrictBatchSize != nil {
		if err := s.setter("lance_scanner_set_strict_batch_size", func(handle uintptr) int32 {
			return s.b.scannerSetStrictBatchSize(handle, *opts.StrictBatchSize)
		}); err != nil {
			return err
		}
	}
	if len(opts.Projection) > 0 {
		if err := s.setProjection(opts.Projection); err != nil {
			return err
		}
	}
	if len(opts.OrderBy) > 0 {
		if err := s.setOrderBy(opts.OrderBy); err != nil {
			return err
		}
	}
	if opts.RowID != nil {
		if err := s.setter("lance_scanner_with_row_id", func(handle uintptr) int32 {
			return s.b.scannerWithRowID(handle, *opts.RowID)
		}); err != nil {
			return err
		}
	}
	if opts.RowAddress != nil {
		if err := s.setter("lance_scanner_with_row_address", func(handle uintptr) int32 {
			return s.b.scannerWithRowAddress(handle, *opts.RowAddress)
		}); err != nil {
			return err
		}
	}
	if opts.IOBufferSize != nil {
		if *opts.IOBufferSize == 0 || *opts.IOBufferSize > math.MaxInt64 {
			return fmt.Errorf("ffi: scan I/O buffer size must be in [1, %d]", int64(math.MaxInt64))
		}
		if err := s.setter("lance_scanner_set_io_buffer_size", func(handle uintptr) int32 {
			return s.b.scannerSetIOBufferSize(handle, *opts.IOBufferSize)
		}); err != nil {
			return err
		}
	}
	if opts.BatchReadahead != nil {
		if *opts.BatchReadahead == 0 {
			return fmt.Errorf("ffi: scan batch readahead must be positive")
		}
		if err := s.setter("lance_scanner_set_batch_readahead", func(handle uintptr) int32 {
			return s.b.scannerSetBatchReadahead(handle, *opts.BatchReadahead)
		}); err != nil {
			return err
		}
	}
	if opts.FragmentReadahead != nil {
		if *opts.FragmentReadahead == 0 {
			return fmt.Errorf("ffi: scan fragment readahead must be positive")
		}
		if err := s.setter("lance_scanner_set_fragment_readahead", func(handle uintptr) int32 {
			return s.b.scannerSetFragmentReadahead(handle, *opts.FragmentReadahead)
		}); err != nil {
			return err
		}
	}
	if opts.TargetParallelism != nil {
		if *opts.TargetParallelism == 0 {
			return fmt.Errorf("ffi: scan target parallelism must be positive")
		}
		if err := s.setter("lance_scanner_set_target_parallelism", func(handle uintptr) int32 {
			return s.b.scannerSetTargetParallelism(handle, *opts.TargetParallelism)
		}); err != nil {
			return err
		}
	}
	if opts.ScanInOrder != nil {
		if err := s.setter("lance_scanner_set_scan_in_order", func(handle uintptr) int32 {
			return s.b.scannerSetScanInOrder(handle, *opts.ScanInOrder)
		}); err != nil {
			return err
		}
	}
	if opts.UseScalarIndex != nil {
		if err := s.setter("lance_scanner_set_use_scalar_index", func(handle uintptr) int32 {
			return s.b.scannerSetUseScalarIndex(handle, *opts.UseScalarIndex)
		}); err != nil {
			return err
		}
	}
	if opts.UseStats != nil {
		if err := s.setter("lance_scanner_set_use_stats", func(handle uintptr) int32 {
			return s.b.scannerSetUseStats(handle, *opts.UseStats)
		}); err != nil {
			return err
		}
	}
	if opts.IncludeDeletedRows != nil {
		if err := s.setter("lance_scanner_set_include_deleted_rows", func(handle uintptr) int32 {
			return s.b.scannerSetIncludeDeletedRows(handle, *opts.IncludeDeletedRows)
		}); err != nil {
			return err
		}
	}
	if opts.BlobHandling != nil {
		switch *opts.BlobHandling {
		case 0, 1, 2:
		default:
			return fmt.Errorf("ffi: invalid blob handling %d", *opts.BlobHandling)
		}
		if err := s.setter("lance_scanner_set_blob_handling", func(handle uintptr) int32 {
			return s.b.scannerSetBlobHandling(handle, *opts.BlobHandling)
		}); err != nil {
			return err
		}
	}
	if opts.MaterializationStyle != nil {
		switch *opts.MaterializationStyle {
		case 0, 1, 2:
			if len(opts.MaterializationEarly) != 0 {
				return fmt.Errorf("ffi: materialization style %d does not accept early-except columns", *opts.MaterializationStyle)
			}
		case 3:
		default:
			return fmt.Errorf("ffi: invalid materialization style %d", *opts.MaterializationStyle)
		}
		list, err := newCStringList(opts.MaterializationEarly)
		if err != nil {
			return fmt.Errorf("ffi: materialization early-except columns: %w", err)
		}
		defer list.keepAlive()
		if err := s.setter("lance_scanner_set_materialization_style", func(handle uintptr) int32 {
			return s.b.scannerSetMaterializationStyle(handle, *opts.MaterializationStyle, list.ptrs, uint64(len(opts.MaterializationEarly)))
		}); err != nil {
			return err
		}
	}
	if opts.FragmentIDsSet {
		if err := s.setter("lance_scanner_set_fragment_ids", func(handle uintptr) int32 {
			return s.b.scannerSetFragmentIDs(handle, opts.FragmentIDs, uint64(len(opts.FragmentIDs)))
		}); err != nil {
			return err
		}
	}
	if opts.DisableScoring {
		if err := s.setter("lance_scanner_disable_scoring_autoprojection", func(handle uintptr) int32 {
			return s.b.scannerDisableScoringAutoprojection(handle)
		}); err != nil {
			return err
		}
	}
	if opts.ScalarIndexSegment != nil {
		if len(opts.ScalarIndexSegment) != 16 {
			return fmt.Errorf("ffi: scalar index segment must be 16 bytes")
		}
		if err := s.setter("lance_scanner_set_scalar_index_segment", func(handle uintptr) int32 {
			return s.b.scannerSetScalarIndexSegment(handle, opts.ScalarIndexSegment)
		}); err != nil {
			return err
		}
	}
	if opts.SubstraitFilter != nil {
		if len(opts.SubstraitFilter) == 0 {
			return fmt.Errorf("ffi: substrait filter must not be empty")
		}
		if err := s.setter("lance_scanner_set_substrait_filter", func(handle uintptr) int32 {
			return s.b.scannerSetSubstraitFilter(handle, opts.SubstraitFilter, uint64(len(opts.SubstraitFilter)))
		}); err != nil {
			return err
		}
	}
	if opts.SubstraitAggregate != nil {
		if err := s.setter("lance_scanner_set_substrait_aggregate", func(handle uintptr) int32 {
			return s.b.scannerSetSubstraitAggregate(handle, opts.SubstraitAggregate, uint64(len(opts.SubstraitAggregate)))
		}); err != nil {
			return err
		}
	}
	for i, filter := range opts.AdditionalSQLFilters {
		if err := validateCString(filter); err != nil {
			return fmt.Errorf("ffi: additional SQL filter %d: %w", i, err)
		}
		if err := s.setter("lance_scanner_additional_sql_filter", func(handle uintptr) int32 {
			return s.b.scannerAdditionalSQLFilter(handle, filter)
		}); err != nil {
			return err
		}
	}
	return s.configureVector(opts.Vector)
}

// configureVector applies vector options in an order that preserves the
// caller's intent. In particular, the distance range is applied after the
// nearest query so that replacing the nearest query cannot silently clear it.
func (s *Scanner) configureVector(opts VectorOptions) error {
	defer runtime.KeepAlive(opts)
	if opts.RowAllowlistSet {
		if err := s.setter("lance_scanner_set_row_addr_allowlist", func(handle uintptr) int32 {
			return s.b.scannerSetRowAddrAllowlist(handle, opts.RowAllowlist, uint64(len(opts.RowAllowlist)))
		}); err != nil {
			return err
		}
	}
	if opts.RowBlocklistSet {
		if err := s.setter("lance_scanner_set_row_addr_blocklist", func(handle uintptr) int32 {
			return s.b.scannerSetRowAddrBlocklist(handle, opts.RowBlocklist, uint64(len(opts.RowBlocklist)))
		}); err != nil {
			return err
		}
	}
	if opts.FastSearch != nil {
		if err := s.setter("lance_scanner_set_fast_search", func(handle uintptr) int32 {
			return s.b.scannerSetFastSearch(handle, *opts.FastSearch)
		}); err != nil {
			return err
		}
	}
	if opts.Metric != nil {
		if err := s.setter("lance_scanner_set_metric", func(handle uintptr) int32 {
			return s.b.scannerSetMetric(handle, *opts.Metric)
		}); err != nil {
			return err
		}
	}
	if opts.Nearest != nil {
		if err := s.setNearest(opts.Nearest); err != nil {
			return err
		}
	}
	if opts.LowerDistance != nil || opts.UpperDistance != nil {
		if err := s.setter("lance_scanner_set_distance_range", func(handle uintptr) int32 {
			return s.b.scannerSetDistanceRange(handle, opts.LowerDistance, opts.UpperDistance)
		}); err != nil {
			return err
		}
	}
	if opts.NProbes != nil {
		if err := s.setter("lance_scanner_set_nprobes", func(handle uintptr) int32 {
			return s.b.scannerSetNProbes(handle, *opts.NProbes)
		}); err != nil {
			return err
		}
	}
	if opts.MinNProbes != nil {
		if err := s.setter("lance_scanner_set_minimum_nprobes", func(handle uintptr) int32 {
			return s.b.scannerSetMinimumNProbes(handle, *opts.MinNProbes)
		}); err != nil {
			return err
		}
	}
	if opts.MaxNProbes != nil {
		if err := s.setter("lance_scanner_set_maximum_nprobes", func(handle uintptr) int32 {
			return s.b.scannerSetMaximumNProbes(handle, *opts.MaxNProbes)
		}); err != nil {
			return err
		}
	}
	if opts.SearchEffort != nil {
		if err := s.setter("lance_scanner_set_search_effort", func(handle uintptr) int32 {
			return s.b.scannerSetSearchEffort(handle, *opts.SearchEffort)
		}); err != nil {
			return err
		}
	}
	if opts.ApproxMode != nil {
		if err := s.setter("lance_scanner_set_approx_mode", func(handle uintptr) int32 {
			return s.b.scannerSetApproxMode(handle, *opts.ApproxMode)
		}); err != nil {
			return err
		}
	}
	if opts.QueryParallelism != nil {
		if err := s.setter("lance_scanner_set_query_parallelism", func(handle uintptr) int32 {
			return s.b.scannerSetQueryParallelism(handle, *opts.QueryParallelism)
		}); err != nil {
			return err
		}
	}
	if opts.RefineFactor != nil {
		if err := s.setter("lance_scanner_set_refine_factor", func(handle uintptr) int32 {
			return s.b.scannerSetRefineFactor(handle, *opts.RefineFactor)
		}); err != nil {
			return err
		}
	}
	if opts.EF != nil {
		if err := s.setter("lance_scanner_set_ef", func(handle uintptr) int32 {
			return s.b.scannerSetEF(handle, *opts.EF)
		}); err != nil {
			return err
		}
	}
	if opts.UseIndex != nil {
		if err := s.setter("lance_scanner_set_use_index", func(handle uintptr) int32 {
			return s.b.scannerSetUseIndex(handle, *opts.UseIndex)
		}); err != nil {
			return err
		}
	}
	if opts.Prefilter != nil {
		if err := s.setter("lance_scanner_set_prefilter", func(handle uintptr) int32 {
			return s.b.scannerSetPrefilter(handle, *opts.Prefilter)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scanner) setNearest(query *NearestQuery) error {
	if err := validateCString(query.Column); err != nil {
		return fmt.Errorf("ffi: nearest column: %w", err)
	}
	var err error
	switch query.Mode {
	case NearestSingle:
		err = s.setter("lance_scanner_nearest", func(handle uintptr) int32 {
			return s.b.scannerNearest(handle, query.Column, query.Data, uint64(query.Count), query.DataType, query.K)
		})
	case NearestBatch:
		err = s.setter("lance_scanner_nearest_batch", func(handle uintptr) int32 {
			return s.b.scannerNearestBatch(handle, query.Column, query.Data, uint64(query.Dimension), uint64(query.Count), query.DataType, query.K)
		})
	case NearestMultiVector:
		err = s.setter("lance_scanner_nearest_multivector", func(handle uintptr) int32 {
			return s.b.scannerNearestMultiVector(handle, query.Column, query.Data, uint64(query.Dimension), uint64(query.Count), query.DataType, query.K)
		})
	default:
		return fmt.Errorf("ffi: unknown nearest mode %d", query.Mode)
	}
	return err
}

func (s *Scanner) setProjection(columns []ProjectionColumn) error {
	all := make([]string, 0, len(columns)*2)
	for _, column := range columns {
		all = append(all, column.OutputName, column.Expression)
	}
	list, err := newCStringList(all)
	if err != nil {
		return err
	}
	raw := make([]projectionColumn, len(columns))
	for i := range columns {
		raw[i] = projectionColumn{
			OutputName: list.ptrs[i*2],
			Expression: list.ptrs[i*2+1],
		}
	}
	err = s.setter("lance_scanner_set_projection", func(handle uintptr) int32 {
		return s.b.scannerSetProjection(handle, raw, uint64(len(raw)))
	})
	list.keepAlive()
	return err
}

func (s *Scanner) setOrderBy(orderings []ColumnOrdering) error {
	names := make([]string, 0, len(orderings))
	for _, ordering := range orderings {
		if ordering.Direction != 0 && ordering.Direction != 1 {
			return fmt.Errorf("ffi: invalid order direction %d", ordering.Direction)
		}
		if ordering.NullOrder != 0 && ordering.NullOrder != 1 {
			return fmt.Errorf("ffi: invalid null order %d", ordering.NullOrder)
		}
		names = append(names, ordering.Column)
	}
	list, err := newCStringList(names)
	if err != nil {
		return err
	}
	raw := make([]columnOrdering, len(orderings))
	for i, ordering := range orderings {
		raw[i] = columnOrdering{
			ColumnName: list.ptrs[i],
			Direction:  ordering.Direction,
			NullOrder:  ordering.NullOrder,
		}
	}
	err = s.setter("lance_scanner_set_order_by", func(handle uintptr) int32 {
		return s.b.scannerSetOrderBy(handle, raw, uint64(len(raw)))
	})
	list.keepAlive()
	return err
}

func (s *Scanner) setter(name string, call func(uintptr) int32) error {
	handleValue := s.handle.Load()
	if handleValue == 0 {
		return ErrClosed
	}
	runtime.LockOSThread()
	rc := call(handleValue)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: %s failed without setting an error", name)
	}
	runtime.UnlockOSThread()
	return nil
}

func (s *Scanner) registerStatistics() error {
	handleValue := s.handle.Load()
	if handleValue == 0 {
		return ErrClosed
	}
	runtime.LockOSThread()
	rc := s.b.scannerSetStatisticsCallback(handleValue, statisticsCallback(), uintptr(s.statsHandle))
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_scanner_set_statistics_callback failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

func (s *Scanner) loadSchema() (*arrow.Schema, error) {
	handleValue := s.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	var raw cdata.ArrowSchema
	runtime.LockOSThread()
	rc := s.b.scannerSchema(handleValue, &raw)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_scanner_schema failed without setting an error")
	}
	runtime.UnlockOSThread()

	defer cdata.ReleaseSchema(&raw)
	schema, err := cdata.ImportSchema(&raw)
	if err != nil {
		return nil, fmt.Errorf("ffi: import scanner schema: %w", err)
	}
	return schema, nil
}

// Schema returns the scanner output schema. The schema is immutable and stays
// valid after the scanner closes.
func (s *Scanner) Schema() *arrow.Schema {
	return s.schema
}

// Statistics returns the most recent native statistics snapshot.
func (s *Scanner) Statistics() ScanStatistics {
	return s.stats.snapshot()
}

// Next polls the scanner once, waiting without holding an OS thread when the
// native stream is pending. The returned batch is owned by the caller.
func (s *Scanner) Next(ctx context.Context) (arrow.RecordBatch, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil, false, ErrClosed
	}
	if s.aborted {
		s.mu.Unlock()
		return nil, false, ErrClosed
	}
	if s.finished {
		s.mu.Unlock()
		return nil, true, nil
	}
	if s.polling {
		s.mu.Unlock()
		return nil, false, fmt.Errorf("ffi: scanner does not support concurrent Next calls")
	}
	handleValue := s.handle.Load()
	if handleValue == 0 {
		s.mu.Unlock()
		return nil, false, ErrClosed
	}
	s.polling = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.polling = false
		s.cond.Broadcast()
		s.mu.Unlock()
	}()

	for {
		s.waiter.drain()

		var batchHandle uintptr
		runtime.LockOSThread()
		status := s.b.scannerPollNext(handleValue, pollWaker(), uintptr(s.waiterHandle), &batchHandle)
		var nativeErr *Error
		if status == pollError {
			nativeErr = s.b.takeError()
		}
		runtime.UnlockOSThread()

		switch status {
		case pollReady:
			if batchHandle == 0 {
				return nil, false, fmt.Errorf("ffi: lance_scanner_poll_next returned READY with a NULL batch")
			}
			if s.isClosing() {
				s.b.batchFree(batchHandle)
				return nil, false, ErrClosed
			}
			batch, err := importNativeBatch(s.b, batchHandle)
			if err != nil {
				return nil, false, err
			}
			return batch, false, nil
		case pollPending:
			select {
			case <-s.waiter.ready:
			case <-ctx.Done():
				s.markAborted()
				return nil, false, ctx.Err()
			case <-s.closeCh:
				return nil, false, ErrClosed
			}
		case pollFinished:
			s.mu.Lock()
			s.finished = true
			s.mu.Unlock()
			return nil, true, nil
		case pollError:
			if nativeErr != nil {
				return nil, false, nativeErr
			}
			return nil, false, fmt.Errorf("ffi: lance_scanner_poll_next failed without setting an error")
		default:
			return nil, false, fmt.Errorf("ffi: lance_scanner_poll_next returned unknown status %d", status)
		}
	}
}

func (s *Scanner) isClosing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}

func (s *Scanner) markAborted() {
	s.mu.Lock()
	s.aborted = true
	s.mu.Unlock()
}

func importNativeBatch(b *bindings, batchHandle uintptr) (arrow.RecordBatch, error) {
	defer b.batchFree(batchHandle)

	var rawArray cdata.ArrowArray
	var rawSchema cdata.ArrowSchema
	imported := false
	defer func() {
		if imported {
			return
		}
		if rawSchema.Release != 0 {
			cdata.ReleaseSchema(&rawSchema)
		}
		if rawArray.Release != 0 {
			cdata.ReleaseArray(&rawArray)
		}
	}()

	runtime.LockOSThread()
	rc := b.batchToArrow(batchHandle, &rawArray, &rawSchema)
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_batch_to_arrow failed without setting an error")
	}
	runtime.UnlockOSThread()

	schema, err := cdata.ImportSchema(&rawSchema)
	cdata.ReleaseSchema(&rawSchema)
	if err != nil {
		return nil, fmt.Errorf("ffi: import batch schema: %w", err)
	}
	batch, err := cdata.ImportRecordBatch(&rawArray, schema)
	if err != nil {
		return nil, fmt.Errorf("ffi: import batch: %w", err)
	}
	imported = true
	return batch, nil
}

// Close releases the native scanner. It waits for an in-flight poll to leave
// native code before closing, then removes the Go waker and statistics
// handles after native close has joined callbacks.
func (s *Scanner) Close() error {
	s.mu.Lock()
	if s.closing {
		done := s.closeDone
		s.mu.Unlock()
		<-done
		return nil
	}
	s.closing = true
	close(s.closeCh)
	for s.polling {
		s.cond.Wait()
	}
	handleValue := s.handle.Swap(0)
	s.mu.Unlock()

	if handleValue != 0 {
		s.b.scannerClose(handleValue)
	}
	if s.statsHandle != 0 {
		handle.Remove(s.statsHandle)
	}
	if s.waiterHandle != 0 {
		handle.Remove(s.waiterHandle)
	}

	s.mu.Lock()
	close(s.closeDone)
	s.mu.Unlock()
	return nil
}

func (s *Scanner) abort() {
	handleValue := s.handle.Swap(0)
	if handleValue != 0 {
		s.b.scannerClose(handleValue)
	}
	if s.statsHandle != 0 {
		handle.Remove(s.statsHandle)
	}
	if s.waiterHandle != 0 {
		handle.Remove(s.waiterHandle)
	}
}
