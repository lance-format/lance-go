package ffi

import (
	"fmt"
	"runtime"
	"sort"
	"unsafe"

	"github.com/lance-format/lance-go/compaction"
)

// optimizeIndicesOptions owns a native LanceOptimizeIndicesOptions handle.
type optimizeIndicesOptions struct {
	b      *bindings
	handle uintptr
}

func (o *optimizeIndicesOptions) ptr() uintptr {
	if o == nil {
		return 0
	}
	return o.handle
}

func (o *optimizeIndicesOptions) Close() {
	if o == nil {
		return
	}
	handle := o.handle
	if handle == 0 {
		return
	}
	o.handle = 0

	b := o.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b.optimizeIndicesOptionsFree(handle)
}

// buildOptimizeIndicesOptions creates native options. A nil options value
// returns a nil handle, which native APIs interpret as defaults. A non-nil
// value starts from native defaults and applies only set fields.
func buildOptimizeIndicesOptions(b *bindings, options *compaction.OptimizeOptions) (*optimizeIndicesOptions, error) {
	if options == nil {
		return nil, nil
	}

	var indexNames *cStringList
	if options.IndexNames.IsSet() {
		names, hasValue := options.IndexNames.Get()
		if hasValue {
			var err error
			indexNames, err = newPresentCStringList(names)
			if err != nil {
				return nil, err
			}
		}
	}

	var propertyKeys, propertyValues []string
	var propertyKeysList, propertyValuesList *cStringList
	if options.TransactionProperties != nil {
		propertyKeys = make([]string, 0, len(options.TransactionProperties))
		for key := range options.TransactionProperties {
			propertyKeys = append(propertyKeys, key)
		}
		sort.Strings(propertyKeys)
		propertyValues = make([]string, len(propertyKeys))
		for i, key := range propertyKeys {
			propertyValues[i] = options.TransactionProperties[key]
		}
		var err error
		propertyKeysList, err = newCStringList(propertyKeys)
		if err != nil {
			return nil, err
		}
		propertyValuesList, err = newCStringList(propertyValues)
		if err != nil {
			return nil, err
		}
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	handle := b.optimizeIndicesOptionsNew()
	if handle == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_optimize_indices_options_new returned NULL without setting an error")
	}

	fail := func(name string) (*optimizeIndicesOptions, error) {
		nativeErr := b.takeError()
		b.optimizeIndicesOptionsFree(handle)
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: %s failed without setting an error", name)
	}

	setOptionalU64 := func(name string, setter func(uintptr, bool, uint64) int32, value compaction.Optional[uint64]) error {
		if !value.IsSet() {
			return nil
		}
		resolved, hasValue := value.Get()
		if rc := setter(handle, hasValue, resolved); rc != 0 {
			_, err := fail(name)
			return err
		}
		return nil
	}

	if err := setOptionalU64("lance_optimize_indices_options_set_num_indices_to_merge", b.optimizeIndicesOptionsSetNumIndicesToMerge, options.NumIndicesToMerge); err != nil {
		return nil, err
	}
	if rc := b.optimizeIndicesOptionsSetEnablePartitionRebalancing(handle, options.EnablePartitionRebalancing); rc != 0 {
		return fail("lance_optimize_indices_options_set_enable_partition_rebalancing")
	}
	if options.IndexNames.IsSet() {
		names, hasValue := options.IndexNames.Get()
		var count uint64
		if hasValue {
			count = uint64(len(names))
		}
		rc := b.optimizeIndicesOptionsSetIndexNames(handle, hasValue, listPtr(indexNames), count)
		runtime.KeepAlive(indexNames)
		if rc != 0 {
			return fail("lance_optimize_indices_options_set_index_names")
		}
	}
	if rc := b.optimizeIndicesOptionsSetRetrain(handle, options.Retrain); rc != 0 {
		return fail("lance_optimize_indices_options_set_retrain")
	}
	if options.TransactionProperties != nil {
		properties := transactionProperties{Count: uint64(len(propertyKeys))}
		if len(propertyKeys) > 0 {
			properties.Keys = (**byte)(unsafe.Pointer(&propertyKeysList.ptrs[0]))
			properties.Values = (**byte)(unsafe.Pointer(&propertyValuesList.ptrs[0]))
		}
		rc := b.optimizeIndicesOptionsSetTransactionProperties(handle, &properties)
		propertyKeysList.keepAlive()
		propertyValuesList.keepAlive()
		runtime.KeepAlive(&properties)
		if rc != 0 {
			return fail("lance_optimize_indices_options_set_transaction_properties")
		}
	}
	if err := setOptionalU64("lance_optimize_indices_options_set_max_rows_per_task", b.optimizeIndicesOptionsSetMaxRowsPerTask, options.MaxRowsPerTask); err != nil {
		return nil, err
	}
	if err := setOptionalU64("lance_optimize_indices_options_set_max_rows_per_segment", b.optimizeIndicesOptionsSetMaxRowsPerSegment, options.MaxRowsPerSegment); err != nil {
		return nil, err
	}

	return &optimizeIndicesOptions{b: b, handle: handle}, nil
}

// OptimizeIndices runs native single-process index optimization in place on d.
func (d *Dataset) OptimizeIndices(options *compaction.OptimizeOptions) error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}

	nativeOptions, err := buildOptimizeIndicesOptions(d.b, options)
	if err != nil {
		return err
	}
	defer nativeOptions.Close()

	runtime.LockOSThread()
	rc := d.b.datasetOptimizeIndices(handle, nativeOptions.ptr())
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_optimize_indices failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}

// PlanOptimizeIndices creates a native index-optimization plan from a dataset
// snapshot.
func (d *Dataset) PlanOptimizeIndices(options *compaction.OptimizeOptions) (*OptimizeIndicesPlan, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}

	nativeOptions, err := buildOptimizeIndicesOptions(d.b, options)
	if err != nil {
		return nil, err
	}
	defer nativeOptions.Close()

	runtime.LockOSThread()
	plan := d.b.datasetPlanOptimizeIndices(handle, nativeOptions.ptr())
	if plan == 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_plan_optimize_indices returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &OptimizeIndicesPlan{b: d.b, handle: plan}, nil
}

// CommitOptimizeIndices commits merged index results to a dataset.
func (d *Dataset) CommitOptimizeIndices(results []*IndexSegmentResult, options *compaction.OptimizeOptions) error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}

	nativeResults := make([]uintptr, len(results))
	for i, result := range results {
		if result == nil {
			return fmt.Errorf("ffi: index segment result %d is nil", i)
		}
		if result.handle == 0 {
			return ErrClosed
		}
		nativeResults[i] = result.handle
	}

	nativeOptions, err := buildOptimizeIndicesOptions(d.b, options)
	if err != nil {
		return err
	}
	defer nativeOptions.Close()

	runtime.LockOSThread()
	rc := d.b.datasetCommitOptimizeIndices(handle, nativeResults, uint64(len(nativeResults)), nativeOptions.ptr())
	runtime.KeepAlive(nativeResults)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_commit_optimize_indices failed without setting an error")
	}
	runtime.UnlockOSThread()
	return nil
}
