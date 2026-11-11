package lance_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/compaction"
	"github.com/lance-format/lance-go/index"
)

func optimizeRows(first, last int64) ([]int64, []string, []bool) {
	ids := make([]int64, 0, last-first+1)
	names := make([]string, 0, last-first+1)
	valid := make([]bool, 0, last-first+1)
	for id := first; id <= last; id++ {
		ids = append(ids, id)
		names = append(names, "row")
		valid = append(valid, true)
	}
	return ids, names, valid
}

// createOptimizeFixture writes initialRows as one-row fragments, builds a
// bitmap index on id, then appends appendedRows as further one-row fragments.
// The returned dataset is open at the appended version.
func createOptimizeFixture(t *testing.T, initialRows, appendedRows int) (*lance.Dataset, string) {
	t.Helper()
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "optimize.lance")
	schema := testSchema()

	ids, names, valid := optimizeRows(1, int64(initialRows))
	created, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, ids, names, valid)),
		lance.WithWriteParams(lance.WriteParams{MaxRowsPerFile: 1, MaxRowsPerGroup: 1}),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	indexed, err := created.CreateScalarIndex(ctx, "id", index.ScalarBitmap, nil, index.ScalarOptions{}, lance.WithIndexName("id_bitmap"))
	if err != nil {
		_ = created.Close()
		t.Fatalf("CreateScalarIndex: %v", err)
	}
	if err := created.Close(); err != nil {
		t.Fatalf("close created dataset: %v", err)
	}
	if err := indexed.Close(); err != nil {
		t.Fatalf("close indexed dataset: %v", err)
	}

	ids, names, valid = optimizeRows(int64(initialRows)+1, int64(initialRows+appendedRows))
	appended, err := lance.Append(ctx, uri, makeReader(t, schema, makeRecord(t, schema, ids, names, valid)),
		lance.WithWriteParams(lance.WriteParams{MaxRowsPerFile: 1, MaxRowsPerGroup: 1}),
	)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := appended.Close(); err != nil {
		t.Fatalf("close appended dataset: %v", err)
	}

	dataset, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := dataset.Close(); err != nil {
			t.Errorf("close dataset: %v", err)
		}
	})
	return dataset, uri
}

func versionOf(t *testing.T, dataset *lance.Dataset) uint64 {
	t.Helper()
	version, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	return version
}

func TestOptimizeIndicesPlanTransportAndCommit(t *testing.T) {
	ctx := context.Background()
	dataset, _ := createOptimizeFixture(t, 8, 8)
	sourceVersion := versionOf(t, dataset)

	taskRows := uint64(4)
	segmentRows := uint64(4)
	options := &compaction.OptimizeOptions{
		IndexNames:        compaction.Some([]string{"id_bitmap"}),
		MaxRowsPerTask:    compaction.Some(taskRows),
		MaxRowsPerSegment: compaction.Some(segmentRows),
	}
	plan, err := dataset.PlanOptimizeIndices(ctx, options)
	if err != nil {
		t.Fatalf("PlanOptimizeIndices: %v", err)
	}
	if plan.ReadVersion() != sourceVersion {
		t.Fatalf("plan ReadVersion = %d, want %d", plan.ReadVersion(), sourceVersion)
	}
	if plan.PlanID() == (index.UUID{}) {
		t.Fatal("plan ID is zero")
	}
	if plan.TaskCount() == 0 {
		t.Fatalf("plan has no tasks; plan JSON: %s", plan.JSON())
	}

	restored, err := lance.OptimizeIndicesPlanFromJSON(plan.JSON())
	if err != nil {
		t.Fatalf("OptimizeIndicesPlanFromJSON: %v", err)
	}
	if restored.PlanID() != plan.PlanID() || restored.ReadVersion() != plan.ReadVersion() {
		t.Fatalf("restored plan identity = %s/%d, want %s/%d",
			restored.PlanID(), restored.ReadVersion(), plan.PlanID(), plan.ReadVersion())
	}
	if restored.TaskCount() != plan.TaskCount() || restored.MergeCount() != plan.MergeCount() {
		t.Fatalf("restored plan counts = %d/%d, want %d/%d",
			restored.TaskCount(), restored.MergeCount(), plan.TaskCount(), plan.MergeCount())
	}
	if restored.JSON() != plan.JSON() {
		t.Fatal("restored plan JSON differs from native JSON")
	}
	if _, err := plan.Task(plan.TaskCount()); err == nil {
		t.Fatal("Task accepted an out-of-range index")
	}
	if _, err := plan.Merge(plan.MergeCount()); err == nil {
		t.Fatal("Merge accepted an out-of-range index")
	}

	for i := uint64(0); i < plan.TaskCount(); i++ {
		task, err := plan.Task(i)
		if err != nil {
			t.Fatalf("Task(%d): %v", i, err)
		}
		if task.PlanID() != plan.PlanID() || task.ReadVersion() != sourceVersion {
			t.Fatalf("task %d identity = %s/%d, want %s/%d",
				i, task.PlanID(), task.ReadVersion(), plan.PlanID(), sourceVersion)
		}
		if task.FragmentCount() == 0 {
			t.Fatalf("task %d has no source fragments", i)
		}
		roundTrip, err := lance.OptimizeIndexTaskFromJSON(task.JSON())
		if err != nil {
			t.Fatalf("OptimizeIndexTaskFromJSON(%d): %v", i, err)
		}
		if roundTrip.PlanID() != task.PlanID() || roundTrip.Group() != task.Group() ||
			roundTrip.FragmentCount() != task.FragmentCount() ||
			roundTrip.NumIndicesToMerge() != task.NumIndicesToMerge() ||
			roundTrip.EnablePartitionRebalancing() != task.EnablePartitionRebalancing() ||
			roundTrip.Retrain() != task.Retrain() || roundTrip.JSON() != task.JSON() {
			t.Fatalf("task %d changed across JSON transport", i)
		}

		reference, err := task.Reference()
		if err != nil {
			t.Fatalf("task %d Reference: %v", i, err)
		}
		if name, err := reference.Name(); err != nil || name != "id_bitmap" {
			t.Fatalf("task %d reference name = %q, %v; want id_bitmap", i, name, err)
		}
		if kind, err := reference.Type(); err != nil || kind != index.SegmentIndexType(index.ScalarBitmap) {
			t.Fatalf("task %d reference type = %v, %v; want BITMAP", i, kind, err)
		}
		if uuid, err := reference.UUID(); err != nil || uuid == (index.UUID{}) {
			t.Fatalf("task %d reference UUID = %s, %v", i, uuid, err)
		}
		if err := reference.Close(); err != nil {
			t.Fatalf("close task %d reference: %v", i, err)
		}
		for j := uint64(0); j < task.OldSegmentCount(); j++ {
			old, err := task.OldSegment(j)
			if err != nil {
				t.Fatalf("task %d OldSegment(%d): %v", i, j, err)
			}
			if name, err := old.Name(); err != nil || name != "id_bitmap" {
				t.Fatalf("task %d old segment %d name = %q, %v", i, j, name, err)
			}
			if err := old.Close(); err != nil {
				t.Fatalf("close task %d old segment %d: %v", i, j, err)
			}
		}
	}

	var taskResults []*lance.IndexSegmentResult
	for i := uint64(0); i < plan.TaskCount(); i++ {
		task, err := plan.Task(i)
		if err != nil {
			t.Fatalf("Task(%d): %v", i, err)
		}
		restoredTask, err := lance.OptimizeIndexTaskFromJSON(task.JSON())
		if err != nil {
			t.Fatalf("OptimizeIndexTaskFromJSON(%d): %v", i, err)
		}
		result, err := restoredTask.Execute(ctx, dataset)
		if err != nil {
			t.Fatalf("task %d Execute: %v", i, err)
		}
		if result == nil {
			continue
		}
		if result.PlanID() != plan.PlanID() || result.ReadVersion() != sourceVersion || result.Group() != task.Group() {
			t.Fatalf("task %d result identity = %s/%d/%d, want %s/%d/%d",
				i, result.PlanID(), result.ReadVersion(), result.Group(), plan.PlanID(), sourceVersion, task.Group())
		}
		restoredResult, err := lance.IndexSegmentResultFromJSON(result.JSON())
		if err != nil {
			t.Fatalf("IndexSegmentResultFromJSON(%d): %v", i, err)
		}
		if restoredResult.PlanID() != result.PlanID() || restoredResult.ReadVersion() != result.ReadVersion() ||
			restoredResult.Group() != result.Group() || restoredResult.ReplaceCount() != result.ReplaceCount() ||
			restoredResult.JSON() != result.JSON() {
			t.Fatalf("task %d result changed across JSON transport", i)
		}
		segment, err := result.Segment()
		if err != nil {
			t.Fatalf("task %d result Segment: %v", i, err)
		}
		restoredSegment, err := restoredResult.Segment()
		if err != nil {
			t.Fatalf("restored task %d result Segment: %v", i, err)
		}
		segmentBytes, err := segment.Encode()
		if err != nil {
			t.Fatalf("task %d result segment Encode: %v", i, err)
		}
		restoredSegmentBytes, err := restoredSegment.Encode()
		if err != nil {
			t.Fatalf("restored task %d result segment Encode: %v", i, err)
		}
		if !bytes.Equal(restoredSegmentBytes, segmentBytes) {
			t.Fatalf("task %d result segment changed across JSON transport", i)
		}
		if name, err := restoredSegment.Name(); err != nil || name != "id_bitmap" {
			t.Fatalf("task %d result segment name = %q, %v; want id_bitmap", i, name, err)
		}
		if err := segment.Close(); err != nil {
			t.Fatalf("close task %d result segment: %v", i, err)
		}
		if err := restoredSegment.Close(); err != nil {
			t.Fatalf("close restored task %d result segment: %v", i, err)
		}
		taskResults = append(taskResults, result)
	}

	results := taskResults
	if plan.MergeCount() > 0 {
		merged := make([]*lance.IndexSegmentResult, 0, plan.MergeCount())
		for i := uint64(0); i < plan.MergeCount(); i++ {
			merge, err := plan.Merge(i)
			if err != nil {
				t.Fatalf("Merge(%d): %v", i, err)
			}
			var inputs []*lance.IndexSegmentResult
			for _, result := range taskResults {
				if result.Group() == merge.Group() {
					inputs = append(inputs, result)
				}
			}
			restoredMerge, err := lance.OptimizeIndexMergeFromJSON(merge.JSON())
			if err != nil {
				t.Fatalf("OptimizeIndexMergeFromJSON(%d): %v", i, err)
			}
			if restoredMerge.PlanID() != merge.PlanID() || restoredMerge.ReadVersion() != merge.ReadVersion() ||
				restoredMerge.Group() != merge.Group() ||
				restoredMerge.OldSegmentCount() != merge.OldSegmentCount() ||
				restoredMerge.ReplaceCount() != merge.ReplaceCount() || restoredMerge.JSON() != merge.JSON() {
				t.Fatalf("merge %d changed across JSON transport", i)
			}
			for j := uint64(0); j < merge.OldSegmentCount(); j++ {
				old, err := merge.OldSegment(j)
				if err != nil {
					t.Fatalf("merge %d OldSegment(%d): %v", i, j, err)
				}
				restoredOld, err := restoredMerge.OldSegment(j)
				if err != nil {
					t.Fatalf("restored merge %d OldSegment(%d): %v", i, j, err)
				}
				oldBytes, err := old.Encode()
				if err != nil {
					t.Fatalf("merge %d old segment %d Encode: %v", i, j, err)
				}
				restoredOldBytes, err := restoredOld.Encode()
				if err != nil {
					t.Fatalf("restored merge %d old segment %d Encode: %v", i, j, err)
				}
				if !bytes.Equal(restoredOldBytes, oldBytes) {
					t.Fatalf("merge %d old segment %d changed across JSON transport", i, j)
				}
				if err := old.Close(); err != nil {
					t.Fatalf("close merge %d old segment %d: %v", i, j, err)
				}
				if err := restoredOld.Close(); err != nil {
					t.Fatalf("close restored merge %d old segment %d: %v", i, j, err)
				}
			}
			result, err := restoredMerge.Execute(ctx, dataset, inputs)
			if err != nil {
				t.Fatalf("merge %d Execute: %v", i, err)
			}
			if result == nil {
				continue
			}
			if result.PlanID() != plan.PlanID() || result.Group() != merge.Group() {
				t.Fatalf("merge %d result identity = %s/%d, want %s/%d",
					i, result.PlanID(), result.Group(), plan.PlanID(), merge.Group())
			}
			merged = append(merged, result)
		}
		results = merged
	}
	if len(results) == 0 {
		t.Fatalf("plan produced no committed results; plan JSON: %s", plan.JSON())
	}

	optimized, err := dataset.CommitOptimizeIndices(ctx, results, nil)
	if err != nil {
		t.Fatalf("CommitOptimizeIndices: %v", err)
	}
	if got := versionOf(t, dataset); got != sourceVersion {
		t.Fatalf("source version after commit = %d, want %d", got, sourceVersion)
	}
	if got := readCompactionIDs(t, dataset); len(got) != 16 || got[0] != 1 || got[15] != 16 {
		t.Fatalf("source rows after commit = %v, want 1..16", got)
	}
	if got := versionOf(t, optimized); got <= sourceVersion {
		t.Fatalf("optimized version = %d, want > %d", got, sourceVersion)
	}
	if got := readCompactionIDs(t, optimized); len(got) != 16 || got[0] != 1 || got[15] != 16 {
		t.Fatalf("optimized rows = %v, want 1..16", got)
	}
	indices, err := optimized.ListIndices()
	if err != nil {
		t.Fatalf("ListIndices: %v", err)
	}
	if len(indices) != 1 || indices[0].Name != "id_bitmap" {
		t.Fatalf("optimized indices = %v, want one id_bitmap index", indices)
	}
}

func TestOptimizeIndicesPreservesSnapshot(t *testing.T) {
	ctx := context.Background()
	dataset, _ := createOptimizeFixture(t, 8, 8)
	sourceVersion := versionOf(t, dataset)

	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()

	optimized, err := dataset.OptimizeIndices(ctx, &compaction.OptimizeOptions{
		IndexNames: compaction.Some([]string{"id_bitmap"}),
	})
	if err != nil {
		t.Fatalf("OptimizeIndices: %v", err)
	}
	if got := versionOf(t, dataset); got != sourceVersion {
		t.Fatalf("source version = %d, want %d", got, sourceVersion)
	}

	var preSnapshot []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("source open scanner Records: %v", err)
		}
		preSnapshot = append(preSnapshot, collectInt64(t, batch, "id")...)
	}
	if len(preSnapshot) != 16 || preSnapshot[0] != 1 || preSnapshot[15] != 16 {
		t.Fatalf("pre-existing scanner rows = %v, want 1..16", preSnapshot)
	}

	if got := versionOf(t, optimized); got <= sourceVersion {
		t.Fatalf("optimized version = %d, want > %d", got, sourceVersion)
	}
	if got := readCompactionIDs(t, optimized); len(got) != 16 || got[0] != 1 || got[15] != 16 {
		t.Fatalf("optimized rows = %v, want 1..16", got)
	}
}

func TestOptimizeIndicesOptionSemantics(t *testing.T) {
	ctx := context.Background()
	dataset, _ := createOptimizeFixture(t, 8, 4)

	empty, err := dataset.PlanOptimizeIndices(ctx, &compaction.OptimizeOptions{
		IndexNames: compaction.Some([]string{}),
	})
	if err != nil {
		t.Fatalf("PlanOptimizeIndices with empty index names: %v", err)
	}
	if empty.TaskCount() != 0 || empty.MergeCount() != 0 {
		t.Fatalf("empty index-name plan has %d tasks and %d merges, want 0/0",
			empty.TaskCount(), empty.MergeCount())
	}
	if empty.ReadVersion() != versionOf(t, dataset) {
		t.Fatalf("empty plan ReadVersion = %d, want %d", empty.ReadVersion(), versionOf(t, dataset))
	}
	if empty.PlanID() == (index.UUID{}) {
		t.Fatal("empty plan ID is zero")
	}

	if _, err := lance.OptimizeIndicesPlanFromJSON("{"); err == nil {
		t.Fatal("OptimizeIndicesPlanFromJSON accepted invalid JSON")
	}
	if _, err := lance.OptimizeIndexTaskFromJSON("{}"); err == nil {
		t.Fatal("OptimizeIndexTaskFromJSON accepted incomplete JSON")
	}
	if _, err := lance.IndexSegmentResultFromJSON("{}"); err == nil {
		t.Fatal("IndexSegmentResultFromJSON accepted incomplete JSON")
	}
	if _, err := dataset.PlanOptimizeIndices(ctx, &compaction.OptimizeOptions{
		IndexNames: compaction.Some([]string{"id\x00bitmap"}),
	}); err == nil {
		t.Fatal("PlanOptimizeIndices accepted an index name containing NUL")
	}
	if _, err := dataset.CommitOptimizeIndices(ctx, nil, &compaction.OptimizeOptions{
		TransactionProperties: map[string]string{"key": "value\x00"},
	}); err == nil {
		t.Fatal("CommitOptimizeIndices accepted a transaction property containing NUL")
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := dataset.OptimizeIndices(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("OptimizeIndices with canceled context = %v, want context.Canceled", err)
	}
	if _, err := dataset.PlanOptimizeIndices(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("PlanOptimizeIndices with canceled context = %v, want context.Canceled", err)
	}
	if _, err := dataset.CommitOptimizeIndices(canceled, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("CommitOptimizeIndices with canceled context = %v, want context.Canceled", err)
	}
	if _, err := dataset.CommitOptimizeIndices(ctx, []*lance.IndexSegmentResult{nil}, nil); err == nil {
		t.Fatal("CommitOptimizeIndices accepted a nil result")
	}

	version := versionOf(t, dataset)
	if _, err := dataset.CommitOptimizeIndices(ctx, nil, nil); err != nil {
		t.Fatalf("CommitOptimizeIndices with no results: %v", err)
	}
	if got := versionOf(t, dataset); got != version {
		t.Fatalf("source version after empty commit = %d, want %d", got, version)
	}
}

func TestOptimizeIndicesCleanDatasetNoOp(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "clean.lance")
	schema := testSchema()
	ids, names, valid := optimizeRows(1, 4)
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, ids, names, valid)))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if err := dataset.Close(); err != nil {
			t.Errorf("close dataset: %v", err)
		}
	})
	version := versionOf(t, dataset)
	optimized, err := dataset.OptimizeIndices(ctx, nil)
	if err != nil {
		t.Fatalf("OptimizeIndices: %v", err)
	}
	if got := versionOf(t, optimized); got != version {
		t.Fatalf("clean dataset version = %d, want unchanged %d", got, version)
	}
	if got := readCompactionIDs(t, optimized); len(got) != 4 || got[0] != 1 || got[3] != 4 {
		t.Fatalf("clean dataset rows = %v, want 1..4", got)
	}
}

func TestOptimizeIndexMergeRejectsForeignPlan(t *testing.T) {
	ctx := context.Background()
	dataset, _ := createOptimizeFixture(t, 8, 8)
	options := &compaction.OptimizeOptions{
		IndexNames:        compaction.Some([]string{"id_bitmap"}),
		MaxRowsPerTask:    compaction.Some(uint64(2)),
		MaxRowsPerSegment: compaction.Some(uint64(2)),
	}
	plan, err := dataset.PlanOptimizeIndices(ctx, options)
	if err != nil {
		t.Fatalf("PlanOptimizeIndices: %v", err)
	}
	other, err := dataset.PlanOptimizeIndices(ctx, options)
	if err != nil {
		t.Fatalf("PlanOptimizeIndices (other): %v", err)
	}
	if plan.PlanID() == other.PlanID() {
		t.Fatal("native plans share an ID")
	}
	if plan.MergeCount() == 0 {
		t.Fatalf("plan has no merges; plan JSON: %s", plan.JSON())
	}

	var taskResult *lance.IndexSegmentResult
	for i := uint64(0); i < plan.TaskCount() && taskResult == nil; i++ {
		task, err := plan.Task(i)
		if err != nil {
			t.Fatalf("Task(%d): %v", i, err)
		}
		taskResult, err = task.Execute(ctx, dataset)
		if err != nil {
			t.Fatalf("task %d Execute: %v", i, err)
		}
	}
	if taskResult == nil {
		t.Fatalf("plan produced no task result; plan JSON: %s", plan.JSON())
	}

	foreignJSON := strings.Replace(taskResult.JSON(), taskResult.PlanID().String(), other.PlanID().String(), 1)
	foreign, err := lance.IndexSegmentResultFromJSON(foreignJSON)
	if err != nil {
		t.Fatalf("IndexSegmentResultFromJSON: %v", err)
	}

	merge, err := plan.Merge(0)
	if err != nil {
		t.Fatalf("Merge(0): %v", err)
	}
	version := versionOf(t, dataset)
	if _, err := merge.Execute(ctx, dataset, []*lance.IndexSegmentResult{foreign}); err == nil ||
		!strings.Contains(err.Error(), "belongs to plan") {
		t.Fatalf("merge foreign-plan result error = %v, want plan ownership error", err)
	}
	if got := versionOf(t, dataset); got != version {
		t.Fatalf("dataset version after rejected merge = %d, want %d", got, version)
	}
}
