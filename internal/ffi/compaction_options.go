package ffi

import (
	"fmt"
	"runtime"

	"github.com/lance-format/lance-go/compaction"
)

// compactionMetrics mirrors LanceCompactionMetrics from the pinned header.
type compactionMetrics struct {
	FragmentsRemoved uint64
	FragmentsAdded   uint64
	FilesRemoved     uint64
	FilesAdded       uint64
}

func (m compactionMetrics) metrics() compaction.Metrics {
	return compaction.Metrics{
		FragmentsRemoved: m.FragmentsRemoved,
		FragmentsAdded:   m.FragmentsAdded,
		FilesRemoved:     m.FilesRemoved,
		FilesAdded:       m.FilesAdded,
	}
}

// compactionOptions owns a native LanceCompactionOptionsV2 handle.
type compactionOptions struct {
	b      *bindings
	handle uintptr
}

func (o *compactionOptions) ptr() uintptr {
	if o == nil {
		return 0
	}
	return o.handle
}

func (o *compactionOptions) Close() {
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
	b.compactionOptionsFree(handle)
}

// buildCompactionOptions creates native options. A nil options value returns a
// nil handle, which native APIs interpret as from-dataset-config options. A
// non-nil value starts from native defaults and applies only set fields.
func buildCompactionOptions(b *bindings, options *compaction.Options) (*compactionOptions, error) {
	if options == nil {
		return nil, nil
	}

	var dataStorageVersion *cStringList
	if value, ok := options.DataStorageVersion.Get(); ok {
		var err error
		dataStorageVersion, err = newCStringList([]string{value})
		if err != nil {
			return nil, err
		}
	}

	properties, keepProperties, err := newTransactionProperties(options.TransactionProperties)
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	handle := b.compactionOptionsNew()
	if handle == 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_compaction_options_v2_new returned NULL without setting an error")
	}

	fail := func(name string) (*compactionOptions, error) {
		nativeErr := b.takeError()
		b.compactionOptionsFree(handle)
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: %s failed without setting an error", name)
	}

	setU64 := func(name string, setter func(uintptr, uint64) int32, value *uint64) error {
		if value == nil {
			return nil
		}
		if rc := setter(handle, *value); rc != 0 {
			_, err := fail(name)
			return err
		}
		return nil
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
	setBool := func(name string, setter func(uintptr, bool) int32, value *bool) error {
		if value == nil {
			return nil
		}
		if rc := setter(handle, *value); rc != 0 {
			_, err := fail(name)
			return err
		}
		return nil
	}

	if err := setU64("lance_compaction_options_v2_set_target_rows_per_fragment", b.compactionOptionsSetTargetRowsPerFragment, options.TargetRowsPerFragment); err != nil {
		return nil, err
	}
	if err := setU64("lance_compaction_options_v2_set_max_rows_per_group", b.compactionOptionsSetMaxRowsPerGroup, options.MaxRowsPerGroup); err != nil {
		return nil, err
	}
	if err := setOptionalU64("lance_compaction_options_v2_set_max_bytes_per_file", b.compactionOptionsSetMaxBytesPerFile, options.MaxBytesPerFile); err != nil {
		return nil, err
	}
	if err := setBool("lance_compaction_options_v2_set_materialize_deletions", b.compactionOptionsSetMaterializeDeletions, options.MaterializeDeletions); err != nil {
		return nil, err
	}
	if options.MaterializeDeletionsThreshold != nil {
		rc := b.compactionOptionsSetMaterializeDeletionsThreshold(handle, float32(*options.MaterializeDeletionsThreshold))
		if rc != 0 {
			return fail("lance_compaction_options_v2_set_materialize_deletions_threshold")
		}
	}
	if err := setOptionalU64("lance_compaction_options_v2_set_num_threads", b.compactionOptionsSetNumThreads, options.NumThreads); err != nil {
		return nil, err
	}
	if err := setOptionalU64("lance_compaction_options_v2_set_batch_size", b.compactionOptionsSetBatchSize, options.BatchSize); err != nil {
		return nil, err
	}
	if err := setOptionalU64("lance_compaction_options_v2_set_io_buffer_size", b.compactionOptionsSetIOBufferSize, options.IOBufferSize); err != nil {
		return nil, err
	}
	if err := setBool("lance_compaction_options_v2_set_defer_index_remap", b.compactionOptionsSetDeferIndexRemap, options.DeferIndexRemap); err != nil {
		return nil, err
	}
	if options.IndexRemapMode != nil {
		rc := b.compactionOptionsSetIndexRemapMode(handle, int32(*options.IndexRemapMode))
		if rc != 0 {
			return fail("lance_compaction_options_v2_set_index_remap_mode")
		}
	}
	if options.CompactionMode.IsSet() {
		mode, hasValue := options.CompactionMode.Get()
		rc := b.compactionOptionsSetCompactionMode(handle, hasValue, int32(mode))
		if rc != 0 {
			return fail("lance_compaction_options_v2_set_compaction_mode")
		}
	}
	if err := setOptionalU64("lance_compaction_options_v2_set_binary_copy_read_batch_bytes", b.compactionOptionsSetBinaryCopyReadBatchBytes, options.BinaryCopyReadBatchBytes); err != nil {
		return nil, err
	}
	if err := setOptionalU64("lance_compaction_options_v2_set_max_source_fragments", b.compactionOptionsSetMaxSourceFragments, options.MaxSourceFragments); err != nil {
		return nil, err
	}
	if err := setOptionalU64("lance_compaction_options_v2_set_max_source_rows", b.compactionOptionsSetMaxSourceRows, options.MaxSourceRows); err != nil {
		return nil, err
	}
	if err := setOptionalU64("lance_compaction_options_v2_set_max_source_bytes", b.compactionOptionsSetMaxSourceBytes, options.MaxSourceBytes); err != nil {
		return nil, err
	}
	if len(options.ExcludedFragmentIDs) > 0 {
		rc := b.compactionOptionsSetExcludedFragmentIDs(handle, options.ExcludedFragmentIDs, uint64(len(options.ExcludedFragmentIDs)))
		runtime.KeepAlive(options.ExcludedFragmentIDs)
		if rc != 0 {
			return fail("lance_compaction_options_v2_set_excluded_fragment_ids")
		}
	}
	if err := setOptionalU64("lance_compaction_options_v2_set_max_overlays_per_fragment", b.compactionOptionsSetMaxOverlaysPerFragment, options.MaxOverlaysPerFragment); err != nil {
		return nil, err
	}
	if options.DataStorageVersion.IsSet() {
		var selector *byte
		if dataStorageVersion != nil {
			selector = &dataStorageVersion.bufs[0][0]
		}
		rc := b.compactionOptionsSetDataStorageVersion(handle, selector)
		runtime.KeepAlive(dataStorageVersion)
		if rc != 0 {
			return fail("lance_compaction_options_v2_set_data_storage_version")
		}
	}
	if options.TransactionProperties != nil {
		rc := b.compactionOptionsSetTransactionProperties(handle, properties)
		keepProperties()
		if rc != 0 {
			return fail("lance_compaction_options_v2_set_transaction_properties")
		}
	}

	return &compactionOptions{b: b, handle: handle}, nil
}

// CompactFiles runs native CompactionOptionsV2 compaction in place on d.
func (d *Dataset) CompactFiles(options *compaction.Options) (compaction.Metrics, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return compaction.Metrics{}, ErrClosed
	}

	nativeOptions, err := buildCompactionOptions(d.b, options)
	if err != nil {
		return compaction.Metrics{}, err
	}
	defer nativeOptions.Close()

	var out compactionMetrics
	runtime.LockOSThread()
	rc := d.b.datasetCompactFilesV2(handle, nativeOptions.ptr(), &out)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return compaction.Metrics{}, nativeErr
		}
		return compaction.Metrics{}, fmt.Errorf("ffi: lance_dataset_compact_files_v2 failed without setting an error")
	}
	runtime.UnlockOSThread()
	return out.metrics(), nil
}

// CommitCompaction commits rewrite results to a dataset.
func (d *Dataset) CommitCompaction(results []*RewriteResult, options *compaction.Options) (compaction.Metrics, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return compaction.Metrics{}, ErrClosed
	}

	nativeResults := make([]uintptr, len(results))
	for i, result := range results {
		if result == nil {
			return compaction.Metrics{}, fmt.Errorf("ffi: compaction rewrite result %d is nil", i)
		}
		if result.handle == 0 {
			return compaction.Metrics{}, ErrClosed
		}
		nativeResults[i] = result.handle
	}

	nativeOptions, err := buildCompactionOptions(d.b, options)
	if err != nil {
		return compaction.Metrics{}, err
	}
	defer nativeOptions.Close()

	var out compactionMetrics
	runtime.LockOSThread()
	rc := d.b.datasetCommitCompaction(handle, nativeResults, uint64(len(nativeResults)), nativeOptions.ptr(), &out)
	runtime.KeepAlive(nativeResults)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return compaction.Metrics{}, nativeErr
		}
		return compaction.Metrics{}, fmt.Errorf("ffi: lance_dataset_commit_compaction failed without setting an error")
	}
	runtime.UnlockOSThread()
	return out.metrics(), nil
}
