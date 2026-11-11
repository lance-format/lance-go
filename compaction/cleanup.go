package compaction

import "time"

// CleanupPolicy selects old versions and files for native cleanup.
//
// The selectors are independent and combine with logical AND. Nil pointers
// leave the corresponding native default unchanged. A nil Versions slice
// leaves the native versions selector absent; a non-nil empty slice is passed
// through as an explicit empty selector and is rejected by native validation.
type CleanupPolicy struct {
	// BeforeTimestamp selects versions created before this time.
	BeforeTimestamp *time.Time

	// BeforeVersion selects versions older than this version.
	BeforeVersion *uint64

	// Versions selects specific versions.
	Versions []uint64

	// RetainVersions keeps this many latest versions. It is mutually
	// exclusive with BeforeVersion in the native selector.
	RetainVersions *uint64

	// DeleteUnverified allows deleting files that are not referenced by a
	// manifest.
	DeleteUnverified *bool

	// ErrorIfTaggedOldVersions fails instead of deleting a version that has
	// tags. Native defaults to true.
	ErrorIfTaggedOldVersions *bool

	// CleanReferencedBranches also cleans versions referenced by branches.
	CleanReferencedBranches *bool

	// DeleteRateLimit limits deletions per second.
	DeleteRateLimit *uint64
}

// CleanupOperationOptions controls one cleanup execution.
//
// A nil *CleanupOperationOptions uses native operation defaults.
type CleanupOperationOptions struct {
	// DeleteConcurrency limits concurrent deletions.
	DeleteConcurrency *uint64

	// DeleteRateLimit overrides CleanupPolicy.DeleteRateLimit.
	DeleteRateLimit *uint64

	// MaxCandidateFiles limits how many candidate files are included in an
	// explanation. Zero is valid and includes no candidates while still
	// reporting aggregate statistics and truncation.
	MaxCandidateFiles *uint64
}

// CleanupOldVersionsOptions configures the older-than cleanup helper.
type CleanupOldVersionsOptions struct {
	// OlderThan is the relative age cutoff. Native Lance owns the exact
	// cutoff calculation.
	OlderThan time.Duration

	// DeleteUnverified allows deleting files that are not referenced by a
	// manifest.
	DeleteUnverified *bool

	// ErrorIfTaggedOldVersions fails instead of deleting a version that has
	// tags.
	ErrorIfTaggedOldVersions *bool
}

// RemovalStats reports the work performed by a cleanup.
type RemovalStats struct {
	BytesRemoved            uint64
	OldVersions             uint64
	DataFilesRemoved        uint64
	TransactionFilesRemoved uint64
	IndexFilesRemoved       uint64
	DeletionFilesRemoved    uint64
	FailedDeletes           uint64
}

// FileKind identifies a cleanup candidate file.
type FileKind int32

const (
	// FileKindManifest is a manifest file.
	FileKindManifest FileKind = 0
	// FileKindData is a data file.
	FileKindData FileKind = 1
	// FileKindTransaction is a transaction file.
	FileKindTransaction FileKind = 2
	// FileKindIndex is an index file.
	FileKindIndex FileKind = 3
	// FileKindDeletion is a deletion file.
	FileKindDeletion FileKind = 4
	// FileKindTemporaryManifest is a temporary manifest file.
	FileKindTemporaryManifest FileKind = 5
)

// CandidateFile is one file observed by CleanupExplain.
type CandidateFile struct {
	Path       string
	Kind       FileKind
	Unverified bool
	SizeBytes  uint64
}

// ReferencedBranch is a branch observed by CleanupExplain.
type ReferencedBranch struct {
	Name             string
	Version          uint64
	CleanupCandidate bool
}

// CleanupExplanation is an immutable snapshot of the cleanup candidates that
// existed when the explanation was requested.
//
// It is not a deletion plan or a reservation. Running cleanup re-evaluates
// the current dataset state, so files listed here may no longer be removable,
// and files not listed here may have become removable.
type CleanupExplanation struct {
	ReadVersion             uint64
	Stats                   RemovalStats
	Candidates              []CandidateFile
	CandidateFilesTruncated bool
	CandidateFileLimit      uint64
	ReferencedBranches      []ReferencedBranch
	Warnings                []string
}
