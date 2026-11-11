package ffi

import (
	"fmt"
	"runtime"
	"sort"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
)

// optionalU64 mirrors LanceOptionalU64.
type optionalU64 struct {
	value uint64
	isSet bool
}

func newOptionalU64(value *uint64) optionalU64 {
	if value == nil {
		return optionalU64{}
	}
	return optionalU64{value: *value, isSet: true}
}

// stringMap mirrors LanceStringMap.
type stringMap struct {
	keys   *uintptr
	values *uintptr
	count  uint64
}

// precomputedShuffleBuffers mirrors LancePrecomputedShuffleBuffers.
type precomputedShuffleBuffers struct {
	path    *byte
	buffers *uintptr
	count   uint64
}

// vectorIndexOptions mirrors LanceVectorIndexOptions.
type vectorIndexOptions struct {
	indexType int32
	metric    int32

	numPartitions         optionalU64
	targetPartitionSize   optionalU64
	maxIters              optionalU64
	sampleRate            optionalU64
	streamingSampleRate   optionalU64
	streamingCoresetRate  optionalU64
	streamingRefinePasses uint64

	precomputedPartitionsFile   *byte
	precomputedShuffleBuffers   *precomputedShuffleBuffers
	shufflePartitionBatches     optionalU64
	shufflePartitionConcurrency optionalU64
	storageOptions              *stringMap

	numSubVectors optionalU64
	numBits       optionalU64

	ivfCentroids       *cdata.ArrowArray
	ivfCentroidsSchema *cdata.ArrowSchema
	pqCodebook         *cdata.ArrowArray
	pqCodebookSchema   *cdata.ArrowSchema
	rabitqModelJSON    *byte

	hnswMaxLevel       optionalU64
	hnswM              optionalU64
	hnswEFConstruction optionalU64

	indexFileVersion int32
	skipTranspose    bool
	runtimeHints     *stringMap
	hasTrain         bool
	train            bool
}

// scalarIndexOptions mirrors LanceScalarIndexOptions.
type scalarIndexOptions struct {
	hasTrain bool
	train    bool
}

// indexPrewarmOptions mirrors LanceIndexPrewarmOptions.
type indexPrewarmOptions struct {
	withPosition        bool
	hasSegmentSelection bool
	segmentUUIDs        *byte
	segmentCount        uint64
}

// VectorIndexOptions holds native vector index build options. Optional fields
// are nil when absent; Arrow models are FixedSizeList arrays owned by the
// caller for the duration of the call.
type VectorIndexOptions struct {
	IndexType int32
	Metric    int32

	NumPartitions         *uint64
	TargetPartitionSize   *uint64
	MaxIters              *uint64
	SampleRate            *uint64
	StreamingSampleRate   *uint64
	StreamingCoresetRate  *uint64
	StreamingRefinePasses uint64

	PrecomputedPartitionsFile   string
	PrecomputedShuffleBuffers   *PrecomputedShuffleBuffers
	ShufflePartitionBatches     *uint64
	ShufflePartitionConcurrency *uint64
	StorageOptions              map[string]string

	NumSubVectors   *uint64
	NumBits         *uint64
	IVFCentroids    arrow.Array
	PQCodebook      arrow.Array
	RabitQModelJSON []byte

	HNSWMaxLevel       *uint64
	HNSWM              *uint64
	HNSWEFConstruction *uint64

	FileVersion   int32
	SkipTranspose bool
	RuntimeHints  map[string]string

	Train *bool
}

// PrecomputedShuffleBuffers mirrors the native precomputed shuffle buffers.
type PrecomputedShuffleBuffers struct {
	Path    string
	Buffers []string
}

// ScalarIndexOptions holds additive scalar index build options.
type ScalarIndexOptions struct {
	Train *bool
}

// PrewarmOptions selects index segments to prewarm. SegmentUUIDs holds raw
// 16-byte UUIDs; HasSelection distinguishes "all" from an explicit empty
// selection.
type PrewarmOptions struct {
	WithPosition bool
	SegmentUUIDs []byte
	HasSelection bool
}

func buildStringMap(options map[string]string) (*stringMap, []*cStringList, error) {
	if len(options) == 0 {
		return nil, nil, nil
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = options[key]
	}
	keyList, err := newCStringList(keys)
	if err != nil {
		return nil, nil, err
	}
	valueList, err := newCStringList(values)
	if err != nil {
		return nil, nil, err
	}
	return &stringMap{
		keys:   &keyList.ptrs[0],
		values: &valueList.ptrs[0],
		count:  uint64(len(keys)),
	}, []*cStringList{keyList, valueList}, nil
}

func exportModel(arr arrow.Array) (*cdata.ArrowArray, *cdata.ArrowSchema, error) {
	raw := &cdata.ArrowArray{}
	if err := cdata.ExportArray(arr, raw); err != nil {
		return nil, nil, fmt.Errorf("ffi: export index model array: %w", err)
	}
	schema := arrow.NewSchema([]arrow.Field{{Name: "model", Type: arr.DataType(), Nullable: false}}, nil)
	rawSchema := &cdata.ArrowSchema{}
	if err := cdata.ExportSchema(schema, rawSchema); err != nil {
		cdata.ReleaseArray(raw)
		return nil, nil, fmt.Errorf("ffi: export index model schema: %w", err)
	}
	return raw, rawSchema, nil
}

func buildVectorIndexOptions(opts *VectorIndexOptions) (*vectorIndexOptions, func(), error) {
	cleanup := func() {}
	if opts == nil {
		return nil, cleanup, fmt.Errorf("ffi: vector index options must not be nil")
	}
	out := &vectorIndexOptions{
		indexType:                   opts.IndexType,
		metric:                      opts.Metric,
		numPartitions:               newOptionalU64(opts.NumPartitions),
		targetPartitionSize:         newOptionalU64(opts.TargetPartitionSize),
		maxIters:                    newOptionalU64(opts.MaxIters),
		sampleRate:                  newOptionalU64(opts.SampleRate),
		streamingSampleRate:         newOptionalU64(opts.StreamingSampleRate),
		streamingCoresetRate:        newOptionalU64(opts.StreamingCoresetRate),
		streamingRefinePasses:       opts.StreamingRefinePasses,
		shufflePartitionBatches:     newOptionalU64(opts.ShufflePartitionBatches),
		shufflePartitionConcurrency: newOptionalU64(opts.ShufflePartitionConcurrency),
		numSubVectors:               newOptionalU64(opts.NumSubVectors),
		numBits:                     newOptionalU64(opts.NumBits),
		hnswMaxLevel:                newOptionalU64(opts.HNSWMaxLevel),
		hnswM:                       newOptionalU64(opts.HNSWM),
		hnswEFConstruction:          newOptionalU64(opts.HNSWEFConstruction),
		indexFileVersion:            opts.FileVersion,
		skipTranspose:               opts.SkipTranspose,
	}
	if opts.Train != nil {
		out.hasTrain = true
		out.train = *opts.Train
	}

	var keep []*cStringList
	var arrays []*cdata.ArrowArray
	var schemas []*cdata.ArrowSchema
	cleanup = func() {
		runtime.KeepAlive(keep)
		for _, arr := range arrays {
			cdata.ReleaseArray(arr)
		}
		for _, schema := range schemas {
			cdata.ReleaseSchema(schema)
		}
	}
	fail := func(err error) (*vectorIndexOptions, func(), error) {
		cleanup()
		return nil, func() {}, err
	}

	if opts.PrecomputedPartitionsFile != "" {
		list, err := newCStringList([]string{opts.PrecomputedPartitionsFile})
		if err != nil {
			return fail(err)
		}
		keep = append(keep, list)
		out.precomputedPartitionsFile = &list.bufs[0][0]
	}
	if opts.PrecomputedShuffleBuffers != nil {
		pathList, err := newCStringList([]string{opts.PrecomputedShuffleBuffers.Path})
		if err != nil {
			return fail(err)
		}
		keep = append(keep, pathList)
		raw := &precomputedShuffleBuffers{
			path:  &pathList.bufs[0][0],
			count: uint64(len(opts.PrecomputedShuffleBuffers.Buffers)),
		}
		if len(opts.PrecomputedShuffleBuffers.Buffers) > 0 {
			bufferList, err := newCStringList(opts.PrecomputedShuffleBuffers.Buffers)
			if err != nil {
				return fail(err)
			}
			keep = append(keep, bufferList)
			raw.buffers = &bufferList.ptrs[0]
		}
		out.precomputedShuffleBuffers = raw
	}
	if opts.StorageOptions != nil {
		mapping, lists, err := buildStringMap(opts.StorageOptions)
		if err != nil {
			return fail(err)
		}
		keep = append(keep, lists...)
		out.storageOptions = mapping
	}
	if opts.RabitQModelJSON != nil {
		list, err := newCStringList([]string{string(opts.RabitQModelJSON)})
		if err != nil {
			return fail(err)
		}
		keep = append(keep, list)
		out.rabitqModelJSON = &list.bufs[0][0]
	}
	if opts.RuntimeHints != nil {
		mapping, lists, err := buildStringMap(opts.RuntimeHints)
		if err != nil {
			return fail(err)
		}
		keep = append(keep, lists...)
		out.runtimeHints = mapping
	}
	if opts.IVFCentroids != nil {
		arr, schema, err := exportModel(opts.IVFCentroids)
		if err != nil {
			return fail(err)
		}
		arrays = append(arrays, arr)
		schemas = append(schemas, schema)
		out.ivfCentroids = arr
		out.ivfCentroidsSchema = schema
	}
	if opts.PQCodebook != nil {
		arr, schema, err := exportModel(opts.PQCodebook)
		if err != nil {
			return fail(err)
		}
		arrays = append(arrays, arr)
		schemas = append(schemas, schema)
		out.pqCodebook = arr
		out.pqCodebookSchema = schema
	}
	return out, cleanup, nil
}

func (d *Dataset) nativeHandle() (uintptr, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}
	return handle, nil
}

// CreateVectorIndex creates a vector index on the open dataset handle.
func (d *Dataset) CreateVectorIndex(column, name string, opts *VectorIndexOptions, replace bool) error {
	handle, err := d.nativeHandle()
	if err != nil {
		return err
	}
	raw, cleanup, err := buildVectorIndexOptions(opts)
	if err != nil {
		return err
	}
	defer cleanup()

	nameList, err := newOptionalCString(name)
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	rc := d.b.datasetCreateVectorIndexWithOptions(handle, column, cStringPointer(nameList), raw, replace)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	nameList.keepAlive()
	if rc != 0 {
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_create_vector_index_with_options failed without setting an error")
	}
	return nil
}

// CreateScalarIndex creates a scalar index. Empty params forwards NULL so the
// native plugin supplies defaults; non-empty params are forwarded verbatim.
func (d *Dataset) CreateScalarIndex(column, name string, indexType int32, params []byte, opts *ScalarIndexOptions, replace bool) error {
	handle, err := d.nativeHandle()
	if err != nil {
		return err
	}
	nameList, err := newOptionalCString(name)
	if err != nil {
		return err
	}
	var paramsList *cStringList
	if len(params) > 0 {
		paramsList, err = newCStringList([]string{string(params)})
		if err != nil {
			return err
		}
	}
	var raw *scalarIndexOptions
	if opts != nil && opts.Train != nil {
		raw = &scalarIndexOptions{hasTrain: true, train: *opts.Train}
	}
	runtime.LockOSThread()
	rc := d.b.datasetCreateScalarIndexWithOptions(handle, column, cStringPointer(nameList), indexType, cStringPointer(paramsList), raw, replace)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	nameList.keepAlive()
	paramsList.keepAlive()
	if rc != 0 {
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_create_scalar_index_with_options failed without setting an error")
	}
	return nil
}

// DropIndex drops a user index by name.
func (d *Dataset) DropIndex(name string) error {
	handle, err := d.nativeHandle()
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	rc := d.b.datasetDropIndex(handle, name)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_drop_index failed without setting an error")
	}
	return nil
}

// IndexCount returns the number of user indexes in the snapshot.
func (d *Dataset) IndexCount() (uint64, error) {
	handle, err := d.nativeHandle()
	if err != nil {
		return 0, err
	}
	runtime.LockOSThread()
	count := d.b.datasetIndexCount(handle)
	var nativeErr *Error
	if count == 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	if nativeErr != nil {
		return 0, nativeErr
	}
	return count, nil
}

func (d *Dataset) jsonString(op string, call func(uintptr) *byte) ([]byte, error) {
	handle, err := d.nativeHandle()
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	ptr := call(handle)
	var nativeErr *Error
	if ptr == nil {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	if ptr == nil {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: %s returned NULL without setting an error", op)
	}
	data := []byte(goString(ptr))
	d.b.freeString(ptr)
	return data, nil
}

// IndexListJSON returns the native user index list JSON.
func (d *Dataset) IndexListJSON() ([]byte, error) {
	return d.jsonString("lance_dataset_index_list_json", func(handle uintptr) *byte {
		return d.b.datasetIndexListJSON(handle)
	})
}

// DescribeIndicesJSON returns the native index description JSON.
func (d *Dataset) DescribeIndicesJSON() ([]byte, error) {
	return d.jsonString("lance_dataset_describe_indices_json", func(handle uintptr) *byte {
		return d.b.datasetDescribeIndicesJSON(handle)
	})
}

// IndexStatisticsJSON returns the native family-specific statistics JSON.
func (d *Dataset) IndexStatisticsJSON(name string) ([]byte, error) {
	return d.jsonString("lance_dataset_index_statistics_json", func(handle uintptr) *byte {
		return d.b.datasetIndexStatisticsJSON(handle, name)
	})
}

// IndexSegmentsJSON returns the native physical segment list JSON.
func (d *Dataset) IndexSegmentsJSON() ([]byte, error) {
	return d.jsonString("lance_dataset_index_segments_json", func(handle uintptr) *byte {
		return d.b.datasetIndexSegmentsJSON(handle)
	})
}

// IndexSegmentCount returns the number of physical segments for index name.
func (d *Dataset) IndexSegmentCount(name string) (uint64, error) {
	handle, err := d.nativeHandle()
	if err != nil {
		return 0, err
	}
	runtime.LockOSThread()
	count := d.b.datasetIndexSegmentCount(handle, name)
	var nativeErr *Error
	if count == 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	if nativeErr != nil {
		return 0, nativeErr
	}
	return count, nil
}

// IndexSegments returns the raw 16-byte UUIDs of index name's segments.
func (d *Dataset) IndexSegments(name string) ([]byte, error) {
	handle, err := d.nativeHandle()
	if err != nil {
		return nil, err
	}
	count, err := d.IndexSegmentCount(name)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return []byte{}, nil
	}
	uuids := make([]byte, count*16)
	var written uint64
	runtime.LockOSThread()
	rc := d.b.datasetIndexSegments(handle, name, uuids, uint64(len(uuids)/16), &written)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_index_segments failed without setting an error")
	}
	if written > uint64(len(uuids)/16) {
		return nil, fmt.Errorf("ffi: lance_dataset_index_segments wrote %d segments into capacity %d", written, len(uuids)/16)
	}
	return uuids[:written*16], nil
}

// PrewarmIndex warms index segments. A nil opts value uses the legacy
// all-segment call.
func (d *Dataset) PrewarmIndex(name string, opts *PrewarmOptions) error {
	handle, err := d.nativeHandle()
	if err != nil {
		return err
	}
	if opts == nil {
		runtime.LockOSThread()
		rc := d.b.datasetPrewarmIndex(handle, name)
		var nativeErr *Error
		if rc != 0 {
			nativeErr = d.b.takeError()
		}
		runtime.UnlockOSThread()
		if rc != 0 {
			if nativeErr != nil {
				return nativeErr
			}
			return fmt.Errorf("ffi: lance_dataset_prewarm_index failed without setting an error")
		}
		return nil
	}

	raw := &indexPrewarmOptions{
		withPosition:        opts.WithPosition,
		hasSegmentSelection: opts.HasSelection,
		segmentCount:        uint64(len(opts.SegmentUUIDs) / 16),
	}
	if len(opts.SegmentUUIDs) > 0 {
		raw.segmentUUIDs = &opts.SegmentUUIDs[0]
	}
	runtime.LockOSThread()
	rc := d.b.datasetPrewarmIndexWithOptions(handle, name, raw)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	runtime.KeepAlive(opts.SegmentUUIDs)
	if rc != 0 {
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_prewarm_index_with_options failed without setting an error")
	}
	return nil
}

func newOptionalCString(value string) (*cStringList, error) {
	if value == "" {
		return nil, nil
	}
	return newCStringList([]string{value})
}

func cStringPointer(list *cStringList) uintptr {
	if list == nil || len(list.ptrs) == 0 {
		return 0
	}
	return list.ptrs[0]
}
