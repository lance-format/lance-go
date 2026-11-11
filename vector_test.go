package lance_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/float16"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
)

func vectorSchema(dimension int, element arrow.DataType) *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "vector", Type: arrow.FixedSizeListOfNonNullable(int32(dimension), element), Nullable: false},
	}, nil)
}

func multiVectorSchema(dimension int) *arrow.Schema {
	inner := arrow.FixedSizeListOfNonNullable(int32(dimension), arrow.PrimitiveTypes.Float32)
	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		{Name: "vectors", Type: arrow.ListOfNonNullable(inner), Nullable: false},
	}, nil)
}

func createVectorDataset(t *testing.T, schema *arrow.Schema, records ...arrow.RecordBatch) *lance.Dataset {
	t.Helper()
	dataset, err := lance.Create(
		context.Background(),
		filepath.Join(t.TempDir(), "data.lance"),
		makeReader(t, schema, records...),
		lance.WithWriteParams(lance.WriteParams{EnableStableRowIDs: true}),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if err := dataset.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return dataset
}

func makeVectorRecord(t *testing.T, schema *arrow.Schema, ids []int64, values [][]float64, dataType lance.VectorDataType) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)
	builder.Field(0).(*array.Int64Builder).AppendValues(ids, nil)
	list := builder.Field(1).(*array.FixedSizeListBuilder)
	switch dataType {
	case lance.VectorDataTypeFloat32:
		valuesBuilder := list.ValueBuilder().(*array.Float32Builder)
		for _, vector := range values {
			list.Append(true)
			valuesBuilder.AppendValues(float32Values(vector), nil)
		}
	case lance.VectorDataTypeFloat16:
		valuesBuilder := list.ValueBuilder().(*array.Float16Builder)
		for _, vector := range values {
			list.Append(true)
			raw := make([]float16.Num, len(vector))
			for i, value := range vector {
				raw[i] = float16.FromBits(float16Bits(value))
			}
			valuesBuilder.AppendValues(raw, nil)
		}
	case lance.VectorDataTypeFloat64:
		valuesBuilder := list.ValueBuilder().(*array.Float64Builder)
		for _, vector := range values {
			list.Append(true)
			valuesBuilder.AppendValues(vector, nil)
		}
	case lance.VectorDataTypeUInt8:
		valuesBuilder := list.ValueBuilder().(*array.Uint8Builder)
		for _, vector := range values {
			list.Append(true)
			raw := make([]uint8, len(vector))
			for i, value := range vector {
				raw[i] = uint8(value)
			}
			valuesBuilder.AppendValues(raw, nil)
		}
	case lance.VectorDataTypeInt8:
		valuesBuilder := list.ValueBuilder().(*array.Int8Builder)
		for _, vector := range values {
			list.Append(true)
			raw := make([]int8, len(vector))
			for i, value := range vector {
				raw[i] = int8(value)
			}
			valuesBuilder.AppendValues(raw, nil)
		}
	default:
		t.Fatalf("unsupported test vector data type %d", dataType)
	}
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return record
}

func makeMultiVectorRecord(t *testing.T, schema *arrow.Schema, ids []int64, vectors [][][]float64) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)
	builder.Field(0).(*array.Int64Builder).AppendValues(ids, nil)
	list := builder.Field(1).(*array.ListBuilder)
	fixed := list.ValueBuilder().(*array.FixedSizeListBuilder)
	values := fixed.ValueBuilder().(*array.Float32Builder)
	for _, row := range vectors {
		list.Append(true)
		for _, subvector := range row {
			fixed.Append(true)
			values.AppendValues(float32Values(subvector), nil)
		}
	}
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return record
}

func float32Values(values []float64) []float32 {
	result := make([]float32, len(values))
	for i, value := range values {
		result[i] = float32(value)
	}
	return result
}

func float16Bits(value float64) uint16 {
	bits := math.Float32bits(float32(value))
	sign := uint16(bits>>16) & 0x8000
	if value == 0 {
		return sign
	}
	exponent := int(bits>>23&0xff) - 127 + 15
	fraction := uint16(bits>>13) & 0x03ff
	return sign | uint16(exponent)<<10 | fraction
}

type nearestRow struct {
	id       int64
	distance float32
}

func collectNearest(t *testing.T, scan *lance.Scanner) []nearestRow {
	t.Helper()
	var result []nearestRow
	for batch, err := range scan.Records(context.Background()) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids := collectInt64(t, batch, "id")
		distances := collectFloat32(t, batch, "_distance")
		for i := range ids {
			result = append(result, nearestRow{id: ids[i], distance: distances[i]})
		}
	}
	return result
}

func collectFloat32(t *testing.T, batch arrow.RecordBatch, field string) []float32 {
	t.Helper()
	column := findColumn(t, batch, field)
	values, ok := column.(*array.Float32)
	if !ok {
		t.Fatalf("column %q has type %T, want *array.Float32", field, column)
	}
	result := make([]float32, values.Len())
	for i := range result {
		result[i] = values.Value(i)
	}
	return result
}

func collectInt32(t *testing.T, batch arrow.RecordBatch, field string) []int32 {
	t.Helper()
	column := findColumn(t, batch, field)
	values, ok := column.(*array.Int32)
	if !ok {
		t.Fatalf("column %q has type %T, want *array.Int32", field, column)
	}
	result := make([]int32, values.Len())
	for i := range result {
		result[i] = values.Value(i)
	}
	return result
}

func collectUint64(t *testing.T, batch arrow.RecordBatch, field string) []uint64 {
	t.Helper()
	column := findColumn(t, batch, field)
	values, ok := column.(*array.Uint64)
	if !ok {
		t.Fatalf("column %q has type %T, want *array.Uint64", field, column)
	}
	result := make([]uint64, values.Len())
	for i := range result {
		result[i] = values.Value(i)
	}
	return result
}

func requireNearestIDs(t *testing.T, rows []nearestRow, want []int64) {
	t.Helper()
	got := make([]int64, len(rows))
	for i, row := range rows {
		got[i] = row.id
	}
	if !slices.Equal(got, want) {
		t.Fatalf("nearest IDs = %v, want %v", got, want)
	}
}

func requireNearestDistances(t *testing.T, rows []nearestRow, want []float32) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("nearest row count = %d, want %d", len(rows), len(want))
	}
	for i, row := range rows {
		if math.Abs(float64(row.distance-want[i])) > 1e-5 {
			t.Fatalf("distance %d = %v, want %v", i, row.distance, want[i])
		}
	}
}

func TestNearestTypedVectors(t *testing.T) {
	requireNative(t)
	tests := []struct {
		name    string
		data    lance.VectorDataType
		element arrow.DataType
		query   lance.Vector
		metric  *lance.VectorMetric
		values  [][]float64
		wantIDs []int64
		want    []float32
	}{
		{
			name:    "float32",
			data:    lance.VectorDataTypeFloat32,
			element: arrow.PrimitiveTypes.Float32,
			query:   lance.NewFloat32Vector([]float32{1, 2}),
			values:  [][]float64{{0, 0}, {1, 2}, {3, 6}},
			wantIDs: []int64{2, 1, 3},
			want:    []float32{0, 5, 20},
		},
		{
			name:    "float64",
			data:    lance.VectorDataTypeFloat64,
			element: arrow.PrimitiveTypes.Float64,
			query:   lance.NewFloat64Vector([]float64{1, 2}),
			values:  [][]float64{{0, 0}, {1, 2}, {3, 6}},
			wantIDs: []int64{2, 1, 3},
			want:    []float32{0, 5, 20},
		},
		{
			name:    "float16",
			data:    lance.VectorDataTypeFloat16,
			element: arrow.FixedWidthTypes.Float16,
			query:   lance.NewFloat16Vector([]uint16{float16Bits(1), float16Bits(2)}),
			values:  [][]float64{{0, 0}, {1, 2}, {3, 6}},
			wantIDs: []int64{2, 1, 3},
			want:    []float32{0, 5, 20},
		},
		{
			name:    "int8",
			data:    lance.VectorDataTypeInt8,
			element: arrow.PrimitiveTypes.Int8,
			query:   lance.NewInt8Vector([]int8{1, 2}),
			values:  [][]float64{{0, 0}, {1, 2}, {3, 6}},
			wantIDs: []int64{2, 1, 3},
			want:    []float32{0, 5, 20},
		},
		{
			name:    "uint8_hamming",
			data:    lance.VectorDataTypeUInt8,
			element: arrow.PrimitiveTypes.Uint8,
			query:   lance.NewUInt8Vector([]uint8{0, 255}),
			metric:  metricPtr(lance.VectorMetricHamming),
			values:  [][]float64{{0, 255}, {255, 0}, {0, 0}},
			wantIDs: []int64{1, 3, 2},
			want:    []float32{0, 8, 16},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema := vectorSchema(2, test.element)
			record := makeVectorRecord(t, schema, []int64{1, 2, 3}, test.values, test.data)
			dataset := createVectorDataset(t, schema, record)
			options := []lance.ScanOption{lance.WithNearest("vector", test.query, 3)}
			if test.metric != nil {
				options = append(options, lance.WithVectorMetric(*test.metric))
			}
			scan, err := dataset.Scan(options...)
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			rows := collectNearest(t, scan)
			requireNearestIDs(t, rows, test.wantIDs)
			requireNearestDistances(t, rows, test.want)
		})
	}
}

func metricPtr(metric lance.VectorMetric) *lance.VectorMetric {
	return &metric
}

func TestNearestMetrics(t *testing.T) {
	requireNative(t)
	schema := vectorSchema(2, arrow.PrimitiveTypes.Float32)
	record := makeVectorRecord(t, schema, []int64{1, 2, 3, 4}, [][]float64{
		{1, 0},
		{0, 1},
		{-1, 0},
		{0, -1},
	}, lance.VectorDataTypeFloat32)
	dataset := createVectorDataset(t, schema, record)
	tests := []struct {
		metric lance.VectorMetric
		want   map[int64]float32
	}{
		{lance.VectorMetricL2, map[int64]float32{1: 0, 2: 2, 3: 4, 4: 2}},
		{lance.VectorMetricCosine, map[int64]float32{1: 0, 2: 1, 3: 2, 4: 1}},
		{lance.VectorMetricDot, map[int64]float32{1: 0, 2: 1, 3: 2, 4: 1}},
	}
	for _, test := range tests {
		t.Run(vectorMetricName(test.metric), func(t *testing.T) {
			scan, err := dataset.Scan(
				lance.WithNearest("vector", lance.NewFloat32Vector([]float32{1, 0}), 4),
				lance.WithVectorMetric(test.metric),
			)
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			rows := collectNearest(t, scan)
			if len(rows) != len(test.want) {
				t.Fatalf("row count = %d, want %d", len(rows), len(test.want))
			}
			for _, row := range rows {
				want, ok := test.want[row.id]
				if !ok {
					t.Fatalf("unexpected id %d", row.id)
				}
				if math.Abs(float64(row.distance-want)) > 1e-5 {
					t.Fatalf("id %d distance = %v, want %v", row.id, row.distance, want)
				}
			}
		})
	}
}

func vectorMetricName(m lance.VectorMetric) string {
	switch m {
	case lance.VectorMetricL2:
		return "l2"
	case lance.VectorMetricCosine:
		return "cosine"
	case lance.VectorMetricDot:
		return "dot"
	default:
		return "unknown"
	}
}

func TestNearestPrefilter(t *testing.T) {
	requireNative(t)
	schema := vectorSchema(2, arrow.PrimitiveTypes.Float32)
	record := makeVectorRecord(t, schema, []int64{1, 2, 3}, [][]float64{
		{0, 0},
		{1, 0},
		{7, 0},
	}, lance.VectorDataTypeFloat32)
	dataset := createVectorDataset(t, schema, record)

	scan, err := dataset.Scan(
		lance.WithSQLFilter("id >= 2"),
		lance.WithNearest("vector", lance.NewFloat32Vector([]float32{0, 0}), 1),
		lance.WithVectorPrefilter(true),
	)
	if err != nil {
		t.Fatalf("Scan prefilter: %v", err)
	}
	requireNearestIDs(t, collectNearest(t, scan), []int64{2})

	scan, err = dataset.Scan(
		lance.WithSQLFilter("id >= 2"),
		lance.WithNearest("vector", lance.NewFloat32Vector([]float32{0, 0}), 1),
		lance.WithVectorPrefilter(false),
	)
	if err != nil {
		t.Fatalf("Scan postfilter: %v", err)
	}
	if rows := collectNearest(t, scan); len(rows) != 0 {
		t.Fatalf("post-filtered nearest rows = %v, want none", rows)
	}
}

func TestNearestRowIDMasks(t *testing.T) {
	requireNative(t)
	schema := vectorSchema(1, arrow.PrimitiveTypes.Float32)
	record := makeVectorRecord(t, schema, []int64{1, 2, 3, 4, 5}, [][]float64{{0}, {1}, {2}, {3}, {4}}, lance.VectorDataTypeFloat32)
	dataset := createVectorDataset(t, schema, record)

	scan, err := dataset.Scan(lance.WithRowID(true))
	if err != nil {
		t.Fatalf("Scan row IDs: %v", err)
	}
	rowIDs := make(map[int64]uint64)
	for batch, err := range scan.Records(context.Background()) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids := collectInt64(t, batch, "id")
		values := collectUint64(t, batch, "_rowid")
		for i := range ids {
			rowIDs[ids[i]] = values[i]
		}
	}

	nearest := func(options ...lance.ScanOption) []nearestRow {
		t.Helper()
		options = append(options, lance.WithNearest("vector", lance.NewFloat32Vector([]float32{0}), 5))
		scan, err := dataset.Scan(options...)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		return collectNearest(t, scan)
	}

	requireNearestIDs(t, nearest(lance.WithRowIDAllowlist(rowIDs[1], rowIDs[3])), []int64{1, 3})
	if rows := nearest(lance.WithRowIDAllowlist()); len(rows) != 0 {
		t.Fatalf("empty allowlist rows = %v, want none", rows)
	}
	wantBlocked := []int64{2, 4, 5}
	requireNearestIDs(t, nearest(lance.WithRowIDBlocklist(rowIDs[1], rowIDs[3])), wantBlocked)
	requireNearestIDs(t, nearest(
		lance.WithRowIDAllowlist(rowIDs[1], rowIDs[2], rowIDs[3]),
		lance.WithRowIDBlocklist(rowIDs[2]),
	), []int64{1, 3})
}

func TestNearestDistanceRange(t *testing.T) {
	requireNative(t)
	schema := vectorSchema(1, arrow.PrimitiveTypes.Float32)
	record := makeVectorRecord(t, schema, []int64{1, 2, 3, 4}, [][]float64{{0}, {1}, {2}, {3}}, lance.VectorDataTypeFloat32)
	dataset := createVectorDataset(t, schema, record)
	lower := float32(1)
	upper := float32(4)
	scan, err := dataset.Scan(
		lance.WithVectorDistanceRange(&lower, &upper),
		lance.WithNearest("vector", lance.NewFloat32Vector([]float32{0}), 4),
	)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	rows := collectNearest(t, scan)
	requireNearestIDs(t, rows, []int64{2})
	requireNearestDistances(t, rows, []float32{1})
}

func TestNearestBatch(t *testing.T) {
	requireNative(t)
	schema := vectorSchema(2, arrow.PrimitiveTypes.Float32)
	record := makeVectorRecord(t, schema, []int64{1, 2, 3, 4, 5}, [][]float64{
		{0, 0},
		{1, 0},
		{2, 0},
		{10, 0},
		{11, 0},
	}, lance.VectorDataTypeFloat32)
	dataset := createVectorDataset(t, schema, record)
	queries := lance.NewFloat32Vector([]float32{
		0, 0,
		2, 0,
		100, 0,
	})
	scan, err := dataset.Scan(lance.WithNearestBatch("vector", queries, 2, 2))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	groups := make(map[int32][]nearestRow)
	for batch, err := range scan.Records(context.Background()) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids := collectInt64(t, batch, "id")
		distances := collectFloat32(t, batch, "_distance")
		queryIndexes := collectInt32(t, batch, "query_index")
		for i := range ids {
			groups[queryIndexes[i]] = append(groups[queryIndexes[i]], nearestRow{id: ids[i], distance: distances[i]})
		}
	}
	want := map[int32][]nearestRow{
		0: {{id: 1, distance: 0}, {id: 2, distance: 1}},
		1: {{id: 3, distance: 0}, {id: 2, distance: 1}},
		2: {{id: 5, distance: 0}, {id: 4, distance: 1}},
	}
	if len(groups) != len(want) {
		t.Fatalf("query groups = %v, want %v", groups, want)
	}
	for query, wantRows := range want {
		rows := groups[query]
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].distance < rows[j].distance
		})
		if !slices.Equal(rows, wantRows) {
			t.Fatalf("query %d rows = %v, want %v", query, rows, wantRows)
		}
	}

	if _, err := dataset.Scan(
		lance.WithLimit(1),
		lance.WithNearestBatch("vector", queries, 2, 1),
	); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("batch with limit error = %v, want ErrInvalidArgument", err)
	}
}

func TestNearestMultiVector(t *testing.T) {
	requireNative(t)
	schema := multiVectorSchema(2)
	record := makeMultiVectorRecord(t, schema, []int64{1, 2, 3}, [][][]float64{
		{{0, 0}, {100, 0}},
		{{2, 0}, {4, 0}},
		{{10, 0}, {20, 0}},
	})
	dataset := createVectorDataset(t, schema, record)
	scan, err := dataset.Scan(lance.WithNearestMultiVector(
		"vectors",
		lance.NewFloat32Vector([]float32{0, 0, 1, 0}),
		2,
		3,
	))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	rows := collectNearest(t, scan)
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].distance < rows[j].distance
	})
	requireNearestIDs(t, rows, []int64{1, 2, 3})
	requireNearestDistances(t, rows, []float32{1, 5, 181})
}

func TestNearestSettingsAccepted(t *testing.T) {
	requireNative(t)
	schema := vectorSchema(2, arrow.PrimitiveTypes.Float32)
	record := makeVectorRecord(t, schema, []int64{1, 2, 3}, [][]float64{{0, 0}, {1, 0}, {2, 0}}, lance.VectorDataTypeFloat32)
	dataset := createVectorDataset(t, schema, record)
	scan, err := dataset.Scan(
		lance.WithNearest("vector", lance.NewFloat32Vector([]float32{0, 0}), 1),
		lance.WithVectorMetric(lance.VectorMetricL2),
		lance.WithVectorNProbes(2),
		lance.WithVectorEF(64),
		lance.WithVectorRefineFactor(1),
		lance.WithVectorSearchEffort(0.5),
		lance.WithVectorApproxMode(lance.ApproxModeAccurate),
		lance.WithVectorQueryParallelism(1),
		lance.WithVectorUseIndex(false),
		lance.WithVectorFastSearch(false),
		lance.WithVectorPrefilter(true),
	)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	rows := collectNearest(t, scan)
	requireNearestIDs(t, rows, []int64{1})
	requireNearestDistances(t, rows, []float32{0})
}

func TestNearestFastSearchWithoutIndex(t *testing.T) {
	requireNative(t)
	schema := vectorSchema(2, arrow.PrimitiveTypes.Float32)
	record := makeVectorRecord(t, schema, []int64{1, 2}, [][]float64{{0, 0}, {1, 0}}, lance.VectorDataTypeFloat32)
	dataset := createVectorDataset(t, schema, record)
	scan, err := dataset.Scan(
		lance.WithNearest("vector", lance.NewFloat32Vector([]float32{0, 0}), 1),
		lance.WithVectorFastSearch(true),
	)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if rows := collectNearest(t, scan); len(rows) != 0 {
		t.Fatalf("fast search without an index returned %v, want none", rows)
	}
}

func TestNearestArgumentValidation(t *testing.T) {
	requireNative(t)
	schema := vectorSchema(2, arrow.PrimitiveTypes.Float32)
	record := makeVectorRecord(t, schema, []int64{1, 2}, [][]float64{{0, 0}, {1, 0}}, lance.VectorDataTypeFloat32)
	dataset := createVectorDataset(t, schema, record)
	query := lance.NewFloat32Vector([]float32{0, 0})
	tooHigh := float32(4)
	tooLow := float32(4)
	nan := float32(math.NaN())
	tests := []struct {
		name    string
		options []lance.ScanOption
	}{
		{"empty column", []lance.ScanOption{lance.WithNearest("", query, 1)}},
		{"zero k", []lance.ScanOption{lance.WithNearest("vector", query, 0)}},
		{"empty vector", []lance.ScanOption{lance.WithNearest("vector", lance.NewFloat32Vector(nil), 1)}},
		{"batch not multiple", []lance.ScanOption{lance.WithNearestBatch("vector", lance.NewFloat32Vector([]float32{0, 0, 1}), 2, 1)}},
		{"batch too many queries", []lance.ScanOption{lance.WithNearestBatch("vector", lance.NewFloat32Vector(make([]float32, 129)), 1, 1)}},
		{"batch candidates", []lance.ScanOption{lance.WithNearestBatch("vector", query, 2, 100001)}},
		{"batch refine candidates", []lance.ScanOption{
			lance.WithNearestBatch("vector", lance.NewFloat32Vector([]float32{0, 0, 1, 0}), 2, 50000),
			lance.WithVectorRefineFactor(2),
		}},
		{"multi too many candidates", []lance.ScanOption{lance.WithNearestMultiVector("vector", lance.NewFloat32Vector([]float32{0, 0, 1, 0}), 2, 50001)}},
		{"multi refine candidates", []lance.ScanOption{
			lance.WithNearestMultiVector("vector", lance.NewFloat32Vector([]float32{0, 0}), 2, 50000),
			lance.WithVectorRefineFactor(3),
		}},
		{"multi int8", []lance.ScanOption{lance.WithNearestMultiVector("vector", lance.NewInt8Vector([]int8{0, 0}), 2, 1)}},
		{"range reversed", []lance.ScanOption{lance.WithVectorDistanceRange(&tooHigh, &tooLow), lance.WithNearest("vector", query, 1)}},
		{"range NaN", []lance.ScanOption{lance.WithVectorDistanceRange(&nan, nil), lance.WithNearest("vector", query, 1)}},
		{"range without nearest", []lance.ScanOption{lance.WithVectorDistanceRange(&tooLow, nil)}},
		{"range with batch", []lance.ScanOption{
			lance.WithVectorDistanceRange(&tooLow, nil),
			lance.WithNearestBatch("vector", lance.NewFloat32Vector([]float32{0, 0}), 2, 1),
		}},
		{"zero nprobes", []lance.ScanOption{lance.WithVectorNProbes(0)}},
		{"min greater than max", []lance.ScanOption{lance.WithVectorMinNProbes(5), lance.WithVectorMaxNProbes(3)}},
		{"fixed and minimum nprobes", []lance.ScanOption{lance.WithVectorNProbes(2), lance.WithVectorMinNProbes(1)}},
		{"search effort range", []lance.ScanOption{lance.WithVectorSearchEffort(1.5)}},
		{"search effort NaN", []lance.ScanOption{lance.WithVectorSearchEffort(math.NaN())}},
		{"metric enum", []lance.ScanOption{lance.WithVectorMetric(lance.VectorMetric(99))}},
		{"approx mode enum", []lance.ScanOption{lance.WithVectorApproxMode(lance.ApproxMode(99))}},
		{"query parallelism", []lance.ScanOption{lance.WithVectorQueryParallelism(-2)}},
		{"zero refine factor", []lance.ScanOption{lance.WithVectorRefineFactor(0)}},
		{"zero minimum nprobes", []lance.ScanOption{lance.WithVectorMinNProbes(0)}},
		{"zero maximum nprobes", []lance.ScanOption{lance.WithVectorMaxNProbes(0)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := dataset.Scan(test.options...); !errors.Is(err, lance.ErrInvalidArgument) {
				t.Fatalf("Scan error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestNearestInvalidArgumentTypeMismatch(t *testing.T) {
	requireNative(t)
	schema := vectorSchema(2, arrow.PrimitiveTypes.Float64)
	record := makeVectorRecord(t, schema, []int64{1}, [][]float64{{0, 0}}, lance.VectorDataTypeFloat64)
	dataset := createVectorDataset(t, schema, record)
	scan, err := dataset.Scan(
		lance.WithNearest("vector", lance.NewFloat32Vector([]float32{0, 0}), 1),
	)
	if err != nil {
		if !errors.Is(err, lance.ErrInvalidArgument) {
			t.Fatalf("Scan error = %v, want ErrInvalidArgument", err)
		}
		return
	}
	var scanErr error
	for _, err := range scan.Records(context.Background()) {
		scanErr = err
		break
	}
	if !errors.Is(scanErr, lance.ErrInvalidArgument) {
		t.Fatalf("Records error = %v, want ErrInvalidArgument", scanErr)
	}
}
