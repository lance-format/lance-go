package lance

import (
	"context"

	"github.com/lance-format/lance-go/compaction"
)

// CleanupOldVersions removes old versions and their unreferenced files.
//
// Cleanup is destructive storage maintenance. It does not take a snapshot
// lease, does not change the dataset's current version, and cannot roll back
// files that were already deleted when a later deletion fails. Snapshot handles
// do not protect removed files: a Dataset or scanner opened at an old version
// may lose access to its storage after cleanup.
//
// OlderThan is relative to the current time and is evaluated by native Lance.
// The native operation is synchronous. ctx is checked before work starts; it is
// not observed once native cleanup is in progress. If cleanup succeeds after
// ctx is canceled, its removal stats are returned instead of a cancellation
// error.
func (d *Dataset) CleanupOldVersions(ctx context.Context, options compaction.CleanupOldVersionsOptions) (compaction.RemovalStats, error) {
	const op = "cleanup old versions"
	if err := checkContext(ctx); err != nil {
		return compaction.RemovalStats{}, err
	}
	if err := d.acquire(op); err != nil {
		return compaction.RemovalStats{}, err
	}
	defer d.res.release()

	stats, err := d.native.CleanupOldVersions(options)
	if err != nil {
		return compaction.RemovalStats{}, wrapError(op, d.uri, err)
	}
	return stats, nil
}

// Cleanup removes unreferenced storage selected by an explicit policy.
//
// Selectors in policy are independent and combine with AND. Set only one of
// BeforeVersion or RetainVersions: native cleanup rejects both together. A nil
// Versions slice means no version selector; a non-nil empty slice is an explicit
// empty selector that native validation rejects.
//
// options overrides operation settings such as delete concurrency and rate
// limit. A nil options value uses native operation defaults. When both policy
// and options set a delete rate limit, the operation option wins.
//
// Cleanup is destructive storage maintenance with no snapshot lease and no
// rollback after partial deletion. It observes the current dataset state and
// re-evaluates references when executed. ctx is checked before native cleanup
// starts; it is not observed while native cleanup is running.
func (d *Dataset) Cleanup(ctx context.Context, policy compaction.CleanupPolicy, options *compaction.CleanupOperationOptions) (compaction.RemovalStats, error) {
	const op = "cleanup"
	if err := checkContext(ctx); err != nil {
		return compaction.RemovalStats{}, err
	}
	if err := d.acquire(op); err != nil {
		return compaction.RemovalStats{}, err
	}
	defer d.res.release()

	stats, err := d.native.Cleanup(policy, options)
	if err != nil {
		return compaction.RemovalStats{}, wrapError(op, d.uri, err)
	}
	return stats, nil
}

// CleanupExplain reports the storage that the policy would consider removing
// from the current dataset state.
//
// The explanation is a snapshot, not a deletion plan or a reservation. Files can
// change before execution, and Cleanup re-evaluates references. MaxCandidateFiles
// limits only the reported candidate list; aggregate stats and the truncation
// flag describe the full candidate set.
//
// CleanupExplain deletes no files and does not take a snapshot lease. ctx is
// checked before native work starts; it is not observed while native work is
// running.
func (d *Dataset) CleanupExplain(ctx context.Context, policy compaction.CleanupPolicy, options *compaction.CleanupOperationOptions) (*compaction.CleanupExplanation, error) {
	const op = "cleanup explain"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()

	explanation, err := d.native.CleanupExplain(policy, options)
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	return explanation, nil
}
