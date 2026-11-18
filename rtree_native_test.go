//go:build lance_rtree_native

package lance_test

import (
	"context"
	"encoding/json"
	"errors"
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

const rtreeGeometryExtension = "geoarrow.linestring"

func rtreeGeometryType() *arrow.ListType {
	coordinate := arrow.StructOf(
		arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
		arrow.Field{Name: "y", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
	)
	return arrow.ListOfField(arrow.Field{Name: "vertices", Type: coordinate, Nullable: false})
}

// rtreeTestSchema mirrors the candidate C fixture: non-null Int32 ids and a
// nullable GeoArrow LineString column stored as List<Struct<x, y>> with the
// geoarrow.linestring extension name. Default GeoArrow metadata is empty, so
// the field carries no ARROW:extension:metadata entry.
func rtreeTestSchema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32, Nullable: false},
		{
			Name:     "geometry",
			Type:     rtreeGeometryType(),
			Nullable: true,
			Metadata: arrow.NewMetadata(
				[]string{"ARROW:extension:name"},
				[]string{rtreeGeometryExtension},
			),
		},
	}, nil)
}

type rtreeLine struct {
	coordinates [][2]float64
	null        bool
}

// rtreeFixtureLines reproduces the native RTree fixture. Null rows and empty
// LineStrings are distinct. The queried geometries also distinguish bounding-box
// candidates from exact intersections: the diagonal at id 100 has a bounding
// box containing POINT (0 10), and the closed rectangle at id 105 has a bounding
// box containing LINESTRING (25 25, 26 26), but neither intersects the query.
func rtreeFixtureLines() []rtreeLine {
	return []rtreeLine{
		{coordinates: [][2]float64{{0, 0}, {10, 10}}},
		{coordinates: [][2]float64{{0, 10}, {10, 10}}},
		{coordinates: [][2]float64{{2, 0}, {2, 10}}},
		{null: true},
		{coordinates: [][2]float64{}},
		{coordinates: [][2]float64{{20, 20}, {30, 20}, {30, 30}, {20, 30}, {20, 20}}},
		{coordinates: [][2]float64{{20, 20}, {30, 30}}},
		{coordinates: [][2]float64{{40, 40}, {50, 50}}},
		{null: true},
		{coordinates: [][2]float64{{60, 60}, {61, 61}}},
		{coordinates: [][2]float64{{0, 5}, {5, 0}}},
		{coordinates: [][2]float64{{100, 100}, {101, 101}}},
		{coordinates: [][2]float64{}},
		{coordinates: [][2]float64{{2, 2}, {3, 2}}},
		{coordinates: [][2]float64{{0, 10}, {5, 10}}},
	}
}

func makeRTreeRecord(t *testing.T, schema *arrow.Schema) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)

	ids := make([]int32, 0, 15)
	for id := int32(100); id <= 114; id++ {
		ids = append(ids, id)
	}
	builder.Field(0).(*array.Int32Builder).AppendValues(ids, nil)

	geometry := builder.Field(1).(*array.ListBuilder)
	values := geometry.ValueBuilder().(*array.StructBuilder)
	x := values.FieldBuilder(0).(*array.Float64Builder)
	y := values.FieldBuilder(1).(*array.Float64Builder)
	for _, line := range rtreeFixtureLines() {
		if line.null {
			geometry.AppendNull()
			continue
		}
		geometry.Append(true)
		for _, point := range line.coordinates {
			values.Append(true)
			x.Append(point[0])
			y.Append(point[1])
		}
	}

	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return record
}

func createRTreeFixture(t *testing.T) (*lance.Dataset, string) {
	t.Helper()
	requireNative(t)

	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "rtree.lance")
	schema := rtreeTestSchema()
	record := makeRTreeRecord(t, schema)
	dataset, err := lance.Create(
		ctx,
		uri,
		makeReader(t, schema, record),
		lance.WithWriteParams(lance.WriteParams{
			MaxRowsPerFile:     5,
			EnableStableRowIDs: true,
		}),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if err := dataset.Close(); err != nil {
			t.Errorf("close dataset: %v", err)
		}
	})
	stored, err := dataset.Schema()
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if stored.NumFields() != 2 || stored.Field(1).Name != "geometry" {
		t.Fatalf("stored schema = %v, want id and geometry", stored)
	}
	if stored.Field(1).Type.ID() != arrow.LIST {
		t.Fatalf("stored geometry type = %v, want list", stored.Field(1).Type)
	}
	if got, ok := stored.Field(1).Metadata.GetValue("ARROW:extension:name"); !ok || got != rtreeGeometryExtension {
		t.Fatalf("stored geometry extension = %q, ok=%v; want %q", got, ok, rtreeGeometryExtension)
	}
	return dataset, uri
}

func collectRTreeIDs(t *testing.T, batch arrow.RecordBatch) []int64 {
	t.Helper()
	column := findColumn(t, batch, "id")
	values, ok := column.(*array.Int32)
	if !ok {
		t.Fatalf("id column has type %T, want *array.Int32", column)
	}
	ids := make([]int64, values.Len())
	for i := range ids {
		ids[i] = int64(values.Value(i))
	}
	return ids
}

func scanRTreeIDs(t *testing.T, dataset *lance.Dataset, filter string, useScalarIndex bool) ([]int64, lance.ScanStatistics) {
	t.Helper()
	scanner, err := dataset.Scan(
		lance.WithColumns("id"),
		lance.WithSQLFilter(filter),
		lance.WithUseScalarIndex(useScalarIndex),
	)
	if err != nil {
		t.Fatalf("Scan(%q, useScalarIndex=%v): %v", filter, useScalarIndex, err)
	}
	var ids []int64
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			_ = scanner.Close()
			t.Fatalf("Records(%q, useScalarIndex=%v): %v", filter, useScalarIndex, err)
		}
		ids = append(ids, collectRTreeIDs(t, batch)...)
	}
	stats := scanner.Statistics()
	if err := scanner.Close(); err != nil {
		t.Fatalf("close scanner: %v", err)
	}
	slices.Sort(ids)
	return ids, stats
}

// scanUsedRTree mirrors the native RTree contract test: loading a RTree or
// observing scalar-index execution/cache metrics proves the indexed path ran.
func scanUsedRTree(stats lance.ScanStatistics) bool {
	if stats.IndicesLoaded > 0 {
		return true
	}
	for _, metric := range stats.Metrics {
		switch metric.Name {
		case "ScalarIndexExec_elapsed_compute":
			return true
		case "index_cache_hits", "index_cache_misses":
			if metric.Value > 0 {
				return true
			}
		}
	}
	return false
}

// TestRTreeScalarIndexGeometryQueriesAndLifecycle requires a native library
// that implements LANCE_SCALAR_RTREE. Select it explicitly against such a
// library; a pre-RTree library fails creation instead of falling back to a flat
// scan or skipping this test.
func TestRTreeScalarIndexGeometryQueriesAndLifecycle(t *testing.T) {
	dataset, uri := createRTreeFixture(t)
	ctx := context.Background()

	fragmentCount, err := dataset.FragmentCount()
	if err != nil {
		t.Fatalf("FragmentCount: %v", err)
	}
	if fragmentCount != 3 {
		t.Fatalf("FragmentCount = %d, want 3", fragmentCount)
	}
	sourceVersion, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if count, err := dataset.IndexCount(); err != nil || count != 0 {
		t.Fatalf("IndexCount before create = %d, %v; want 0, nil", count, err)
	}

	if _, err := dataset.CreateScalarIndex(
		ctx,
		"geometry",
		index.ScalarRTree,
		json.RawMessage(`{"page_size":1}`),
		index.ScalarOptions{},
		lance.WithIndexName("bad_rtree_page_size"),
	); !errors.Is(err, lance.ErrInvalidArgument) || !strings.Contains(err.Error(), "page_size must be at least 2") {
		t.Fatalf("invalid RTree page_size = %v, want native ErrInvalidArgument", err)
	}
	if count, err := dataset.IndexCount(); err != nil || count != 0 {
		t.Fatalf("IndexCount after invalid page_size = %d, %v; want 0, nil", count, err)
	}
	if version, err := dataset.Version(); err != nil || version != sourceVersion {
		t.Fatalf("Version after invalid page_size = %d, %v; want %d", version, err, sourceVersion)
	}

	if _, err := dataset.CreateScalarIndex(
		ctx,
		"id",
		index.ScalarRTree,
		nil,
		index.ScalarOptions{},
		lance.WithIndexName("bad_rtree_schema"),
	); !errors.Is(err, lance.ErrIndex) || !strings.Contains(err.Error(), "GeoArrow") {
		t.Fatalf("RTree on non-geometry column = %v, want native ErrIndex", err)
	}
	if count, err := dataset.IndexCount(); err != nil || count != 0 {
		t.Fatalf("IndexCount after schema error = %d, %v; want 0, nil", count, err)
	}

	indexed, err := dataset.CreateScalarIndex(
		ctx,
		"geometry",
		index.ScalarRTree,
		nil,
		index.ScalarOptions{},
		lance.WithIndexName("geometry_rtree"),
	)
	if err != nil {
		t.Fatalf("CreateScalarIndex RTree: %v", err)
	}
	t.Cleanup(func() {
		if err := indexed.Close(); err != nil {
			t.Errorf("close indexed dataset: %v", err)
		}
	})
	if count, err := dataset.IndexCount(); err != nil || count != 0 {
		t.Fatalf("source IndexCount after create = %d, %v; want 0, nil", count, err)
	}
	if count, err := indexed.IndexCount(); err != nil || count != 1 {
		t.Fatalf("indexed IndexCount = %d, %v; want 1, nil", count, err)
	}
	if version, err := indexed.Version(); err != nil || version <= sourceVersion {
		t.Fatalf("indexed Version = %d, %v; want > %d", version, err, sourceVersion)
	}

	indices, err := indexed.ListIndices()
	if err != nil {
		t.Fatalf("ListIndices: %v", err)
	}
	if len(indices) != 1 {
		t.Fatalf("ListIndices length = %d, want 1", len(indices))
	}
	if indices[0].Name != "geometry_rtree" || indices[0].Type != "RTree" {
		t.Fatalf("ListIndices entry = %+v, want geometry_rtree RTree", indices[0])
	}
	if !slices.Equal(indices[0].Columns, []string{"geometry"}) {
		t.Fatalf("RTree columns = %v, want [geometry]", indices[0].Columns)
	}

	descriptions, err := indexed.DescribeIndices()
	if err != nil {
		t.Fatalf("DescribeIndices: %v", err)
	}
	var described *index.Description
	for i := range descriptions {
		if descriptions[i].Name == "geometry_rtree" {
			described = &descriptions[i]
		}
	}
	if described == nil {
		t.Fatal("DescribeIndices did not include geometry_rtree")
	}
	if described.IndexType != "RTree" {
		t.Fatalf("described index type = %q, want RTree", described.IndexType)
	}
	if !strings.HasSuffix(described.TypeURL, "RTreeIndexDetails") {
		t.Fatalf("described type URL = %q, want RTreeIndexDetails suffix", described.TypeURL)
	}
	if !slices.Equal(described.FieldNames, []string{"geometry"}) {
		t.Fatalf("described fields = %v, want [geometry]", described.FieldNames)
	}
	if described.NumRowsIndexed != 15 {
		t.Fatalf("described rows = %d, want 15", described.NumRowsIndexed)
	}
	if len(described.Segments) != 1 {
		t.Fatalf("described segments = %d, want 1", len(described.Segments))
	}

	segments, err := indexed.IndexSegments()
	if err != nil {
		t.Fatalf("IndexSegments: %v", err)
	}
	if len(segments) != 1 || segments[0].Name != "geometry_rtree" || segments[0].Type != "RTree" {
		t.Fatalf("IndexSegments = %+v, want one geometry_rtree RTree segment", segments)
	}
	if !slices.Equal(segments[0].FragmentIDs, []uint32{0, 1, 2}) {
		t.Fatalf("RTree fragment coverage = %v, want [0 1 2]", segments[0].FragmentIDs)
	}

	builder, err := indexed.NewScalarSegmentBuilder(
		ctx,
		"geometry",
		"worker_geometry_rtree",
		index.ScalarRTree,
		json.RawMessage(`{"page_size":4}`),
		index.ScalarOptions{},
		index.SegmentScope{FragmentIDs: []uint32{0}},
	)
	if err != nil {
		t.Fatalf("NewScalarSegmentBuilder: %v", err)
	}
	data, err := builder.Execute(ctx)
	if err != nil {
		_ = builder.Close()
		t.Fatalf("SegmentBuilder.Execute: %v", err)
	}
	if err := builder.Close(); err != nil {
		t.Fatalf("close segment builder: %v", err)
	}
	metadata, err := lance.ParseIndexSegmentMetadata(data)
	if err != nil {
		t.Fatalf("ParseIndexSegmentMetadata: %v", err)
	}
	kind, err := metadata.Type()
	if err != nil {
		_ = metadata.Close()
		t.Fatalf("segment metadata Type: %v", err)
	}
	if kind != index.SegmentIndexType(index.ScalarRTree) {
		_ = metadata.Close()
		t.Fatalf("segment metadata type = %v, want %d", kind, index.ScalarRTree)
	}
	if scalar, ok := kind.Scalar(); !ok || scalar != index.ScalarRTree {
		_ = metadata.Close()
		t.Fatalf("segment metadata scalar type = %d, %v; want %d, true", scalar, ok, index.ScalarRTree)
	}
	if kind.String() != "RTREE" {
		_ = metadata.Close()
		t.Fatalf("segment metadata type name = %q, want RTREE", kind.String())
	}
	typeURL, present, err := metadata.DetailsTypeURL()
	if err != nil {
		_ = metadata.Close()
		t.Fatalf("segment metadata DetailsTypeURL: %v", err)
	}
	if !present || !strings.HasSuffix(typeURL, "RTreeIndexDetails") {
		_ = metadata.Close()
		t.Fatalf("segment metadata type URL = %q, present=%v; want RTreeIndexDetails", typeURL, present)
	}
	if err := metadata.Close(); err != nil {
		t.Fatalf("close segment metadata: %v", err)
	}

	queries := []struct {
		name   string
		filter string
		want   []int64
	}{
		{
			name:   "point_candidates_require_exact_intersection",
			filter: "ST_Intersects(geometry, ST_GeomFromText('POINT (0 10)'))",
			want:   []int64{101, 114},
		},
		{
			name:   "line_candidates_require_exact_intersection",
			filter: "ST_Intersects(geometry, ST_GeomFromText('LINESTRING (25 25, 26 26)'))",
			want:   []int64{106},
		},
		{
			name:   "point_origin",
			filter: "ST_Intersects(geometry, ST_GeomFromText('POINT (0 0)'))",
			want:   []int64{100},
		},
		{
			name:   "null_geometries",
			filter: "geometry IS NULL",
			want:   []int64{103, 108},
		},
	}
	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			indexedIDs, indexedStats := scanRTreeIDs(t, indexed, query.filter, true)
			if !slices.Equal(indexedIDs, query.want) {
				t.Fatalf("indexed ids = %v, want %v", indexedIDs, query.want)
			}
			if !scanUsedRTree(indexedStats) {
				t.Fatalf("indexed scan did not load or execute RTree: %+v", indexedStats)
			}

			flatIDs, flatStats := scanRTreeIDs(t, indexed, query.filter, false)
			if !slices.Equal(flatIDs, query.want) {
				t.Fatalf("flat ids = %v, want %v", flatIDs, query.want)
			}
			if flatStats.IndicesLoaded != 0 {
				t.Fatalf("flat scan loaded %d indices, want 0", flatStats.IndicesLoaded)
			}
		})
	}

	if err := indexed.PrewarmIndex(ctx, "geometry_rtree", index.PrewarmOptions{}); err != nil {
		t.Fatalf("PrewarmIndex: %v", err)
	}
	if err := indexed.Close(); err != nil {
		t.Fatalf("close indexed dataset: %v", err)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open indexed dataset: %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("close reopened dataset: %v", err)
		}
	})
	reopenedIDs, reopenedStats := scanRTreeIDs(
		t,
		reopened,
		"ST_Intersects(geometry, ST_GeomFromText('LINESTRING (25 25, 26 26)'))",
		true,
	)
	if !slices.Equal(reopenedIDs, []int64{106}) {
		t.Fatalf("reopened indexed ids = %v, want [106]", reopenedIDs)
	}
	if !scanUsedRTree(reopenedStats) {
		t.Fatalf("reopened scan did not load or execute RTree: %+v", reopenedStats)
	}

	replaced, err := reopened.CreateScalarIndex(
		ctx,
		"geometry",
		index.ScalarRTree,
		json.RawMessage(`{"page_size":8}`),
		index.ScalarOptions{},
		lance.WithIndexName("geometry_rtree"),
		lance.WithIndexReplace(true),
	)
	if err != nil {
		t.Fatalf("replace RTree: %v", err)
	}
	t.Cleanup(func() {
		if err := replaced.Close(); err != nil {
			t.Errorf("close replaced dataset: %v", err)
		}
	})
	if count, err := replaced.IndexCount(); err != nil || count != 1 {
		t.Fatalf("replaced IndexCount = %d, %v; want 1, nil", count, err)
	}
	replacedIDs, replacedStats := scanRTreeIDs(
		t,
		replaced,
		"ST_Intersects(geometry, ST_GeomFromText('POINT (0 10)'))",
		true,
	)
	if !slices.Equal(replacedIDs, []int64{101, 114}) {
		t.Fatalf("replaced indexed ids = %v, want [101 114]", replacedIDs)
	}
	if !scanUsedRTree(replacedStats) {
		t.Fatalf("replaced scan did not load or execute RTree: %+v", replacedStats)
	}

	dropped, err := replaced.DropIndex(ctx, "geometry_rtree")
	if err != nil {
		t.Fatalf("DropIndex RTree: %v", err)
	}
	t.Cleanup(func() {
		if err := dropped.Close(); err != nil {
			t.Errorf("close dropped dataset: %v", err)
		}
	})
	if count, err := dropped.IndexCount(); err != nil || count != 0 {
		t.Fatalf("dropped IndexCount = %d, %v; want 0, nil", count, err)
	}
	if indices, err := dropped.ListIndices(); err != nil || len(indices) != 0 {
		t.Fatalf("dropped ListIndices = %+v, %v; want empty", indices, err)
	}
	if count, err := replaced.IndexCount(); err != nil || count != 1 {
		t.Fatalf("replaced source IndexCount after drop = %d, %v; want 1, nil", count, err)
	}

	afterDrop, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after drop: %v", err)
	}
	t.Cleanup(func() {
		if err := afterDrop.Close(); err != nil {
			t.Errorf("close after-drop dataset: %v", err)
		}
	})
	if count, err := afterDrop.IndexCount(); err != nil || count != 0 {
		t.Fatalf("after-drop IndexCount = %d, %v; want 0, nil", count, err)
	}
}
