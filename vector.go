package lance

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/lance-format/lance-go/internal/ffi"
)

// VectorDataType identifies the element representation of a query vector.
//
// The values of a Vector for a given data type are interpreted as follows:
// Float16 uses IEEE 754 binary16 bit patterns, and UInt8 and Int8 use unsigned
// and signed 8-bit integers respectively. Hamming distance is only defined for
// UInt8 vectors.
type VectorDataType int32

const (
	// VectorDataTypeFloat32 uses 32-bit IEEE 754 floating-point values.
	VectorDataTypeFloat32 VectorDataType = 0
	// VectorDataTypeFloat16 uses 16-bit IEEE 754 binary16 bit patterns.
	VectorDataTypeFloat16 VectorDataType = 1
	// VectorDataTypeFloat64 uses 64-bit IEEE 754 floating-point values.
	VectorDataTypeFloat64 VectorDataType = 2
	// VectorDataTypeUInt8 uses unsigned 8-bit integer values.
	VectorDataTypeUInt8 VectorDataType = 3
	// VectorDataTypeInt8 uses signed 8-bit integer values.
	VectorDataTypeInt8 VectorDataType = 4
)

func (t VectorDataType) width() int {
	switch t {
	case VectorDataTypeFloat32:
		return 4
	case VectorDataTypeFloat16:
		return 2
	case VectorDataTypeFloat64:
		return 8
	case VectorDataTypeUInt8, VectorDataTypeInt8:
		return 1
	default:
		return 0
	}
}

// VectorMetric selects the distance metric used for vector search. The dataset
// or index metric is used when it is not set explicitly.
type VectorMetric = Metric

const (
	// VectorMetricL2 uses squared Euclidean distance.
	VectorMetricL2 VectorMetric = MetricL2
	// VectorMetricCosine uses cosine distance.
	VectorMetricCosine VectorMetric = MetricCosine
	// VectorMetricDot uses dot-product distance.
	VectorMetricDot VectorMetric = MetricDot
	// VectorMetricHamming uses Hamming distance for UInt8 vectors.
	VectorMetricHamming VectorMetric = MetricHamming
)

// ApproxMode controls how much accuracy a vector index search trades for speed.
type ApproxMode int32

const (
	// ApproxModeFast favors speed over accuracy.
	ApproxModeFast ApproxMode = 0
	// ApproxModeNormal balances speed and accuracy.
	ApproxModeNormal ApproxMode = 1
	// ApproxModeAccurate favors accuracy over speed.
	ApproxModeAccurate ApproxMode = 2
)

func (m ApproxMode) valid() bool {
	return m >= ApproxModeFast && m <= ApproxModeAccurate
}

// Vector is an immutable, owned query vector buffer. Construct it with
// NewFloat32Vector, NewFloat16Vector, NewFloat64Vector, NewUInt8Vector, or
// NewInt8Vector. The constructors copy their input, so the caller may reuse or
// mutate the source slice immediately after the call.
type Vector struct {
	dataType VectorDataType
	values   any
}

// NewFloat32Vector returns a float32 query vector.
func NewFloat32Vector(values []float32) Vector {
	return Vector{dataType: VectorDataTypeFloat32, values: append([]float32(nil), values...)}
}

// NewFloat16Vector returns a float16 query vector. Each value is an IEEE 754
// binary16 bit pattern.
func NewFloat16Vector(values []uint16) Vector {
	return Vector{dataType: VectorDataTypeFloat16, values: append([]uint16(nil), values...)}
}

// NewFloat64Vector returns a float64 query vector.
func NewFloat64Vector(values []float64) Vector {
	return Vector{dataType: VectorDataTypeFloat64, values: append([]float64(nil), values...)}
}

// NewUInt8Vector returns an unsigned 8-bit integer query vector.
func NewUInt8Vector(values []uint8) Vector {
	return Vector{dataType: VectorDataTypeUInt8, values: append([]uint8(nil), values...)}
}

// NewInt8Vector returns a signed 8-bit integer query vector.
func NewInt8Vector(values []int8) Vector {
	return Vector{dataType: VectorDataTypeInt8, values: append([]int8(nil), values...)}
}

// DataType returns the vector element type.
func (v Vector) DataType() VectorDataType {
	return v.dataType
}

// Len returns the number of elements in the vector. For a batch matrix this is
// the total element count across all queries, and for a multi-vector input it
// is the total element count across all subvectors.
func (v Vector) Len() int {
	switch values := v.values.(type) {
	case []float32:
		return len(values)
	case []uint16:
		return len(values)
	case []float64:
		return len(values)
	case []uint8:
		return len(values)
	case []int8:
		return len(values)
	default:
		return 0
	}
}

// data returns a pointer to the first element. The Vector must remain alive
// until the native call that reads this pointer has returned.
func (v Vector) data() unsafe.Pointer {
	switch values := v.values.(type) {
	case []float32:
		return unsafe.Pointer(unsafe.SliceData(values))
	case []uint16:
		return unsafe.Pointer(unsafe.SliceData(values))
	case []float64:
		return unsafe.Pointer(unsafe.SliceData(values))
	case []uint8:
		return unsafe.Pointer(unsafe.SliceData(values))
	case []int8:
		return unsafe.Pointer(unsafe.SliceData(values))
	default:
		return nil
	}
}

type nearestKind uint8

const (
	nearestKindSingle nearestKind = iota
	nearestKindBatch
	nearestKindMultiVector
)

type nearestQueryConfig struct {
	kind      nearestKind
	column    string
	vector    Vector
	dimension int
	k         uint32
}

type vectorConfig struct {
	nearest          *nearestQueryConfig
	metric           *VectorMetric
	nprobes          *uint32
	minNProbes       *uint32
	maxNProbes       *uint32
	ef               *uint32
	refineFactor     *uint32
	searchEffort     *float64
	approxMode       *ApproxMode
	queryParallelism *int32
	useIndex         *bool
	fastSearch       *bool
	prefilter        *bool
	lowerDistance    *float32
	upperDistance    *float32
	rowAllowlist     []uint64
	rowAllowlistSet  bool
	rowBlocklist     []uint64
	rowBlocklistSet  bool
}

const (
	maxBatchQueries     = 128
	maxVectorCandidates = 100000
	maxBatchQueryBytes  = 64 << 20
)

// WithNearest returns the k nearest rows to a single query vector. The column
// must have the same vector dimension as query. Native also accepts Float32
// queries for Float16 and Float64 columns by coercing their element type; other
// combinations require matching element types. k must be positive.
//
// This option is mutually exclusive with WithNearestBatch and
// WithNearestMultiVector; when several are given, the last one wins. A scan
// with a nearest query is ordered by distance and returns the _distance column.
func WithNearest(column string, query Vector, k uint32) ScanOption {
	return func(config *scanConfig) error {
		if err := validateVectorColumn(column); err != nil {
			return err
		}
		if k == 0 {
			return fmt.Errorf("vector k must be positive")
		}
		if query.Len() == 0 {
			return fmt.Errorf("nearest query vector must not be empty")
		}
		config.vector.nearest = &nearestQueryConfig{
			kind:   nearestKindSingle,
			column: column,
			vector: query,
			k:      k,
		}
		return nil
	}
}

// WithNearestBatch returns the k nearest rows for each row of a row-major query
// matrix. queries holds numQueries*dimension elements, where dimension is the
// query vector dimension. The column must contain FixedSizeList vectors of the
// same element type and dimension.
//
// The result contains the query_index column, which identifies the query that
// produced each row. Results for different queries have no global ordering.
// At most 128 queries and 64 MiB of query data are accepted. numQueries*k,
// multiplied by refineFactor when set, must not exceed 100000. A global limit
// or offset cannot be combined with a batch query.
func WithNearestBatch(column string, queries Vector, dimension int, k uint32) ScanOption {
	return func(config *scanConfig) error {
		if err := validateVectorColumn(column); err != nil {
			return err
		}
		if k == 0 {
			return fmt.Errorf("vector k must be positive")
		}
		if dimension <= 0 || dimension > math.MaxInt32 {
			return fmt.Errorf("batch query dimension %d must be between 1 and %d", dimension, math.MaxInt32)
		}
		if queries.Len() == 0 {
			return fmt.Errorf("batch query matrix must not be empty")
		}
		if queries.Len()%dimension != 0 {
			return fmt.Errorf("batch query length %d is not a multiple of dimension %d", queries.Len(), dimension)
		}
		if count := queries.Len() / dimension; count > maxBatchQueries {
			return fmt.Errorf("batch query count %d must not exceed %d", count, maxBatchQueries)
		}
		if width := queries.dataType.width(); width == 0 {
			return fmt.Errorf("unsupported batch query data type %d", queries.dataType)
		} else if uint64(queries.Len()) > uint64(maxBatchQueryBytes)/uint64(width) {
			return fmt.Errorf("batch query data must not exceed %d bytes", maxBatchQueryBytes)
		}
		config.vector.nearest = &nearestQueryConfig{
			kind:      nearestKindBatch,
			column:    column,
			vector:    queries,
			dimension: dimension,
			k:         k,
		}
		return nil
	}
}

// WithNearestMultiVector returns the nearest rows for a multi-vector query.
// vectors holds numVectors*dimension elements, where dimension is the
// dimension of each subvector. The column must contain List values whose
// elements are FixedSizeList vectors of dimension with float16, float32, or
// float64 elements.
//
// At most 128 subvectors are accepted, and numVectors*k and k*refineFactor
// must each not exceed 100000. The result contains the nearest rows for the
// combined query. WithLimit and WithOffset apply to these combined results
// through the native result window.
func WithNearestMultiVector(column string, vectors Vector, dimension int, k uint32) ScanOption {
	return func(config *scanConfig) error {
		if err := validateVectorColumn(column); err != nil {
			return err
		}
		if k == 0 {
			return fmt.Errorf("vector k must be positive")
		}
		switch vectors.dataType {
		case VectorDataTypeFloat16, VectorDataTypeFloat32, VectorDataTypeFloat64:
		default:
			return fmt.Errorf("multi-vector queries must use float16, float32, or float64 elements")
		}
		if dimension <= 0 || dimension > math.MaxInt32 {
			return fmt.Errorf("multi-vector dimension %d must be between 1 and %d", dimension, math.MaxInt32)
		}
		if vectors.Len() == 0 {
			return fmt.Errorf("multi-vector query must not be empty")
		}
		if vectors.Len()%dimension != 0 {
			return fmt.Errorf("multi-vector query length %d is not a multiple of dimension %d", vectors.Len(), dimension)
		}
		count := vectors.Len() / dimension
		if count > maxBatchQueries {
			return fmt.Errorf("multi-vector query count %d must not exceed %d", count, maxBatchQueries)
		}
		if uint64(count)*uint64(k) > maxVectorCandidates {
			return fmt.Errorf("multi-vector query count times k must not exceed %d", maxVectorCandidates)
		}
		config.vector.nearest = &nearestQueryConfig{
			kind:      nearestKindMultiVector,
			column:    column,
			vector:    vectors,
			dimension: dimension,
			k:         k,
		}
		return nil
	}
}

func validateVectorColumn(column string) error {
	if column == "" {
		return fmt.Errorf("vector column must not be empty")
	}
	if err := validateCString(column); err != nil {
		return fmt.Errorf("vector column: %w", err)
	}
	return nil
}

// WithVectorMetric selects the distance metric for the scan, overriding the
// dataset or index metric. Hamming is only valid for UInt8 columns.
func WithVectorMetric(metric VectorMetric) ScanOption {
	return func(config *scanConfig) error {
		if metric < VectorMetricL2 || metric > VectorMetricHamming {
			return fmt.Errorf("invalid vector metric %d", metric)
		}
		config.vector.metric = &metric
		return nil
	}
}

// WithVectorNProbes sets the number of IVF partitions to probe. It cannot be
// combined with WithVectorMinNProbes or WithVectorMaxNProbes.
func WithVectorNProbes(n uint32) ScanOption {
	return func(config *scanConfig) error {
		if n == 0 {
			return fmt.Errorf("vector nprobes must be positive")
		}
		config.vector.nprobes = &n
		return nil
	}
}

// WithVectorMinNProbes sets the minimum number of IVF partitions to probe.
func WithVectorMinNProbes(n uint32) ScanOption {
	return func(config *scanConfig) error {
		if n == 0 {
			return fmt.Errorf("vector minimum nprobes must be positive")
		}
		config.vector.minNProbes = &n
		return nil
	}
}

// WithVectorMaxNProbes sets the maximum number of IVF partitions to probe.
func WithVectorMaxNProbes(n uint32) ScanOption {
	return func(config *scanConfig) error {
		if n == 0 {
			return fmt.Errorf("vector maximum nprobes must be positive")
		}
		config.vector.maxNProbes = &n
		return nil
	}
}

// WithVectorEF sets the HNSW ef search parameter.
func WithVectorEF(ef uint32) ScanOption {
	return func(config *scanConfig) error {
		config.vector.ef = &ef
		return nil
	}
}

// WithVectorRefineFactor re-ranks k*factor candidates using the original vector
// values. The factor must be positive. It is only meaningful for indexed
// searches.
func WithVectorRefineFactor(factor uint32) ScanOption {
	return func(config *scanConfig) error {
		if factor == 0 {
			return fmt.Errorf("vector refine factor must be positive")
		}
		config.vector.refineFactor = &factor
		return nil
	}
}

// WithVectorSearchEffort trades accuracy for speed in [0,1], where a higher
// value searches more thoroughly. An explicit effort overrides a fixed nprobes
// count, while explicit minimum and maximum nprobes bounds remain effective.
func WithVectorSearchEffort(effort float64) ScanOption {
	return func(config *scanConfig) error {
		if math.IsNaN(effort) || math.IsInf(effort, 0) || effort < 0 || effort > 1 {
			return fmt.Errorf("vector search effort %v must be finite and in [0,1]", effort)
		}
		config.vector.searchEffort = &effort
		return nil
	}
}

// WithVectorApproxMode selects the vector index accuracy mode.
func WithVectorApproxMode(mode ApproxMode) ScanOption {
	return func(config *scanConfig) error {
		if !mode.valid() {
			return fmt.Errorf("invalid vector approximate mode %d", mode)
		}
		config.vector.approxMode = &mode
		return nil
	}
}

// WithVectorQueryParallelism sets the number of batches searched in parallel.
// A value of -1 lets the native implementation choose.
func WithVectorQueryParallelism(n int32) ScanOption {
	return func(config *scanConfig) error {
		if n < -1 {
			return fmt.Errorf("vector query parallelism %d must be at least -1", n)
		}
		config.vector.queryParallelism = &n
		return nil
	}
}

// WithVectorUseIndex controls whether vector indexes may be used.
func WithVectorUseIndex(use bool) ScanOption {
	return func(config *scanConfig) error {
		config.vector.useIndex = &use
		return nil
	}
}

// WithVectorFastSearch enables index-only search with weak consistency. Rows
// that are not yet visible to the index may be missing, and the search can
// return no rows when no usable index exists. It does not fall back to an exact
// scan, even with WithVectorUseIndex(false).
func WithVectorFastSearch(fast bool) ScanOption {
	return func(config *scanConfig) error {
		config.vector.fastSearch = &fast
		return nil
	}
}

// WithVectorPrefilter controls whether the SQL filter is applied before or
// after vector ranking. Prefiltering before ranking yields exact filtered
// nearest neighbors; post-filtering only filters the ranked candidates.
func WithVectorPrefilter(prefilter bool) ScanOption {
	return func(config *scanConfig) error {
		config.vector.prefilter = &prefilter
		return nil
	}
}

// WithVectorDistanceRange restricts a single-vector nearest search to
// lower <= distance < upper. A nil bound is unbounded. Bounds must be finite,
// and lower must be less than upper when both are set. L2 distance is squared
// Euclidean distance.
func WithVectorDistanceRange(lower, upper *float32) ScanOption {
	return func(config *scanConfig) error {
		if lower != nil && (math.IsNaN(float64(*lower)) || math.IsInf(float64(*lower), 0)) {
			return fmt.Errorf("vector lower distance must be finite")
		}
		if upper != nil && (math.IsNaN(float64(*upper)) || math.IsInf(float64(*upper), 0)) {
			return fmt.Errorf("vector upper distance must be finite")
		}
		if lower != nil && upper != nil && *lower >= *upper {
			return fmt.Errorf("vector lower distance %v must be less than upper distance %v", *lower, *upper)
		}
		if lower != nil {
			value := *lower
			config.vector.lowerDistance = &value
		} else {
			config.vector.lowerDistance = nil
		}
		if upper != nil {
			value := *upper
			config.vector.upperDistance = &value
		} else {
			config.vector.upperDistance = nil
		}
		return nil
	}
}

// WithRowIDAllowlist restricts the scan to the given stable row IDs. The IDs
// are in the dataset's _rowid space; they are not positions within a batch or
// fragment. Calling it with no IDs selects no rows.
func WithRowIDAllowlist(rowIDs ...uint64) ScanOption {
	return func(config *scanConfig) error {
		config.vector.rowAllowlist = append([]uint64(nil), rowIDs...)
		config.vector.rowAllowlistSet = true
		return nil
	}
}

// WithRowIDBlocklist excludes the given stable row IDs from the scan. The IDs
// are in the dataset's _rowid space; they are not positions within a batch or
// fragment.
func WithRowIDBlocklist(rowIDs ...uint64) ScanOption {
	return func(config *scanConfig) error {
		config.vector.rowBlocklist = append([]uint64(nil), rowIDs...)
		config.vector.rowBlocklistSet = true
		return nil
	}
}

func (c *scanConfig) validateVector() error {
	vector := &c.vector
	if vector.minNProbes != nil && vector.maxNProbes != nil && *vector.minNProbes > *vector.maxNProbes {
		return fmt.Errorf("vector minimum nprobes %d must not exceed maximum nprobes %d", *vector.minNProbes, *vector.maxNProbes)
	}
	if vector.nprobes != nil && (vector.minNProbes != nil || vector.maxNProbes != nil) {
		return fmt.Errorf("vector nprobes cannot be combined with minimum or maximum nprobes")
	}
	if vector.nearest == nil {
		if vector.lowerDistance != nil || vector.upperDistance != nil {
			return fmt.Errorf("vector distance range requires a nearest query")
		}
		return nil
	}

	query := vector.nearest
	if vector.lowerDistance != nil || vector.upperDistance != nil {
		if query.kind != nearestKindSingle {
			return fmt.Errorf("vector distance range requires a single-vector nearest query")
		}
	}
	refine := uint64(1)
	if vector.refineFactor != nil {
		refine = uint64(*vector.refineFactor)
	}
	switch query.kind {
	case nearestKindBatch:
		candidates := uint64(query.vector.Len()/query.dimension) * uint64(query.k)
		if candidates > maxVectorCandidates || refine > maxVectorCandidates/candidates {
			return fmt.Errorf("batch query count times k times refine factor must not exceed %d", maxVectorCandidates)
		}
	case nearestKindMultiVector:
		if uint64(query.vector.Len()/query.dimension)*uint64(query.k) > maxVectorCandidates ||
			uint64(query.k) > maxVectorCandidates/refine {
			return fmt.Errorf("multi-vector count times k and k times refine factor must not exceed %d", maxVectorCandidates)
		}
	}
	return nil
}

func (c *scanConfig) ffiVectorOptions() ffi.VectorOptions {
	vector := &c.vector
	options := ffi.VectorOptions{
		RowAllowlist:    vector.rowAllowlist,
		RowAllowlistSet: vector.rowAllowlistSet,
		RowBlocklist:    vector.rowBlocklist,
		RowBlocklistSet: vector.rowBlocklistSet,
		LowerDistance:   vector.lowerDistance,
		UpperDistance:   vector.upperDistance,
	}
	if vector.metric != nil {
		value := int32(*vector.metric)
		options.Metric = &value
	}
	if vector.nprobes != nil {
		options.NProbes = vector.nprobes
	}
	if vector.minNProbes != nil {
		options.MinNProbes = vector.minNProbes
	}
	if vector.maxNProbes != nil {
		options.MaxNProbes = vector.maxNProbes
	}
	if vector.ef != nil {
		options.EF = vector.ef
	}
	if vector.refineFactor != nil {
		options.RefineFactor = vector.refineFactor
	}
	if vector.searchEffort != nil {
		options.SearchEffort = vector.searchEffort
	}
	if vector.approxMode != nil {
		value := int32(*vector.approxMode)
		options.ApproxMode = &value
	}
	if vector.queryParallelism != nil {
		options.QueryParallelism = vector.queryParallelism
	}
	if vector.useIndex != nil {
		options.UseIndex = vector.useIndex
	}
	if vector.fastSearch != nil {
		options.FastSearch = vector.fastSearch
	}
	if vector.prefilter != nil {
		options.Prefilter = vector.prefilter
	}
	if vector.nearest != nil {
		query := vector.nearest
		mode := ffi.NearestSingle
		dimension := 0
		count := query.vector.Len()
		switch query.kind {
		case nearestKindBatch:
			mode = ffi.NearestBatch
			dimension = query.dimension
			count = query.vector.Len() / query.dimension
		case nearestKindMultiVector:
			mode = ffi.NearestMultiVector
			dimension = query.dimension
			count = query.vector.Len() / query.dimension
		}
		options.Nearest = &ffi.NearestQuery{
			Column:    query.column,
			Data:      query.vector.data(),
			DataType:  int32(query.vector.dataType),
			K:         query.k,
			Mode:      mode,
			Dimension: dimension,
			Count:     count,
		}
	}
	return options
}
