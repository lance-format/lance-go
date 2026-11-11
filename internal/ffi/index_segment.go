package ffi

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
	"github.com/lance-format/lance-go/internal/handle"
)

// emptyFragmentID provides a stable non-NULL pointer for an explicitly empty
// fragment selection. Native parsers map non-NULL plus zero count to an explicit
// empty coverage, while a zero-length Go slice has no guaranteed data pointer.
var emptyFragmentID uint32

// maxInt bounds a native byte count before it is converted to an int.
const maxInt = int(^uint(0) >> 1)

// SegmentSelection selects which fragments an index segment covers and
// optionally fixes its identity. FragmentIDs uses the native three-way
// convention: nil means the whole dataset, while a non-nil empty slice means an
// explicit empty coverage.
type SegmentSelection struct {
	FragmentIDs []uint32
	UUID        *[16]byte
}

// segmentBuildOptions mirrors LanceIndexSegmentBuildOptions.
type segmentBuildOptions struct {
	fragmentIDs        *uint32
	fragmentCount      uint64
	indexUUID          *byte
	ivfCentroids       *cdata.ArrowArray
	ivfCentroidsSchema *cdata.ArrowSchema
	pqCodebook         *cdata.ArrowArray
	pqCodebookSchema   *cdata.ArrowSchema
	mode               int32
}

// segmentScope mirrors LanceVectorIndexSegmentScope.
type segmentScope struct {
	fragmentIDs   *uint32
	fragmentCount uint64
	indexUUID     *byte
}

// buildSegmentOptions copies the selection into the native layout. The UUID is
// copied into a local array whose address escapes through the returned options,
// so the native call never depends on the caller's storage staying put.
func buildSegmentOptions(selection SegmentSelection) (*segmentBuildOptions, func()) {
	options := &segmentBuildOptions{}
	switch {
	case selection.FragmentIDs == nil:
	case len(selection.FragmentIDs) == 0:
		options.fragmentIDs = &emptyFragmentID
	default:
		options.fragmentIDs = &selection.FragmentIDs[0]
		options.fragmentCount = uint64(len(selection.FragmentIDs))
	}
	var uuid [16]byte
	if selection.UUID != nil {
		uuid = *selection.UUID
		options.indexUUID = &uuid[0]
	}
	return options, func() {
		runtime.KeepAlive(selection.FragmentIDs)
		runtime.KeepAlive(&uuid)
	}
}

func buildVectorScope(selection SegmentSelection) (*segmentScope, func()) {
	scope := &segmentScope{}
	switch {
	case selection.FragmentIDs == nil:
	case len(selection.FragmentIDs) == 0:
		scope.fragmentIDs = &emptyFragmentID
	default:
		scope.fragmentIDs = &selection.FragmentIDs[0]
		scope.fragmentCount = uint64(len(selection.FragmentIDs))
	}
	var uuid [16]byte
	if selection.UUID != nil {
		uuid = *selection.UUID
		scope.indexUUID = &uuid[0]
	}
	return scope, func() {
		runtime.KeepAlive(selection.FragmentIDs)
		runtime.KeepAlive(&uuid)
	}
}

// SegmentBuilder owns one native uncommitted index segment builder. Builders
// are single-use and must be closed.
type SegmentBuilder struct {
	b        *bindings
	handle   atomic.Uintptr
	mu       sync.Mutex
	executed bool
	progress *progressState
	// progressID keeps the observer reachable from the native callback. Handle
	// identifiers are never reused, so a stale registration cannot observe a
	// different build.
	progressID uint64
}

func (d *Dataset) newSegmentBuilder(op string, created uintptr, nativeErr *Error) (*SegmentBuilder, error) {
	if created == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: %s failed without setting an error", op)
	}
	builder := &SegmentBuilder{b: d.b}
	builder.handle.Store(created)
	return builder, nil
}

// NewScalarSegmentBuilder creates a scalar segment builder. A non-nil options
// value forwards its train flag; params is the plugin-specific JSON options
// blob, with nil meaning the native default.
func (d *Dataset) NewScalarSegmentBuilder(column, name string, indexType int32, params []byte, options *ScalarIndexOptions, selection SegmentSelection) (*SegmentBuilder, error) {
	dataset, err := d.nativeHandle()
	if err != nil {
		return nil, err
	}
	if column == "" {
		return nil, invalidf("ffi: index column must not be empty")
	}
	buildOptions, keepBuildOptions := buildSegmentOptions(selection)
	defer keepBuildOptions()

	var paramsList *cStringList
	if len(params) > 0 {
		paramsList, err = newCStringList([]string{string(params)})
		if err != nil {
			return nil, err
		}
		defer paramsList.keepAlive()
	}
	nameList, err := newOptionalCString(name)
	if err != nil {
		return nil, err
	}
	defer nameList.keepAlive()

	var scalarOptions *scalarIndexOptions
	if options != nil && options.Train != nil {
		scalarOptions = &scalarIndexOptions{hasTrain: true, train: *options.Train}
	}

	runtime.LockOSThread()
	var created uintptr
	if scalarOptions == nil {
		created = d.b.segmentBuilderNewScalar(dataset, column, cStringPointer(nameList), indexType, cStringPointer(paramsList), buildOptions)
	} else {
		created = d.b.segmentBuilderNewScalarWithOptions(dataset, column, cStringPointer(nameList), indexType, cStringPointer(paramsList), scalarOptions, buildOptions)
	}
	var nativeErr *Error
	if created == 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	return d.newSegmentBuilder("lance_index_segment_builder_new_scalar", created, nativeErr)
}

// NewVectorSegmentBuilder creates a vector segment builder. The builder takes
// its own dataset snapshot, so the dataset handle does not need to outlive it.
func (d *Dataset) NewVectorSegmentBuilder(column, name string, options *VectorIndexOptions, selection SegmentSelection) (*SegmentBuilder, error) {
	dataset, err := d.nativeHandle()
	if err != nil {
		return nil, err
	}
	if column == "" {
		return nil, invalidf("ffi: index column must not be empty")
	}
	raw, cleanup, err := buildVectorIndexOptions(options)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	scope, keepScope := buildVectorScope(selection)
	defer keepScope()
	nameList, err := newOptionalCString(name)
	if err != nil {
		return nil, err
	}
	defer nameList.keepAlive()

	runtime.LockOSThread()
	created := d.b.segmentBuilderNewVectorWithOptions(dataset, column, cStringPointer(nameList), raw, scope)
	var nativeErr *Error
	if created == 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	return d.newSegmentBuilder("lance_index_segment_builder_new_vector_with_options", created, nativeErr)
}

// SetProgress installs or replaces the progress observer. The callback must be
// installed before execution; its observer remains registered until Execute
// returns or the builder is closed.
func (b *SegmentBuilder) SetProgress(fn func(ProgressObservation)) error {
	if fn == nil {
		return invalidf("ffi: index build progress callback must not be nil")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	dataset, err := b.nativeHandle()
	if err != nil {
		return err
	}
	if b.executed {
		return invalidf("ffi: index segment builder has already been executed")
	}

	first := b.progress == nil
	state := b.progress
	id := b.progressID
	if first {
		state = &progressState{}
		id = handle.New(state)
	}
	runtime.LockOSThread()
	rc := b.b.segmentBuilderSetProgressCallback(dataset, progressCallback(), uintptr(id))
	var nativeErr *Error
	if rc != 0 {
		nativeErr = b.b.takeError()
	}
	runtime.UnlockOSThread()
	if rc != 0 {
		if first {
			handle.Remove(id)
		}
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_index_segment_builder_set_progress_callback failed without setting an error")
	}
	// The observer becomes visible only after the native call accepted the
	// registration, so a rejected replacement leaves the previous observer in
	// effect.
	state.set(fn)
	if first {
		b.progress = state
		b.progressID = id
	}
	return nil
}

// Execute builds and returns the encoded IndexMetadata protobuf for one
// uncommitted segment. A builder can be executed at most once.
func (b *SegmentBuilder) Execute() ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	dataset, err := b.nativeHandle()
	if err != nil {
		return nil, err
	}
	if b.executed {
		return nil, invalidf("ffi: index segment builder is single-use and has already been executed")
	}
	b.executed = true
	if b.progressID != 0 {
		// Native drains every in-flight callback before Execute returns, so the
		// observer can be unregistered immediately afterwards.
		defer func() {
			handle.Remove(b.progressID)
			b.progressID = 0
		}()
	}

	var data *byte
	var length uint64
	runtime.LockOSThread()
	rc := b.b.segmentBuilderExecuteUncommitted(dataset, &data, &length)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = b.b.takeError()
	}
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_index_segment_builder_execute_uncommitted failed without setting an error")
	}
	if data == nil || length == 0 {
		if data != nil {
			b.b.freeBytes(data)
		}
		return nil, fmt.Errorf("ffi: lance_index_segment_builder_execute_uncommitted returned empty segment metadata")
	}
	if length > uint64(maxInt) {
		b.b.freeBytes(data)
		return nil, fmt.Errorf("ffi: index segment metadata length %d exceeds the addressable size", length)
	}
	encoded := append([]byte(nil), unsafe.Slice(data, int(length))...)
	b.b.freeBytes(data)
	return encoded, nil
}

// Close releases the native builder. It is idempotent and waits for a running
// Execute to return.
func (b *SegmentBuilder) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	native := b.handle.Swap(0)
	if b.progressID != 0 {
		handle.Remove(b.progressID)
		b.progressID = 0
		b.progress = nil
	}
	if native == 0 {
		return nil
	}
	b.b.segmentBuilderFree(native)
	return nil
}

func (b *SegmentBuilder) nativeHandle() (uintptr, error) {
	dataset := b.handle.Load()
	if dataset == 0 {
		return 0, ErrClosed
	}
	return dataset, nil
}

// TrainerOptions mirrors the additive standalone trainer controls. A nil field
// keeps the native default.
type TrainerOptions struct {
	SampleRate *uint64
	MaxIters   *uint64
}

// trainerOptions mirrors LanceIndexTrainerOptions.
type trainerOptions struct {
	sampleRate optionalU64
	maxIters   optionalU64
}

func buildTrainerOptions(options *TrainerOptions) *trainerOptions {
	if options == nil || (options.SampleRate == nil && options.MaxIters == nil) {
		return nil
	}
	return &trainerOptions{
		sampleRate: newOptionalU64(options.SampleRate),
		maxIters:   newOptionalU64(options.MaxIters),
	}
}

// trainerFragments converts a fragment selection into its native argument pair.
// Native trainers reject a non-NULL pointer with a zero count, so an explicitly
// empty Go selection is rejected here instead of being silently reinterpreted
// as the whole dataset.
func trainerFragments(fragmentIDs []uint32) (*uint32, func(), error) {
	if fragmentIDs == nil {
		return nil, func() {}, nil
	}
	if len(fragmentIDs) == 0 {
		return nil, func() {}, invalidf("ffi: trainer fragment IDs must be nil for the whole dataset or non-empty")
	}
	return &fragmentIDs[0], func() { runtime.KeepAlive(fragmentIDs) }, nil
}

// TrainIVFModel exports a trained IVF centroids model. The returned array is
// owned by the caller and must be released.
func (d *Dataset) TrainIVFModel(column string, numPartitions uint32, metric int32, fragmentIDs []uint32, options *TrainerOptions) (arrow.Array, arrow.Field, error) {
	dataset, err := d.nativeHandle()
	if err != nil {
		return nil, arrow.Field{}, err
	}
	if column == "" {
		return nil, arrow.Field{}, invalidf("ffi: index column must not be empty")
	}
	fragments, keepFragments, err := trainerFragments(fragmentIDs)
	if err != nil {
		return nil, arrow.Field{}, err
	}
	defer keepFragments()
	raw := buildTrainerOptions(options)

	var output cdata.ArrowArray
	var outputSchema cdata.ArrowSchema
	runtime.LockOSThread()
	var rc int32
	if raw == nil {
		rc = d.b.indexTrainIVFModel(dataset, column, numPartitions, metric, fragments, uint64(len(fragmentIDs)), &output, &outputSchema)
	} else {
		rc = d.b.indexTrainIVFModelWithOptions(dataset, column, numPartitions, metric, fragments, uint64(len(fragmentIDs)), raw, &output, &outputSchema)
	}
	var nativeErr *Error
	if rc != 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	if rc != 0 {
		cdata.ReleaseArray(&output)
		cdata.ReleaseSchema(&outputSchema)
		if nativeErr != nil {
			return nil, arrow.Field{}, nativeErr
		}
		return nil, arrow.Field{}, fmt.Errorf("ffi: lance_index_train_ivf_model failed without setting an error")
	}
	return importModelArray(&output, &outputSchema)
}

// TrainPQModel exports a trained PQ codebook conditioned on the given IVF
// centroids. Both the centroids and the returned array remain caller-owned; the
// native trainer borrows the centroid array synchronously.
func (d *Dataset) TrainPQModel(column string, numSubVectors, numBits uint32, metric int32, ivfCentroids arrow.Array, fragmentIDs []uint32, options *TrainerOptions) (arrow.Array, arrow.Field, error) {
	dataset, err := d.nativeHandle()
	if err != nil {
		return nil, arrow.Field{}, err
	}
	if column == "" {
		return nil, arrow.Field{}, invalidf("ffi: index column must not be empty")
	}
	if ivfCentroids == nil {
		return nil, arrow.Field{}, invalidf("ffi: PQ training requires IVF centroid models")
	}
	fragments, keepFragments, err := trainerFragments(fragmentIDs)
	if err != nil {
		return nil, arrow.Field{}, err
	}
	defer keepFragments()
	centroids, centroidsSchema, err := exportModel(ivfCentroids)
	if err != nil {
		return nil, arrow.Field{}, err
	}
	defer func() {
		cdata.ReleaseArray(centroids)
		cdata.ReleaseSchema(centroidsSchema)
	}()
	raw := buildTrainerOptions(options)

	var output cdata.ArrowArray
	var outputSchema cdata.ArrowSchema
	runtime.LockOSThread()
	var rc int32
	if raw == nil {
		rc = d.b.indexTrainPQModel(dataset, column, numSubVectors, numBits, metric, fragments, uint64(len(fragmentIDs)), centroids, centroidsSchema, &output, &outputSchema)
	} else {
		rc = d.b.indexTrainPQModelWithOptions(dataset, column, numSubVectors, numBits, metric, fragments, uint64(len(fragmentIDs)), centroids, centroidsSchema, raw, &output, &outputSchema)
	}
	var nativeErr *Error
	if rc != 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	if rc != 0 {
		cdata.ReleaseArray(&output)
		cdata.ReleaseSchema(&outputSchema)
		if nativeErr != nil {
			return nil, arrow.Field{}, nativeErr
		}
		return nil, arrow.Field{}, fmt.Errorf("ffi: lance_index_train_pq_model failed without setting an error")
	}
	return importModelArray(&output, &outputSchema)
}

// BuildRQModel returns the library JSON for an IVF-RQ rotation model.
func BuildRQModel(dimension, numBits uint32) ([]byte, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	model := b.indexBuildRQModel(dimension, numBits)
	var nativeErr *Error
	if model == nil {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()
	if model == nil {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_index_build_rq_model failed without setting an error")
	}
	encoded := []byte(goString(model))
	b.freeString(model)
	return encoded, nil
}

// importModelArray takes ownership of one exported model array. The schema is
// borrowed while the data type is imported, then released exactly once; the
// array is consumed by ImportArray, which also releases it on failure.
func importModelArray(array *cdata.ArrowArray, schema *cdata.ArrowSchema) (arrow.Array, arrow.Field, error) {
	imported, err := cdata.ImportSchema(schema)
	cdata.ReleaseSchema(schema)
	if err != nil {
		cdata.ReleaseArray(array)
		return nil, arrow.Field{}, err
	}
	if imported == nil || imported.NumFields() != 1 {
		cdata.ReleaseArray(array)
		return nil, arrow.Field{}, fmt.Errorf("ffi: trained model schema must contain exactly one field")
	}
	field := imported.Field(0)
	value, err := cdata.ImportArray(array, field.Type)
	if err != nil {
		return nil, arrow.Field{}, err
	}
	return value, field, nil
}

// CommitIndexSegments commits encoded segment metadata to the open dataset in
// one version. The native call mutates the handle in place.
func (d *Dataset) CommitIndexSegments(indexName, column string, segments [][]byte) error {
	dataset, err := d.nativeHandle()
	if err != nil {
		return err
	}
	if indexName == "" {
		return invalidf("ffi: index name must not be empty")
	}
	if column == "" {
		return invalidf("ffi: index column must not be empty")
	}
	if len(segments) == 0 {
		return invalidf("ffi: at least one index segment is required")
	}
	pointers := make([]*byte, len(segments))
	lengths := make([]uint64, len(segments))
	for i, segment := range segments {
		if len(segment) == 0 {
			return invalidf("ffi: index segment %d has empty metadata", i)
		}
		pointers[i] = &segment[0]
		lengths[i] = uint64(len(segment))
	}
	runtime.LockOSThread()
	rc := d.b.datasetCommitIndexSegments(dataset, indexName, column, pointers, lengths, uint64(len(segments)))
	var nativeErr *Error
	if rc != 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	runtime.KeepAlive(segments)
	if rc != 0 {
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_commit_index_segments failed without setting an error")
	}
	return nil
}

// MergeIndexSegments merges encoded uncommitted segment metadata without
// committing it. An empty set is forwarded so native validation decides.
func (d *Dataset) MergeIndexSegments(segments [][]byte) ([]byte, error) {
	dataset, err := d.nativeHandle()
	if err != nil {
		return nil, err
	}
	pointers := make([]*byte, len(segments))
	lengths := make([]uint64, len(segments))
	for i, segment := range segments {
		if len(segment) == 0 {
			return nil, invalidf("ffi: index segment %d has empty metadata", i)
		}
		pointers[i] = &segment[0]
		lengths[i] = uint64(len(segment))
	}
	var data *byte
	var length uint64
	runtime.LockOSThread()
	rc := d.b.datasetMergeIndexSegments(dataset, pointers, lengths, uint64(len(segments)), &data, &length)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	runtime.KeepAlive(segments)
	if rc != 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_merge_index_segments failed without setting an error")
	}
	if data == nil || length == 0 {
		if data != nil {
			d.b.freeBytes(data)
		}
		return nil, fmt.Errorf("ffi: lance_dataset_merge_index_segments returned empty metadata")
	}
	if length > uint64(maxInt) {
		d.b.freeBytes(data)
		return nil, fmt.Errorf("ffi: merged index metadata length %d exceeds the addressable size", length)
	}
	merged := append([]byte(nil), unsafe.Slice(data, int(length))...)
	d.b.freeBytes(data)
	return merged, nil
}

// MergeIndexMetadata writes or merges the root artifact of one index. It does
// not commit a dataset version; publication requires a separate index
// creation.
func (d *Dataset) MergeIndexMetadata(uuid [16]byte, indexType string, batchReadhead *uint64, progress func(ProgressObservation)) error {
	dataset, err := d.nativeHandle()
	if err != nil {
		return err
	}
	if indexType == "" {
		return invalidf("ffi: index type must not be empty")
	}
	var state *progressState
	var id uint64
	var callback uintptr
	if progress != nil {
		state = &progressState{}
		state.set(progress)
		id = handle.New(state)
		defer handle.Remove(id)
		callback = progressCallback()
	}
	var readhead uint64
	if batchReadhead != nil {
		readhead = *batchReadhead
	}
	runtime.LockOSThread()
	rc := d.b.datasetMergeIndexMetadata(dataset, &uuid[0], indexType, batchReadhead != nil, readhead, callback, uintptr(id))
	var nativeErr *Error
	if rc != 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	runtime.KeepAlive(state)
	if rc != 0 {
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_merge_index_metadata failed without setting an error")
	}
	return nil
}
