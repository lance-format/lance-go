// Package compaction contains the option, metric, and transport values used
// by dataset compaction.
//
// The types in this package are independent of a Dataset and of the native
// bindings. Dataset methods in the root package perform the actual native
// calls. Plan, task, and rewrite-result values are immutable snapshots: the
// native serialized JSON is authoritative, so a plan can be sent to another
// process and executed there without carrying a native handle across the
// boundary.
package compaction

import "fmt"

// Mode selects how compaction rewrites fragments.
type Mode int32

const (
	// ModeReencode rewrites data through the normal encoding path.
	ModeReencode Mode = 0
	// ModeTryBinaryCopy uses binary copy when it is safe, and re-encodes
	// fragments that cannot be copied.
	ModeTryBinaryCopy Mode = 1
	// ModeForceBinaryCopy requires binary copy and fails when a fragment
	// cannot be copied.
	ModeForceBinaryCopy Mode = 2
)

// String returns the native option name.
func (m Mode) String() string {
	switch m {
	case ModeReencode:
		return "Reencode"
	case ModeTryBinaryCopy:
		return "TryBinaryCopy"
	case ModeForceBinaryCopy:
		return "ForceBinaryCopy"
	default:
		return fmt.Sprintf("Mode(%d)", int32(m))
	}
}

// IndexRemapMode selects how compaction remaps indexes.
type IndexRemapMode int32

const (
	// IndexRemapCompact remaps indexes through the compaction path.
	IndexRemapCompact IndexRemapMode = 0
	// IndexRemapDirect remaps indexes directly.
	IndexRemapDirect IndexRemapMode = 1
)

// String returns the native option name.
func (m IndexRemapMode) String() string {
	switch m {
	case IndexRemapCompact:
		return "Compact"
	case IndexRemapDirect:
		return "Direct"
	default:
		return fmt.Sprintf("IndexRemapMode(%d)", int32(m))
	}
}

// Optional represents a native optional option. Its zero value is unset,
// which leaves the corresponding native default unchanged.
//
// Some and None map to the native setter's has_value flag. The distinction
// matters for options whose native default is Some(...), such as
// MaxOverlaysPerFragment and BinaryCopyReadBatchBytes.
type Optional[T any] struct {
	set   bool
	value *T
}

// Some returns an optional value with a value present.
func Some[T any](value T) Optional[T] {
	return Optional[T]{set: true, value: &value}
}

// None returns an optional value that is explicitly absent.
func None[T any]() Optional[T] {
	return Optional[T]{set: true}
}

// Unset returns an unset optional value.
func Unset[T any]() Optional[T] {
	return Optional[T]{}
}

// IsSet reports whether the option was set to either a value or none.
func (o Optional[T]) IsSet() bool { return o.set }

// IsNone reports whether the option was explicitly set to none.
func (o Optional[T]) IsNone() bool { return o.set && o.value == nil }

// Get returns the option value and whether a value is present.
func (o Optional[T]) Get() (T, bool) {
	if o.value == nil {
		var zero T
		return zero, false
	}
	return *o.value, true
}

// Metrics reports the fragments and files changed by a compaction.
type Metrics struct {
	FragmentsRemoved uint64
	FragmentsAdded   uint64
	FilesRemoved     uint64
	FilesAdded       uint64
}

// Options configures native CompactionOptionsV2.
//
// A nil *Options passed to a Dataset method selects the native
// from-dataset-config options (the C API's NULL options). A non-nil Options
// starts from native defaults and applies only the fields that are set.
//
// Pointer fields distinguish an absent value from an explicit zero or false.
// Optional fields additionally distinguish an explicit native None from an
// unset value. ExcludedFragmentIDs and TransactionProperties are copied before
// the call returns.
type Options struct {
	// TargetRowsPerFragment is the target number of rows per output
	// fragment. Nil leaves the native default (1048576) unchanged.
	TargetRowsPerFragment *uint64

	// MaxRowsPerGroup is the maximum number of rows per output group. Nil
	// leaves the native default (1024) unchanged.
	MaxRowsPerGroup *uint64

	// MaxBytesPerFile limits output file size. Nil leaves the native
	// default (none) unchanged; Some(0) is an explicit native value.
	MaxBytesPerFile Optional[uint64]

	// MaterializeDeletions controls whether deletions are materialized.
	MaterializeDeletions *bool

	// MaterializeDeletionsThreshold is the deletion density above which
	// deletions are materialized. It is converted to a native float.
	MaterializeDeletionsThreshold *float64

	// NumThreads limits native compaction parallelism.
	NumThreads Optional[uint64]

	// BatchSize sets the native read batch size.
	BatchSize Optional[uint64]

	// IOBufferSize sets the native I/O buffer size.
	IOBufferSize Optional[uint64]

	// DeferIndexRemap defers index remapping to the commit path.
	DeferIndexRemap *bool

	// IndexRemapMode selects the index remap strategy.
	IndexRemapMode *IndexRemapMode

	// CompactionMode selects re-encode or binary copy.
	CompactionMode Optional[Mode]

	// BinaryCopyReadBatchBytes sets the binary-copy read batch size.
	BinaryCopyReadBatchBytes Optional[uint64]

	// MaxSourceFragments limits how many source fragments one task reads.
	MaxSourceFragments Optional[uint64]

	// MaxSourceRows limits how many source rows one task reads.
	MaxSourceRows Optional[uint64]

	// MaxSourceBytes limits how many source bytes one task reads.
	MaxSourceBytes Optional[uint64]

	// ExcludedFragmentIDs lists fragments that must not be compacted.
	ExcludedFragmentIDs []uint32

	// MaxOverlaysPerFragment limits the number of deletion overlays
	// considered per fragment.
	MaxOverlaysPerFragment Optional[uint64]

	// DataStorageVersion selects the output storage version, for example
	// "2.1" or "stable". None explicitly clears the native option.
	DataStorageVersion Optional[string]

	// TransactionProperties adds properties to the commit transaction. Nil
	// leaves the native option unset; a non-nil map, including an empty one,
	// sets it. The map is copied before the call returns.
	//
	// Native plan and task JSON does not serialize transaction properties.
	// Pass them again to CommitCompaction when a plan or result crossed a
	// JSON transport boundary.
	TransactionProperties map[string]string
}
