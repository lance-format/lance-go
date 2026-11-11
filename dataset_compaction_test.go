package lance_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/compaction"
)

func compactionPtr[T any](value T) *T {
	return &value
}

func readCompactionIDs(t *testing.T, dataset *lance.Dataset) []int64 {
	t.Helper()
	scanner, err := dataset.Scan()
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
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func createFragmentedDataset(t *testing.T, rows int) (*lance.Dataset, string) {
	t.Helper()
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	ids := make([]int64, rows)
	names := make([]string, rows)
	valid := make([]bool, rows)
	for i := range ids {
		ids[i] = int64(i + 1)
		names[i] = "row"
		valid[i] = true
	}
	record := makeRecord(t, schema, ids, names, valid)
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record),
		lance.WithWriteParams(lance.WriteParams{MaxRowsPerFile: 1, MaxRowsPerGroup: 1}),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = dataset.Close() })
	return dataset, uri
}

func TestCompactFilesRewritesFragments(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, uri := createFragmentedDataset(t, 8)

	version, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	wantIDs := []int64{1, 2, 3, 4, 5, 6, 7, 8}
	if got := readCompactionIDs(t, dataset); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("source IDs = %v, want %v", got, wantIDs)
	}

	// Keep a scanner open across the compaction. It must continue to see the
	// pre-compaction snapshot because compaction publishes through a clone.
	sourceScanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer sourceScanner.Close()

	compacted, metrics, err := dataset.CompactFiles(ctx, &compaction.Options{
		TargetRowsPerFragment: compactionPtr(uint64(4)),
		MaxRowsPerGroup:       compactionPtr(uint64(1)),
	})
	if err != nil {
		t.Fatalf("CompactFiles: %v", err)
	}
	defer compacted.Close()

	if metrics.FragmentsRemoved == 0 || metrics.FragmentsAdded == 0 {
		t.Fatalf("fragment metrics = %+v, want both removed and added", metrics)
	}
	if metrics.FilesRemoved == 0 || metrics.FilesAdded == 0 {
		t.Fatalf("file metrics = %+v, want both removed and added", metrics)
	}

	compactedVersion, err := compacted.Version()
	if err != nil {
		t.Fatalf("compacted Version: %v", err)
	}
	// Native compaction first reserves fragment IDs, then commits the rewrite.
	if compactedVersion != version+2 {
		t.Fatalf("compacted version = %d, want %d", compactedVersion, version+2)
	}
	sourceVersion, err := dataset.Version()
	if err != nil {
		t.Fatalf("source Version: %v", err)
	}
	if sourceVersion != version {
		t.Fatalf("source version = %d, want %d", sourceVersion, version)
	}
	if got := readCompactionIDs(t, compacted); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("compacted IDs = %v, want %v", got, wantIDs)
	}
	if got := readCompactionIDs(t, dataset); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("source IDs after compaction = %v, want %v", got, wantIDs)
	}

	var scanned []int64
	for batch, err := range sourceScanner.Records(ctx) {
		if err != nil {
			t.Fatalf("source scanner Records: %v", err)
		}
		scanned = append(scanned, collectInt64(t, batch, "id")...)
	}
	if !reflect.DeepEqual(scanned, wantIDs) {
		t.Fatalf("open scanner IDs after compaction = %v, want %v", scanned, wantIDs)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()
	if got := readCompactionIDs(t, reopened); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("reopened IDs = %v, want %v", got, wantIDs)
	}
}

func TestCompactFilesCleanDatasetNoOp(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"}, []bool{true, true, true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	version, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	compacted, metrics, err := dataset.CompactFiles(ctx, nil)
	if err != nil {
		t.Fatalf("CompactFiles: %v", err)
	}
	defer compacted.Close()

	if metrics != (compaction.Metrics{}) {
		t.Fatalf("metrics = %+v, want zero for a clean dataset", metrics)
	}
	compactedVersion, err := compacted.Version()
	if err != nil {
		t.Fatalf("compacted Version: %v", err)
	}
	if compactedVersion != version {
		t.Fatalf("compacted version = %d, want unchanged %d", compactedVersion, version)
	}
	wantIDs := []int64{1, 2, 3, 4}
	if got := readCompactionIDs(t, compacted); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("IDs = %v, want %v", got, wantIDs)
	}
}

func TestCompactionPlanJSONTransport(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, uri := createFragmentedDataset(t, 8)

	version, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	plan, err := dataset.PlanCompaction(ctx, &compaction.Options{
		TargetRowsPerFragment: compactionPtr(uint64(4)),
		MaxRowsPerGroup:       compactionPtr(uint64(1)),
		MaterializeDeletions:  compactionPtr(false),
		IndexRemapMode:        compactionPtr(compaction.IndexRemapDirect),
		CompactionMode:        compaction.Some(compaction.ModeReencode),
		TransactionProperties: map[string]string{"origin": "compaction-test"},
	})
	if err != nil {
		t.Fatalf("PlanCompaction: %v", err)
	}
	if plan.ReadVersion() != version {
		t.Fatalf("plan read version = %d, want %d", plan.ReadVersion(), version)
	}
	if plan.TaskCount() == 0 {
		t.Fatal("plan has no tasks")
	}
	planOptions := plan.Options()
	if planOptions.TargetRowsPerFragment == nil || *planOptions.TargetRowsPerFragment != 4 {
		t.Fatalf("plan target rows = %v, want 4", planOptions.TargetRowsPerFragment)
	}
	if planOptions.MaxRowsPerGroup == nil || *planOptions.MaxRowsPerGroup != 1 {
		t.Fatalf("plan max rows per group = %v, want 1", planOptions.MaxRowsPerGroup)
	}
	resolvedVersion, ok := planOptions.DataStorageVersion.Get()
	if !ok || resolvedVersion == "" {
		t.Fatalf("plan data storage version = %q (set=%v), want resolved selector", resolvedVersion, ok)
	}
	if planOptions.IndexRemapMode == nil || *planOptions.IndexRemapMode != compaction.IndexRemapDirect {
		t.Fatalf("plan index remap mode = %v, want Direct", planOptions.IndexRemapMode)
	}
	if mode, ok := planOptions.CompactionMode.Get(); !ok || mode != compaction.ModeReencode {
		t.Fatalf("plan compaction mode = %v (set=%v), want Reencode", mode, ok)
	}
	if got := planOptions.TransactionProperties["origin"]; got != "compaction-test" {
		t.Fatalf("plan transaction property = %q, want compaction-test", got)
	}
	if plan.JSON() == "" {
		t.Fatal("plan JSON is empty")
	}

	restoredPlan, err := lance.CompactionPlanFromJSON(plan.JSON())
	if err != nil {
		t.Fatalf("CompactionPlanFromJSON: %v", err)
	}
	if restoredPlan.ReadVersion() != plan.ReadVersion() || restoredPlan.TaskCount() != plan.TaskCount() {
		t.Fatalf("restored plan version/tasks = %d/%d, want %d/%d",
			restoredPlan.ReadVersion(), restoredPlan.TaskCount(), plan.ReadVersion(), plan.TaskCount())
	}
	if restoredPlan.JSON() != plan.JSON() {
		t.Fatal("restored plan JSON differs from the input")
	}

	task, err := plan.Task(0)
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if task.ReadVersion() != plan.ReadVersion() {
		t.Fatalf("task read version = %d, want %d", task.ReadVersion(), plan.ReadVersion())
	}
	if task.FragmentCount() == 0 {
		t.Fatal("task has no source fragments")
	}
	if metadata, err := task.FragmentMetadataJSON(0); err != nil || metadata == "" {
		t.Fatalf("task fragment metadata JSON = %q, err = %v", metadata, err)
	}
	taskOptions := task.Options()
	taskVersion, ok := taskOptions.DataStorageVersion.Get()
	if !ok || taskVersion != resolvedVersion {
		t.Fatalf("task data storage version = %q (set=%v), want %q", taskVersion, ok, resolvedVersion)
	}
	if got := taskOptions.TransactionProperties["origin"]; got != "compaction-test" {
		t.Fatalf("task transaction property = %q, want compaction-test", got)
	}
	if task.JSON() == "" {
		t.Fatal("task JSON is empty")
	}

	restoredTask, err := lance.CompactionTaskFromJSON(task.JSON())
	if err != nil {
		t.Fatalf("CompactionTaskFromJSON: %v", err)
	}
	if restoredTask.JSON() != task.JSON() {
		t.Fatal("restored task JSON differs from the input")
	}
	if restoredTask.FragmentCount() != task.FragmentCount() {
		t.Fatalf("restored task fragments = %d, want %d", restoredTask.FragmentCount(), task.FragmentCount())
	}

	result, err := restoredTask.Execute(ctx, dataset)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ReadVersion() != task.ReadVersion() {
		t.Fatalf("result read version = %d, want %d", result.ReadVersion(), task.ReadVersion())
	}
	if result.OriginalFragmentCount() != task.FragmentCount() {
		t.Fatalf("result original fragments = %d, want %d", result.OriginalFragmentCount(), task.FragmentCount())
	}
	if result.NewFragmentCount() == 0 {
		t.Fatal("result has no new fragments")
	}
	if result.Metrics().FragmentsRemoved == 0 || result.Metrics().FragmentsAdded == 0 {
		t.Fatalf("result metrics = %+v, want both removed and added", result.Metrics())
	}
	if original, err := result.OriginalFragmentMetadataJSON(0); err != nil || original == "" {
		t.Fatalf("result original fragment metadata JSON = %q, err = %v", original, err)
	}
	if rewritten, err := result.NewFragmentMetadataJSON(0); err != nil || rewritten == "" {
		t.Fatalf("result new fragment metadata JSON = %q, err = %v", rewritten, err)
	}
	if result.JSON() == "" {
		t.Fatal("result JSON is empty")
	}

	restoredResult, err := lance.RewriteResultFromJSON(result.JSON())
	if err != nil {
		t.Fatalf("RewriteResultFromJSON: %v", err)
	}
	if restoredResult.Metrics() != result.Metrics() {
		t.Fatalf("restored result metrics = %+v, want %+v", restoredResult.Metrics(), result.Metrics())
	}
	if restoredResult.JSON() != result.JSON() {
		t.Fatal("restored result JSON differs from the input")
	}

	// Commit a result that crossed the JSON boundary. Only the first task is
	// committed; the remaining fragments are untouched and the row set is
	// unchanged.
	committed, commitMetrics, err := dataset.CommitCompaction(ctx,
		[]*lance.RewriteResult{restoredResult}, &taskOptions)
	if err != nil {
		t.Fatalf("CommitCompaction: %v", err)
	}
	defer committed.Close()

	if commitMetrics.FragmentsRemoved == 0 || commitMetrics.FragmentsAdded == 0 {
		t.Fatalf("commit metrics = %+v, want both removed and added", commitMetrics)
	}
	committedVersion, err := committed.Version()
	if err != nil {
		t.Fatalf("committed Version: %v", err)
	}
	if committedVersion <= version {
		t.Fatalf("committed version = %d, want greater than %d", committedVersion, version)
	}
	if sourceVersion, err := dataset.Version(); err != nil {
		t.Fatalf("source Version: %v", err)
	} else if sourceVersion != version {
		t.Fatalf("source version = %d, want %d", sourceVersion, version)
	}

	wantIDs := []int64{1, 2, 3, 4, 5, 6, 7, 8}
	if got := readCompactionIDs(t, dataset); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("source IDs after commit = %v, want %v", got, wantIDs)
	}
	if got := readCompactionIDs(t, committed); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("committed IDs = %v, want %v", got, wantIDs)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()
	if got := readCompactionIDs(t, reopened); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("reopened IDs = %v, want %v", got, wantIDs)
	}
}

func TestCompactionPlanOutlivesDataset(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, uri := createFragmentedDataset(t, 4)

	plan, err := dataset.PlanCompaction(ctx, &compaction.Options{
		TargetRowsPerFragment: compactionPtr(uint64(2)),
	})
	if err != nil {
		t.Fatalf("PlanCompaction: %v", err)
	}
	if plan.TaskCount() == 0 {
		t.Fatal("plan has no tasks")
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("Close source dataset: %v", err)
	}

	// Plan, task and metadata are copied out of the native owner before the
	// source handle is closed. They must remain readable and executable.
	task, err := plan.Task(0)
	if err != nil {
		t.Fatalf("Task after closing source dataset: %v", err)
	}
	if task.FragmentCount() == 0 {
		t.Fatal("task has no source fragments")
	}
	if metadata, err := task.FragmentMetadataJSON(0); err != nil || metadata == "" {
		t.Fatalf("task fragment metadata after closing source: %q, %v", metadata, err)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()

	result, err := task.Execute(ctx, reopened)
	if err != nil {
		t.Fatalf("Execute after closing source dataset: %v", err)
	}
	taskOptions := task.Options()
	committed, _, err := reopened.CommitCompaction(ctx,
		[]*lance.RewriteResult{result}, &taskOptions)
	if err != nil {
		t.Fatalf("CommitCompaction: %v", err)
	}
	defer committed.Close()

	wantIDs := []int64{1, 2, 3, 4}
	if got := readCompactionIDs(t, committed); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("committed IDs = %v, want %v", got, wantIDs)
	}
}

func TestCompactionValidation(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, _ := createFragmentedDataset(t, 4)

	if _, err := lance.CompactionPlanFromJSON("not json"); err == nil {
		t.Fatal("CompactionPlanFromJSON accepted invalid JSON")
	}
	if _, err := lance.CompactionTaskFromJSON("{}"); err == nil {
		t.Fatal("CompactionTaskFromJSON accepted an empty task")
	}
	if _, err := lance.RewriteResultFromJSON("{}"); err == nil {
		t.Fatal("RewriteResultFromJSON accepted an empty result")
	}

	if _, err := dataset.PlanCompaction(ctx, &compaction.Options{
		DataStorageVersion: compaction.Some("not-a-lance-version"),
	}); err == nil {
		t.Fatal("PlanCompaction accepted an invalid data storage version")
	}
	if _, _, err := dataset.CompactFiles(ctx, &compaction.Options{
		CompactionMode: compaction.Some(compaction.Mode(99)),
	}); err == nil {
		t.Fatal("CompactFiles accepted an invalid compaction mode")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := dataset.CompactFiles(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("CompactFiles with canceled context error = %v, want context.Canceled", err)
	}

	closed, _ := createFragmentedDataset(t, 2)
	if err := closed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, err := closed.CompactFiles(ctx, nil); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("CompactFiles on closed dataset error = %v, want ErrClosed", err)
	}
}
