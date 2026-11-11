package lance

import (
	"context"
	"fmt"

	"github.com/lance-format/lance-go/compaction"
	"github.com/lance-format/lance-go/index"
	"github.com/lance-format/lance-go/internal/ffi"
)

// OptimizeIndices runs native single-process index optimization and returns a
// new Dataset at the committed version. Native optimization plans, executes
// and merges index segments in turn before committing the merged results. d is
// not changed: its version, rows and open scanners keep the pre-optimization
// snapshot.
//
// A nil options value selects native defaults. A non-nil value starts from
// native defaults and applies the fields that are set.
//
// Native optimization is synchronous. ctx is checked before work starts; it is
// not observed once native work is in progress. If the commit succeeds after
// ctx is canceled, the committed Dataset is returned instead of an error that
// could be mistaken for an uncommitted optimization. A clean dataset is a
// native no-op and leaves the version unchanged.
func (d *Dataset) OptimizeIndices(ctx context.Context, options *compaction.OptimizeOptions) (*Dataset, error) {
	const op = "optimize indices"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateOptimizeOptions(options); err != nil {
		return nil, invalidError(op, d.uri, "%v", err)
	}

	return d.mutate(op, func(native *ffi.Dataset) error {
		return native.OptimizeIndices(options)
	})
}

// OptimizeIndicesPlan is an immutable snapshot of a native index-optimization
// plan. It carries the plan ID, read version, task and merge snapshots, and the
// native serialized plan JSON. A plan does not reserve files or publish
// anything.
//
// The native plan, task and merge JSON is authoritative across a transport
// boundary. Native JSON does not serialize OptimizeOptions, so transaction
// properties must be passed again to CommitOptimizeIndices.
type OptimizeIndicesPlan struct {
	uri         string
	planID      index.UUID
	readVersion uint64
	json        string
	tasks       []*OptimizeIndexTask
	merges      []*OptimizeIndexMerge
}

// PlanOptimizeIndices creates an index-optimization plan from the current
// dataset snapshot. It writes no files and publishes no version. options
// follows the same rules as OptimizeIndices.
func (d *Dataset) PlanOptimizeIndices(ctx context.Context, options *compaction.OptimizeOptions) (*OptimizeIndicesPlan, error) {
	const op = "plan optimize indices"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateOptimizeOptions(options); err != nil {
		return nil, invalidError(op, d.uri, "%v", err)
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()

	native, err := d.native.PlanOptimizeIndices(options)
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	defer native.Close()

	plan, err := snapshotOptimizeIndicesPlan(native)
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	plan.uri = d.uri
	for _, task := range plan.tasks {
		task.uri = d.uri
	}
	for _, merge := range plan.merges {
		merge.uri = d.uri
	}
	return plan, nil
}

// OptimizeIndicesPlanFromJSON restores an index-optimization plan snapshot from
// native serialized JSON. All task, merge and metadata snapshots are copied
// eagerly. OptimizeOptions are not part of the native JSON.
func OptimizeIndicesPlanFromJSON(data string) (*OptimizeIndicesPlan, error) {
	const op = "optimize indices plan from JSON"
	if err := validateCString(data); err != nil {
		return nil, invalidError(op, "", "JSON: %v", err)
	}

	native, err := ffi.OptimizeIndicesPlanFromJSON(data)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	defer native.Close()

	plan, err := snapshotOptimizeIndicesPlan(native)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return plan, nil
}

// PlanID returns the native plan identifier.
func (p *OptimizeIndicesPlan) PlanID() index.UUID {
	return p.planID
}

// ReadVersion returns the dataset version the plan was created from.
func (p *OptimizeIndicesPlan) ReadVersion() uint64 {
	return p.readVersion
}

// TaskCount returns the number of build tasks in the plan.
func (p *OptimizeIndicesPlan) TaskCount() uint64 {
	return uint64(len(p.tasks))
}

// Task returns one build task snapshot.
func (p *OptimizeIndicesPlan) Task(index uint64) (*OptimizeIndexTask, error) {
	if index >= uint64(len(p.tasks)) {
		return nil, invalidError("optimize indices plan task", p.uri,
			"index %d out of range [0,%d)", index, len(p.tasks))
	}
	return p.tasks[index], nil
}

// MergeCount returns the number of merge groups in the plan.
func (p *OptimizeIndicesPlan) MergeCount() uint64 {
	return uint64(len(p.merges))
}

// Merge returns one merge group snapshot.
func (p *OptimizeIndicesPlan) Merge(index uint64) (*OptimizeIndexMerge, error) {
	if index >= uint64(len(p.merges)) {
		return nil, invalidError("optimize indices plan merge", p.uri,
			"index %d out of range [0,%d)", index, len(p.merges))
	}
	return p.merges[index], nil
}

// JSON returns the native serialized plan JSON.
func (p *OptimizeIndicesPlan) JSON() string {
	return p.json
}

// OptimizeIndexTask is an immutable snapshot of one build task. It carries the
// task JSON, source fragment coverage, reference and old segment metadata
// copies, and the task's native merge options.
type OptimizeIndexTask struct {
	uri                        string
	planID                     index.UUID
	readVersion                uint64
	group                      uint32
	json                       string
	fragments                  []uint32
	reference                  []byte
	oldSegments                [][]byte
	numIndicesToMerge          compaction.Optional[uint64]
	enablePartitionRebalancing bool
	retrain                    bool
}

// OptimizeIndexTaskFromJSON restores a build task snapshot from native
// serialized JSON.
func OptimizeIndexTaskFromJSON(data string) (*OptimizeIndexTask, error) {
	const op = "optimize index task from JSON"
	if err := validateCString(data); err != nil {
		return nil, invalidError(op, "", "JSON: %v", err)
	}

	native, err := ffi.OptimizeIndexTaskFromJSON(data)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	defer native.Close()

	task, err := snapshotOptimizeIndexTask(native)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return task, nil
}

// PlanID returns the plan identifier this task belongs to.
func (t *OptimizeIndexTask) PlanID() index.UUID {
	return t.planID
}

// ReadVersion returns the dataset version the task reads.
func (t *OptimizeIndexTask) ReadVersion() uint64 {
	return t.readVersion
}

// Group returns the merge group this task feeds.
func (t *OptimizeIndexTask) Group() uint32 {
	return t.group
}

// FragmentCount returns the number of source fragments the task covers.
func (t *OptimizeIndexTask) FragmentCount() uint64 {
	return uint64(len(t.fragments))
}

// FragmentID returns one source fragment id.
func (t *OptimizeIndexTask) FragmentID(index uint64) (uint32, error) {
	if index >= uint64(len(t.fragments)) {
		return 0, invalidError("optimize index task fragment", t.uri,
			"index %d out of range [0,%d)", index, len(t.fragments))
	}
	return t.fragments[index], nil
}

// Reference returns an owned copy of the task's reference segment metadata.
// Close the metadata when it is no longer needed.
func (t *OptimizeIndexTask) Reference() (*IndexSegmentMetadata, error) {
	const op = "optimize index task reference"
	native, err := ffi.ParseSegmentMetadata(t.reference)
	if err != nil {
		return nil, wrapError(op, t.uri, err)
	}
	return &IndexSegmentMetadata{native: native}, nil
}

// OldSegmentCount returns the number of old index segments the task may fold
// into its output.
func (t *OptimizeIndexTask) OldSegmentCount() uint64 {
	return uint64(len(t.oldSegments))
}

// OldSegment returns an owned copy of one old segment's metadata. Close the
// metadata when it is no longer needed.
func (t *OptimizeIndexTask) OldSegment(index uint64) (*IndexSegmentMetadata, error) {
	if index >= uint64(len(t.oldSegments)) {
		return nil, invalidError("optimize index task old segment", t.uri,
			"index %d out of range [0,%d)", index, len(t.oldSegments))
	}
	native, err := ffi.ParseSegmentMetadata(t.oldSegments[index])
	if err != nil {
		return nil, wrapError("optimize index task old segment", t.uri, err)
	}
	return &IndexSegmentMetadata{native: native}, nil
}

// NumIndicesToMerge returns the task's optional merge limit.
func (t *OptimizeIndexTask) NumIndicesToMerge() compaction.Optional[uint64] {
	return t.numIndicesToMerge
}

// EnablePartitionRebalancing reports whether the task may rebalance partitions.
func (t *OptimizeIndexTask) EnablePartitionRebalancing() bool {
	return t.enablePartitionRebalancing
}

// Retrain reports whether the task retrains a vector index.
func (t *OptimizeIndexTask) Retrain() bool {
	return t.retrain
}

// JSON returns the native serialized task JSON.
func (t *OptimizeIndexTask) JSON() string {
	return t.json
}

// Execute runs the task against a dataset snapshot. It writes index files but
// publishes no dataset version. A nil result with a nil error means native
// optimization found no work for this task.
//
// ctx is checked before work starts; it is not observed once native execution
// is in progress.
func (t *OptimizeIndexTask) Execute(ctx context.Context, dataset *Dataset) (*IndexSegmentResult, error) {
	const op = "execute optimize index task"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if dataset == nil {
		return nil, invalidError(op, t.uri, "dataset must not be nil")
	}
	if err := dataset.acquire(op); err != nil {
		return nil, err
	}
	defer dataset.res.release()

	nativeTask, err := ffi.OptimizeIndexTaskFromJSON(t.json)
	if err != nil {
		return nil, wrapError(op, t.uri, err)
	}
	defer nativeTask.Close()

	nativeResult, err := nativeTask.Execute(dataset.native)
	if err != nil {
		return nil, wrapError(op, t.uri, err)
	}
	if nativeResult == nil {
		return nil, nil
	}
	defer nativeResult.Close()

	result, err := snapshotIndexSegmentResult(nativeResult)
	if err != nil {
		return nil, wrapError(op, t.uri, err)
	}
	result.uri = t.uri
	return result, nil
}

// OptimizeIndexMerge is an immutable snapshot of one merge group. Native merge
// execution validates the dataset version, plan ID and group against the
// supplied task results.
type OptimizeIndexMerge struct {
	uri         string
	planID      index.UUID
	readVersion uint64
	group       uint32
	json        string
	oldSegments [][]byte
	replaced    []index.UUID
}

// OptimizeIndexMergeFromJSON restores a merge snapshot from native serialized
// JSON.
func OptimizeIndexMergeFromJSON(data string) (*OptimizeIndexMerge, error) {
	const op = "optimize index merge from JSON"
	if err := validateCString(data); err != nil {
		return nil, invalidError(op, "", "JSON: %v", err)
	}

	native, err := ffi.OptimizeIndexMergeFromJSON(data)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	defer native.Close()

	merge, err := snapshotOptimizeIndexMerge(native)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return merge, nil
}

// PlanID returns the plan identifier this merge belongs to.
func (m *OptimizeIndexMerge) PlanID() index.UUID {
	return m.planID
}

// ReadVersion returns the dataset version the merge reads.
func (m *OptimizeIndexMerge) ReadVersion() uint64 {
	return m.readVersion
}

// Group returns the merge group.
func (m *OptimizeIndexMerge) Group() uint32 {
	return m.group
}

// OldSegmentCount returns the number of old index segments the merge folds into
// its output.
func (m *OptimizeIndexMerge) OldSegmentCount() uint64 {
	return uint64(len(m.oldSegments))
}

// OldSegment returns an owned copy of one old segment's metadata. Close the
// metadata when it is no longer needed.
func (m *OptimizeIndexMerge) OldSegment(index uint64) (*IndexSegmentMetadata, error) {
	if index >= uint64(len(m.oldSegments)) {
		return nil, invalidError("optimize index merge old segment", m.uri,
			"index %d out of range [0,%d)", index, len(m.oldSegments))
	}
	native, err := ffi.ParseSegmentMetadata(m.oldSegments[index])
	if err != nil {
		return nil, wrapError("optimize index merge old segment", m.uri, err)
	}
	return &IndexSegmentMetadata{native: native}, nil
}

// ReplaceCount returns the number of index segments the merge replaces.
func (m *OptimizeIndexMerge) ReplaceCount() uint64 {
	return uint64(len(m.replaced))
}

// ReplacedUUID returns one replaced segment's UUID.
func (m *OptimizeIndexMerge) ReplacedUUID(index uint64) (index.UUID, error) {
	if index >= uint64(len(m.replaced)) {
		return index.UUID{}, invalidError("optimize index merge replace", m.uri,
			"index %d out of range [0,%d)", index, len(m.replaced))
	}
	return m.replaced[index], nil
}

// JSON returns the native serialized merge JSON.
func (m *OptimizeIndexMerge) JSON() string {
	return m.json
}

// Execute runs the merge against a dataset snapshot and task results. It
// writes index files but publishes no dataset version. A nil result with a nil
// error means native optimization found no work for this group.
//
// Native validation remains authoritative: results from another plan, wrong
// groups and a dataset that cannot be read at the merge's read version are
// rejected.
func (m *OptimizeIndexMerge) Execute(ctx context.Context, dataset *Dataset, results []*IndexSegmentResult) (*IndexSegmentResult, error) {
	const op = "execute optimize index merge"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if dataset == nil {
		return nil, invalidError(op, m.uri, "dataset must not be nil")
	}
	for i, result := range results {
		if result == nil {
			return nil, invalidError(op, m.uri, "result %d is nil", i)
		}
		if result.json == "" {
			return nil, invalidError(op, m.uri, "result %d has no JSON", i)
		}
	}
	if err := dataset.acquire(op); err != nil {
		return nil, err
	}
	defer dataset.res.release()

	nativeMerge, err := ffi.OptimizeIndexMergeFromJSON(m.json)
	if err != nil {
		return nil, wrapError(op, m.uri, err)
	}
	defer nativeMerge.Close()

	nativeResults := make([]*ffi.IndexSegmentResult, len(results))
	for i, result := range results {
		nativeResult, err := ffi.IndexSegmentResultFromJSON(result.json)
		if err != nil {
			for _, opened := range nativeResults[:i] {
				opened.Close()
			}
			return nil, wrapError(op, m.uri, err)
		}
		nativeResults[i] = nativeResult
	}
	defer func() {
		for _, nativeResult := range nativeResults {
			nativeResult.Close()
		}
	}()

	nativeResult, err := nativeMerge.Execute(dataset.native, nativeResults)
	if err != nil {
		return nil, wrapError(op, m.uri, err)
	}
	if nativeResult == nil {
		return nil, nil
	}
	defer nativeResult.Close()

	result, err := snapshotIndexSegmentResult(nativeResult)
	if err != nil {
		return nil, wrapError(op, m.uri, err)
	}
	result.uri = m.uri
	return result, nil
}

// IndexSegmentResult is an immutable snapshot of one optimized index segment
// and the segments it replaces.
type IndexSegmentResult struct {
	uri         string
	planID      index.UUID
	readVersion uint64
	group       uint32
	json        string
	segment     []byte
	replaced    []index.UUID
}

// IndexSegmentResultFromJSON restores an index segment result snapshot from
// native serialized JSON.
func IndexSegmentResultFromJSON(data string) (*IndexSegmentResult, error) {
	const op = "index segment result from JSON"
	if err := validateCString(data); err != nil {
		return nil, invalidError(op, "", "JSON: %v", err)
	}

	native, err := ffi.IndexSegmentResultFromJSON(data)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	defer native.Close()

	result, err := snapshotIndexSegmentResult(native)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return result, nil
}

// PlanID returns the plan identifier this result belongs to.
func (r *IndexSegmentResult) PlanID() index.UUID {
	return r.planID
}

// ReadVersion returns the dataset version the result read.
func (r *IndexSegmentResult) ReadVersion() uint64 {
	return r.readVersion
}

// Group returns the merge group this result belongs to.
func (r *IndexSegmentResult) Group() uint32 {
	return r.group
}

// Segment returns an owned copy of the produced segment metadata. Close the
// metadata when it is no longer needed.
func (r *IndexSegmentResult) Segment() (*IndexSegmentMetadata, error) {
	native, err := ffi.ParseSegmentMetadata(r.segment)
	if err != nil {
		return nil, wrapError("index segment result segment", r.uri, err)
	}
	return &IndexSegmentMetadata{native: native}, nil
}

// ReplaceCount returns the number of index segments the result replaces.
func (r *IndexSegmentResult) ReplaceCount() uint64 {
	return uint64(len(r.replaced))
}

// ReplacedUUID returns one replaced segment's UUID.
func (r *IndexSegmentResult) ReplacedUUID(index uint64) (index.UUID, error) {
	if index >= uint64(len(r.replaced)) {
		return index.UUID{}, invalidError("index segment result replace", r.uri,
			"index %d out of range [0,%d)", index, len(r.replaced))
	}
	return r.replaced[index], nil
}

// JSON returns the native serialized result JSON.
func (r *IndexSegmentResult) JSON() string {
	return r.json
}

// CommitOptimizeIndices commits optimized index results and returns a new
// Dataset at the committed version. d is not changed: its version, rows and
// open scanners keep the pre-commit snapshot.
//
// Results may come from task or merge Execute, or from
// IndexSegmentResultFromJSON. Native commit semantics are preserved: empty
// results retain the native no-op/MemWAL behavior, and every result must have
// been planned from the same dataset version.
//
// Native plan, task, merge and result JSON does not serialize
// OptimizeOptions. Pass TransactionProperties again when the results crossed a
// JSON transport boundary.
//
// The native commit is synchronous. ctx is checked before work starts; it is
// not observed once the commit is in progress. If the commit succeeds after ctx
// is canceled, the committed Dataset is returned.
func (d *Dataset) CommitOptimizeIndices(ctx context.Context, results []*IndexSegmentResult, options *compaction.OptimizeOptions) (*Dataset, error) {
	const op = "commit optimize indices"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateOptimizeOptions(options); err != nil {
		return nil, invalidError(op, d.uri, "%v", err)
	}
	for i, result := range results {
		if result == nil {
			return nil, invalidError(op, d.uri, "result %d is nil", i)
		}
		if result.json == "" {
			return nil, invalidError(op, d.uri, "result %d has no JSON", i)
		}
	}

	return d.mutate(op, func(native *ffi.Dataset) error {
		nativeResults := make([]*ffi.IndexSegmentResult, len(results))
		for i, result := range results {
			nativeResult, err := ffi.IndexSegmentResultFromJSON(result.json)
			if err != nil {
				for _, opened := range nativeResults[:i] {
					opened.Close()
				}
				return err
			}
			nativeResults[i] = nativeResult
		}
		defer func() {
			for _, nativeResult := range nativeResults {
				nativeResult.Close()
			}
		}()

		return native.CommitOptimizeIndices(nativeResults, options)
	})
}

func snapshotOptimizeIndicesPlan(native *ffi.OptimizeIndicesPlan) (*OptimizeIndicesPlan, error) {
	planID, err := native.PlanID()
	if err != nil {
		return nil, err
	}
	readVersion, err := native.ReadVersion()
	if err != nil {
		return nil, err
	}
	data, err := native.JSON()
	if err != nil {
		return nil, err
	}
	taskCount, err := native.TaskCount()
	if err != nil {
		return nil, err
	}
	mergeCount, err := native.MergeCount()
	if err != nil {
		return nil, err
	}

	plan := &OptimizeIndicesPlan{
		planID:      index.UUID(planID),
		readVersion: readVersion,
		json:        data,
		tasks:       make([]*OptimizeIndexTask, 0, taskCount),
		merges:      make([]*OptimizeIndexMerge, 0, mergeCount),
	}
	for i := uint64(0); i < taskCount; i++ {
		nativeTask, err := native.TaskAt(i)
		if err != nil {
			return nil, err
		}
		task, err := snapshotOptimizeIndexTask(nativeTask)
		nativeTask.Close()
		if err != nil {
			return nil, err
		}
		plan.tasks = append(plan.tasks, task)
	}
	for i := uint64(0); i < mergeCount; i++ {
		nativeMerge, err := native.MergeAt(i)
		if err != nil {
			return nil, err
		}
		merge, err := snapshotOptimizeIndexMerge(nativeMerge)
		nativeMerge.Close()
		if err != nil {
			return nil, err
		}
		plan.merges = append(plan.merges, merge)
	}
	return plan, nil
}

func snapshotOptimizeIndexTask(native *ffi.OptimizeIndexTask) (*OptimizeIndexTask, error) {
	planID, err := native.PlanID()
	if err != nil {
		return nil, err
	}
	readVersion, err := native.ReadVersion()
	if err != nil {
		return nil, err
	}
	group, err := native.Group()
	if err != nil {
		return nil, err
	}
	fragmentCount, err := native.FragmentCount()
	if err != nil {
		return nil, err
	}
	fragments := make([]uint32, fragmentCount)
	for i := range fragments {
		fragments[i], err = native.FragmentIDAt(uint64(i))
		if err != nil {
			return nil, err
		}
	}
	reference, err := native.Reference()
	if err != nil {
		return nil, err
	}
	encodedReference, err := encodeOwnedSegmentMetadata(reference)
	if err != nil {
		return nil, err
	}
	oldSegmentCount, err := native.OldSegmentCount()
	if err != nil {
		return nil, err
	}
	oldSegments := make([][]byte, oldSegmentCount)
	for i := range oldSegments {
		metadata, err := native.OldSegmentAt(uint64(i))
		if err != nil {
			return nil, err
		}
		oldSegments[i], err = encodeOwnedSegmentMetadata(metadata)
		if err != nil {
			return nil, err
		}
	}
	numIndicesToMerge, err := native.NumIndicesToMerge()
	if err != nil {
		return nil, err
	}
	enablePartitionRebalancing, err := native.EnablePartitionRebalancing()
	if err != nil {
		return nil, err
	}
	retrain, err := native.Retrain()
	if err != nil {
		return nil, err
	}
	data, err := native.JSON()
	if err != nil {
		return nil, err
	}

	return &OptimizeIndexTask{
		planID:                     index.UUID(planID),
		readVersion:                readVersion,
		group:                      group,
		json:                       data,
		fragments:                  fragments,
		reference:                  encodedReference,
		oldSegments:                oldSegments,
		numIndicesToMerge:          numIndicesToMerge,
		enablePartitionRebalancing: enablePartitionRebalancing,
		retrain:                    retrain,
	}, nil
}

func snapshotOptimizeIndexMerge(native *ffi.OptimizeIndexMerge) (*OptimizeIndexMerge, error) {
	planID, err := native.PlanID()
	if err != nil {
		return nil, err
	}
	readVersion, err := native.ReadVersion()
	if err != nil {
		return nil, err
	}
	group, err := native.Group()
	if err != nil {
		return nil, err
	}
	oldSegmentCount, err := native.OldSegmentCount()
	if err != nil {
		return nil, err
	}
	oldSegments := make([][]byte, oldSegmentCount)
	for i := range oldSegments {
		metadata, err := native.OldSegmentAt(uint64(i))
		if err != nil {
			return nil, err
		}
		oldSegments[i], err = encodeOwnedSegmentMetadata(metadata)
		if err != nil {
			return nil, err
		}
	}
	replaceCount, err := native.ReplaceCount()
	if err != nil {
		return nil, err
	}
	replaced := make([]index.UUID, replaceCount)
	for i := range replaced {
		uuid, err := native.ReplaceUUIDAt(uint64(i))
		if err != nil {
			return nil, err
		}
		replaced[i] = index.UUID(uuid)
	}
	data, err := native.JSON()
	if err != nil {
		return nil, err
	}

	return &OptimizeIndexMerge{
		planID:      index.UUID(planID),
		readVersion: readVersion,
		group:       group,
		json:        data,
		oldSegments: oldSegments,
		replaced:    replaced,
	}, nil
}

func snapshotIndexSegmentResult(native *ffi.IndexSegmentResult) (*IndexSegmentResult, error) {
	planID, err := native.PlanID()
	if err != nil {
		return nil, err
	}
	readVersion, err := native.ReadVersion()
	if err != nil {
		return nil, err
	}
	group, err := native.Group()
	if err != nil {
		return nil, err
	}
	segment, err := native.Segment()
	if err != nil {
		return nil, err
	}
	encodedSegment, err := encodeOwnedSegmentMetadata(segment)
	if err != nil {
		return nil, err
	}
	replaceCount, err := native.ReplaceCount()
	if err != nil {
		return nil, err
	}
	replaced := make([]index.UUID, replaceCount)
	for i := range replaced {
		uuid, err := native.ReplaceUUIDAt(uint64(i))
		if err != nil {
			return nil, err
		}
		replaced[i] = index.UUID(uuid)
	}
	data, err := native.JSON()
	if err != nil {
		return nil, err
	}

	return &IndexSegmentResult{
		planID:      index.UUID(planID),
		readVersion: readVersion,
		group:       group,
		json:        data,
		segment:     encodedSegment,
		replaced:    replaced,
	}, nil
}

func encodeOwnedSegmentMetadata(metadata *ffi.SegmentMetadata) ([]byte, error) {
	if metadata == nil {
		return nil, fmt.Errorf("ffi: segment metadata is nil")
	}
	defer func() {
		_ = metadata.Close()
	}()
	return metadata.Encode()
}

func validateOptimizeOptions(options *compaction.OptimizeOptions) error {
	if options == nil {
		return nil
	}
	if options.IndexNames.IsSet() {
		if names, ok := options.IndexNames.Get(); ok {
			for i, name := range names {
				if err := validateCString(name); err != nil {
					return fmt.Errorf("index name %d: %w", i, err)
				}
			}
		}
	}
	for key, value := range options.TransactionProperties {
		if err := validateCString(key); err != nil {
			return fmt.Errorf("transaction property key: %w", err)
		}
		if err := validateCString(value); err != nil {
			return fmt.Errorf("transaction property %q: %w", key, err)
		}
	}
	return nil
}
