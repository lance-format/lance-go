package lance

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lance-format/lance-go/index"
	"github.com/lance-format/lance-go/internal/ffi"
)

// Metric is the vector distance metric accepted by vector indexes.
type Metric = index.Metric

const (
	MetricL2      = index.MetricL2
	MetricCosine  = index.MetricCosine
	MetricDot     = index.MetricDot
	MetricHamming = index.MetricHamming
)

// CreateIndexOption configures index creation.
type CreateIndexOption func(*createIndexConfig) error

type createIndexConfig struct {
	name    string
	replace bool
}

// WithIndexName names the created index. An empty name leaves the index
// unnamed.
func WithIndexName(name string) CreateIndexOption {
	return func(config *createIndexConfig) error {
		if err := validateCString(name); err != nil {
			return fmt.Errorf("index name: %w", err)
		}
		config.name = name
		return nil
	}
}

// WithIndexReplace replaces an existing index of the same name when true.
func WithIndexReplace(replace bool) CreateIndexOption {
	return func(config *createIndexConfig) error {
		config.replace = replace
		return nil
	}
}

func buildCreateIndexConfig(opts []CreateIndexOption) (createIndexConfig, error) {
	var config createIndexConfig
	for _, option := range opts {
		if option == nil {
			return config, fmt.Errorf("nil CreateIndexOption")
		}
		if err := option(&config); err != nil {
			return config, err
		}
	}
	return config, nil
}

// CreateVectorIndex builds a vector index and returns a new Dataset snapshot.
// This Dataset keeps its current version and catalog. Options cover every
// native vector index build field; nil optional fields keep native defaults.
// An error can occur after the index version is committed, during metadata
// reload. Reopen the dataset to determine the published state before retrying.
func (d *Dataset) CreateVectorIndex(ctx context.Context, column string, options index.VectorOptions, opts ...CreateIndexOption) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if column == "" {
		return nil, invalidError("create vector index", d.uri, "column must not be empty")
	}
	if err := validateCString(column); err != nil {
		return nil, invalidError("create vector index", d.uri, "column: %v", err)
	}
	config, err := buildCreateIndexConfig(opts)
	if err != nil {
		return nil, invalidError("create vector index", d.uri, "%v", err)
	}
	native := ffiVectorIndexOptions(options)
	return d.mutate("create vector index", func(clone *ffi.Dataset) error {
		return clone.CreateVectorIndex(column, config.name, native, config.replace)
	})
}

// CreateVectorIndexWithParams builds a vector index from legacy parameters.
// It is equivalent to CreateVectorIndex with params.Options().
func (d *Dataset) CreateVectorIndexWithParams(ctx context.Context, column string, params index.VectorParams, opts ...CreateIndexOption) (*Dataset, error) {
	return d.CreateVectorIndex(ctx, column, params.Options(), opts...)
}

// CreateScalarIndex builds a scalar index and returns a new Dataset snapshot.
// params is forwarded verbatim as the native plugin JSON; nil leaves the
// native parameter string unset. Unknown plugin keys are preserved.
func (d *Dataset) CreateScalarIndex(ctx context.Context, column string, indexType index.ScalarType, params json.RawMessage, options index.ScalarOptions, opts ...CreateIndexOption) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if column == "" {
		return nil, invalidError("create scalar index", d.uri, "column must not be empty")
	}
	if err := validateCString(column); err != nil {
		return nil, invalidError("create scalar index", d.uri, "column: %v", err)
	}
	config, err := buildCreateIndexConfig(opts)
	if err != nil {
		return nil, invalidError("create scalar index", d.uri, "%v", err)
	}
	native := &ffi.ScalarIndexOptions{Train: options.Train}
	var paramsJSON []byte
	if params != nil {
		if !json.Valid(params) {
			return nil, invalidError("create scalar index", d.uri, "params must be valid JSON")
		}
		paramsJSON = params
	}
	return d.mutate("create scalar index", func(clone *ffi.Dataset) error {
		return clone.CreateScalarIndex(column, config.name, int32(indexType), paramsJSON, native, config.replace)
	})
}

// DropIndex drops the named index and returns a new Dataset snapshot.
func (d *Dataset) DropIndex(ctx context.Context, name string) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, invalidError("drop index", d.uri, "index name must not be empty")
	}
	if err := validateCString(name); err != nil {
		return nil, invalidError("drop index", d.uri, "index name: %v", err)
	}
	return d.mutate("drop index", func(clone *ffi.Dataset) error {
		return clone.DropIndex(name)
	})
}

// IndexCount returns the number of user indexes in this snapshot.
func (d *Dataset) IndexCount() (uint64, error) {
	if err := d.acquire("index count"); err != nil {
		return 0, err
	}
	defer d.res.release()
	count, err := d.native.IndexCount()
	if err != nil {
		return 0, wrapError("index count", d.uri, err)
	}
	return count, nil
}

// ListIndices lists user indexes. System indexes are omitted.
func (d *Dataset) ListIndices() ([]index.Summary, error) {
	raw, err := d.indexJSON("list indices", func(native *ffi.Dataset) ([]byte, error) {
		return native.IndexListJSON()
	})
	if err != nil {
		return nil, err
	}
	var indices []index.Summary
	if err := json.Unmarshal(raw, &indices); err != nil {
		return nil, fmt.Errorf("lance: list indices %s: decode native JSON: %w", d.uri, err)
	}
	return indices, nil
}

// DescribeIndices describes every index, including system indexes.
func (d *Dataset) DescribeIndices() ([]index.Description, error) {
	raw, err := d.indexJSON("describe indices", func(native *ffi.Dataset) ([]byte, error) {
		return native.DescribeIndicesJSON()
	})
	if err != nil {
		return nil, err
	}
	var indices []index.Description
	if err := json.Unmarshal(raw, &indices); err != nil {
		return nil, fmt.Errorf("lance: describe indices %s: decode native JSON: %w", d.uri, err)
	}
	return indices, nil
}

// IndexStatistics returns the family-specific statistics JSON for one index.
func (d *Dataset) IndexStatistics(name string) (index.Statistics, error) {
	if name == "" {
		return nil, invalidError("index statistics", d.uri, "index name must not be empty")
	}
	if err := validateCString(name); err != nil {
		return nil, invalidError("index statistics", d.uri, "index name: %v", err)
	}
	raw, err := d.indexJSON("index statistics", func(native *ffi.Dataset) ([]byte, error) {
		return native.IndexStatisticsJSON(name)
	})
	if err != nil {
		return nil, err
	}
	return index.Statistics(raw), nil
}

// IndexSegments lists the physical segments of all user indexes.
func (d *Dataset) IndexSegments() ([]index.SegmentListing, error) {
	raw, err := d.indexJSON("index segments", func(native *ffi.Dataset) ([]byte, error) {
		return native.IndexSegmentsJSON()
	})
	if err != nil {
		return nil, err
	}
	var segments []index.SegmentListing
	if err := json.Unmarshal(raw, &segments); err != nil {
		return nil, fmt.Errorf("lance: index segments %s: decode native JSON: %w", d.uri, err)
	}
	return segments, nil
}

// IndexSegmentUUIDs returns the raw UUIDs of the physical segments of one
// index.
func (d *Dataset) IndexSegmentUUIDs(name string) ([]index.UUID, error) {
	if name == "" {
		return nil, invalidError("index segment UUIDs", d.uri, "index name must not be empty")
	}
	if err := validateCString(name); err != nil {
		return nil, invalidError("index segment UUIDs", d.uri, "index name: %v", err)
	}
	if err := d.acquire("index segment UUIDs"); err != nil {
		return nil, err
	}
	defer d.res.release()
	raw, err := d.native.IndexSegments(name)
	if err != nil {
		return nil, wrapError("index segment UUIDs", d.uri, err)
	}
	uuids := make([]index.UUID, len(raw)/16)
	for i := range uuids {
		copy(uuids[i][:], raw[i*16:(i+1)*16])
	}
	return uuids, nil
}

// PrewarmIndex warms the named index into the configured session cache. A nil
// SegmentUUIDs selection warms every segment; a non-nil empty selection warms
// none. Prewarming is best-effort cache population, not a residency
// guarantee.
func (d *Dataset) PrewarmIndex(ctx context.Context, name string, options index.PrewarmOptions) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if name == "" {
		return invalidError("prewarm index", d.uri, "index name must not be empty")
	}
	if err := validateCString(name); err != nil {
		return invalidError("prewarm index", d.uri, "index name: %v", err)
	}
	native, err := ffiPrewarmOptions(options)
	if err != nil {
		return invalidError("prewarm index", d.uri, "%v", err)
	}
	if err := d.acquire("prewarm index"); err != nil {
		return err
	}
	defer d.res.release()
	if err := d.native.PrewarmIndex(name, native); err != nil {
		return wrapError("prewarm index", d.uri, err)
	}
	return nil
}

func (d *Dataset) indexJSON(op string, read func(*ffi.Dataset) ([]byte, error)) ([]byte, error) {
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()
	raw, err := read(d.native)
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	return raw, nil
}

func ffiVectorIndexOptions(options index.VectorOptions) *ffi.VectorIndexOptions {
	native := &ffi.VectorIndexOptions{
		IndexType:                   int32(options.IndexType),
		Metric:                      int32(options.Metric),
		NumPartitions:               options.NumPartitions,
		TargetPartitionSize:         options.TargetPartitionSize,
		MaxIters:                    options.MaxIters,
		SampleRate:                  options.SampleRate,
		StreamingSampleRate:         options.StreamingSampleRate,
		StreamingCoresetRate:        options.StreamingCoresetRate,
		StreamingRefinePasses:       options.StreamingRefinePasses,
		PrecomputedPartitionsFile:   options.PrecomputedPartitionsFile,
		ShufflePartitionBatches:     options.ShufflePartitionBatches,
		ShufflePartitionConcurrency: options.ShufflePartitionConcurrency,
		StorageOptions:              options.StorageOptions,
		NumSubVectors:               options.NumSubVectors,
		NumBits:                     options.NumBits,
		IVFCentroids:                options.IVFCentroids,
		PQCodebook:                  options.PQCodebook,
		RabitQModelJSON:             options.RabitQModelJSON,
		HNSWMaxLevel:                options.HNSWMaxLevel,
		HNSWM:                       options.HNSWM,
		HNSWEFConstruction:          options.HNSWEFConstruction,
		FileVersion:                 int32(options.FileVersion),
		SkipTranspose:               options.SkipTranspose,
		RuntimeHints:                options.RuntimeHints,
		Train:                       options.Train,
	}
	if options.PrecomputedShuffleBuffers != nil {
		native.PrecomputedShuffleBuffers = &ffi.PrecomputedShuffleBuffers{
			Path:    options.PrecomputedShuffleBuffers.Path,
			Buffers: options.PrecomputedShuffleBuffers.Buffers,
		}
	}
	return native
}

func ffiPrewarmOptions(options index.PrewarmOptions) (*ffi.PrewarmOptions, error) {
	native := &ffi.PrewarmOptions{WithPosition: options.WithPosition}
	if options.SegmentUUIDs != nil {
		native.HasSelection = true
		native.SegmentUUIDs = make([]byte, 0, len(options.SegmentUUIDs)*16)
		for _, uuid := range options.SegmentUUIDs {
			native.SegmentUUIDs = append(native.SegmentUUIDs, uuid[:]...)
		}
	}
	return native, nil
}
