package lance

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lance-format/lance-go/compaction"
	"github.com/lance-format/lance-go/internal/ffi"
)

// CompactFiles rewrites small fragments in d and returns a new Dataset at the
// committed compaction version. Native compaction may commit fragment-ID
// reservations before the rewrite, so the version need not advance by one.
// d is not changed: its version, rows and open
// scanners keep the pre-compaction snapshot.
//
// A nil options value asks native compaction to derive its effective options
// from the dataset configuration. A non-nil options value starts from native
// defaults and applies the fields that are set.
//
// Compaction writes new data files before committing metadata. The native
// operation is synchronous. ctx is checked before work starts; it is not
// observed once the native commit is in progress. If the commit succeeds after
// ctx is canceled, the committed Dataset is returned instead of an error that
// could be mistaken for an uncommitted compaction.
func (d *Dataset) CompactFiles(ctx context.Context, options *compaction.Options) (*Dataset, compaction.Metrics, error) {
	const op = "compact files"
	if err := checkContext(ctx); err != nil {
		return nil, compaction.Metrics{}, err
	}
	if err := validateCompactionOptions(options); err != nil {
		return nil, compaction.Metrics{}, invalidError(op, d.uri, "%v", err)
	}

	var metrics compaction.Metrics
	next, err := d.mutate(op, func(native *ffi.Dataset) error {
		var err error
		metrics, err = native.CompactFiles(options)
		return err
	})
	if err != nil {
		return nil, compaction.Metrics{}, err
	}
	return next, metrics, nil
}

// CompactionPlan is an immutable snapshot of a native compaction plan. It
// carries the read version, effective options, task JSON and task snapshots.
// A plan does not reserve files or publish anything.
type CompactionPlan struct {
	uri         string
	readVersion uint64
	json        string
	options     compaction.Options
	tasks       []*CompactionTask
}

// PlanCompaction creates a compaction plan from the current dataset snapshot.
// It writes no files and publishes no version. options follows the same rules
// as CompactFiles.
func (d *Dataset) PlanCompaction(ctx context.Context, options *compaction.Options) (*CompactionPlan, error) {
	const op = "plan compaction"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateCompactionOptions(options); err != nil {
		return nil, invalidError(op, d.uri, "%v", err)
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()

	native, err := d.native.PlanCompaction(options)
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	defer native.Close()

	plan, err := snapshotCompactionPlan(native)
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	plan.uri = d.uri
	for _, task := range plan.tasks {
		task.uri = d.uri
	}
	if options != nil && options.TransactionProperties != nil {
		// Native plan JSON omits transaction properties. Keep the properties
		// supplied to this plan so CommitCompaction can reuse them in-process.
		properties := cloneTransactionProperties(options.TransactionProperties)
		plan.options.TransactionProperties = properties
		for _, task := range plan.tasks {
			task.options.TransactionProperties = cloneTransactionProperties(properties)
		}
	}
	return plan, nil
}

// CompactionPlanFromJSON restores a plan snapshot from native serialized JSON.
// All task snapshots, effective options and metadata are copied eagerly.
func CompactionPlanFromJSON(data string) (*CompactionPlan, error) {
	const op = "compaction plan from JSON"
	if err := validateCString(data); err != nil {
		return nil, invalidError(op, "", "JSON: %v", err)
	}

	native, err := ffi.CompactionPlanFromJSON(data)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	defer native.Close()

	plan, err := snapshotCompactionPlan(native)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return plan, nil
}

// ReadVersion returns the dataset version the plan was created from.
func (p *CompactionPlan) ReadVersion() uint64 {
	return p.readVersion
}

// TaskCount returns the number of compaction tasks in the plan.
func (p *CompactionPlan) TaskCount() uint64 {
	return uint64(len(p.tasks))
}

// Task returns the task at index. The returned task is an independent snapshot;
// it can be serialized and executed later.
func (p *CompactionPlan) Task(index uint64) (*CompactionTask, error) {
	if index >= uint64(len(p.tasks)) {
		return nil, invalidError("compaction plan task", p.uri,
			"index %d out of range [0,%d)", index, len(p.tasks))
	}
	return p.tasks[index].clone(), nil
}

// Options returns the effective compaction options resolved by the native
// planner. They include the exact data storage version selected for this plan.
func (p *CompactionPlan) Options() compaction.Options {
	return cloneCompactionOptions(p.options)
}

// JSON returns the native serialized plan JSON.
func (p *CompactionPlan) JSON() string {
	return p.json
}

// CompactionTask is an immutable compaction task snapshot.
type CompactionTask struct {
	uri                  string
	readVersion          uint64
	json                 string
	options              compaction.Options
	fragmentMetadataJSON []string
}

// CompactionTaskFromJSON restores a task snapshot from native serialized JSON.
func CompactionTaskFromJSON(data string) (*CompactionTask, error) {
	const op = "compaction task from JSON"
	if err := validateCString(data); err != nil {
		return nil, invalidError(op, "", "JSON: %v", err)
	}

	native, err := ffi.CompactionTaskFromJSON(data)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	defer native.Close()

	task, err := snapshotCompactionTask(native)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return task, nil
}

// ReadVersion returns the dataset version the task must execute against.
func (t *CompactionTask) ReadVersion() uint64 {
	return t.readVersion
}

// FragmentCount returns the number of source fragments in the task.
func (t *CompactionTask) FragmentCount() uint64 {
	return uint64(len(t.fragmentMetadataJSON))
}

// FragmentMetadataJSON returns one source fragment's metadata JSON.
func (t *CompactionTask) FragmentMetadataJSON(index uint64) (string, error) {
	if index >= uint64(len(t.fragmentMetadataJSON)) {
		return "", invalidError("compaction task fragment", t.uri,
			"index %d out of range [0,%d)", index, len(t.fragmentMetadataJSON))
	}
	return t.fragmentMetadataJSON[index], nil
}

// Options returns the effective options carried by the task.
func (t *CompactionTask) Options() compaction.Options {
	return cloneCompactionOptions(t.options)
}

// JSON returns the native serialized task JSON.
func (t *CompactionTask) JSON() string {
	return t.json
}

// Execute rewrites the task's fragments against d and returns a rewrite result.
// It writes new data files but does not commit or publish metadata. The result
// is independent of d; call CommitCompaction to publish it.
//
// The task is executed at its planned read version. If d has advanced since the
// task was planned, native execution checks out the planned version. ctx is
// checked before execution starts; it is not observed once native execution is
// in progress. If execution succeeds after ctx is canceled, the result is
// returned instead of a cancellation error.
func (t *CompactionTask) Execute(ctx context.Context, d *Dataset) (*RewriteResult, error) {
	const op = "execute compaction task"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if d == nil {
		return nil, invalidError(op, "", "dataset is nil")
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()

	nativeTask, err := ffi.CompactionTaskFromJSON(t.json)
	if err != nil {
		return nil, wrapError(op, t.uri, err)
	}
	defer nativeTask.Close()

	nativeResult, err := nativeTask.Execute(d.native)
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	defer nativeResult.Close()

	result, err := snapshotRewriteResult(nativeResult)
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	result.uri = d.uri
	return result, nil
}

func (t *CompactionTask) clone() *CompactionTask {
	clone := *t
	clone.options = cloneCompactionOptions(t.options)
	clone.fragmentMetadataJSON = append([]string(nil), t.fragmentMetadataJSON...)
	return &clone
}

func cloneCompactionOptions(options compaction.Options) compaction.Options {
	clone := options
	clone.TargetRowsPerFragment = clonePtr(options.TargetRowsPerFragment)
	clone.MaxRowsPerGroup = clonePtr(options.MaxRowsPerGroup)
	clone.MaterializeDeletions = clonePtr(options.MaterializeDeletions)
	clone.MaterializeDeletionsThreshold = clonePtr(options.MaterializeDeletionsThreshold)
	clone.DeferIndexRemap = clonePtr(options.DeferIndexRemap)
	clone.IndexRemapMode = clonePtr(options.IndexRemapMode)
	clone.ExcludedFragmentIDs = append([]uint32(nil), options.ExcludedFragmentIDs...)
	clone.TransactionProperties = cloneTransactionProperties(options.TransactionProperties)
	return clone
}

func cloneTransactionProperties(properties map[string]string) map[string]string {
	if properties == nil {
		return nil
	}
	clone := make(map[string]string, len(properties))
	for key, value := range properties {
		clone[key] = value
	}
	return clone
}

func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

// RewriteResult is an immutable snapshot of a compaction task rewrite.
type RewriteResult struct {
	uri                          string
	readVersion                  uint64
	json                         string
	metrics                      compaction.Metrics
	originalFragmentMetadataJSON []string
	newFragmentMetadataJSON      []string
}

// RewriteResultFromJSON restores a rewrite result from native serialized JSON.
func RewriteResultFromJSON(data string) (*RewriteResult, error) {
	const op = "compaction rewrite result from JSON"
	if err := validateCString(data); err != nil {
		return nil, invalidError(op, "", "JSON: %v", err)
	}

	native, err := ffi.RewriteResultFromJSON(data)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	defer native.Close()

	result, err := snapshotRewriteResult(native)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return result, nil
}

// ReadVersion returns the dataset version the result was produced from.
func (r *RewriteResult) ReadVersion() uint64 {
	return r.readVersion
}

// Metrics returns the rewrite metrics reported by native execution.
func (r *RewriteResult) Metrics() compaction.Metrics {
	return r.metrics
}

// OriginalFragmentCount returns the number of source fragments.
func (r *RewriteResult) OriginalFragmentCount() uint64 {
	return uint64(len(r.originalFragmentMetadataJSON))
}

// NewFragmentCount returns the number of rewritten fragments.
func (r *RewriteResult) NewFragmentCount() uint64 {
	return uint64(len(r.newFragmentMetadataJSON))
}

// OriginalFragmentMetadataJSON returns one source fragment's metadata JSON.
func (r *RewriteResult) OriginalFragmentMetadataJSON(index uint64) (string, error) {
	if index >= uint64(len(r.originalFragmentMetadataJSON)) {
		return "", invalidError("compaction rewrite original fragment", r.uri,
			"index %d out of range [0,%d)", index, len(r.originalFragmentMetadataJSON))
	}
	return r.originalFragmentMetadataJSON[index], nil
}

// NewFragmentMetadataJSON returns one rewritten fragment's metadata JSON.
func (r *RewriteResult) NewFragmentMetadataJSON(index uint64) (string, error) {
	if index >= uint64(len(r.newFragmentMetadataJSON)) {
		return "", invalidError("compaction rewrite new fragment", r.uri,
			"index %d out of range [0,%d)", index, len(r.newFragmentMetadataJSON))
	}
	return r.newFragmentMetadataJSON[index], nil
}

// JSON returns the native serialized rewrite result JSON.
func (r *RewriteResult) JSON() string {
	return r.json
}

// CommitCompaction commits rewrite results and returns a new Dataset at the
// committed version. d is not changed: its version, rows and open scanners keep
// the pre-commit snapshot.
//
// Results may come from Execute or RewriteResultFromJSON. A nil options value
// asks native commit to use its default options. Pass the plan or task Options
// to use the exact settings that produced the results.
//
// The native commit is synchronous. ctx is checked before work starts; it is not
// observed once the commit is in progress. If the commit succeeds after ctx is
// canceled, the committed Dataset is returned.
func (d *Dataset) CommitCompaction(ctx context.Context, results []*RewriteResult, options *compaction.Options) (*Dataset, compaction.Metrics, error) {
	const op = "commit compaction"
	if err := checkContext(ctx); err != nil {
		return nil, compaction.Metrics{}, err
	}
	if err := validateCompactionOptions(options); err != nil {
		return nil, compaction.Metrics{}, invalidError(op, d.uri, "%v", err)
	}
	for i, result := range results {
		if result == nil {
			return nil, compaction.Metrics{}, invalidError(op, d.uri, "result %d is nil", i)
		}
		if result.json == "" {
			return nil, compaction.Metrics{}, invalidError(op, d.uri, "result %d has no JSON", i)
		}
	}

	var metrics compaction.Metrics
	next, err := d.mutate(op, func(native *ffi.Dataset) error {
		nativeResults := make([]*ffi.RewriteResult, len(results))
		for i, result := range results {
			nativeResult, err := ffi.RewriteResultFromJSON(result.json)
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

		var commitErr error
		metrics, commitErr = native.CommitCompaction(nativeResults, options)
		return commitErr
	})
	if err != nil {
		return nil, compaction.Metrics{}, err
	}
	return next, metrics, nil
}

func snapshotCompactionPlan(native *ffi.CompactionPlan) (*CompactionPlan, error) {
	readVersion, err := native.ReadVersion()
	if err != nil {
		return nil, err
	}
	data, err := native.JSON()
	if err != nil {
		return nil, err
	}
	options, err := decodeCompactionOptions(data, "plan")
	if err != nil {
		return nil, err
	}
	taskCount, err := native.TaskCount()
	if err != nil {
		return nil, err
	}

	plan := &CompactionPlan{
		readVersion: readVersion,
		json:        data,
		options:     options,
		tasks:       make([]*CompactionTask, 0),
	}
	for i := uint64(0); i < taskCount; i++ {
		nativeTask, err := native.TaskAt(i)
		if err != nil {
			return nil, err
		}
		task, err := snapshotCompactionTask(nativeTask)
		nativeTask.Close()
		if err != nil {
			return nil, err
		}
		plan.tasks = append(plan.tasks, task)
	}
	return plan, nil
}

func snapshotCompactionTask(native *ffi.CompactionTask) (*CompactionTask, error) {
	readVersion, err := native.ReadVersion()
	if err != nil {
		return nil, err
	}
	data, err := native.JSON()
	if err != nil {
		return nil, err
	}
	options, err := decodeCompactionOptions(data, "task")
	if err != nil {
		return nil, err
	}
	fragmentCount, err := native.FragmentCount()
	if err != nil {
		return nil, err
	}

	task := &CompactionTask{
		readVersion: readVersion,
		json:        data,
		options:     options,
	}
	for i := uint64(0); i < fragmentCount; i++ {
		metadata, err := native.FragmentMetadataJSONAt(i)
		if err != nil {
			return nil, err
		}
		task.fragmentMetadataJSON = append(task.fragmentMetadataJSON, metadata)
	}
	return task, nil
}

func snapshotRewriteResult(native *ffi.RewriteResult) (*RewriteResult, error) {
	readVersion, err := native.ReadVersion()
	if err != nil {
		return nil, err
	}
	metrics, err := native.Metrics()
	if err != nil {
		return nil, err
	}
	originalCount, err := native.OriginalFragmentCount()
	if err != nil {
		return nil, err
	}
	newCount, err := native.NewFragmentCount()
	if err != nil {
		return nil, err
	}

	result := &RewriteResult{
		readVersion: readVersion,
		metrics:     metrics,
	}
	for i := uint64(0); i < originalCount; i++ {
		metadata, err := native.OriginalFragmentMetadataJSONAt(i)
		if err != nil {
			return nil, err
		}
		result.originalFragmentMetadataJSON = append(result.originalFragmentMetadataJSON, metadata)
	}
	for i := uint64(0); i < newCount; i++ {
		metadata, err := native.NewFragmentMetadataJSONAt(i)
		if err != nil {
			return nil, err
		}
		result.newFragmentMetadataJSON = append(result.newFragmentMetadataJSON, metadata)
	}

	data, err := native.JSON()
	if err != nil {
		return nil, err
	}
	result.json = data
	return result, nil
}

// jsonOptional distinguishes a missing JSON field from an explicit null.
type jsonOptional[T any] struct {
	set   bool
	value *T
}

func (o *jsonOptional[T]) UnmarshalJSON(data []byte) error {
	o.set = true
	if string(data) == "null" {
		o.value = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.value = &value
	return nil
}

func (o jsonOptional[T]) optional() compaction.Optional[T] {
	if !o.set {
		return compaction.Unset[T]()
	}
	if o.value == nil {
		return compaction.None[T]()
	}
	return compaction.Some(*o.value)
}

type compactionOptionsJSON struct {
	TargetRowsPerFragment         *uint64              `json:"target_rows_per_fragment"`
	MaxRowsPerGroup               *uint64              `json:"max_rows_per_group"`
	MaxBytesPerFile               jsonOptional[uint64] `json:"max_bytes_per_file"`
	MaterializeDeletions          *bool                `json:"materialize_deletions"`
	MaterializeDeletionsThreshold *float64             `json:"materialize_deletions_threshold"`
	NumThreads                    jsonOptional[uint64] `json:"num_threads"`
	BatchSize                     jsonOptional[uint64] `json:"batch_size"`
	IOBufferSize                  jsonOptional[uint64] `json:"io_buffer_size"`
	DeferIndexRemap               *bool                `json:"defer_index_remap"`
	IndexRemapMode                *string              `json:"index_remap_mode"`
	CompactionMode                jsonOptional[string] `json:"compaction_mode"`
	BinaryCopyReadBatchBytes      jsonOptional[uint64] `json:"binary_copy_read_batch_bytes"`
	MaxSourceFragments            jsonOptional[uint64] `json:"max_source_fragments"`
	MaxSourceRows                 jsonOptional[uint64] `json:"max_source_rows"`
	MaxSourceBytes                jsonOptional[uint64] `json:"max_source_bytes"`
	ExcludedFragmentIDs           []uint32             `json:"excluded_fragment_ids"`
	MaxOverlaysPerFragment        jsonOptional[uint64] `json:"max_overlays_per_fragment"`
	DataStorageVersion            jsonOptional[string] `json:"data_storage_version"`
}

type compactionDocumentJSON struct {
	Options json.RawMessage `json:"options"`
}

func decodeCompactionOptions(document, kind string) (compaction.Options, error) {
	var envelope compactionDocumentJSON
	if err := json.Unmarshal([]byte(document), &envelope); err != nil {
		return compaction.Options{}, fmt.Errorf("invalid %s JSON: %w", kind, err)
	}
	if len(envelope.Options) == 0 {
		return compaction.Options{}, fmt.Errorf("%s JSON has no options", kind)
	}

	var raw compactionOptionsJSON
	if err := json.Unmarshal(envelope.Options, &raw); err != nil {
		return compaction.Options{}, fmt.Errorf("invalid %s options JSON: %w", kind, err)
	}
	if raw.IndexRemapMode == nil {
		return compaction.Options{}, fmt.Errorf("%s options JSON has no index_remap_mode", kind)
	}
	indexRemapMode, err := parseIndexRemapMode(*raw.IndexRemapMode)
	if err != nil {
		return compaction.Options{}, fmt.Errorf("%s options: %w", kind, err)
	}

	options := compaction.Options{
		TargetRowsPerFragment:         raw.TargetRowsPerFragment,
		MaxRowsPerGroup:               raw.MaxRowsPerGroup,
		MaxBytesPerFile:               raw.MaxBytesPerFile.optional(),
		MaterializeDeletions:          raw.MaterializeDeletions,
		MaterializeDeletionsThreshold: raw.MaterializeDeletionsThreshold,
		NumThreads:                    raw.NumThreads.optional(),
		BatchSize:                     raw.BatchSize.optional(),
		IOBufferSize:                  raw.IOBufferSize.optional(),
		DeferIndexRemap:               raw.DeferIndexRemap,
		IndexRemapMode:                &indexRemapMode,
		CompactionMode:                compaction.Unset[compaction.Mode](),
		BinaryCopyReadBatchBytes:      raw.BinaryCopyReadBatchBytes.optional(),
		MaxSourceFragments:            raw.MaxSourceFragments.optional(),
		MaxSourceRows:                 raw.MaxSourceRows.optional(),
		MaxSourceBytes:                raw.MaxSourceBytes.optional(),
		ExcludedFragmentIDs:           append([]uint32(nil), raw.ExcludedFragmentIDs...),
		MaxOverlaysPerFragment:        raw.MaxOverlaysPerFragment.optional(),
		DataStorageVersion:            raw.DataStorageVersion.optional(),
	}
	if raw.CompactionMode.set {
		if raw.CompactionMode.value == nil {
			options.CompactionMode = compaction.None[compaction.Mode]()
		} else {
			mode, err := parseCompactionMode(*raw.CompactionMode.value)
			if err != nil {
				return compaction.Options{}, fmt.Errorf("%s options: %w", kind, err)
			}
			options.CompactionMode = compaction.Some(mode)
		}
	}
	return options, nil
}

func parseIndexRemapMode(value string) (compaction.IndexRemapMode, error) {
	switch value {
	case "Compact":
		return compaction.IndexRemapCompact, nil
	case "Direct":
		return compaction.IndexRemapDirect, nil
	default:
		return 0, fmt.Errorf("unknown native index remap mode %q", value)
	}
}

func parseCompactionMode(value string) (compaction.Mode, error) {
	switch value {
	case "Reencode":
		return compaction.ModeReencode, nil
	case "TryBinaryCopy":
		return compaction.ModeTryBinaryCopy, nil
	case "ForceBinaryCopy":
		return compaction.ModeForceBinaryCopy, nil
	default:
		return 0, fmt.Errorf("unknown native compaction mode %q", value)
	}
}

func validateCompactionOptions(options *compaction.Options) error {
	if options == nil {
		return nil
	}
	if version, ok := options.DataStorageVersion.Get(); ok {
		if err := validateCString(version); err != nil {
			return fmt.Errorf("data storage version: %w", err)
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
