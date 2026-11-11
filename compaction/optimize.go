package compaction

// OptimizeOptions configures native index optimization.
//
// A nil *OptimizeOptions passed to a Dataset method selects the native
// defaults (the C API's NULL options). A non-nil value starts from native
// defaults and applies only the fields that are set.
//
// Unset optional fields leave the native default unchanged; None sets the
// native option to None explicitly; Some sets it, including an explicit zero
// or an empty slice. IndexNames and TransactionProperties are copied before
// the call returns.
//
// Native plan, task, merge and result JSON do not serialize OptimizeOptions.
// Task JSON carries the merge options that execution re-applies
// (NumIndicesToMerge, EnablePartitionRebalancing and Retrain). IndexNames and
// the row bounds only select and split the plan. TransactionProperties apply
// only to a commit, so pass them again to CommitOptimizeIndices when the
// results crossed a JSON transport boundary.
type OptimizeOptions struct {
	// NumIndicesToMerge limits how many existing index segments one merge
	// absorbs. Unset leaves the native default (None): a split build appends a
	// delta, while the single-process optimizer may instead merge all delta
	// segments when a vector index's partitions need rebalancing. Some(n)
	// merges the latest n segments plus the new updates, and Some(0) disables
	// merging and appends a delta.
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

	// MaxRowsPerTask bounds the rows one build task covers. Unset and None both
	// leave the native bound unset (unbounded); Some sets it, including zero.
	MaxRowsPerTask Optional[uint64]

	// MaxRowsPerSegment bounds the rows in one output segment. Unset and None
	// both leave the native bound unset (unbounded); Some sets it, including
	// zero.
	MaxRowsPerSegment Optional[uint64]
}
