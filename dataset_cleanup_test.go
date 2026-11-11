package lance_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/compaction"
)

func createCleanupHistory(t *testing.T) (*lance.Dataset, string) {
	t.Helper()
	ctx := context.Background()
	dataset, uri, schema := newMutationDataset(t)
	if err := dataset.Close(); err != nil {
		t.Fatalf("Close initial dataset: %v", err)
	}

	for i := 0; i < 3; i++ {
		record := makeRecord(t, schema, []int64{int64(5 + i)}, []string{"x"}, []bool{true})
		next, err := lance.Append(ctx, uri, makeReader(t, schema, record))
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if err := next.Close(); err != nil {
			t.Fatalf("Close appended dataset %d: %v", i, err)
		}
	}

	latest, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open latest: %v", err)
	}
	t.Cleanup(func() { _ = latest.Close() })
	return latest, uri
}

func TestCleanupExplainAndExecute(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, uri := createCleanupHistory(t)

	version, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	before := readMutationRows(t, dataset)
	if len(before) != 7 {
		t.Fatalf("rows before cleanup = %d, want 7", len(before))
	}

	policy := compaction.CleanupPolicy{
		RetainVersions:           compactionPtr(uint64(1)),
		DeleteUnverified:         compactionPtr(false),
		ErrorIfTaggedOldVersions: compactionPtr(false),
	}
	explain, err := dataset.CleanupExplain(ctx, policy, &compaction.CleanupOperationOptions{
		MaxCandidateFiles: compactionPtr(uint64(0)),
	})
	if err != nil {
		t.Fatalf("CleanupExplain: %v", err)
	}
	if explain.ReadVersion != version {
		t.Fatalf("explanation version = %d, want %d", explain.ReadVersion, version)
	}
	if explain.CandidateFileLimit != 0 {
		t.Fatalf("candidate file limit = %d, want explicit zero", explain.CandidateFileLimit)
	}
	if len(explain.Candidates) != 0 {
		t.Fatalf("explanation returned %d candidates despite limit zero", len(explain.Candidates))
	}
	if explain.Stats.OldVersions == 0 {
		t.Fatal("explanation reported no old versions for a four-version history")
	}
	if explain.Stats.FailedDeletes != 0 {
		t.Fatalf("explanation failed deletes = %d, want 0", explain.Stats.FailedDeletes)
	}

	// Explain only observes. It must not advance the dataset or remove rows.
	if got, err := dataset.Version(); err != nil {
		t.Fatalf("Version after explain: %v", err)
	} else if got != version {
		t.Fatalf("version after explain = %d, want %d", got, version)
	}
	if got := readMutationRows(t, dataset); !reflect.DeepEqual(got, before) {
		t.Fatalf("rows after explain = %v, want %v", got, before)
	}

	// A one-hour relative cutoff must not remove versions created moments ago,
	// even though the history has old versions.
	stats, err := dataset.CleanupOldVersions(ctx, compaction.CleanupOldVersionsOptions{
		OlderThan:                time.Hour,
		DeleteUnverified:         compactionPtr(false),
		ErrorIfTaggedOldVersions: compactionPtr(false),
	})
	if err != nil {
		t.Fatalf("CleanupOldVersions with one-hour cutoff: %v", err)
	}
	if stats.OldVersions != 0 {
		t.Fatalf("one-hour cleanup removed %d versions, want 0", stats.OldVersions)
	}
	if got, err := dataset.Version(); err != nil {
		t.Fatalf("Version after duration cleanup: %v", err)
	} else if got != version {
		t.Fatalf("version after duration cleanup = %d, want %d", got, version)
	}

	stats, err = dataset.Cleanup(ctx, policy, &compaction.CleanupOperationOptions{
		DeleteConcurrency: compactionPtr(uint64(1)),
		MaxCandidateFiles: compactionPtr(uint64(0)),
	})
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if stats.OldVersions == 0 {
		t.Fatal("cleanup removed no old versions")
	}
	if stats.FailedDeletes != 0 {
		t.Fatalf("cleanup failed deletes = %d, want 0", stats.FailedDeletes)
	}
	if got, err := dataset.Version(); err != nil {
		t.Fatalf("Version after cleanup: %v", err)
	} else if got != version {
		t.Fatalf("version after cleanup = %d, want unchanged %d", got, version)
	}
	if got := readMutationRows(t, dataset); !reflect.DeepEqual(got, before) {
		t.Fatalf("rows after cleanup = %v, want %v", got, before)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after cleanup: %v", err)
	}
	defer reopened.Close()
	if got := readMutationRows(t, reopened); !reflect.DeepEqual(got, before) {
		t.Fatalf("reopened rows after cleanup = %v, want %v", got, before)
	}

	// A zero-duration cleanup is valid and is a no-op once the old versions
	// have already been removed.
	stats, err = dataset.CleanupOldVersions(ctx, compaction.CleanupOldVersionsOptions{OlderThan: 0})
	if err != nil {
		t.Fatalf("CleanupOldVersions with zero duration: %v", err)
	}
	if stats.OldVersions != 0 {
		t.Fatalf("zero-duration cleanup removed %d versions after cleanup, want 0", stats.OldVersions)
	}
	if got, err := dataset.Version(); err != nil {
		t.Fatalf("Version after zero-duration cleanup: %v", err)
	} else if got != version {
		t.Fatalf("version after zero-duration cleanup = %d, want %d", got, version)
	}
}

func TestCleanupRejectsConflictingPolicy(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, _ := createCleanupHistory(t)

	if _, err := dataset.Cleanup(ctx, compaction.CleanupPolicy{
		Versions: []uint64{},
	}, nil); err == nil {
		t.Fatal("Cleanup accepted an explicit empty versions selector")
	}
	if _, err := dataset.Cleanup(ctx, compaction.CleanupPolicy{
		BeforeVersion:  compactionPtr(uint64(1)),
		RetainVersions: compactionPtr(uint64(1)),
	}, nil); err == nil {
		t.Fatal("Cleanup accepted both before_version and retain_versions")
	}
}
