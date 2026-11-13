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
		NumIndicesToMerge: compaction.Some(uint64(0)),
		IndexNames:        compaction.Some([]string{"name_fts"}),
	})
	if err != nil {
		t.Fatalf("OptimizeIndices: %v", err)
	}
	defer optimized.Close()

	query, err := lance.NewFTSMatchQuery("alpha", "", lance.FTSMatchOptions{})
	if err != nil {
		t.Fatalf("NewFTSMatchQuery: %v", err)
	}
	defer query.Close()
	if ids := scanIDs(t, optimized, lance.WithFTSQuery(query)); !equalIDs(ids, 1, 2, 4, 6, 7) {
		t.Fatalf("optimized FTS ids = %v, want [1 2 4 6 7]", ids)
	}

	if err := optimized.Close(); err != nil {
		t.Fatalf("close optimized dataset: %v", err)
	}
	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("reopen optimized dataset: %v", err)
	}
	defer reopened.Close()
	if ids := scanIDs(t, reopened, lance.WithFTSQuery(query)); !equalIDs(ids, 1, 2, 4, 6, 7) {
		t.Fatalf("reopened FTS ids = %v, want [1 2 4 6 7]", ids)
	}
}
