package ffi

import (
	"fmt"
	"runtime"
	"time"

	"github.com/lance-format/lance-go/compaction"
)

// removalStats mirrors LanceRemovalStats from the pinned header.
type removalStats struct {
	BytesRemoved            uint64
	OldVersions             uint64
	DataFilesRemoved        uint64
	TransactionFilesRemoved uint64
	IndexFilesRemoved       uint64
	DeletionFilesRemoved    uint64
	FailedDeletes           uint64
}

func (s removalStats) stats() compaction.RemovalStats {
	return compaction.RemovalStats{
		BytesRemoved:            s.BytesRemoved,
		OldVersions:             s.OldVersions,
		DataFilesRemoved:        s.DataFilesRemoved,
		TransactionFilesRemoved: s.TransactionFilesRemoved,
		IndexFilesRemoved:       s.IndexFilesRemoved,
		DeletionFilesRemoved:    s.DeletionFilesRemoved,
		FailedDeletes:           s.FailedDeletes,
	}
}

// cleanupPolicy mirrors LanceCleanupPolicy. Zero values leave native defaults
// unchanged.
type cleanupPolicy struct {
	HasBeforeTimestamp          int32
	BeforeTimestampSeconds      int64
	BeforeTimestampNanos        uint32
	HasBeforeVersion            int32
	BeforeVersion               uint64
	Versions                    *uint64
	NumVersions                 uint64
	HasRetainVersions           int32
	RetainVersions              uint64
	HasDeleteUnverified         int32
	DeleteUnverified            bool
	HasErrorIfTaggedOldVersions int32
	ErrorIfTaggedOldVersions    bool
	HasCleanReferencedBranches  int32
	CleanReferencedBranches     bool
	HasDeleteRateLimit          int32
	DeleteRateLimit             uint64
}

// cleanupOperationOptions mirrors LanceCleanupOperationOptions.
type cleanupOperationOptions struct {
	HasDeleteConcurrency int32
	DeleteConcurrency    uint64
	HasDeleteRateLimit   int32
	DeleteRateLimit      uint64
	HasMaxCandidateFiles int32
	MaxCandidateFiles    uint64
}

// cleanupOldVersionsOptions mirrors LanceCleanupOldVersionsOptions.
type cleanupOldVersionsOptions struct {
	OlderThanSeconds            int64
	OlderThanNanos              int32
	HasDeleteUnverified         int32
	DeleteUnverified            bool
	HasErrorIfTaggedOldVersions int32
	ErrorIfTaggedOldVersions    bool
}

func buildCleanupPolicy(policy compaction.CleanupPolicy) (*cleanupPolicy, []uint64) {
	raw := &cleanupPolicy{}
	if policy.BeforeTimestamp != nil {
		raw.HasBeforeTimestamp = 1
		raw.BeforeTimestampSeconds = policy.BeforeTimestamp.Unix()
		raw.BeforeTimestampNanos = uint32(policy.BeforeTimestamp.Nanosecond())
	}
	if policy.BeforeVersion != nil {
		raw.HasBeforeVersion = 1
		raw.BeforeVersion = *policy.BeforeVersion
	}
	versions := policy.Versions
	if versions != nil {
		raw.NumVersions = uint64(len(versions))
		if len(versions) == 0 {
			// A non-NULL pointer with a zero count represents an explicit
			// empty selector. Native validation rejects it.
			versions = []uint64{0}
		}
		raw.Versions = &versions[0]
	}
	if policy.RetainVersions != nil {
		raw.HasRetainVersions = 1
		raw.RetainVersions = *policy.RetainVersions
	}
	if policy.DeleteUnverified != nil {
		raw.HasDeleteUnverified = 1
		raw.DeleteUnverified = *policy.DeleteUnverified
	}
	if policy.ErrorIfTaggedOldVersions != nil {
		raw.HasErrorIfTaggedOldVersions = 1
		raw.ErrorIfTaggedOldVersions = *policy.ErrorIfTaggedOldVersions
	}
	if policy.CleanReferencedBranches != nil {
		raw.HasCleanReferencedBranches = 1
		raw.CleanReferencedBranches = *policy.CleanReferencedBranches
	}
	if policy.DeleteRateLimit != nil {
		raw.HasDeleteRateLimit = 1
		raw.DeleteRateLimit = *policy.DeleteRateLimit
	}
	return raw, versions
}

func buildCleanupOperation(options *compaction.CleanupOperationOptions) *cleanupOperationOptions {
	if options == nil {
		return nil
	}
	raw := &cleanupOperationOptions{}
	if options.DeleteConcurrency != nil {
		raw.HasDeleteConcurrency = 1
		raw.DeleteConcurrency = *options.DeleteConcurrency
	}
	if options.DeleteRateLimit != nil {
		raw.HasDeleteRateLimit = 1
		raw.DeleteRateLimit = *options.DeleteRateLimit
	}
	if options.MaxCandidateFiles != nil {
		raw.HasMaxCandidateFiles = 1
		raw.MaxCandidateFiles = *options.MaxCandidateFiles
	}
	return raw
}

// CleanupOldVersions removes versions older than the requested duration.
//
// Cleanup is destructive storage maintenance. It does not take a snapshot
// lease and cannot roll back successful deletions if a later deletion fails.
// Snapshot handles do not protect removed files.
func (d *Dataset) CleanupOldVersions(options compaction.CleanupOldVersionsOptions) (compaction.RemovalStats, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return compaction.RemovalStats{}, ErrClosed
	}

	seconds := int64(options.OlderThan / time.Second)
	nanos := int32(options.OlderThan - time.Duration(seconds)*time.Second)
	raw := cleanupOldVersionsOptions{
		OlderThanSeconds: seconds,
		OlderThanNanos:   nanos,
	}
	if options.DeleteUnverified != nil {
		raw.HasDeleteUnverified = 1
		raw.DeleteUnverified = *options.DeleteUnverified
	}
	if options.ErrorIfTaggedOldVersions != nil {
		raw.HasErrorIfTaggedOldVersions = 1
		raw.ErrorIfTaggedOldVersions = *options.ErrorIfTaggedOldVersions
	}

	var out removalStats
	runtime.LockOSThread()
	rc := d.b.datasetCleanupOldVersions(handle, &raw, &out)
	runtime.KeepAlive(&raw)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return compaction.RemovalStats{}, nativeErr
		}
		return compaction.RemovalStats{}, fmt.Errorf("ffi: lance_dataset_cleanup_old_versions failed without setting an error")
	}
	runtime.UnlockOSThread()
	return out.stats(), nil
}

// Cleanup runs native cleanup with an explicit policy.
func (d *Dataset) Cleanup(policy compaction.CleanupPolicy, options *compaction.CleanupOperationOptions) (compaction.RemovalStats, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return compaction.RemovalStats{}, ErrClosed
	}

	rawPolicy, versions := buildCleanupPolicy(policy)
	rawOptions := buildCleanupOperation(options)

	var out removalStats
	runtime.LockOSThread()
	rc := d.b.datasetCleanup(handle, rawPolicy, rawOptions, &out)
	runtime.KeepAlive(rawPolicy)
	runtime.KeepAlive(versions)
	runtime.KeepAlive(rawOptions)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return compaction.RemovalStats{}, nativeErr
		}
		return compaction.RemovalStats{}, fmt.Errorf("ffi: lance_dataset_cleanup failed without setting an error")
	}
	runtime.UnlockOSThread()
	return out.stats(), nil
}

// cleanupExplanation owns a native LanceCleanupExplanation handle.
type cleanupExplanation struct {
	b      *bindings
	handle uintptr
}

func (e *cleanupExplanation) Close() {
	if e == nil {
		return
	}
	handle := e.handle
	if handle == 0 {
		return
	}
	e.handle = 0

	b := e.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b.cleanupExplanationClose(handle)
}

func (e *cleanupExplanation) read() (*compaction.CleanupExplanation, error) {
	handle := e.handle
	if handle == 0 {
		return nil, ErrClosed
	}
	b := e.b

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	readVersion := b.cleanupExplanationReadVersion(handle)
	if err := b.takeError(); err != nil {
		return nil, err
	}

	var nativeStats removalStats
	if rc := b.cleanupExplanationStats(handle, &nativeStats); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_cleanup_explanation_stats failed without setting an error")
	}

	candidateCount := b.cleanupExplanationCandidateCount(handle)
	if err := b.takeError(); err != nil {
		return nil, err
	}
	candidates := make([]compaction.CandidateFile, 0, int(candidateCount))
	for i := uint64(0); i < candidateCount; i++ {
		pathPtr := b.cleanupExplanationCandidatePathAt(handle, i)
		if pathPtr == nil {
			if nativeErr := b.takeError(); nativeErr != nil {
				return nil, nativeErr
			}
			return nil, fmt.Errorf("ffi: lance_cleanup_explanation_candidate_path_at returned NULL without setting an error")
		}
		path := goString(pathPtr)
		b.freeString(pathPtr)

		kind := b.cleanupExplanationCandidateKindAt(handle, i)
		if err := b.takeError(); err != nil {
			return nil, err
		}
		if kind < 0 {
			return nil, fmt.Errorf("ffi: lance_cleanup_explanation_candidate_kind_at failed without setting an error")
		}
		unverified := b.cleanupExplanationCandidateUnverifiedAt(handle, i)
		if err := b.takeError(); err != nil {
			return nil, err
		}
		if unverified < 0 {
			return nil, fmt.Errorf("ffi: lance_cleanup_explanation_candidate_unverified_at failed without setting an error")
		}
		sizeBytes := b.cleanupExplanationCandidateSizeBytesAt(handle, i)
		if err := b.takeError(); err != nil {
			return nil, err
		}
		candidates = append(candidates, compaction.CandidateFile{
			Path:       path,
			Kind:       compaction.FileKind(kind),
			Unverified: unverified != 0,
			SizeBytes:  sizeBytes,
		})
	}

	truncated := b.cleanupExplanationCandidateFilesTruncated(handle)
	if err := b.takeError(); err != nil {
		return nil, err
	}
	if truncated < 0 {
		return nil, fmt.Errorf("ffi: lance_cleanup_explanation_candidate_files_truncated failed without setting an error")
	}
	candidateLimit := b.cleanupExplanationCandidateFileLimit(handle)
	if err := b.takeError(); err != nil {
		return nil, err
	}

	branchCount := b.cleanupExplanationReferencedBranchCount(handle)
	if err := b.takeError(); err != nil {
		return nil, err
	}
	branches := make([]compaction.ReferencedBranch, 0, int(branchCount))
	for i := uint64(0); i < branchCount; i++ {
		namePtr := b.cleanupExplanationReferencedBranchNameAt(handle, i)
		if namePtr == nil {
			if nativeErr := b.takeError(); nativeErr != nil {
				return nil, nativeErr
			}
			return nil, fmt.Errorf("ffi: lance_cleanup_explanation_referenced_branch_name_at returned NULL without setting an error")
		}
		name := goString(namePtr)
		b.freeString(namePtr)

		version := b.cleanupExplanationReferencedBranchVersionAt(handle, i)
		if err := b.takeError(); err != nil {
			return nil, err
		}
		candidate := b.cleanupExplanationReferencedBranchCleanupCandidateAt(handle, i)
		if err := b.takeError(); err != nil {
			return nil, err
		}
		if candidate < 0 {
			return nil, fmt.Errorf("ffi: lance_cleanup_explanation_referenced_branch_cleanup_candidate_at failed without setting an error")
		}
		branches = append(branches, compaction.ReferencedBranch{
			Name:             name,
			Version:          version,
			CleanupCandidate: candidate != 0,
		})
	}

	warningCount := b.cleanupExplanationWarningCount(handle)
	if err := b.takeError(); err != nil {
		return nil, err
	}
	warnings := make([]string, 0, int(warningCount))
	for i := uint64(0); i < warningCount; i++ {
		warningPtr := b.cleanupExplanationWarningAt(handle, i)
		if warningPtr == nil {
			if nativeErr := b.takeError(); nativeErr != nil {
				return nil, nativeErr
			}
			return nil, fmt.Errorf("ffi: lance_cleanup_explanation_warning_at returned NULL without setting an error")
		}
		warning := goString(warningPtr)
		b.freeString(warningPtr)
		warnings = append(warnings, warning)
	}

	return &compaction.CleanupExplanation{
		ReadVersion:             readVersion,
		Stats:                   nativeStats.stats(),
		Candidates:              candidates,
		CandidateFilesTruncated: truncated != 0,
		CandidateFileLimit:      candidateLimit,
		ReferencedBranches:      branches,
		Warnings:                warnings,
	}, nil
}

// CleanupExplain reports cleanup candidates for the current dataset state.
func (d *Dataset) CleanupExplain(policy compaction.CleanupPolicy, options *compaction.CleanupOperationOptions) (*compaction.CleanupExplanation, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}

	rawPolicy, versions := buildCleanupPolicy(policy)
	rawOptions := buildCleanupOperation(options)

	runtime.LockOSThread()
	explanationHandle := d.b.datasetCleanupExplain(handle, rawPolicy, rawOptions)
	runtime.KeepAlive(rawPolicy)
	runtime.KeepAlive(versions)
	runtime.KeepAlive(rawOptions)
	if explanationHandle == 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_cleanup_explain returned NULL without setting an error")
	}
	runtime.UnlockOSThread()

	explanation := &cleanupExplanation{b: d.b, handle: explanationHandle}
	defer explanation.Close()
	return explanation.read()
}
