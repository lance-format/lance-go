package ffi

import (
	"fmt"
	"runtime"

	"github.com/lance-format/lance-go/compaction"
)

// OptimizeIndicesPlan is an opaque native LanceOptimizeIndicesPlan handle.
type OptimizeIndicesPlan struct {
	b      *bindings
	handle uintptr
}

// Close releases the native plan handle. It is not safe for concurrent use.
func (p *OptimizeIndicesPlan) Close() {
	if p == nil {
		return
	}
	handle := p.handle
	if handle == 0 {
		return
	}
	p.handle = 0

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b.optimizeIndicesPlanClose(handle)
}

// PlanID returns the native plan identifier.
func (p *OptimizeIndicesPlan) PlanID() ([16]byte, error) {
	handle := p.handle
	if handle == 0 {
		return [16]byte{}, ErrClosed
	}

	var uuid [16]byte
	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.optimizeIndicesPlanPlanID(handle, &uuid[0]); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return [16]byte{}, nativeErr
		}
		return [16]byte{}, fmt.Errorf("ffi: lance_optimize_indices_plan_plan_id failed without setting an error")
	}
	return uuid, nil
}

// ReadVersion returns the dataset version the plan was created from.
func (p *OptimizeIndicesPlan) ReadVersion() (uint64, error) {
	handle := p.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	version := b.optimizeIndicesPlanReadVersion(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return version, nil
}

// TaskCount returns the number of tasks in the plan.
func (p *OptimizeIndicesPlan) TaskCount() (uint64, error) {
	handle := p.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	count := b.optimizeIndicesPlanTaskCount(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return count, nil
}

// MergeCount returns the number of merges in the plan.
func (p *OptimizeIndicesPlan) MergeCount() (uint64, error) {
	handle := p.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	count := b.optimizeIndicesPlanMergeCount(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return count, nil
}

// TaskAt returns an owned snapshot of one plan task.
func (p *OptimizeIndicesPlan) TaskAt(index uint64) (*OptimizeIndexTask, error) {
	handle := p.handle
	if handle == 0 {
		return nil, ErrClosed
	}

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	task := b.optimizeIndicesPlanTaskAt(handle, index)
	if task == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_indices_plan_task_at returned NULL without setting an error")
	}
	return &OptimizeIndexTask{b: b, handle: task}, nil
}

// MergeAt returns an owned snapshot of one plan merge.
func (p *OptimizeIndicesPlan) MergeAt(index uint64) (*OptimizeIndexMerge, error) {
	handle := p.handle
	if handle == 0 {
		return nil, ErrClosed
	}

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	merge := b.optimizeIndicesPlanMergeAt(handle, index)
	if merge == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_indices_plan_merge_at returned NULL without setting an error")
	}
	return &OptimizeIndexMerge{b: b, handle: merge}, nil
}

// JSON returns the plan's native serialized JSON.
func (p *OptimizeIndicesPlan) JSON() (string, error) {
	handle := p.handle
	if handle == 0 {
		return "", ErrClosed
	}

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ptr := b.optimizeIndicesPlanJSON(handle)
	if ptr == nil {
		if nativeErr := b.takeError(); nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_optimize_indices_plan_json returned NULL without setting an error")
	}
	value := goString(ptr)
	b.freeString(ptr)
	return value, nil
}

// OptimizeIndicesPlanFromJSON restores a native plan from serialized JSON.
func OptimizeIndicesPlanFromJSON(data string) (*OptimizeIndicesPlan, error) {
	if err := validateCString(data); err != nil {
		return nil, err
	}
	b, err := load()
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	plan := b.optimizeIndicesPlanFromJSON(data)
	if plan == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_indices_plan_from_json returned NULL without setting an error")
	}
	return &OptimizeIndicesPlan{b: b, handle: plan}, nil
}

// OptimizeIndexTask is an opaque native LanceOptimizeIndexTask handle.
type OptimizeIndexTask struct {
	b      *bindings
	handle uintptr
}

// Close releases the native task handle. It is not safe for concurrent use.
func (t *OptimizeIndexTask) Close() {
	if t == nil {
		return
	}
	handle := t.handle
	if handle == 0 {
		return
	}
	t.handle = 0

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b.optimizeIndexTaskClose(handle)
}

// PlanID returns the native plan identifier for this task.
func (t *OptimizeIndexTask) PlanID() ([16]byte, error) {
	handle := t.handle
	if handle == 0 {
		return [16]byte{}, ErrClosed
	}

	var uuid [16]byte
	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.optimizeIndexTaskPlanID(handle, &uuid[0]); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return [16]byte{}, nativeErr
		}
		return [16]byte{}, fmt.Errorf("ffi: lance_optimize_index_task_plan_id failed without setting an error")
	}
	return uuid, nil
}

// ReadVersion returns the dataset version the task reads.
func (t *OptimizeIndexTask) ReadVersion() (uint64, error) {
	handle := t.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	version := b.optimizeIndexTaskReadVersion(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return version, nil
}

// Group returns the task's merge group.
func (t *OptimizeIndexTask) Group() (uint32, error) {
	handle := t.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	group := b.optimizeIndexTaskGroup(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return group, nil
}

// FragmentCount returns the number of source fragments.
func (t *OptimizeIndexTask) FragmentCount() (uint64, error) {
	handle := t.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	count := b.optimizeIndexTaskFragmentCount(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return count, nil
}

// FragmentIDAt returns one source fragment id.
func (t *OptimizeIndexTask) FragmentIDAt(index uint64) (uint32, error) {
	handle := t.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	var fragmentID uint32
	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.optimizeIndexTaskFragmentIDAt(handle, index, &fragmentID); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_optimize_index_task_fragment_id_at failed without setting an error")
	}
	return fragmentID, nil
}

// Reference returns an owned copy of the task's reference segment metadata.
func (t *OptimizeIndexTask) Reference() (*SegmentMetadata, error) {
	handle := t.handle
	if handle == 0 {
		return nil, ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	metadata := b.optimizeIndexTaskReference(handle)
	if metadata == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_index_task_reference returned NULL without setting an error")
	}
	return &SegmentMetadata{b: b, handle: metadata}, nil
}

// OldSegmentCount returns the number of replaced index segments.
func (t *OptimizeIndexTask) OldSegmentCount() (uint64, error) {
	handle := t.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	count := b.optimizeIndexTaskOldSegmentCount(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return count, nil
}

// OldSegmentAt returns an owned copy of one replaced segment's metadata.
func (t *OptimizeIndexTask) OldSegmentAt(index uint64) (*SegmentMetadata, error) {
	handle := t.handle
	if handle == 0 {
		return nil, ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	metadata := b.optimizeIndexTaskOldSegmentAt(handle, index)
	if metadata == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_index_task_old_segment_at returned NULL without setting an error")
	}
	return &SegmentMetadata{b: b, handle: metadata}, nil
}

// NumIndicesToMerge returns the task's optional merge limit.
func (t *OptimizeIndexTask) NumIndicesToMerge() (compaction.Optional[uint64], error) {
	handle := t.handle
	if handle == 0 {
		return compaction.Unset[uint64](), ErrClosed
	}

	var hasValue bool
	var value uint64
	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.optimizeIndexTaskNumIndicesToMerge(handle, &hasValue, &value); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return compaction.Unset[uint64](), nativeErr
		}
		return compaction.Unset[uint64](), fmt.Errorf("ffi: lance_optimize_index_task_num_indices_to_merge failed without setting an error")
	}
	if !hasValue {
		return compaction.None[uint64](), nil
	}
	return compaction.Some(value), nil
}

// EnablePartitionRebalancing reports the task's rebalancing flag.
func (t *OptimizeIndexTask) EnablePartitionRebalancing() (bool, error) {
	handle := t.handle
	if handle == 0 {
		return false, ErrClosed
	}

	var value bool
	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.optimizeIndexTaskEnablePartitionRebalancing(handle, &value); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return false, nativeErr
		}
		return false, fmt.Errorf("ffi: lance_optimize_index_task_enable_partition_rebalancing failed without setting an error")
	}
	return value, nil
}

// Retrain reports the task's retrain flag.
func (t *OptimizeIndexTask) Retrain() (bool, error) {
	handle := t.handle
	if handle == 0 {
		return false, ErrClosed
	}

	var value bool
	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.optimizeIndexTaskRetrain(handle, &value); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return false, nativeErr
		}
		return false, fmt.Errorf("ffi: lance_optimize_index_task_retrain failed without setting an error")
	}
	return value, nil
}

// Execute runs the task against a dataset snapshot. A nil result means native
// optimization found no work for this task.
func (t *OptimizeIndexTask) Execute(dataset *Dataset) (*IndexSegmentResult, error) {
	if dataset == nil {
		return nil, fmt.Errorf("ffi: dataset must not be nil")
	}
	datasetHandle := dataset.handle.Load()
	if datasetHandle == 0 {
		return nil, ErrClosed
	}
	taskHandle := t.handle
	if taskHandle == 0 {
		return nil, ErrClosed
	}

	var result uintptr
	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	rc := b.optimizeIndexTaskExecute(datasetHandle, taskHandle, &result)
	if rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_index_task_execute failed without setting an error")
	}
	if result == 0 {
		return nil, nil
	}
	return &IndexSegmentResult{b: b, handle: result}, nil
}

// JSON returns the task's native serialized JSON.
func (t *OptimizeIndexTask) JSON() (string, error) {
	handle := t.handle
	if handle == 0 {
		return "", ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ptr := b.optimizeIndexTaskJSON(handle)
	if ptr == nil {
		if nativeErr := b.takeError(); nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_optimize_index_task_json returned NULL without setting an error")
	}
	value := goString(ptr)
	b.freeString(ptr)
	return value, nil
}

// OptimizeIndexTaskFromJSON restores a native task from serialized JSON.
func OptimizeIndexTaskFromJSON(data string) (*OptimizeIndexTask, error) {
	if err := validateCString(data); err != nil {
		return nil, err
	}
	b, err := load()
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	task := b.optimizeIndexTaskFromJSON(data)
	if task == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_index_task_from_json returned NULL without setting an error")
	}
	return &OptimizeIndexTask{b: b, handle: task}, nil
}

// OptimizeIndexMerge is an opaque native LanceOptimizeIndexMerge handle.
type OptimizeIndexMerge struct {
	b      *bindings
	handle uintptr
}

// Close releases the native merge handle. It is not safe for concurrent use.
func (m *OptimizeIndexMerge) Close() {
	if m == nil {
		return
	}
	handle := m.handle
	if handle == 0 {
		return
	}
	m.handle = 0

	b := m.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b.optimizeIndexMergeClose(handle)
}

// PlanID returns the native plan identifier for this merge.
func (m *OptimizeIndexMerge) PlanID() ([16]byte, error) {
	handle := m.handle
	if handle == 0 {
		return [16]byte{}, ErrClosed
	}

	var uuid [16]byte
	b := m.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.optimizeIndexMergePlanID(handle, &uuid[0]); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return [16]byte{}, nativeErr
		}
		return [16]byte{}, fmt.Errorf("ffi: lance_optimize_index_merge_plan_id failed without setting an error")
	}
	return uuid, nil
}

// ReadVersion returns the dataset version the merge reads.
func (m *OptimizeIndexMerge) ReadVersion() (uint64, error) {
	handle := m.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := m.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	version := b.optimizeIndexMergeReadVersion(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return version, nil
}

// Group returns the merge group.
func (m *OptimizeIndexMerge) Group() (uint32, error) {
	handle := m.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := m.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	group := b.optimizeIndexMergeGroup(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return group, nil
}

// OldSegmentCount returns the number of segments merged by this merge.
func (m *OptimizeIndexMerge) OldSegmentCount() (uint64, error) {
	handle := m.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := m.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	count := b.optimizeIndexMergeOldSegmentCount(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return count, nil
}

// OldSegmentAt returns an owned copy of one merged segment's metadata.
func (m *OptimizeIndexMerge) OldSegmentAt(index uint64) (*SegmentMetadata, error) {
	handle := m.handle
	if handle == 0 {
		return nil, ErrClosed
	}

	b := m.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	metadata := b.optimizeIndexMergeOldSegmentAt(handle, index)
	if metadata == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_index_merge_old_segment_at returned NULL without setting an error")
	}
	return &SegmentMetadata{b: b, handle: metadata}, nil
}

// ReplaceCount returns the number of index segments replaced by the merge.
func (m *OptimizeIndexMerge) ReplaceCount() (uint64, error) {
	handle := m.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := m.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	count := b.optimizeIndexMergeReplaceCount(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return count, nil
}

// ReplaceUUIDAt returns one replaced segment's UUID.
func (m *OptimizeIndexMerge) ReplaceUUIDAt(index uint64) ([16]byte, error) {
	handle := m.handle
	if handle == 0 {
		return [16]byte{}, ErrClosed
	}

	var uuid [16]byte
	b := m.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.optimizeIndexMergeReplaceUUIDAt(handle, index, &uuid[0]); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return [16]byte{}, nativeErr
		}
		return [16]byte{}, fmt.Errorf("ffi: lance_optimize_index_merge_replace_uuid_at failed without setting an error")
	}
	return uuid, nil
}

// Execute runs the merge against a dataset snapshot and task results. A nil
// result means native optimization found no work for this group.
func (m *OptimizeIndexMerge) Execute(dataset *Dataset, results []*IndexSegmentResult) (*IndexSegmentResult, error) {
	if dataset == nil {
		return nil, fmt.Errorf("ffi: dataset must not be nil")
	}
	datasetHandle := dataset.handle.Load()
	if datasetHandle == 0 {
		return nil, ErrClosed
	}
	mergeHandle := m.handle
	if mergeHandle == 0 {
		return nil, ErrClosed
	}

	handles := make([]uintptr, len(results))
	for i, result := range results {
		if result == nil {
			return nil, fmt.Errorf("ffi: index segment result %d is nil", i)
		}
		if result.handle == 0 {
			return nil, ErrClosed
		}
		handles[i] = result.handle
	}

	var out uintptr
	b := m.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	rc := b.optimizeIndexMergeExecute(datasetHandle, mergeHandle, handles, uint64(len(handles)), &out)
	runtime.KeepAlive(handles)
	if rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_index_merge_execute failed without setting an error")
	}
	if out == 0 {
		return nil, nil
	}
	return &IndexSegmentResult{b: b, handle: out}, nil
}

// JSON returns the merge's native serialized JSON.
func (m *OptimizeIndexMerge) JSON() (string, error) {
	handle := m.handle
	if handle == 0 {
		return "", ErrClosed
	}

	b := m.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ptr := b.optimizeIndexMergeJSON(handle)
	if ptr == nil {
		if nativeErr := b.takeError(); nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_optimize_index_merge_json returned NULL without setting an error")
	}
	value := goString(ptr)
	b.freeString(ptr)
	return value, nil
}

// OptimizeIndexMergeFromJSON restores a native merge from serialized JSON.
func OptimizeIndexMergeFromJSON(data string) (*OptimizeIndexMerge, error) {
	if err := validateCString(data); err != nil {
		return nil, err
	}
	b, err := load()
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	merge := b.optimizeIndexMergeFromJSON(data)
	if merge == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_index_merge_from_json returned NULL without setting an error")
	}
	return &OptimizeIndexMerge{b: b, handle: merge}, nil
}

// IndexSegmentResult is an opaque native LanceIndexSegmentResult handle.
type IndexSegmentResult struct {
	b      *bindings
	handle uintptr
}

// Close releases the native result handle. It is not safe for concurrent use.
func (r *IndexSegmentResult) Close() {
	if r == nil {
		return
	}
	handle := r.handle
	if handle == 0 {
		return
	}
	r.handle = 0

	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b.indexSegmentResultClose(handle)
}

// PlanID returns the native plan identifier for this result.
func (r *IndexSegmentResult) PlanID() ([16]byte, error) {
	handle := r.handle
	if handle == 0 {
		return [16]byte{}, ErrClosed
	}

	var uuid [16]byte
	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.indexSegmentResultPlanID(handle, &uuid[0]); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return [16]byte{}, nativeErr
		}
		return [16]byte{}, fmt.Errorf("ffi: lance_index_segment_result_plan_id failed without setting an error")
	}
	return uuid, nil
}

// ReadVersion returns the dataset version the result read.
func (r *IndexSegmentResult) ReadVersion() (uint64, error) {
	handle := r.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	version := b.indexSegmentResultReadVersion(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return version, nil
}

// Group returns the result's merge group.
func (r *IndexSegmentResult) Group() (uint32, error) {
	handle := r.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	group := b.indexSegmentResultGroup(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return group, nil
}

// Segment returns an owned copy of the produced segment metadata.
func (r *IndexSegmentResult) Segment() (*SegmentMetadata, error) {
	handle := r.handle
	if handle == 0 {
		return nil, ErrClosed
	}

	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	metadata := b.indexSegmentResultSegment(handle)
	if metadata == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_index_segment_result_segment returned NULL without setting an error")
	}
	return &SegmentMetadata{b: b, handle: metadata}, nil
}

// ReplaceCount returns the number of index segments replaced by the result.
func (r *IndexSegmentResult) ReplaceCount() (uint64, error) {
	handle := r.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	count := b.indexSegmentResultReplaceCount(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return count, nil
}

// ReplaceUUIDAt returns one replaced segment's UUID.
func (r *IndexSegmentResult) ReplaceUUIDAt(index uint64) ([16]byte, error) {
	handle := r.handle
	if handle == 0 {
		return [16]byte{}, ErrClosed
	}

	var uuid [16]byte
	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.indexSegmentResultReplaceUUIDAt(handle, index, &uuid[0]); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return [16]byte{}, nativeErr
		}
		return [16]byte{}, fmt.Errorf("ffi: lance_index_segment_result_replace_uuid_at failed without setting an error")
	}
	return uuid, nil
}

// JSON returns the result's native serialized JSON.
func (r *IndexSegmentResult) JSON() (string, error) {
	handle := r.handle
	if handle == 0 {
		return "", ErrClosed
	}

	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ptr := b.indexSegmentResultJSON(handle)
	if ptr == nil {
		if nativeErr := b.takeError(); nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_index_segment_result_json returned NULL without setting an error")
	}
	value := goString(ptr)
	b.freeString(ptr)
	return value, nil
}

// IndexSegmentResultFromJSON restores a native result from serialized JSON.
func IndexSegmentResultFromJSON(data string) (*IndexSegmentResult, error) {
	if err := validateCString(data); err != nil {
		return nil, err
	}
	b, err := load()
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result := b.indexSegmentResultFromJSON(data)
	if result == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_index_segment_result_from_json returned NULL without setting an error")
	}
	return &IndexSegmentResult{b: b, handle: result}, nil
}
