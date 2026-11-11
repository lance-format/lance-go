package ffi

import (
	"fmt"
	"runtime"

	"github.com/lance-format/lance-go/compaction"
)

// CompactionPlan is an opaque native LanceCompactionPlan handle.
type CompactionPlan struct {
	b      *bindings
	handle uintptr
}

// Close releases the native plan handle. It is not safe for concurrent use.
func (p *CompactionPlan) Close() {
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
	b.compactionPlanClose(handle)
}

// PlanCompaction creates a native compaction plan from a dataset snapshot.
func (d *Dataset) PlanCompaction(options *compaction.Options) (*CompactionPlan, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}

	nativeOptions, err := buildCompactionOptions(d.b, options)
	if err != nil {
		return nil, err
	}
	defer nativeOptions.Close()

	runtime.LockOSThread()
	plan := d.b.datasetPlanCompaction(handle, nativeOptions.ptr())
	if plan == 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_plan_compaction returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &CompactionPlan{b: d.b, handle: plan}, nil
}

// ReadVersion returns the dataset version the plan was created from.
func (p *CompactionPlan) ReadVersion() (uint64, error) {
	handle := p.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	version := b.compactionPlanReadVersion(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return version, nil
}

// TaskCount returns the number of tasks in the plan.
func (p *CompactionPlan) TaskCount() (uint64, error) {
	handle := p.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	count := b.compactionPlanTaskCount(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return count, nil
}

// TaskAt returns an owned snapshot of one plan task.
func (p *CompactionPlan) TaskAt(index uint64) (*CompactionTask, error) {
	handle := p.handle
	if handle == 0 {
		return nil, ErrClosed
	}

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	task := b.compactionPlanTaskAt(handle, index)
	if task == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_compaction_plan_task_at returned NULL without setting an error")
	}
	return &CompactionTask{b: b, handle: task}, nil
}

// JSON returns the plan's native serialized JSON.
func (p *CompactionPlan) JSON() (string, error) {
	handle := p.handle
	if handle == 0 {
		return "", ErrClosed
	}

	b := p.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ptr := b.compactionPlanJSON(handle)
	if ptr == nil {
		if nativeErr := b.takeError(); nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_compaction_plan_json returned NULL without setting an error")
	}
	value := goString(ptr)
	b.freeString(ptr)
	return value, nil
}

// CompactionPlanFromJSON restores a native plan from serialized JSON.
func CompactionPlanFromJSON(data string) (*CompactionPlan, error) {
	if err := validateCString(data); err != nil {
		return nil, err
	}
	b, err := load()
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	plan := b.compactionPlanFromJSON(data)
	if plan == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_compaction_plan_from_json returned NULL without setting an error")
	}
	return &CompactionPlan{b: b, handle: plan}, nil
}

// CompactionTask is an opaque native LanceCompactionTask handle.
type CompactionTask struct {
	b      *bindings
	handle uintptr
}

// Close releases the native task handle. It is not safe for concurrent use.
func (t *CompactionTask) Close() {
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
	b.compactionTaskClose(handle)
}

// ReadVersion returns the dataset version the task must read.
func (t *CompactionTask) ReadVersion() (uint64, error) {
	handle := t.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	version := b.compactionTaskReadVersion(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return version, nil
}

// FragmentCount returns the number of source fragments in the task.
func (t *CompactionTask) FragmentCount() (uint64, error) {
	handle := t.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	count := b.compactionTaskFragmentCount(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return count, nil
}

// FragmentMetadataJSONAt returns one source fragment's metadata JSON.
func (t *CompactionTask) FragmentMetadataJSONAt(index uint64) (string, error) {
	handle := t.handle
	if handle == 0 {
		return "", ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ptr := b.compactionTaskFragmentMetadataJSONAt(handle, index)
	if ptr == nil {
		if nativeErr := b.takeError(); nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_compaction_task_fragment_metadata_json_at returned NULL without setting an error")
	}
	value := goString(ptr)
	b.freeString(ptr)
	return value, nil
}

// Execute executes the task against dataset at the task's read version.
func (t *CompactionTask) Execute(dataset *Dataset) (*RewriteResult, error) {
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

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result := b.compactionTaskExecute(datasetHandle, taskHandle)
	if result == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_compaction_task_execute returned NULL without setting an error")
	}
	return &RewriteResult{b: b, handle: result}, nil
}

// JSON returns the task's native serialized JSON.
func (t *CompactionTask) JSON() (string, error) {
	handle := t.handle
	if handle == 0 {
		return "", ErrClosed
	}

	b := t.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ptr := b.compactionTaskJSON(handle)
	if ptr == nil {
		if nativeErr := b.takeError(); nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_compaction_task_json returned NULL without setting an error")
	}
	value := goString(ptr)
	b.freeString(ptr)
	return value, nil
}

// CompactionTaskFromJSON restores a native task from serialized JSON.
func CompactionTaskFromJSON(data string) (*CompactionTask, error) {
	if err := validateCString(data); err != nil {
		return nil, err
	}
	b, err := load()
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	task := b.compactionTaskFromJSON(data)
	if task == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_compaction_task_from_json returned NULL without setting an error")
	}
	return &CompactionTask{b: b, handle: task}, nil
}

// RewriteResult is an opaque native LanceRewriteResult handle.
type RewriteResult struct {
	b      *bindings
	handle uintptr
}

// Close releases the native rewrite result handle. It is not safe for
// concurrent use.
func (r *RewriteResult) Close() {
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
	b.rewriteResultClose(handle)
}

// ReadVersion returns the dataset version the result read.
func (r *RewriteResult) ReadVersion() (uint64, error) {
	handle := r.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	version := b.rewriteResultReadVersion(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return version, nil
}

// Metrics returns the rewrite metrics.
func (r *RewriteResult) Metrics() (compaction.Metrics, error) {
	handle := r.handle
	if handle == 0 {
		return compaction.Metrics{}, ErrClosed
	}

	b := r.b
	var out compactionMetrics
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	rc := b.rewriteResultMetrics(handle, &out)
	if rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return compaction.Metrics{}, nativeErr
		}
		return compaction.Metrics{}, fmt.Errorf("ffi: lance_rewrite_result_metrics failed without setting an error")
	}
	return out.metrics(), nil
}

// OriginalFragmentCount returns the number of source fragments.
func (r *RewriteResult) OriginalFragmentCount() (uint64, error) {
	return r.fragmentCount(r.b.rewriteResultOriginalFragmentCount)
}

// NewFragmentCount returns the number of rewritten fragments.
func (r *RewriteResult) NewFragmentCount() (uint64, error) {
	return r.fragmentCount(r.b.rewriteResultNewFragmentCount)
}

func (r *RewriteResult) fragmentCount(fn func(uintptr) uint64) (uint64, error) {
	handle := r.handle
	if handle == 0 {
		return 0, ErrClosed
	}

	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	count := fn(handle)
	if err := b.takeError(); err != nil {
		return 0, err
	}
	return count, nil
}

// OriginalFragmentMetadataJSONAt returns one source fragment's metadata JSON.
func (r *RewriteResult) OriginalFragmentMetadataJSONAt(index uint64) (string, error) {
	return r.fragmentMetadataJSON("lance_rewrite_result_original_fragment_metadata_json_at", r.b.rewriteResultOriginalFragmentMetadataJSONAt, index)
}

// NewFragmentMetadataJSONAt returns one rewritten fragment's metadata JSON.
func (r *RewriteResult) NewFragmentMetadataJSONAt(index uint64) (string, error) {
	return r.fragmentMetadataJSON("lance_rewrite_result_new_fragment_metadata_json_at", r.b.rewriteResultNewFragmentMetadataJSONAt, index)
}

func (r *RewriteResult) fragmentMetadataJSON(name string, fn func(uintptr, uint64) *byte, index uint64) (string, error) {
	handle := r.handle
	if handle == 0 {
		return "", ErrClosed
	}

	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ptr := fn(handle, index)
	if ptr == nil {
		if nativeErr := b.takeError(); nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: %s returned NULL without setting an error", name)
	}
	value := goString(ptr)
	b.freeString(ptr)
	return value, nil
}

// JSON returns the rewrite result's native serialized JSON.
func (r *RewriteResult) JSON() (string, error) {
	handle := r.handle
	if handle == 0 {
		return "", ErrClosed
	}

	b := r.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ptr := b.rewriteResultJSON(handle)
	if ptr == nil {
		if nativeErr := b.takeError(); nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_rewrite_result_json returned NULL without setting an error")
	}
	value := goString(ptr)
	b.freeString(ptr)
	return value, nil
}

// RewriteResultFromJSON restores a native rewrite result from serialized JSON.
func RewriteResultFromJSON(data string) (*RewriteResult, error) {
	if err := validateCString(data); err != nil {
		return nil, err
	}
	b, err := load()
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result := b.rewriteResultFromJSON(data)
	if result == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_rewrite_result_from_json returned NULL without setting an error")
	}
	return &RewriteResult{b: b, handle: result}, nil
}
