package lance_test

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
)

// progressRecorder captures observations and detects calls that begin after
// the synchronous create call returned.
type progressRecorder struct {
	mu       sync.Mutex
	events   []lance.Progress
	returned atomic.Bool
	late     atomic.Int64
}

func (r *progressRecorder) observe(progress lance.Progress) {
	if r.returned.Load() {
		r.late.Add(1)
	}
	r.mu.Lock()
	r.events = append(r.events, progress)
	r.mu.Unlock()
}

func (r *progressRecorder) finish() {
	r.returned.Store(true)
}

func (r *progressRecorder) snapshot() []lance.Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]lance.Progress(nil), r.events...)
}

func (r *progressRecorder) requireNoLateCalls(t *testing.T) {
	t.Helper()
	if late := r.late.Load(); late != 0 {
		t.Fatalf("%d progress callbacks began after the create call returned", late)
	}
}

func readScannerIDs(t *testing.T, scanner *lance.Scanner) []int64 {
	t.Helper()
	var ids []int64
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("scan records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	if err := scanner.Close(); err != nil {
		t.Fatalf("close scanner: %v", err)
	}
	slices.Sort(ids)
	return ids
}

func TestScalarIndexCreationProgress(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "scalar-progress.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"}, []bool{true, true, true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	sourceVersion, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	sourceScan, err := dataset.Scan(lance.WithColumns("id"))
	if err != nil {
		t.Fatalf("source Scan: %v", err)
	}

	failure := &progressRecorder{}
	if _, err := dataset.CreateScalarIndex(ctx, "missing", index.ScalarBTree, nil, index.ScalarOptions{}, lance.WithIndexName("bad"), lance.WithIndexProgress(failure.observe)); err == nil {
		t.Fatal("CreateScalarIndex on a missing column succeeded")
	}
	failure.finish()
	failure.requireNoLateCalls(t)
	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("source Version after failed create = %d, %v; want %d", version, err, sourceVersion)
	}

	defaultProgress := &progressRecorder{}
	indexed, err := dataset.CreateScalarIndex(ctx, "id", index.ScalarBTree, nil, index.ScalarOptions{}, lance.WithIndexName("id_btree"), lance.WithIndexProgress(defaultProgress.observe))
	if err != nil {
		t.Fatalf("CreateScalarIndex with default train: %v", err)
	}
	defer indexed.Close()
	defaultProgress.finish()
	defaultProgress.requireNoLateCalls(t)
	completed := requireProgressInvariants(t, defaultProgress.snapshot())
	if !completed["load_data"] {
		t.Fatalf("scalar progress did not complete load_data: %v", completed)
	}

	train := true
	trainedProgress := &progressRecorder{}
	replaced, err := indexed.CreateScalarIndex(ctx, "id", index.ScalarBTree, nil, index.ScalarOptions{Train: &train}, lance.WithIndexName("id_btree"), lance.WithIndexReplace(true), lance.WithIndexProgress(trainedProgress.observe))
	if err != nil {
		t.Fatalf("CreateScalarIndex with train option: %v", err)
	}
	defer replaced.Close()
	trainedProgress.finish()
	trainedProgress.requireNoLateCalls(t)
	requireProgressInvariants(t, trainedProgress.snapshot())

	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("source Version after create = %d, %v; want %d", version, err, sourceVersion)
	}
	if count, err := dataset.IndexCount(); err != nil || count != 0 {
		t.Fatalf("source IndexCount after create = %d, %v; want 0, nil", count, err)
	}
	if ids := readScannerIDs(t, sourceScan); !slices.Equal(ids, []int64{1, 2, 3, 4}) {
		t.Fatalf("source scanner ids after create = %v, want [1 2 3 4]", ids)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if ids := scanIDs(t, reopened, lance.WithSQLFilter("id = 3")); !slices.Equal(ids, []int64{3}) {
		t.Fatalf("reopened scalar query ids = %v, want [3]", ids)
	}
}

func TestVectorIndexCreationProgress(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, uri := segmentVectorDataset(t)
	sourceVersion, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}

	legacyProgress := &progressRecorder{}
	indexed, err := dataset.CreateVectorIndexWithParams(ctx, "vector", index.VectorParams{
		IndexType:     index.VectorTypeIVFFlat,
		Metric:        index.MetricL2,
		NumPartitions: 2,
		MaxIterations: 10,
		SampleRate:    128,
	}, lance.WithIndexName("vector_idx"), lance.WithIndexProgress(legacyProgress.observe))
	if err != nil {
		t.Fatalf("CreateVectorIndexWithParams: %v", err)
	}
	defer indexed.Close()
	legacyProgress.finish()
	legacyProgress.requireNoLateCalls(t)
	completed := requireProgressInvariants(t, legacyProgress.snapshot())
	if !completed["shuffle"] {
		t.Fatalf("vector progress did not complete shuffle: %v", completed)
	}

	partitions := uint64(2)
	maxIters := uint64(10)
	sampleRate := uint64(128)
	train := true
	optionsProgress := &progressRecorder{}
	replaced, err := indexed.CreateVectorIndex(ctx, "vector", index.VectorOptions{
		IndexType:     index.VectorTypeIVFFlat,
		Metric:        index.MetricL2,
		NumPartitions: &partitions,
		MaxIters:      &maxIters,
		SampleRate:    &sampleRate,
		Train:         &train,
	}, lance.WithIndexName("vector_idx"), lance.WithIndexReplace(true), lance.WithIndexProgress(optionsProgress.observe))
	if err != nil {
		t.Fatalf("CreateVectorIndex with options: %v", err)
	}
	defer replaced.Close()
	optionsProgress.finish()
	optionsProgress.requireNoLateCalls(t)
	requireProgressInvariants(t, optionsProgress.snapshot())

	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("source Version after create = %d, %v; want %d", version, err, sourceVersion)
	}
	if count, err := dataset.IndexCount(); err != nil || count != 0 {
		t.Fatalf("source IndexCount after create = %d, %v; want 0, nil", count, err)
	}

	failure := &progressRecorder{}
	if _, err := replaced.CreateVectorIndex(ctx, "missing", index.VectorOptions{IndexType: index.VectorTypeIVFFlat, Metric: index.MetricL2}, lance.WithIndexName("bad"), lance.WithIndexProgress(failure.observe)); err == nil {
		t.Fatal("CreateVectorIndex on a missing column succeeded")
	}
	failure.finish()
	failure.requireNoLateCalls(t)

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	requireSegmentNearest(t, reopened, []float32{1, 2, 3, 4}, 1)
}

func TestIndexCreationProgressPanicIsIsolated(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "panic-progress.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"}, []bool{true, true, true, true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	var calls atomic.Int64
	indexed, err := dataset.CreateScalarIndex(ctx, "id", index.ScalarBTree, nil, index.ScalarOptions{}, lance.WithIndexName("panic_btree"), lance.WithIndexProgress(func(lance.Progress) {
		calls.Add(1)
		panic("observer panic")
	}))
	if err != nil {
		t.Fatalf("CreateScalarIndex with panicking observer: %v", err)
	}
	defer indexed.Close()
	if calls.Load() == 0 {
		t.Fatal("panicking observer was never called")
	}
}
