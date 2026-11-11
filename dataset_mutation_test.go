package lance_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
)

func newMutationDataset(t *testing.T) (*lance.Dataset, string, *arrow.Schema) {
	t.Helper()
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"}, []bool{true, true, true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = dataset.Close() })
	return dataset, uri, schema
}

type mutationRow struct {
	ID         int64
	Name       string
	NameValid  bool
	Score      int64
	ScoreValid bool
}

func readMutationRows(t *testing.T, dataset *lance.Dataset) []mutationRow {
	t.Helper()
	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()

	var rows []mutationRow
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids := collectInt64(t, batch, "id")
		names := findColumn(t, batch, "name").(*array.String)
		var scores *array.Int64
		for i := 0; i < int(batch.NumCols()); i++ {
			if batch.Schema().Field(i).Name == "score" {
				scores, _ = batch.Column(i).(*array.Int64)
			}
		}
		for i, id := range ids {
			row := mutationRow{
				ID:        id,
				Name:      names.Value(i),
				NameValid: !names.IsNull(i),
			}
			if scores != nil {
				row.ScoreValid = !scores.IsNull(i)
				if row.ScoreValid {
					row.Score = scores.Value(i)
				}
			}
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func mutationIDs(rows []mutationRow) []int64 {
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

func mutationNames(rows []mutationRow) []string {
	names := make([]string, len(rows))
	for i, row := range rows {
		names[i] = row.Name
	}
	return names
}

func requireMutationRows(t *testing.T, dataset *lance.Dataset, ids []int64, names []string) []mutationRow {
	t.Helper()
	rows := readMutationRows(t, dataset)
	if got := mutationIDs(rows); !slices.Equal(got, ids) {
		t.Fatalf("rows ids = %v, want %v", got, ids)
	}
	if names != nil {
		if got := mutationNames(rows); !slices.Equal(got, names) {
			t.Fatalf("rows names = %v, want %v", got, names)
		}
	}
	return rows
}

func mutationVersion(t *testing.T, dataset *lance.Dataset) uint64 {
	t.Helper()
	version, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	return version
}

// releaseCountingReader records how many times the Arrow stream releases its
// retained reference to the reader. It embeds the original reader so the
// stream's Retain and all iteration methods still reach it.
type releaseCountingReader struct {
	array.RecordReader
	releases int
}

func (r *releaseCountingReader) Release() {
	r.releases++
	r.RecordReader.Release()
}

func makeMergeReader(t *testing.T, ids, scores []int64) array.RecordReader {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "score", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)
	idBuilder := builder.Field(0).(*array.Int64Builder)
	scoreBuilder := builder.Field(1).(*array.Int64Builder)
	idValid := make([]bool, len(ids))
	for i := range idValid {
		idValid[i] = true
	}
	scoreValid := make([]bool, len(scores))
	for i := range scoreValid {
		scoreValid[i] = true
	}
	idBuilder.AppendValues(ids, idValid)
	scoreBuilder.AppendValues(scores, scoreValid)
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return makeReader(t, schema, record)
}

func TestDeleteCreatesNewSnapshot(t *testing.T) {
	dataset, _, _ := newMutationDataset(t)
	ctx := context.Background()
	baseVersion := mutationVersion(t, dataset)

	next, deleted, err := dataset.Delete(ctx, "id > 2")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	defer next.Close()
	if deleted != 2 {
		t.Fatalf("deleted rows = %d, want 2", deleted)
	}
	if version := mutationVersion(t, next); version <= baseVersion {
		t.Fatalf("result version = %d, want > %d", version, baseVersion)
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if version := mutationVersion(t, dataset); version != baseVersion {
		t.Fatalf("receiver version = %d, want %d", version, baseVersion)
	}
	requireMutationRows(t, next, []int64{1, 2}, []string{"a", "b"})

	_, _, err = dataset.Delete(ctx, "id >>>")
	if !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("invalid Delete error = %v, want ErrInvalidArgument", err)
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if version := mutationVersion(t, dataset); version != baseVersion {
		t.Fatalf("receiver version after failed delete = %d, want %d", version, baseVersion)
	}
}

func TestUpdateCreatesNewSnapshot(t *testing.T) {
	t.Run("predicate", func(t *testing.T) {
		dataset, _, _ := newMutationDataset(t)
		ctx := context.Background()

		next, updated, err := dataset.Update(ctx, "id >= 3",
			lance.ColumnUpdate{Column: "name", Value: "'x'"},
			lance.ColumnUpdate{Column: "id", Value: "id + 10"},
		)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		defer next.Close()
		if updated != 2 {
			t.Fatalf("updated rows = %d, want 2", updated)
		}
		requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
		requireMutationRows(t, next, []int64{1, 2, 13, 14}, []string{"a", "b", "x", "x"})

		_, _, err = dataset.Update(ctx, "", lance.ColumnUpdate{Column: "missing", Value: "1"})
		if !errors.Is(err, lance.ErrInvalidArgument) {
			t.Fatalf("unknown column error = %v, want ErrInvalidArgument", err)
		}
		requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	})

	t.Run("empty predicate updates every row", func(t *testing.T) {
		dataset, _, _ := newMutationDataset(t)
		next, updated, err := dataset.Update(context.Background(), "",
			lance.ColumnUpdate{Column: "name", Value: "'all'"},
		)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		defer next.Close()
		if updated != 4 {
			t.Fatalf("updated rows = %d, want 4", updated)
		}
		requireMutationRows(t, next, []int64{1, 2, 3, 4}, []string{"all", "all", "all", "all"})
	})
}

func TestMergeInsertModesAndStats(t *testing.T) {
	dataset, _, schema := newMutationDataset(t)
	ctx := context.Background()

	defaultSource := &releaseCountingReader{RecordReader: makeReader(t, schema,
		makeRecord(t, schema, []int64{2, 5}, []string{"b2", "e"}, []bool{true, true}),
	)}
	next, stats, err := dataset.MergeInsert(ctx, []string{"id"}, defaultSource, lance.MergeInsertParams{})
	if err != nil {
		t.Fatalf("MergeInsert default: %v", err)
	}
	defer next.Close()
	if defaultSource.releases != 1 {
		t.Fatalf("stream released reader %d times, want 1", defaultSource.releases)
	}
	if stats != (lance.MergeInsertResult{InsertedRows: 1}) {
		t.Fatalf("default stats = %+v, want inserted 1", stats)
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	requireMutationRows(t, next, []int64{1, 2, 3, 4, 5}, []string{"a", "b", "c", "d", "e"})

	updateSource := makeReader(t, schema,
		makeRecord(t, schema, []int64{2, 5}, []string{"b2", "e2"}, []bool{true, true}),
	)
	next2, stats2, err := next.MergeInsert(ctx, []string{"id"}, updateSource, lance.MergeInsertParams{
		WhenMatched: lance.MergeWhenMatchedUpdateAll,
	})
	if err != nil {
		t.Fatalf("MergeInsert update-all: %v", err)
	}
	defer next2.Close()
	if stats2 != (lance.MergeInsertResult{UpdatedRows: 2}) {
		t.Fatalf("update-all stats = %+v, want updated 2", stats2)
	}
	requireMutationRows(t, next2, []int64{1, 2, 3, 4, 5}, []string{"a", "b2", "c", "d", "e2"})

	deleteSource := makeReader(t, schema,
		makeRecord(t, schema, []int64{2}, []string{"ignored"}, []bool{true}),
	)
	next3, stats3, err := next2.MergeInsert(ctx, []string{"id"}, deleteSource, lance.MergeInsertParams{
		WhenMatched:    lance.MergeWhenMatchedDelete,
		WhenNotMatched: lance.MergeWhenNotMatchedDoNothing,
	})
	if err != nil {
		t.Fatalf("MergeInsert delete: %v", err)
	}
	defer next3.Close()
	if stats3 != (lance.MergeInsertResult{DeletedRows: 1}) {
		t.Fatalf("delete stats = %+v, want deleted 1", stats3)
	}
	requireMutationRows(t, next3, []int64{1, 3, 4, 5}, []string{"a", "c", "d", "e2"})

	badSource := makeReader(t, schema,
		makeRecord(t, schema, []int64{2}, []string{"ignored"}, []bool{true}),
	)
	if _, _, err := next3.MergeInsert(ctx, []string{"missing"}, badSource, lance.MergeInsertParams{}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("unknown join key error = %v, want ErrInvalidArgument", err)
	}
	requireMutationRows(t, next3, []int64{1, 3, 4, 5}, []string{"a", "c", "d", "e2"})
}

func TestMergeAddsColumns(t *testing.T) {
	dataset, _, _ := newMutationDataset(t)
	ctx := context.Background()
	baseVersion := mutationVersion(t, dataset)

	source := &releaseCountingReader{RecordReader: makeMergeReader(t, []int64{1, 3}, []int64{10, 30})}
	next, err := dataset.Merge(ctx, "id", "id", source)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	defer next.Close()
	if source.releases != 1 {
		t.Fatalf("stream released reader %d times, want 1", source.releases)
	}
	if version := mutationVersion(t, next); version <= baseVersion {
		t.Fatalf("result version = %d, want > %d", version, baseVersion)
	}

	resultSchema, err := next.Schema()
	if err != nil {
		t.Fatalf("result Schema: %v", err)
	}
	if !resultSchema.HasField("score") {
		t.Fatalf("result schema has no score column: %v", resultSchema)
	}
	originalSchema, err := dataset.Schema()
	if err != nil {
		t.Fatalf("original Schema: %v", err)
	}
	if originalSchema.HasField("score") {
		t.Fatalf("receiver schema gained score column: %v", originalSchema)
	}

	rows := requireMutationRows(t, next, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if !rows[0].ScoreValid || rows[0].Score != 10 {
		t.Fatalf("score for id 1 = %d (valid %v), want 10", rows[0].Score, rows[0].ScoreValid)
	}
	if rows[1].ScoreValid {
		t.Fatalf("score for id 2 is valid, want NULL")
	}
	if !rows[2].ScoreValid || rows[2].Score != 30 {
		t.Fatalf("score for id 3 = %d (valid %v), want 30", rows[2].Score, rows[2].ScoreValid)
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	badSource := makeMergeReader(t, []int64{1}, []int64{10})
	if _, err := dataset.Merge(ctx, "missing", "id", badSource); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("unknown left key error = %v, want ErrInvalidArgument", err)
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
}

func TestTruncateCreatesEmptySnapshot(t *testing.T) {
	dataset, _, _ := newMutationDataset(t)
	ctx := context.Background()

	next, err := dataset.Truncate(ctx)
	if err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	defer next.Close()
	rows, err := next.CountRows(ctx)
	if err != nil {
		t.Fatalf("result CountRows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("result rows = %d, want 0", rows)
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
}

func TestMutationValidationAndCanceledContext(t *testing.T) {
	dataset, _, schema := newMutationDataset(t)
	ctx := context.Background()
	validUpdate := lance.ColumnUpdate{Column: "name", Value: "'x'"}
	validSource := func() array.RecordReader {
		return makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"x"}, []bool{true}))
	}

	checks := []struct {
		name string
		call func() error
	}{
		{"empty delete predicate", func() error {
			_, _, err := dataset.Delete(ctx, "")
			return err
		}},
		{"delete NUL", func() error {
			_, _, err := dataset.Delete(ctx, "id\x00 > 1")
			return err
		}},
		{"empty update list", func() error {
			_, _, err := dataset.Update(ctx, "")
			return err
		}},
		{"empty update column", func() error {
			_, _, err := dataset.Update(ctx, "", lance.ColumnUpdate{Value: "1"})
			return err
		}},
		{"empty update value", func() error {
			_, _, err := dataset.Update(ctx, "", lance.ColumnUpdate{Column: "name"})
			return err
		}},
		{"update NUL predicate", func() error {
			_, _, err := dataset.Update(ctx, "id\x00", validUpdate)
			return err
		}},
		{"merge insert nil source", func() error {
			_, _, err := dataset.MergeInsert(ctx, []string{"id"}, nil, lance.MergeInsertParams{})
			return err
		}},
		{"merge insert empty keys", func() error {
			_, _, err := dataset.MergeInsert(ctx, nil, validSource(), lance.MergeInsertParams{})
			return err
		}},
		{"merge insert NUL key", func() error {
			_, _, err := dataset.MergeInsert(ctx, []string{"id\x00"}, validSource(), lance.MergeInsertParams{})
			return err
		}},
		{"merge insert missing expression", func() error {
			_, _, err := dataset.MergeInsert(ctx, []string{"id"}, validSource(), lance.MergeInsertParams{
				WhenMatched: lance.MergeWhenMatchedUpdateIf,
			})
			return err
		}},
		{"merge insert extraneous expression", func() error {
			_, _, err := dataset.MergeInsert(ctx, []string{"id"}, validSource(), lance.MergeInsertParams{
				WhenMatchedExpression: "true",
			})
			return err
		}},
		{"merge insert bad mode", func() error {
			_, _, err := dataset.MergeInsert(ctx, []string{"id"}, validSource(), lance.MergeInsertParams{
				WhenMatched: lance.MergeWhenMatched(99),
			})
			return err
		}},
		{"merge nil source", func() error {
			_, err := dataset.Merge(ctx, "id", "id", nil)
			return err
		}},
		{"merge NUL key", func() error {
			_, err := dataset.Merge(ctx, "id\x00", "id", makeMergeReader(t, []int64{1}, []int64{10}))
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, lance.ErrInvalidArgument) {
				t.Fatalf("error = %v, want ErrInvalidArgument", err)
			}
		})
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := dataset.Delete(canceled, "id > 0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Delete error = %v, want context.Canceled", err)
	}

	if err := dataset.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, err := dataset.Delete(ctx, "id > 0"); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("closed Delete error = %v, want ErrClosed", err)
	}
}

func TestMutationKeepsScannerSnapshot(t *testing.T) {
	dataset, _, _ := newMutationDataset(t)
	ctx := context.Background()

	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()

	next, deleted, err := dataset.Delete(ctx, "id > 2")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	defer next.Close()
	if deleted != 2 {
		t.Fatalf("deleted rows = %d, want 2", deleted)
	}

	var scanned []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("scanner Records: %v", err)
		}
		scanned = append(scanned, collectInt64(t, batch, "id")...)
	}
	sort.Slice(scanned, func(i, j int) bool { return scanned[i] < scanned[j] })
	if !slices.Equal(scanned, []int64{1, 2, 3, 4}) {
		t.Fatalf("scanner ids = %v, want [1 2 3 4]", scanned)
	}
	requireMutationRows(t, next, []int64{1, 2}, []string{"a", "b"})
}

// blockingSchemaReader parks the mutation after the receiver snapshot has
// been cloned but before the native merge starts. This makes Close ordering
// deterministic without timers.
type blockingSchemaReader struct {
	array.RecordReader
	entered chan struct{}
	unblock chan struct{}
	once    sync.Once
}

func (r *blockingSchemaReader) Schema() *arrow.Schema {
	r.once.Do(func() {
		close(r.entered)
		<-r.unblock
	})
	return r.RecordReader.Schema()
}

func TestMutationCloseWaitsForInFlightOperation(t *testing.T) {
	dataset, _, schema := newMutationDataset(t)
	source := &blockingSchemaReader{
		RecordReader: makeReader(t, schema,
			makeRecord(t, schema, []int64{5}, []string{"e"}, []bool{true}),
		),
		entered: make(chan struct{}),
		unblock: make(chan struct{}),
	}
	type mutationResult struct {
		dataset *lance.Dataset
		stats   lance.MergeInsertResult
		err     error
	}
	mutationDone := make(chan mutationResult, 1)
	go func() {
		next, stats, err := dataset.MergeInsert(context.Background(), []string{"id"}, source, lance.MergeInsertParams{})
		mutationDone <- mutationResult{dataset: next, stats: stats, err: err}
	}()
	<-source.entered

	closeStarted := make(chan struct{})
	closeDone := make(chan error, 1)
	go func() {
		close(closeStarted)
		closeDone <- dataset.Close()
	}()
	<-closeStarted
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned while mutation was in flight: %v", err)
	default:
	}

	close(source.unblock)
	result := <-mutationDone
	if result.err != nil {
		t.Fatalf("MergeInsert: %v", result.err)
	}
	defer result.dataset.Close()
	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
	if result.stats != (lance.MergeInsertResult{InsertedRows: 1}) {
		t.Fatalf("stats = %+v, want inserted 1", result.stats)
	}
	requireMutationRows(t, result.dataset, []int64{1, 2, 3, 4, 5}, []string{"a", "b", "c", "d", "e"})
}

func TestMutationCloseIndependence(t *testing.T) {
	t.Run("close result", func(t *testing.T) {
		dataset, _, _ := newMutationDataset(t)
		next, _, err := dataset.Delete(context.Background(), "id > 2")
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if err := next.Close(); err != nil {
			t.Fatalf("close result: %v", err)
		}
		requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	})

	t.Run("close source", func(t *testing.T) {
		dataset, _, _ := newMutationDataset(t)
		next, _, err := dataset.Delete(context.Background(), "id > 2")
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		defer next.Close()
		if err := dataset.Close(); err != nil {
			t.Fatalf("close source: %v", err)
		}
		requireMutationRows(t, next, []int64{1, 2}, []string{"a", "b"})
	})
}
