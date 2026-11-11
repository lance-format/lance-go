package lance_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
)

func makeVectorFragment(t *testing.T, schema *arrow.Schema, ids []int64, vectors [][]float32) arrow.RecordBatch {
	t.Helper()
	if len(ids) != len(vectors) {
		t.Fatalf("id/vector count mismatch: %d vs %d", len(ids), len(vectors))
	}
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)
	builder.Field(0).(*array.Int64Builder).AppendValues(ids, nil)
	fixed := builder.Field(1).(*array.FixedSizeListBuilder)
	values := fixed.ValueBuilder().(*array.Float32Builder)
	for _, vector := range vectors {
		fixed.Append(true)
		values.AppendValues(vector, nil)
	}
	return builder.NewRecordBatch()
}

// segmentVectorDataset writes two fragments of four-dimensional vectors and
// returns the reopened dataset plus its URI. The fixture is large enough for
// native IVF and PQ training, which need more rows than codewords.
func segmentVectorDataset(t *testing.T) (*lance.Dataset, string) {
	t.Helper()
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "segments.lance")
	schema := vectorTestSchema()
	first := makeVectorFragment(t, schema, segmentIDs(1, 64), segmentVectors(1, 64))
	created, err := lance.Create(ctx, uri, makeReader(t, schema, first))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := created.Close(); err != nil {
		t.Fatalf("close created dataset: %v", err)
	}
	second := makeVectorFragment(t, schema, segmentIDs(65, 128), segmentVectors(65, 128))
	appended, err := lance.Append(ctx, uri, makeReader(t, schema, second))
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

// segmentIDs returns the inclusive id range [first, last].
func segmentIDs(first, last int64) []int64 {
	ids := make([]int64, 0, last-first+1)
	for id := first; id <= last; id++ {
		ids = append(ids, id)
	}
	return ids
}

// segmentVectors returns one deterministic vector per id, where component j of
// the vector for id k is k+j. The vector for id 1 is therefore {1, 2, 3, 4}.
func segmentVectors(first, last int64) [][]float32 {
	vectors := make([][]float32, 0, last-first+1)
	for id := first; id <= last; id++ {
		vector := make([]float32, 4)
		for component := range vector {
			vector[component] = float32(id) + float32(component)
		}
		vectors = append(vectors, vector)
	}
	return vectors
}

func trainedIVFCentroids(t *testing.T, dataset *lance.Dataset, partitions uint32) *lance.Model {
	t.Helper()
	centroids, err := dataset.TrainIVFModel(context.Background(), "vector", partitions, index.MetricL2, nil, index.TrainerOptions{})
	if err != nil {
		t.Fatalf("TrainIVFModel: %v", err)
	}
	t.Cleanup(centroids.Release)
	return centroids
}

func buildVectorSegment(t *testing.T, dataset *lance.Dataset, centroids *lance.Model, partitions uint64, scope index.SegmentScope) []byte {
	t.Helper()
	ctx := context.Background()
	options := index.VectorOptions{
		IndexType:     index.VectorTypeIVFFlat,
		Metric:        index.MetricL2,
		NumPartitions: &partitions,
		IVFCentroids:  centroids,
	}
	builder, err := dataset.NewVectorSegmentBuilder(ctx, "vector", "distributed", options, scope)
	if err != nil {
		t.Fatalf("NewVectorSegmentBuilder: %v", err)
	}
	data, err := builder.Execute(ctx)
	if err != nil {
		_ = builder.Close()
		t.Fatalf("Execute: %v", err)
	}
	if err := builder.Close(); err != nil {
		t.Fatalf("close segment builder: %v", err)
	}
	return data
}

func parseSegment(t *testing.T, data []byte) *lance.IndexSegmentMetadata {
	t.Helper()
	metadata, err := lance.ParseIndexSegmentMetadata(data)
	if err != nil {
		t.Fatalf("ParseIndexSegmentMetadata: %v", err)
	}
	t.Cleanup(func() {
		if err := metadata.Close(); err != nil {
			t.Errorf("close segment metadata: %v", err)
		}
	})
	return metadata
}

func requireSegmentNearest(t *testing.T, dataset *lance.Dataset, vector []float32, want int64) {
	t.Helper()
	scan, err := dataset.Scan(
		lance.WithNearest("vector", lance.NewFloat32Vector(vector), 1),
		lance.WithVectorMetric(index.MetricL2),
		lance.WithVectorNProbes(2),
		lance.WithVectorFastSearch(true),
	)
	if err != nil {
		t.Fatalf("nearest scan: %v", err)
	}
	defer func() {
		if err := scan.Close(); err != nil {
			t.Errorf("close scan: %v", err)
		}
	}()
	rows := collectNearest(t, scan)
	requireNearestIDs(t, rows, []int64{want})
}

// requireProgressInvariants checks the documented progress callback contract
// and returns the stages that reported both a start and a completion.
func requireProgressInvariants(t *testing.T, events []lance.Progress) map[string]bool {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no progress events were observed")
	}
	started := make(map[string]bool)
	completed := make(map[string]bool)
	for position, event := range events {
		if event.Stage == "" {
			t.Fatalf("progress event %d has an empty stage: %+v", position, event)
		}
		switch event.Event {
		case lance.ProgressStart:
			if event.Completed != 0 {
				t.Fatalf("start event %d reports %d completed units", position, event.Completed)
			}
			started[event.Stage] = true
		case lance.ProgressAdvance:
			if event.Total != 0 {
				t.Fatalf("advance event %d reports total %d, want 0", position, event.Total)
			}
		case lance.ProgressComplete:
			if event.Total != 0 || event.Completed != 0 {
				t.Fatalf("complete event %d reports total/completed %d/%d, want 0/0", position, event.Total, event.Completed)
			}
			completed[event.Stage] = true
		default:
			t.Fatalf("progress event %d has unknown event code %d", position, event.Event)
		}
	}
	for stage := range started {
		if !completed[stage] {
			t.Fatalf("stage %q started but never completed", stage)
		}
	}
	for stage := range completed {
		if !started[stage] {
			t.Fatalf("stage %q completed without starting", stage)
		}
	}
	return completed
}

func TestDistributedVectorIndexSegments(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, uri := segmentVectorDataset(t)
	sourceVersion, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}

	centroids := trainedIVFCentroids(t, dataset, 2)
	whole := buildVectorSegment(t, dataset, centroids, 2, index.SegmentScope{})
	wholeMetadata := parseSegment(t, whole)
	coverage, err := wholeMetadata.FragmentIDs()
	if err != nil {
		t.Fatalf("whole-dataset coverage: %v", err)
	}
	if len(coverage) < 2 {
		t.Fatalf("whole-dataset segment covers %v, want both fragments", coverage)
	}
	if !slices.IsSorted(coverage) {
		t.Fatalf("fragment coverage %v is not ascending", coverage)
	}
	if version, err := wholeMetadata.DatasetVersion(); err != nil || version != sourceVersion {
		t.Fatalf("segment dataset version = %d, %v, want %d", version, err, sourceVersion)
	}
	if name, err := wholeMetadata.Name(); err != nil || name != "distributed" {
		t.Fatalf("segment index name = %q, %v, want distributed", name, err)
	}
	wholeType, err := wholeMetadata.Type()
	if err != nil {
		t.Fatalf("segment index type: %v", err)
	}
	if vectorType, ok := wholeType.Vector(); !ok || vectorType != index.VectorTypeIVFFlat {
		t.Fatalf("segment index type = %v, want IVFFlat", wholeType)
	}

	var segments [][]byte
	for _, fragmentID := range coverage {
		segment := buildVectorSegment(t, dataset, centroids, 2, index.SegmentScope{FragmentIDs: []uint32{fragmentID}})
		metadata := parseSegment(t, segment)
		ids, err := metadata.FragmentIDs()
		if err != nil {
			t.Fatalf("segment %d coverage: %v", fragmentID, err)
		}
		if !slices.Equal(ids, []uint32{fragmentID}) {
			t.Fatalf("segment %d covers %v, want only %d", fragmentID, ids, fragmentID)
		}
		segments = append(segments, segment)
	}

	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("dataset version after building segments = %d, %v, want %d", version, err, sourceVersion)
	}
	if count, err := dataset.IndexCount(); err != nil || count != 0 {
		t.Fatalf("index count after building segments = %d, %v, want 0", count, err)
	}

	if _, err := dataset.CommitIndexSegments(ctx, "malformed", "vector", [][]byte{[]byte("not index metadata")}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("commit malformed segment error = %v, want ErrInvalidArgument", err)
	}
	// The keyed column is part of the segment value, so a different existing
	// column cannot publish it.
	if _, err := dataset.CommitIndexSegments(ctx, "distributed", "id", [][]byte{segments[0]}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("commit under a mismatched column error = %v, want ErrInvalidArgument", err)
	}
	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("dataset version after rejected commit = %d, %v, want %d", version, err, sourceVersion)
	}

	indexed, err := dataset.CommitIndexSegments(ctx, "distributed", "vector", segments)
	if err != nil {
		t.Fatalf("CommitIndexSegments: %v", err)
	}
	indexedVersion, err := indexed.Version()
	if err != nil {
		t.Fatalf("indexed Version: %v", err)
	}
	if indexedVersion != sourceVersion+1 {
		t.Fatalf("indexed version = %d, want %d", indexedVersion, sourceVersion+1)
	}
	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("source version after commit = %d, %v, want %d", version, err, sourceVersion)
	}
	if count, err := dataset.IndexCount(); err != nil || count != 0 {
		t.Fatalf("source index count after commit = %d, %v, want 0", count, err)
	}

	listings, err := indexed.IndexSegments()
	if err != nil {
		t.Fatalf("IndexSegments: %v", err)
	}
	if len(listings) != len(segments) {
		t.Fatalf("index segment count = %d, want %d", len(listings), len(segments))
	}
	listed := make([]uint32, 0, len(listings))
	for _, listing := range listings {
		if listing.Name != "distributed" {
			t.Fatalf("segment name = %q, want distributed", listing.Name)
		}
		if len(listing.FragmentIDs) != 1 {
			t.Fatalf("segment covers %v, want exactly one fragment", listing.FragmentIDs)
		}
		listed = append(listed, listing.FragmentIDs[0])
	}
	slices.Sort(listed)
	if !slices.Equal(listed, coverage) {
		t.Fatalf("listed fragment coverage = %v, want %v", listed, coverage)
	}

	requireSegmentNearest(t, indexed, []float32{1, 2, 3, 4}, 1)
	if err := indexed.Close(); err != nil {
		t.Fatalf("close indexed dataset: %v", err)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("reopen indexed dataset: %v", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("close reopened dataset: %v", err)
		}
	}()
	if version, err := reopened.Version(); err != nil || version != indexedVersion {
		t.Fatalf("reopened version = %d, %v, want %d", version, err, indexedVersion)
	}
	requireSegmentNearest(t, reopened, []float32{1, 2, 3, 4}, 1)
}

func TestIndexSegmentMerge(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, _ := segmentVectorDataset(t)
	sourceVersion, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	centroids := trainedIVFCentroids(t, dataset, 2)
	wholeMetadata := parseSegment(t, buildVectorSegment(t, dataset, centroids, 2, index.SegmentScope{}))
	coverage, err := wholeMetadata.FragmentIDs()
	if err != nil {
		t.Fatalf("whole-dataset coverage: %v", err)
	}

	var segments [][]byte
	for _, fragmentID := range coverage {
		segments = append(segments, buildVectorSegment(t, dataset, centroids, 2, index.SegmentScope{FragmentIDs: []uint32{fragmentID}}))
	}

	for _, invalid := range [][][]byte{
		{segments[0], segments[0]},
		{[]byte("not index metadata")},
	} {
		if _, err := dataset.MergeIndexSegments(ctx, invalid); !errors.Is(err, lance.ErrInvalidArgument) {
			t.Fatalf("merge of %d segment(s) error = %v, want ErrInvalidArgument", len(invalid), err)
		}
	}

	merged, err := dataset.MergeIndexSegments(ctx, segments)
	if err != nil {
		t.Fatalf("MergeIndexSegments: %v", err)
	}
	mergedMetadata := parseSegment(t, merged)
	mergedCoverage, err := mergedMetadata.FragmentIDs()
	if err != nil {
		t.Fatalf("merged coverage: %v", err)
	}
	if !slices.Equal(mergedCoverage, coverage) {
		t.Fatalf("merged coverage = %v, want %v", mergedCoverage, coverage)
	}
	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("dataset version after merge = %d, %v, want %d", version, err, sourceVersion)
	}

	indexed, err := dataset.CommitIndexSegments(ctx, "distributed", "vector", [][]byte{merged})
	if err != nil {
		t.Fatalf("CommitIndexSegments: %v", err)
	}
	defer func() {
		if err := indexed.Close(); err != nil {
			t.Errorf("close indexed dataset: %v", err)
		}
	}()
	if version, err := indexed.Version(); err != nil || version != sourceVersion+1 {
		t.Fatalf("indexed version = %d, %v, want %d", version, err, sourceVersion+1)
	}
	listings, err := indexed.IndexSegments()
	if err != nil {
		t.Fatalf("IndexSegments: %v", err)
	}
	if len(listings) != 1 || !slices.Equal(listings[0].FragmentIDs, coverage) {
		t.Fatalf("merged index segments = %+v, want one segment covering %v", listings, coverage)
	}
	requireSegmentNearest(t, indexed, []float32{1, 2, 3, 4}, 1)
}

func TestIndexSegmentProgressAndMetadata(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "scalar.lance")
	schema := testSchema()
	record := makeRecord(t, schema,
		[]int64{1, 2, 3, 4},
		[]string{"alpha beta", "alpha gamma", "beta gamma", "delta epsilon"},
		[]bool{true, true, true, true},
	)
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() {
		if err := dataset.Close(); err != nil {
			t.Errorf("close dataset: %v", err)
		}
	}()
	sourceVersion, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}

	builder, err := dataset.NewScalarSegmentBuilder(
		ctx, "name", "fts", index.ScalarInverted,
		json.RawMessage(`{"with_position":true}`),
		index.ScalarOptions{Train: ptr(true)},
		index.SegmentScope{FragmentIDs: []uint32{0}},
	)
	if err != nil {
		t.Fatalf("NewScalarSegmentBuilder: %v", err)
	}
	if err := builder.SetProgress(nil); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("SetProgress(nil) error = %v, want ErrInvalidArgument", err)
	}

	var replacedMu sync.Mutex
	replacedCalls := 0
	if err := builder.SetProgress(func(lance.Progress) {
		replacedMu.Lock()
		replacedCalls++
		replacedMu.Unlock()
	}); err != nil {
		t.Fatalf("SetProgress(replaced): %v", err)
	}

	var mu sync.Mutex
	var observed []lance.Progress
	if err := builder.SetProgress(func(progress lance.Progress) {
		mu.Lock()
		observed = append(observed, progress)
		mu.Unlock()
	}); err != nil {
		t.Fatalf("SetProgress(observed): %v", err)
	}

	segment, err := builder.Execute(ctx)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := builder.Execute(ctx); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("second Execute error = %v, want ErrInvalidArgument", err)
	}
	if err := builder.SetProgress(func(lance.Progress) {}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("SetProgress after Execute error = %v, want ErrInvalidArgument", err)
	}
	if err := builder.Close(); err != nil {
		t.Fatalf("close segment builder: %v", err)
	}
	if _, err := builder.Execute(ctx); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("Execute after Close error = %v, want ErrClosed", err)
	}
	if err := builder.SetProgress(func(lance.Progress) {}); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("SetProgress after Close error = %v, want ErrClosed", err)
	}

	replacedMu.Lock()
	replacedCount := replacedCalls
	replacedMu.Unlock()
	if replacedCount != 0 {
		t.Fatalf("replaced progress callback received %d events, want 0", replacedCount)
	}
	mu.Lock()
	events := append([]lance.Progress(nil), observed...)
	mu.Unlock()
	completed := requireProgressInvariants(t, events)
	if !completed["load_data"] {
		t.Fatalf("inverted index build did not complete the load_data stage: %v", completed)
	}
	for _, event := range events {
		switch event.Event {
		case lance.ProgressStart:
			if event.Unit == "" {
				t.Fatalf("%s start event has an empty unit", event.Stage)
			}
		case lance.ProgressAdvance:
			if event.Unit != "" {
				t.Fatalf("%s advance event reports unit %q, want empty", event.Stage, event.Unit)
			}
		}
	}

	metadata := parseSegment(t, segment)
	metadataUUID, err := metadata.UUID()
	if err != nil {
		t.Fatalf("metadata UUID: %v", err)
	}
	if name, err := metadata.Name(); err != nil || name != "fts" {
		t.Fatalf("metadata name = %q, %v, want fts", name, err)
	}
	if version, err := metadata.DatasetVersion(); err != nil || version != sourceVersion {
		t.Fatalf("metadata dataset version = %d, %v, want %d", version, err, sourceVersion)
	}
	segmentType, err := metadata.Type()
	if err != nil {
		t.Fatalf("metadata type: %v", err)
	}
	if scalarType, ok := segmentType.Scalar(); !ok || scalarType != index.ScalarInverted {
		t.Fatalf("metadata index type = %v, want Inverted", segmentType)
	}
	fieldIDs, err := metadata.FieldIDs()
	if err != nil {
		t.Fatalf("metadata fields: %v", err)
	}
	if len(fieldIDs) != 1 {
		t.Fatalf("metadata fields = %v, want one field", fieldIDs)
	}
	coveringFields, err := metadata.CoveringFieldIDs()
	if err != nil {
		t.Fatalf("metadata covering fields: %v", err)
	}
	if len(coveringFields) != 1 {
		t.Fatalf("metadata covering fields = %v, want one field", coveringFields)
	}
	coverage, err := metadata.FragmentIDs()
	if err != nil {
		t.Fatalf("metadata coverage: %v", err)
	}
	if !slices.Equal(coverage, []uint32{0}) {
		t.Fatalf("metadata coverage = %v, want [0]", coverage)
	}
	if typeURL, present, err := metadata.DetailsTypeURL(); err != nil || !present || typeURL == "" {
		t.Fatalf("metadata details type URL = %q, %v, %v", typeURL, present, err)
	}
	if _, present, err := metadata.Details(); err != nil || !present {
		t.Fatalf("metadata details present = %v, %v; want present", present, err)
	}
	files, err := metadata.Files()
	if err != nil {
		t.Fatalf("metadata files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("inverted segment recorded no index files")
	}
	for _, file := range files {
		if file.Path == "" {
			t.Fatalf("metadata file = %+v, want a non-empty path", file)
		}
	}

	encoded, err := metadata.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	reparsed := parseSegment(t, encoded)
	reencoded, err := reparsed.Encode()
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if string(reencoded) != string(encoded) {
		t.Fatalf("encode is not stable across a parse round trip")
	}
	clone, err := metadata.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	cloneWithCoverage, err := metadata.CloneWithFragmentIDs([]uint32{0, 1})
	if err != nil {
		clone.Close()
		t.Fatalf("CloneWithFragmentIDs: %v", err)
	}
	keptCoverage, err := metadata.CloneWithFragmentIDs(nil)
	if err != nil {
		clone.Close()
		if err := cloneWithCoverage.Close(); err != nil {
			t.Fatalf("close coverage clone: %v", err)
		}
		t.Fatalf("CloneWithFragmentIDs(nil): %v", err)
	}
	emptyCoverage, err := metadata.CloneWithFragmentIDs([]uint32{})
	if err != nil {
		clone.Close()
		if err := cloneWithCoverage.Close(); err != nil {
			t.Fatalf("close coverage clone: %v", err)
		}
		if err := keptCoverage.Close(); err != nil {
			t.Fatalf("close kept coverage clone: %v", err)
		}
		t.Fatalf("CloneWithFragmentIDs(empty): %v", err)
	}
	if err := metadata.Close(); err != nil {
		t.Fatalf("close metadata: %v", err)
	}
	if _, err := metadata.Encode(); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("Encode after Close error = %v, want ErrClosed", err)
	}
	if _, err := clone.Encode(); err != nil {
		t.Fatalf("cloned metadata unusable after closing the source: %v", err)
	}
	if err := clone.Close(); err != nil {
		t.Fatalf("close clone: %v", err)
	}
	if cloneCoverage, err := cloneWithCoverage.FragmentIDs(); err != nil || !slices.Equal(cloneCoverage, []uint32{0, 1}) {
		t.Fatalf("replaced coverage = %v, %v, want [0 1]", cloneCoverage, err)
	}
	if err := cloneWithCoverage.Close(); err != nil {
		t.Fatalf("close coverage clone: %v", err)
	}
	if kept, err := keptCoverage.FragmentIDs(); err != nil || !slices.Equal(kept, coverage) {
		t.Fatalf("kept coverage = %v, %v, want %v", kept, err, coverage)
	}
	if err := keptCoverage.Close(); err != nil {
		t.Fatalf("close kept coverage clone: %v", err)
	}
	if empty, err := emptyCoverage.FragmentIDs(); err != nil || len(empty) != 0 {
		t.Fatalf("empty coverage = %v, %v, want []", empty, err)
	}
	if err := emptyCoverage.Close(); err != nil {
		t.Fatalf("close empty coverage clone: %v", err)
	}

	if _, err := lance.ParseIndexSegmentMetadata(nil); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("ParseIndexSegmentMetadata(nil) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := lance.ParseIndexSegmentMetadata([]byte("not index metadata")); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("ParseIndexSegmentMetadata(garbage) error = %v, want ErrInvalidArgument", err)
	}

	// Bitmap segments carry a present but empty details payload, which must stay
	// distinguishable from an absent payload.
	bitmapBuilder, err := dataset.NewScalarSegmentBuilder(
		ctx, "id", "id_bitmap", index.ScalarBitmap, nil, index.ScalarOptions{},
		index.SegmentScope{FragmentIDs: []uint32{0}},
	)
	if err != nil {
		t.Fatalf("NewScalarSegmentBuilder(bitmap): %v", err)
	}
	bitmapSegment, err := bitmapBuilder.Execute(ctx)
	if err != nil {
		_ = bitmapBuilder.Close()
		t.Fatalf("bitmap Execute: %v", err)
	}
	if err := bitmapBuilder.Close(); err != nil {
		t.Fatalf("close bitmap builder: %v", err)
	}
	bitmapMetadata := parseSegment(t, bitmapSegment)
	bitmapTypeURL, present, err := bitmapMetadata.DetailsTypeURL()
	if err != nil || !present || !strings.HasSuffix(bitmapTypeURL, "BitmapIndexDetails") {
		t.Fatalf("bitmap details type URL = %q, %v, %v", bitmapTypeURL, present, err)
	}
	bitmapDetails, present, err := bitmapMetadata.Details()
	if err != nil || !present {
		t.Fatalf("bitmap details present = %v, %v; want present", present, err)
	}
	if len(bitmapDetails) != 0 {
		t.Fatalf("bitmap details length = %d, want present but empty", len(bitmapDetails))
	}

	// The root artifact is merged from the still-uncommitted segment files, so
	// merging must not publish a dataset version. Publishing renames the index
	// through its own commit, mirroring the native capability tests.
	var mergeMu sync.Mutex
	var mergeEvents []lance.Progress
	if err := dataset.MergeIndexMetadata(ctx, metadataUUID, "Inverted", index.MergeMetadataOptions{
		BatchReadhead: ptr(uint64(0)),
		Progress: func(progress lance.Progress) {
			mergeMu.Lock()
			mergeEvents = append(mergeEvents, progress)
			mergeMu.Unlock()
		},
	}); err != nil {
		t.Fatalf("MergeIndexMetadata: %v", err)
	}
	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("version after metadata merge = %d, %v, want %d", version, err, sourceVersion)
	}
	mergeMu.Lock()
	mergeSnapshot := append([]lance.Progress(nil), mergeEvents...)
	mergeMu.Unlock()
	if len(mergeSnapshot) > 0 {
		requireProgressInvariants(t, mergeSnapshot)
	}

	indexed, err := dataset.CommitIndexSegments(ctx, "fts-committed", "name", [][]byte{segment})
	if err != nil {
		t.Fatalf("CommitIndexSegments: %v", err)
	}
	defer func() {
		if err := indexed.Close(); err != nil {
			t.Errorf("close indexed dataset: %v", err)
		}
	}()
	indexVersion, err := indexed.Version()
	if err != nil {
		t.Fatalf("indexed Version: %v", err)
	}
	if indexVersion != sourceVersion+1 {
		t.Fatalf("indexed version = %d, want %d", indexVersion, sourceVersion+1)
	}

	if err := indexed.MergeIndexMetadata(ctx, metadataUUID, "BTree", index.MergeMetadataOptions{}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("MergeIndexMetadata(BTree) error = %v, want ErrInvalidArgument", err)
	}
	if err := indexed.MergeIndexMetadata(ctx, metadataUUID, "", index.MergeMetadataOptions{}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("MergeIndexMetadata(empty type) error = %v, want ErrInvalidArgument", err)
	}
}

func TestIndexSegmentModels(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset, _ := segmentVectorDataset(t)
	sourceVersion, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}

	centroids := trainedIVFCentroids(t, dataset, 2)
	if centroids.Len() != 2 {
		t.Fatalf("IVF centroid rows = %d, want one per partition", centroids.Len())
	}
	listType, ok := centroids.DataType().(*arrow.FixedSizeListType)
	if !ok || listType.Len() != 4 || !arrow.TypeEqual(listType.ElemType(), arrow.PrimitiveTypes.Float32) {
		t.Fatalf("IVF centroid type = %s, want fixed-size list of 4 float32", centroids.DataType())
	}

	codebook, err := dataset.TrainPQModel(ctx, "vector", 2, 4, index.MetricL2, centroids, nil, index.TrainerOptions{})
	if err != nil {
		t.Fatalf("TrainPQModel: %v", err)
	}
	defer codebook.Release()
	if want := 2 * (1 << 4); codebook.Len() != want {
		t.Fatalf("PQ codebook rows = %d, want %d", codebook.Len(), want)
	}
	codebookType, ok := codebook.DataType().(*arrow.FixedSizeListType)
	if !ok || codebookType.Len() != 2 || !arrow.TypeEqual(codebookType.ElemType(), arrow.PrimitiveTypes.Float32) {
		t.Fatalf("PQ codebook type = %s, want fixed-size list of 2 float32", codebook.DataType())
	}

	// Native PQ training reads the centroid provenance from the centroids field
	// metadata and rejects models trained for a different metric.
	cosineCentroids, err := dataset.TrainIVFModel(ctx, "vector", 2, index.MetricCosine, nil, index.TrainerOptions{})
	if err != nil {
		t.Fatalf("TrainIVFModel(cosine): %v", err)
	}
	defer cosineCentroids.Release()
	if _, err := dataset.TrainPQModel(ctx, "vector", 2, 4, index.MetricL2, cosineCentroids, nil, index.TrainerOptions{}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("PQ training with mismatched centroid metric error = %v, want ErrInvalidArgument", err)
	}

	partitions, subVectors, bits := uint64(2), uint64(2), uint64(4)
	options := index.VectorOptions{
		IndexType:     index.VectorTypeIVFPQ,
		Metric:        index.MetricL2,
		NumPartitions: &partitions,
		NumSubVectors: &subVectors,
		NumBits:       &bits,
		IVFCentroids:  centroids,
		PQCodebook:    codebook,
	}
	builder, err := dataset.NewVectorSegmentBuilder(ctx, "vector", "pq", options, index.SegmentScope{})
	if err != nil {
		t.Fatalf("NewVectorSegmentBuilder(IVFPQ): %v", err)
	}
	segment, err := builder.Execute(ctx)
	if err != nil {
		_ = builder.Close()
		t.Fatalf("Execute(IVFPQ): %v", err)
	}
	if err := builder.Close(); err != nil {
		t.Fatalf("close IVFPQ builder: %v", err)
	}
	metadata := parseSegment(t, segment)
	segmentType, err := metadata.Type()
	if err != nil {
		t.Fatalf("IVFPQ metadata type: %v", err)
	}
	if vectorType, ok := segmentType.Vector(); !ok || vectorType != index.VectorTypeIVFPQ {
		t.Fatalf("IVFPQ metadata type = %v, want IVFPQ", segmentType)
	}

	// The same trained centroids remain usable by another builder: native
	// borrows the model pairs synchronously and leaves them caller-owned.
	reused := buildVectorSegment(t, dataset, centroids, 2, index.SegmentScope{FragmentIDs: []uint32{0}})

	reusedMetadata := parseSegment(t, reused)
	if reusedCoverage, err := reusedMetadata.FragmentIDs(); err != nil || !slices.Equal(reusedCoverage, []uint32{0}) {
		t.Fatalf("reused-model segment coverage = %v, %v, want [0]", reusedCoverage, err)
	}

	// One commit call cannot mix index types even when every segment is valid
	// on its own.
	if _, err := dataset.CommitIndexSegments(ctx, "mixed", "vector", [][]byte{segment, reused}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("mixed-type commit error = %v, want ErrInvalidArgument", err)
	}
	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("dataset version after rejected mixed commit = %d, %v, want %d", version, err, sourceVersion)
	}

	indexed, err := dataset.CommitIndexSegments(ctx, "pq", "vector", [][]byte{segment})
	if err != nil {
		t.Fatalf("CommitIndexSegments: %v", err)
	}
	defer func() {
		if err := indexed.Close(); err != nil {
			t.Errorf("close indexed dataset: %v", err)
		}
	}()
	if version, err := indexed.Version(); err != nil || version != sourceVersion+1 {
		t.Fatalf("indexed version = %d, %v, want %d", version, err, sourceVersion+1)
	}
	listings, err := indexed.IndexSegments()
	if err != nil {
		t.Fatalf("IndexSegments: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("index segment count = %d, want 1", len(listings))
	}

	// A PQ codebook is only meaningful for the IVF-PQ family; other vector
	// index types reject it instead of silently ignoring it.
	flatOptions := options
	flatOptions.IndexType = index.VectorTypeIVFFlat
	mismatched, err := dataset.NewVectorSegmentBuilder(ctx, "vector", "flat-with-codebook", flatOptions, index.SegmentScope{})
	if err == nil {
		_, err = mismatched.Execute(ctx)
		_ = mismatched.Close()
	}
	if !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("PQ codebook on IVF-Flat error = %v, want ErrInvalidArgument", err)
	}
}

func TestRQModel(t *testing.T) {
	requireNative(t)
	model, err := lance.BuildRQModel(8, 0)
	if err != nil {
		t.Fatalf("BuildRQModel: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(model, &decoded); err != nil {
		t.Fatalf("RQ model is not valid JSON: %v", err)
	}
	if bits, ok := decoded["num_bits"].(float64); !ok || bits != 5 {
		t.Fatalf("default RQ num_bits = %v, want 5", decoded["num_bits"])
	}
	if _, err := lance.BuildRQModel(6, 0); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("BuildRQModel(6) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := lance.BuildRQModel(0, 0); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("BuildRQModel(0) error = %v, want ErrInvalidArgument", err)
	}
}
