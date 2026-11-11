package lance_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
)

func ftsTestDataset(t *testing.T) (*lance.Dataset, string) {
	t.Helper()
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema,
		[]int64{1, 2, 3, 4, 5},
		[]string{"alpha beta", "alpha gamma", "beta gamma", "alpha beta gamma", "delta epsilon"},
		[]bool{true, true, true, true, true},
	)
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { dataset.Close() })
	indexed, err := dataset.CreateScalarIndex(ctx, "name", index.ScalarInverted, json.RawMessage(`{"with_position":true}`), index.ScalarOptions{}, lance.WithIndexName("name_fts"))
	if err != nil {
		t.Fatalf("CreateScalarIndex: %v", err)
	}
	t.Cleanup(func() { indexed.Close() })
	return indexed, uri
}

func scanIDs(t *testing.T, dataset *lance.Dataset, opts ...lance.ScanOption) []int64 {
	t.Helper()
	scanner, err := dataset.Scan(append([]lance.ScanOption{lance.WithColumns("id")}, opts...)...)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()
	var ids []int64
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	slices.Sort(ids)
	return ids
}

func equalIDs(got []int64, want ...int64) bool {
	return slices.Equal(got, want)
}

func TestFTSMatchPhraseBooleanAndBoost(t *testing.T) {
	requireNative(t)
	dataset, _ := ftsTestDataset(t)
	ctx := context.Background()

	matchOr, err := lance.NewFTSMatchQuery("alpha delta", "", lance.FTSMatchOptions{Operator: lance.MatchOperatorOr})
	if err != nil {
		t.Fatalf("NewFTSMatchQuery OR: %v", err)
	}
	scanner, err := dataset.Scan(lance.WithColumns("id"), lance.WithFTSQuery(matchOr))
	if err != nil {
		t.Fatalf("Scan with match query: %v", err)
	}
	// The scanner retains its own query, so closing the source before reading
	// is safe.
	if err := matchOr.Close(); err != nil {
		t.Fatalf("Close match query after attach: %v", err)
	}
	var ids []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("match OR records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	scanner.Close()
	slices.Sort(ids)
	if !equalIDs(ids, 1, 2, 4, 5) {
		t.Fatalf("match OR ids = %v, want [1 2 4 5]", ids)
	}

	matchAnd, err := lance.NewFTSMatchQuery("alpha beta", "", lance.FTSMatchOptions{Operator: lance.MatchOperatorAnd})
	if err != nil {
		t.Fatalf("NewFTSMatchQuery AND: %v", err)
	}
	defer matchAnd.Close()
	if ids := scanIDs(t, dataset, lance.WithFTSQuery(matchAnd)); !equalIDs(ids, 1, 4) {
		t.Fatalf("match AND ids = %v, want [1 4]", ids)
	}

	fuzzy, err := lance.NewFTSMatchQuery("alph", "", lance.FTSMatchOptions{Fuzziness: 1})
	if err != nil {
		t.Fatalf("NewFTSMatchQuery fuzzy: %v", err)
	}
	defer fuzzy.Close()
	if ids := scanIDs(t, dataset, lance.WithFTSQuery(fuzzy)); !equalIDs(ids, 1, 2, 4) {
		t.Fatalf("fuzzy match ids = %v, want [1 2 4]", ids)
	}

	phrase, err := lance.NewFTSPhraseQuery("alpha beta", "", lance.FTSPhraseOptions{})
	if err != nil {
		t.Fatalf("NewFTSPhraseQuery: %v", err)
	}
	defer phrase.Close()
	if ids := scanIDs(t, dataset, lance.WithFTSQuery(phrase)); !equalIDs(ids, 1, 4) {
		t.Fatalf("phrase ids = %v, want [1 4]", ids)
	}

	alpha, err := lance.NewFTSMatchQuery("alpha", "", lance.FTSMatchOptions{})
	if err != nil {
		t.Fatalf("alpha query: %v", err)
	}
	defer alpha.Close()
	delta, err := lance.NewFTSMatchQuery("delta", "", lance.FTSMatchOptions{})
	if err != nil {
		t.Fatalf("delta query: %v", err)
	}
	defer delta.Close()
	boolean, err := lance.NewFTSBooleanQuery(
		[]lance.FTSOccur{lance.FTSOccurMust, lance.FTSOccurMustNot},
		[]*lance.FTSQuery{alpha, delta},
	)
	if err != nil {
		t.Fatalf("NewFTSBooleanQuery: %v", err)
	}
	defer boolean.Close()
	if ids := scanIDs(t, dataset, lance.WithFTSQuery(boolean)); !equalIDs(ids, 1, 2, 4) {
		t.Fatalf("boolean ids = %v, want [1 2 4]", ids)
	}

	gamma, err := lance.NewFTSMatchQuery("gamma", "", lance.FTSMatchOptions{})
	if err != nil {
		t.Fatalf("gamma query: %v", err)
	}
	defer gamma.Close()
	boosted, err := lance.NewFTSBoostQuery(alpha, gamma, lance.FTSBoostOptions{NegativeBoost: ptr(float32(0.1))})
	if err != nil {
		t.Fatalf("NewFTSBoostQuery: %v", err)
	}
	defer boosted.Close()
	if ids := scanIDs(t, dataset, lance.WithFTSQuery(boosted)); !equalIDs(ids, 1, 2, 4) {
		t.Fatalf("boost ids = %v, want [1 2 4]", ids)
	}
}

func TestFTSMultiMatchAndConstructionErrors(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "title", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "body", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)
	builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
	builder.Field(1).(*array.StringBuilder).AppendValues([]string{"alpha beta", "gamma"}, nil)
	builder.Field(2).(*array.StringBuilder).AppendValues([]string{"delta", "alpha epsilon"}, nil)
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)

	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()
	indexed, err := dataset.CreateScalarIndex(ctx, "title", index.ScalarInverted, json.RawMessage(`{"with_position":true}`), index.ScalarOptions{}, lance.WithIndexName("title_fts"))
	if err != nil {
		t.Fatalf("CreateScalarIndex title: %v", err)
	}
	defer indexed.Close()
	withBody, err := indexed.CreateScalarIndex(ctx, "body", index.ScalarInverted, json.RawMessage(`{"with_position":true}`), index.ScalarOptions{}, lance.WithIndexName("body_fts"))
	if err != nil {
		t.Fatalf("CreateScalarIndex body: %v", err)
	}
	defer withBody.Close()

	multi, err := lance.NewFTSMultiMatchQuery("alpha", []string{"title", "body"}, lance.FTSMultiMatchOptions{
		Boosts:   []float32{1, 2},
		Operator: lance.MatchOperatorOr,
	})
	if err != nil {
		t.Fatalf("NewFTSMultiMatchQuery: %v", err)
	}
	defer multi.Close()
	if ids := scanIDs(t, withBody, lance.WithFTSQuery(multi)); !equalIDs(ids, 1, 2) {
		t.Fatalf("multi-match ids = %v, want [1 2]", ids)
	}

	combined, err := lance.NewFTSCombinedFieldsQuery("alpha", []string{"title", "body"}, lance.FTSCombinedFieldsOptions{
		Boosts:   []float32{1, 1},
		Operator: lance.MatchOperatorOr,
	})
	if err != nil {
		t.Fatalf("NewFTSCombinedFieldsQuery: %v", err)
	}
	defer combined.Close()
	if ids := scanIDs(t, withBody, lance.WithFTSQuery(combined)); !equalIDs(ids, 1, 2) {
		t.Fatalf("combined-fields ids = %v, want [1 2]", ids)
	}

	if _, err := lance.NewFTSMultiMatchQuery("alpha", nil, lance.FTSMultiMatchOptions{}); err == nil {
		t.Fatalf("NewFTSMultiMatchQuery with no columns succeeded")
	}
	emptyBoolean, err := lance.NewFTSBooleanQuery(nil, nil)
	if err != nil {
		t.Fatalf("NewFTSBooleanQuery empty: %v", err)
	}
	defer emptyBoolean.Close()
	assertFTSPlanningError(t, withBody, "boolean query must have at least one", lance.WithFTSQuery(emptyBoolean))
}

func TestFTSPreparedQueriesPinSnapshot(t *testing.T) {
	requireNative(t)
	dataset, uri := ftsTestDataset(t)
	ctx := context.Background()

	prepared, err := dataset.PrepareFTSMatchQuery(ctx, "name", "alpha", lance.FTSMatchPrepareOptions{
		Operator: lance.MatchOperatorOr,
		Coverage: lance.FTSCoverageStrict,
	})
	if err != nil {
		t.Fatalf("PrepareFTSMatchQuery: %v", err)
	}
	scanner, err := dataset.Scan(lance.WithColumns("id"), lance.WithFTSQueryContext(prepared))
	if err != nil {
		t.Fatalf("Scan with prepared context: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("Close prepared after attach: %v", err)
	}
	var ids []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("prepared records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	scanner.Close()
	slices.Sort(ids)
	if !equalIDs(ids, 1, 2, 4) {
		t.Fatalf("prepared ids = %v, want [1 2 4]", ids)
	}

	prepared, err = dataset.PrepareFTSMatchQuery(ctx, "name", "alpha", lance.FTSMatchPrepareOptions{
		Coverage: lance.FTSCoverageIndexOnly,
	})
	if err != nil {
		t.Fatalf("PrepareFTSMatchQuery index-only: %v", err)
	}
	defer prepared.Close()
	if ids := scanIDs(t, dataset, lance.WithFTSQueryContext(prepared)); !equalIDs(ids, 1, 2, 4) {
		t.Fatalf("index-only ids = %v, want [1 2 4]", ids)
	}

	version, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	reopened, err := lance.Open(ctx, uri, lance.WithVersion(version))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if _, err := reopened.Scan(lance.WithFTSQueryContext(prepared)); err == nil {
		t.Fatalf("prepared context attached to a reopened snapshot")
	}

	phrase, err := dataset.PrepareFTSPhraseQuery(ctx, "name", "alpha beta", lance.FTSPhrasePrepareOptions{})
	if err != nil {
		t.Fatalf("PrepareFTSPhraseQuery: %v", err)
	}
	defer phrase.Close()
	if ids := scanIDs(t, dataset, lance.WithFTSQueryContext(phrase)); !equalIDs(ids, 1, 4) {
		t.Fatalf("prepared phrase ids = %v, want [1 4]", ids)
	}

	uuids, err := dataset.IndexSegmentUUIDs("name_fts")
	if err != nil {
		t.Fatalf("IndexSegmentUUIDs: %v", err)
	}
	if len(uuids) == 0 {
		t.Fatalf("no FTS segments to select")
	}
	segmentQuery, err := lance.NewFTSMatchQuery("alpha", "", lance.FTSMatchOptions{})
	if err != nil {
		t.Fatalf("NewFTSMatchQuery: %v", err)
	}
	defer segmentQuery.Close()
	if scanner, err := dataset.Scan(lance.WithFTSQuery(segmentQuery), lance.WithFTSIndexSegments(uuids...)); err == nil {
		scanner.Close()
		t.Fatal("segment selection with an unprepared query succeeded")
	} else if !strings.Contains(err.Error(), "prepared query context") {
		t.Fatalf("unexpected segment-selection error: %v", err)
	}
	if ids := scanIDs(t, dataset, lance.WithFTSQueryContext(prepared), lance.WithFTSIndexSegments(uuids...)); !equalIDs(ids, 1, 2, 4) {
		t.Fatalf("prepared segment-selected ids = %v, want [1 2 4]", ids)
	}
}

func TestMinHashLSHScan(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema,
		[]int64{1, 2, 3},
		[]string{"hello world", "hello world", "goodbye moon"},
		[]bool{true, true, true},
	)
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()
	indexed, err := dataset.CreateScalarIndex(ctx, "name", index.ScalarMinHashLSH, json.RawMessage(`{}`), index.ScalarOptions{}, lance.WithIndexName("name_minhash"))
	if err != nil {
		t.Fatalf("CreateScalarIndex MinHash: %v", err)
	}
	defer indexed.Close()

	ids := scanIDs(t, indexed, lance.WithMinHashQuery("name", "hello world"), lance.WithLimit(1))
	if len(ids) != 1 {
		t.Fatalf("MinHash ids = %v, want exactly one row", ids)
	}
	if ids[0] != 1 && ids[0] != 2 {
		t.Fatalf("MinHash nearest id = %d, want 1 or 2", ids[0])
	}

	scanner, err := indexed.Scan(lance.WithColumns("id", "_distance"), lance.WithMinHashQuery("name", "hello world"), lance.WithLimit(1))
	if err != nil {
		t.Fatalf("Scan MinHash distance: %v", err)
	}
	defer scanner.Close()
	sawDistance := false
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("MinHash distance records: %v", err)
		}
		for i := 0; i < int(batch.NumCols()); i++ {
			if batch.Schema().Field(i).Name == "_distance" {
				sawDistance = true
			}
		}
	}
	if !sawDistance {
		t.Fatalf("MinHash scan did not return _distance")
	}

	if ids := scanIDs(t, indexed, lance.WithMinHashQuery("name", ""), lance.WithLimit(1)); len(ids) != 0 {
		t.Fatalf("MinHash empty text ids = %v, want no rows", ids)
	}

	assertFTSPlanningError(t, indexed, "MinHash search requires a limit", lance.WithMinHashQuery("name", "hello world"))
	if _, err := indexed.Scan(lance.WithMinHashQuery("", "hello world"), lance.WithLimit(1)); err == nil {
		t.Fatalf("MinHash scan with empty column succeeded")
	}
}

// Planning errors must be observed by consuming the lazy native stream.
func assertFTSPlanningError(t *testing.T, dataset *lance.Dataset, message string, opts ...lance.ScanOption) {
	t.Helper()
	scanner, err := dataset.Scan(opts...)
	if err != nil {
		t.Fatalf("Scan failed before planning: %v", err)
	}
	defer scanner.Close()
	for _, err := range scanner.Records(context.Background()) {
		if err != nil {
			if !strings.Contains(err.Error(), message) {
				t.Fatalf("planning error = %v, want %q", err, message)
			}
			return
		}
	}
	t.Fatalf("planning succeeded, want %q", message)
}
