package lance_test

import (
	"context"
	"testing"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/compaction"
)

func TestFTSAppendOptimizeIndicesPreservesMatchesAfterReopen(t *testing.T) {
	requireNative(t)
	_, uri := ftsTestDataset(t)
	ctx := context.Background()

	schema := testSchema()
	appended, err := lance.Append(ctx, uri, makeReader(t, schema, makeRecord(t, schema,
		[]int64{6, 7},
		[]string{"alpha omega", "alpha pi"},
		[]bool{true, true},
	)))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	defer appended.Close()

	optimized, err := appended.OptimizeIndices(ctx, &compaction.OptimizeOptions{
		NumIndicesToMerge: compaction.Some(uint64(1)),
		IndexNames:        compaction.Some([]string{"name_fts"}),
	})
	if err != nil {
		t.Fatalf("OptimizeIndices: %v", err)
	}
	defer optimized.Close()

	assertIndexedMatches := func(dataset *lance.Dataset, stage string) {
		t.Helper()
		// Strict preparation rules out a flat scan of unindexed appended rows.
		prepared, err := dataset.PrepareFTSMatchQuery(ctx, "name", "alpha", lance.FTSMatchPrepareOptions{
			Coverage: lance.FTSCoverageStrict,
		})
		if err != nil {
			t.Fatalf("%s strict FTS preparation: %v", stage, err)
		}
		defer prepared.Close()
		if ids := scanIDs(t, dataset, lance.WithFTSQueryContext(prepared)); !equalIDs(ids, 1, 2, 4, 6, 7) {
			t.Fatalf("%s indexed FTS ids = %v, want [1 2 4 6 7]", stage, ids)
		}
	}
	assertIndexedMatches(optimized, "optimized")

	if err := optimized.Close(); err != nil {
		t.Fatalf("close optimized dataset: %v", err)
	}
	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("reopen optimized dataset: %v", err)
	}
	defer reopened.Close()
	assertIndexedMatches(reopened, "reopened")
}
