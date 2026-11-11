package lance

import (
	"context"
	"encoding/json"

	"github.com/lance-format/lance-go/index"
	"github.com/lance-format/lance-go/internal/ffi"
)

// Progress describes one advisory observation from an index build.
type Progress = index.Progress

// ProgressEvent identifies the kind of a progress observation.
type ProgressEvent = index.ProgressEvent

const (
	// ProgressStart begins a stage.
	ProgressStart = index.ProgressStart
	// ProgressAdvance reports completed work within a stage.
	ProgressAdvance = index.ProgressAdvance
	// ProgressComplete ends a stage.
	ProgressComplete = index.ProgressComplete
)

// SegmentBuilder builds one physical index segment without committing it. The
// resulting metadata bytes can be moved to another process, inspected, merged,
// or committed together with other segments through CommitIndexSegments.
//
// A builder owns a private snapshot of the dataset it was created from, so the
// originating Dataset may be closed before the builder executes. A builder
// executes at most once and must be closed when it is no longer needed.
type SegmentBuilder struct {
	uri    string
	native *ffi.SegmentBuilder
	res    *resource
}

func newSegmentBuilder(uri string, native *ffi.SegmentBuilder) *SegmentBuilder {
	return &SegmentBuilder{uri: uri, native: native, res: newResource(func() { _ = native.Close() })}
}

// SetProgress installs a progress observer for the next execution, replacing a
// previously installed observer. The observer runs on native worker threads and
// may be called concurrently; it must not block, call back into lance, or panic.
// A nil observer is rejected: builders without an observer simply skip these
// calls. Progress is advisory and never affects the build result.
func (b *SegmentBuilder) SetProgress(fn index.ProgressFunc) error {
	const op = "SegmentBuilder.SetProgress"
	if b == nil || b.res == nil {
		return invalidError(op, "", "segment builder is nil")
	}
	if !b.res.acquire() {
		return closedError(op, b.uri)
	}
	defer b.res.release()
	if err := b.native.SetProgress(adaptProgress(fn)); err != nil {
		return wrapError(op, b.uri, err)
	}
	return nil
}

// Execute builds the segment and returns its encoded index metadata. The bytes
// are a self-contained transport value: they can be written to disk, parsed
// with ParseIndexSegmentMetadata, or committed by any process that opens a
// dataset containing the segment files. Execute does not commit a dataset
// version, and no native work can be cancelled once it starts; a cancelled
// context is only observed before execution begins.
//
// Execute may be called once, and the builder stays consumed even when
// execution fails. A failed execution leaves the builder unusable; close it and
// build a new one.
func (b *SegmentBuilder) Execute(ctx context.Context) ([]byte, error) {
	const op = "SegmentBuilder.Execute"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if b == nil || b.res == nil {
		return nil, invalidError(op, "", "segment builder is nil")
	}
	if !b.res.acquire() {
		return nil, closedError(op, b.uri)
	}
	defer b.res.release()
	data, err := b.native.Execute()
	if err != nil {
		return nil, wrapError(op, b.uri, err)
	}
	return data, nil
}

// Close releases the builder. It waits for an in-flight Execute or SetProgress
// to finish and is safe to call more than once.
func (b *SegmentBuilder) Close() error {
	const op = "SegmentBuilder.Close"
	if b == nil || b.res == nil {
		return invalidError(op, "", "segment builder is nil")
	}
	b.res.close()
	return nil
}

// NewVectorSegmentBuilder prepares a vector index segment for the current
// dataset snapshot. options describe the final index (type, metric, and
// training inputs); scope restricts the segment to a fragment subset and
// optionally assigns its index UUID.
//
// The builder reuses trained IVF centroids and PQ codebooks carried by options
// and trains any missing model locally; options.Train defaults to true, matching
// the native default. Set Train false to require the complete supplied model set
// and skip training. Training with supplied PQ codebooks and the dot metric
// requires the scope to cover every fragment, while Train false accepts fragment
// subsets at this native revision.
func (d *Dataset) NewVectorSegmentBuilder(ctx context.Context, column, name string, options index.VectorOptions, scope index.SegmentScope) (*SegmentBuilder, error) {
	const op = "Dataset.NewVectorSegmentBuilder"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()
	if err := validateCString(column); err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	if err := validateCString(name); err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	native, err := d.native.NewVectorSegmentBuilder(column, name, ffiVectorIndexOptions(options), ffiSegmentSelection(scope))
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	return newSegmentBuilder(d.uri, native), nil
}

// NewScalarSegmentBuilder prepares a scalar index segment such as an inverted
// or bitmap segment for the current dataset snapshot.
func (d *Dataset) NewScalarSegmentBuilder(ctx context.Context, column, name string, indexType index.ScalarType, params json.RawMessage, options index.ScalarOptions, scope index.SegmentScope) (*SegmentBuilder, error) {
	const op = "Dataset.NewScalarSegmentBuilder"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()
	if err := validateCString(column); err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	if err := validateCString(name); err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	native, err := d.native.NewScalarSegmentBuilder(column, name, int32(indexType), params, ffiScalarOptions(options), ffiSegmentSelection(scope))
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	return newSegmentBuilder(d.uri, native), nil
}

// CommitIndexSegments commits assembled segment metadata as one new dataset
// version and returns the dataset at that version. The receiver keeps its
// snapshot. Native validation owns segment compatibility, coverage, and
// replacement rules, so an invalid set is rejected without changing a version.
func (d *Dataset) CommitIndexSegments(ctx context.Context, indexName, column string, segments [][]byte) (*Dataset, error) {
	const op = "Dataset.CommitIndexSegments"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateCString(indexName); err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	if err := validateCString(column); err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	if len(segments) == 0 {
		return nil, invalidError(op, d.uri, "at least one index segment is required")
	}
	return d.mutate(op, func(clone *ffi.Dataset) error {
		return clone.CommitIndexSegments(indexName, column, segments)
	})
}

// MergeIndexSegments merges uncommitted segment metadata into one uncommitted
// metadata value. Native validation owns compatibility, replay, staleness, and
// coverage. The receiver snapshot is not modified and the merged metadata still
// has to be committed.
func (d *Dataset) MergeIndexSegments(ctx context.Context, segments [][]byte) ([]byte, error) {
	const op = "Dataset.MergeIndexSegments"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()
	merged, err := d.native.MergeIndexSegments(segments)
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	return merged, nil
}

// MergeIndexMetadata writes and merges an index's root artifact without
// committing a dataset version. At this native revision only the inverted root
// artifact is mergeable; other index types fail with a native error. A failure
// may leave partially written files behind, so retrying the same merge is the
// intended recovery. Callers publish the result with their own index creation
// path once every worker has merged its portion.
func (d *Dataset) MergeIndexMetadata(ctx context.Context, uuid index.UUID, indexType string, options index.MergeMetadataOptions) error {
	const op = "Dataset.MergeIndexMetadata"
	if err := checkContext(ctx); err != nil {
		return err
	}
	if indexType == "" {
		return invalidError(op, d.uri, "indexType must not be empty")
	}
	if err := validateCString(indexType); err != nil {
		return wrapError(op, d.uri, err)
	}
	if err := d.acquire(op); err != nil {
		return err
	}
	defer d.res.release()
	if err := d.native.MergeIndexMetadata([16]byte(uuid), indexType, options.BatchReadhead, adaptProgress(options.Progress)); err != nil {
		return wrapError(op, d.uri, err)
	}
	return nil
}

func ffiSegmentSelection(scope index.SegmentScope) ffi.SegmentSelection {
	selection := ffi.SegmentSelection{FragmentIDs: scope.FragmentIDs}
	if scope.UUID != nil {
		uuid := [16]byte(*scope.UUID)
		selection.UUID = &uuid
	}
	return selection
}

func ffiScalarOptions(options index.ScalarOptions) *ffi.ScalarIndexOptions {
	if options.Train == nil {
		return nil
	}
	return &ffi.ScalarIndexOptions{Train: options.Train}
}

func adaptProgress(fn index.ProgressFunc) func(ffi.ProgressObservation) {
	if fn == nil {
		return nil
	}
	return func(observation ffi.ProgressObservation) {
		fn(index.Progress{
			Event:     index.ProgressEvent(observation.Event),
			Stage:     observation.Stage,
			Total:     observation.Total,
			Unit:      observation.Unit,
			Completed: observation.Completed,
		})
	}
}
