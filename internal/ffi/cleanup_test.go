package ffi

import "testing"

func TestCleanupExplanationCopiesBorrowedStrings(t *testing.T) {
	path := []byte("_versions/1.manifest\x00")
	branch := []byte("archive\x00")
	warning := []byte("unverified file\x00")
	closes := 0
	b := &bindings{
		lastErrorCode: func() int32 { return 0 },
		freeString: func(*byte) {
			t.Error("borrowed explanation string passed to lance_free_string")
		},
		cleanupExplanationReadVersion:                        func(uintptr) uint64 { return 2 },
		cleanupExplanationStats:                              func(uintptr, *removalStats) int32 { return 0 },
		cleanupExplanationCandidateCount:                     func(uintptr) uint64 { return 1 },
		cleanupExplanationCandidatePathAt:                    func(uintptr, uint64) *byte { return &path[0] },
		cleanupExplanationCandidateKindAt:                    func(uintptr, uint64) int32 { return 0 },
		cleanupExplanationCandidateUnverifiedAt:              func(uintptr, uint64) int32 { return 0 },
		cleanupExplanationCandidateSizeBytesAt:               func(uintptr, uint64) uint64 { return 23 },
		cleanupExplanationCandidateFilesTruncated:            func(uintptr) int32 { return 0 },
		cleanupExplanationCandidateFileLimit:                 func(uintptr) uint64 { return 10 },
		cleanupExplanationReferencedBranchCount:              func(uintptr) uint64 { return 1 },
		cleanupExplanationReferencedBranchNameAt:             func(uintptr, uint64) *byte { return &branch[0] },
		cleanupExplanationReferencedBranchVersionAt:          func(uintptr, uint64) uint64 { return 1 },
		cleanupExplanationReferencedBranchCleanupCandidateAt: func(uintptr, uint64) int32 { return 0 },
		cleanupExplanationWarningCount:                       func(uintptr) uint64 { return 1 },
		cleanupExplanationWarningAt:                          func(uintptr, uint64) *byte { return &warning[0] },
		cleanupExplanationClose: func(uintptr) {
			closes++
			// The producer owns all three strings until its snapshot is closed.
			clear(path)
			clear(branch)
			clear(warning)
		},
	}
	explanation := &cleanupExplanation{b: b, handle: 1}
	defer explanation.Close()
	snapshot, err := explanation.read()
	if err != nil {
		t.Fatal(err)
	}
	explanation.Close()
	explanation.Close()
	if closes != 1 {
		t.Fatalf("producer closes = %d, want 1", closes)
	}
	if len(snapshot.Candidates) != 1 || snapshot.Candidates[0].Path != "_versions/1.manifest" {
		t.Fatalf("copied candidate path = %+v", snapshot.Candidates)
	}
	if len(snapshot.ReferencedBranches) != 1 || snapshot.ReferencedBranches[0].Name != "archive" {
		t.Fatalf("copied branch name = %+v", snapshot.ReferencedBranches)
	}
	if len(snapshot.Warnings) != 1 || snapshot.Warnings[0] != "unverified file" {
		t.Fatalf("copied warnings = %v", snapshot.Warnings)
	}
}
