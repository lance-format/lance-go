package compaction

// OptimizeOptions configures native index optimization.
//
// A nil *OptimizeOptions passed to a Dataset method selects the native
// defaults (the C API's NULL options). A non-nil value starts from native
// defaults and applies only the fields that are set.
//
// Pointer fields distinguish an absent value from an explicit zero or false.
// Optional fields additionally distinguish an explicit native None from an
// unset value. IndexNames and TransactionProperties are copied before the
// call returns.
//
// Native plan, task, merge and result JSON does not serialize these options.
// They only configure plan creation, single-process optimization and the
// commit transaction, so pass TransactionProperties again to
// CommitOptimizeIndices when the results crossed a JSON transport boundary.
type OptimizeOptions struct {
	// NumIndicesToMerge limits how many existing delta index segments one
	// index merge covers. Unset leaves the native default (none, which starts
	// a new delta segment when an index is split); Some(n) merges up to n
	// updates plus the latest delta segments, and Some(0) requests append-only
	// behavior.
	NumIndicesToMerge Optional[uint64]

	// EnablePartitionRebalancing lets native optimization rebalance index
	// partitions. Native validation rejects it together with Some(0) or with
	// MaxRowsPerTask or MaxRowsPerSegment.
	EnablePartitionRebalancing bool

	// IndexNames limits optimization to the named indices. Unset and None
	// leave the native default of every index; Some(nil) or Some with an
	// empty slice explicitly selects no index.
	IndexNames Optional[[]string]

	// Retrain merges every segment of a selected vector index into one and
	// retrains it. Native optimization only supports this for v3 vector
	// indices.
	Retrain bool

	// TransactionProperties adds properties to the commit transaction. Nil
	// leaves the native option unset; a non-nil map, including an empty one,
	// sets it. The map is copied before the call returns.
	TransactionProperties map[string]string

	// MaxRowsPerTask bounds the rows one task reads. None leaves the native
	// option unset; unset also leaves it unset.
	MaxRowsPerTask Optional[uint64]

	// MaxRowsPerSegment bounds the rows in one output segment. None leaves
	// the native option unset; unset also leaves it unset.
	MaxRowsPerSegment Optional[uint64]
}
