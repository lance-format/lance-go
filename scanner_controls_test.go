package lance_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
)

func TestScannerExecutionControls(t *testing.T) {
	requireNative(t)
	dataset, fragments, _ := newScannerControlDataset(t)

	ids := scanIDs(t, dataset,
		lance.WithIOBufferSize(1<<20),
		lance.WithBatchReadahead(2),
		lance.WithFragmentReadahead(1),
		lance.WithTargetParallelism(2),
		lance.WithScanInOrder(false),
		lance.WithUseStats(false),
	)
	if !equalIDs(ids, 1, 2, 3, 4, 5, 6, 7) {
		t.Fatalf("control scan ids = %v, want 1..7", ids)
	}

	orderedScanner, err := dataset.Scan(lance.WithBatchSize(1))
	if err != nil {
		t.Fatalf("Scan ordered: %v", err)
	}
	var ordered []int64
	for batch, err := range orderedScanner.Records(context.Background()) {
		if err != nil {
			orderedScanner.Close()
			t.Fatalf("ordered records: %v", err)
		}
		ordered = append(ordered, collectInt64(t, batch, "id")...)
	}
	if err := orderedScanner.Close(); err != nil {
		t.Fatalf("Close ordered scanner: %v", err)
	}
	if !slices.Equal(ordered, []int64{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("ordered scan ids = %v, want 1..7", ordered)
	}

	ids = scanIDs(t, dataset, lance.WithFragmentIDs(fragments[1]))
	if !equalIDs(ids, 3, 4) {
		t.Fatalf("fragment ids = %v, want [3 4]", ids)
	}
	ids = scanIDs(t, dataset, lance.WithFragmentIDs())
	if len(ids) != 0 {
		t.Fatalf("empty fragment selection returned %v", ids)
	}
	// Unknown fragment IDs select no fragments; native filtering does not
	// reject IDs that are absent from the dataset snapshot.
	ids = scanIDs(t, dataset, lance.WithFragmentIDs(fragments[0], 999))
	if !equalIDs(ids, 1, 2) {
		t.Fatalf("fragment selection with unknown id = %v, want [1 2]", ids)
	}

	fragment, err := dataset.OpenFragment(fragments[0])
	if err != nil {
		t.Fatalf("OpenFragment: %v", err)
	}
	defer fragment.Close()
	scanner, err := fragment.Scan(lance.WithFragmentIDs(fragments[2]))
	if err != nil {
		t.Fatalf("Fragment.Scan with fragment ids: %v", err)
	}
	ids = scanScannerIDs(t, scanner)
	if !equalIDs(ids, 5, 6, 7) {
		t.Fatalf("fragment scan replaced ids = %v, want [5 6 7]", ids)
	}

	input := []uint64{fragments[0]}
	fragmentOption := lance.WithFragmentIDs(input...)
	input[0] = fragments[2]
	ids = scanIDs(t, dataset, fragmentOption)
	if !equalIDs(ids, 1, 2) {
		t.Fatalf("fragment ids were not copied: got %v, want [1 2]", ids)
	}
}

func TestScannerStrictBatchSize(t *testing.T) {
	requireNative(t)
	dataset := newSingleFragmentDataset(t, []int64{1, 2, 3, 4, 5, 6, 7})

	for _, opts := range [][]lance.ScanOption{
		{lance.WithStrictBatchSize(true), lance.WithBatchSizeBytes(1 << 20)},
		{lance.WithBatchSizeBytes(1 << 20), lance.WithStrictBatchSize(true)},
	} {
		if scanner, err := dataset.Scan(opts...); err == nil {
			scanner.Close()
			t.Fatalf("Scan(%v) succeeded, want strict batch conflict", opts)
		}
	}

	scanner, err := dataset.Scan(lance.WithBatchSize(3), lance.WithStrictBatchSize(true))
	if err != nil {
		t.Fatalf("Scan with strict batch size: %v", err)
	}
	defer scanner.Close()
	var lengths []int64
	var ids []int64
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("strict batch records: %v", err)
		}
		lengths = append(lengths, batch.NumRows())
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	if !slices.Equal(lengths, []int64{3, 3, 1}) {
		t.Fatalf("strict batch lengths = %v, want [3 3 1]", lengths)
	}
	if !equalIDs(ids, 1, 2, 3, 4, 5, 6, 7) {
		t.Fatalf("strict batch ids = %v, want 1..7", ids)
	}
}

func TestScannerIncludeDeletedRows(t *testing.T) {
	requireNative(t)
	dataset, _ := newBlobDataset(t, blobTestValues())

	ids, deleted := scanIDsAndDeleted(t, dataset, lance.WithIncludeDeletedRows(true))
	if len(ids) != 6 {
		t.Fatalf("include deleted ids = %v, want six rows", ids)
	}
	deletedByID := make(map[int64]bool, len(ids))
	for i, id := range ids {
		deletedByID[id] = deleted[i]
	}
	for id := int64(0); id <= 5; id++ {
		got, ok := deletedByID[id]
		if !ok {
			t.Fatalf("include deleted missing id %d in %v", id, ids)
		}
		if want := id == 5; got != want {
			t.Fatalf("id %d rowid deleted = %v, want %v", id, got, want)
		}
	}

	if _, err := dataset.Scan(
		lance.WithColumns("id"),
		lance.WithIncludeDeletedRows(true),
	); err == nil {
		t.Fatal("include deleted without row IDs succeeded")
	}

	// This fixture has no scalar index, so enabling index use still selects
	// a flat scan. Both settings must preserve tombstoned rows.
	for _, useIndex := range []bool{true, false} {
		ids, deleted = scanIDsAndDeleted(t, dataset,
			lance.WithIncludeDeletedRows(true),
			lance.WithSQLFilter("id >= 0"),
			lance.WithUseScalarIndex(useIndex),
		)
		if len(ids) != 6 {
			t.Fatalf("filtered include deleted useIndex=%v ids = %v, want six rows", useIndex, ids)
		}
		deletedByID = make(map[int64]bool, len(ids))
		for i, id := range ids {
			deletedByID[id] = deleted[i]
		}
		for id := int64(0); id <= 5; id++ {
			got, ok := deletedByID[id]
			if want := id == 5; !ok || got != want {
				t.Fatalf("filtered id %d present=%v deleted=%v, want present with deleted=%v", id, ok, got, want)
			}
		}
	}
}

func TestScannerBlobHandling(t *testing.T) {
	requireNative(t)
	dataset, _ := newBlobDataset(t, blobTestValues())
	want := blobTestValues()

	scanner, err := dataset.Scan(lance.WithBlobHandling(lance.BlobHandlingAllBinary))
	if err != nil {
		t.Fatalf("Scan all binary: %v", err)
	}
	got := make(map[int64]blobTestValue)
	var retained arrow.RecordBatch
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			scanner.Close()
			t.Fatalf("all binary records: %v", err)
		}
		ids := collectInt64(t, batch, "id")
		blob, ok := findColumn(t, batch, "blob").(*array.LargeBinary)
		if !ok {
			scanner.Close()
			t.Fatalf("all binary blob type = %T, want *array.LargeBinary", findColumn(t, batch, "blob"))
		}
		if retained == nil && slices.Contains(ids, 0) {
			retained = batch
			retained.Retain()
		}
		for i, id := range ids {
			if blob.IsNull(i) {
				got[id] = blobTestValue{Null: true}
				continue
			}
			got[id] = blobTestValue{Data: append([]byte(nil), blob.Value(i)...)}
		}
	}
	if err := scanner.Close(); err != nil {
		t.Fatalf("Close all binary scanner: %v", err)
	}
	if retained == nil {
		t.Fatal("all binary scan did not return id 0")
	}
	defer retained.Release()
	retainedBlob := findColumn(t, retained, "blob").(*array.LargeBinary)
	for i, id := range collectInt64(t, retained, "id") {
		if id != 0 {
			continue
		}
		if retainedBlob.IsNull(i) || !bytes.Equal(retainedBlob.Value(i), want[0].Data) {
			t.Fatalf("retained all binary id 0 blob = %v, null=%v, want %v", retainedBlob.Value(i), retainedBlob.IsNull(i), want[0].Data)
		}
	}
	for id, expected := range want {
		actual, ok := got[int64(id)]
		if !ok {
			t.Fatalf("all binary missing id %d", id)
		}
		if actual.Null != expected.Null {
			t.Fatalf("id %d null = %v, want %v", id, actual.Null, expected.Null)
		}
		if !actual.Null && !bytes.Equal(actual.Data, expected.Data) {
			t.Fatalf("id %d blob = %v, want %v", id, actual.Data, expected.Data)
		}
	}

	for _, handling := range []lance.BlobHandling{
		lance.BlobHandlingDescriptors,
		lance.BlobHandlingAllDescriptions,
	} {
		scanner, err := dataset.Scan(
			lance.WithColumns("id", "blob"),
			lance.WithBlobHandling(handling),
		)
		if err != nil {
			t.Fatalf("Scan blob handling %d: %v", handling, err)
		}
		seen := false
		for batch, err := range scanner.Records(context.Background()) {
			if err != nil {
				scanner.Close()
				t.Fatalf("blob handling %d records: %v", handling, err)
			}
			seen = true
			indices := batch.Schema().FieldIndices("blob")
			if len(indices) != 1 {
				scanner.Close()
				t.Fatalf("blob handling %d schema = %v, want one blob field", handling, batch.Schema())
			}
			field := batch.Schema().Field(indices[0])
			structType, ok := field.Type.(*arrow.StructType)
			if !ok {
				scanner.Close()
				t.Fatalf("blob handling %d field type = %T, want struct", handling, field.Type)
			}
			wantChildren := []string{"kind", "position", "size", "blob_id", "blob_uri"}
			if structType.NumFields() != len(wantChildren) {
				scanner.Close()
				t.Fatalf("blob handling %d has %d children, want %d", handling, structType.NumFields(), len(wantChildren))
			}
			for i, name := range wantChildren {
				if got := structType.Field(i).Name; got != name {
					scanner.Close()
					t.Fatalf("blob handling %d child %d = %q, want %q", handling, i, got, name)
				}
			}
			break
		}
		if err := scanner.Close(); err != nil {
			t.Fatalf("Close blob handling %d scanner: %v", handling, err)
		}
		if !seen {
			t.Fatalf("blob handling %d returned no rows", handling)
		}
	}
}

func TestScannerMaterializationStyle(t *testing.T) {
	requireNative(t)
	dataset, fragments, _ := newScannerControlDataset(t)

	for _, style := range []lance.MaterializationStyle{
		lance.MaterializationHeuristic,
		lance.MaterializationAllLate,
		lance.MaterializationAllEarly,
	} {
		ids := scanIDs(t, dataset,
			lance.WithFragmentIDs(fragments[2]),
			lance.WithMaterializationStyle(style),
		)
		if !equalIDs(ids, 5, 6, 7) {
			t.Fatalf("materialization style %d ids = %v, want [5 6 7]", style, ids)
		}
	}

	ids := scanIDs(t, dataset,
		lance.WithFragmentIDs(fragments[2]),
		lance.WithMaterializationStyle(lance.MaterializationAllEarlyExcept, "id"),
	)
	if !equalIDs(ids, 5, 6, 7) {
		t.Fatalf("materialization early except ids = %v, want [5 6 7]", ids)
	}
	ids = scanIDs(t, dataset,
		lance.WithFragmentIDs(fragments[2]),
		lance.WithMaterializationStyle(lance.MaterializationAllEarlyExcept),
	)
	if !equalIDs(ids, 5, 6, 7) {
		t.Fatalf("materialization empty early except ids = %v, want [5 6 7]", ids)
	}

	for _, option := range []lance.ScanOption{
		lance.WithMaterializationStyle(lance.MaterializationAllEarly, "id"),
		lance.WithMaterializationStyle(lance.MaterializationAllEarlyExcept, "missing"),
		lance.WithMaterializationStyle(lance.MaterializationAllEarlyExcept, "id\x00"),
		lance.WithMaterializationStyle(lance.MaterializationStyle(99)),
	} {
		if scanner, err := dataset.Scan(option); err == nil {
			scanner.Close()
			t.Fatalf("invalid materialization option succeeded")
		}
	}
}

func TestScannerScoringAutoprojection(t *testing.T) {
	requireNative(t)

	vectorDataset := createVectorDataset(t,
		vectorSchema(2, arrow.PrimitiveTypes.Float32),
		makeVectorRecord(t,
			vectorSchema(2, arrow.PrimitiveTypes.Float32),
			[]int64{1, 2, 3},
			[][]float64{{1, 0}, {0, 1}, {2, 0}},
			lance.VectorDataTypeFloat32,
		),
	)
	query := lance.NewFloat32Vector([]float32{1, 0})
	scanner, err := vectorDataset.Scan(
		lance.WithColumns("id"),
		lance.WithNearest("vector", query, 2),
	)
	if err != nil {
		t.Fatalf("vector Scan: %v", err)
	}
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			scanner.Close()
			t.Fatalf("vector records: %v", err)
		}
		if !batch.Schema().HasField("_distance") {
			scanner.Close()
			t.Fatalf("vector scan schema = %v, want _distance", batch.Schema())
		}
		break
	}
	if err := scanner.Close(); err != nil {
		t.Fatalf("Close vector scanner: %v", err)
	}

	scanner, err = vectorDataset.Scan(
		lance.WithColumns("id"),
		lance.WithNearest("vector", query, 2),
		lance.WithoutScoringAutoprojection(),
	)
	if err != nil {
		t.Fatalf("vector Scan without scoring: %v", err)
	}
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			scanner.Close()
			t.Fatalf("vector records without scoring: %v", err)
		}
		if batch.Schema().HasField("_distance") {
			scanner.Close()
			t.Fatalf("vector scan schema = %v, want no _distance", batch.Schema())
		}
		break
	}
	if err := scanner.Close(); err != nil {
		t.Fatalf("Close vector scanner without scoring: %v", err)
	}

	ftsDataset, _ := ftsTestDataset(t)
	ftsQuery, err := lance.NewFTSMatchQuery("alpha", "", lance.FTSMatchOptions{})
	if err != nil {
		t.Fatalf("NewFTSMatchQuery: %v", err)
	}
	scanner, err = ftsDataset.Scan(lance.WithColumns("id"), lance.WithFTSQuery(ftsQuery))
	if err != nil {
		t.Fatalf("FTS Scan: %v", err)
	}
	if err := ftsQuery.Close(); err != nil {
		t.Fatalf("Close FTS query after Scan: %v", err)
	}
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			scanner.Close()
			t.Fatalf("FTS records: %v", err)
		}
		if !batch.Schema().HasField("_score") {
			scanner.Close()
			t.Fatalf("FTS scan schema = %v, want _score", batch.Schema())
		}
		break
	}
	if err := scanner.Close(); err != nil {
		t.Fatalf("Close FTS scanner: %v", err)
	}

	ftsQuery, err = lance.NewFTSMatchQuery("alpha", "", lance.FTSMatchOptions{})
	if err != nil {
		t.Fatalf("NewFTSMatchQuery without scoring: %v", err)
	}
	scanner, err = ftsDataset.Scan(
		lance.WithColumns("id"),
		lance.WithFTSQuery(ftsQuery),
		lance.WithoutScoringAutoprojection(),
	)
	if err != nil {
		t.Fatalf("FTS Scan without scoring: %v", err)
	}
	if err := ftsQuery.Close(); err != nil {
		t.Fatalf("Close FTS query after Scan without scoring: %v", err)
	}
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			scanner.Close()
			t.Fatalf("FTS records without scoring: %v", err)
		}
		if batch.Schema().HasField("_score") {
			scanner.Close()
			t.Fatalf("FTS scan schema = %v, want no _score", batch.Schema())
		}
		break
	}
	if err := scanner.Close(); err != nil {
		t.Fatalf("Close FTS scanner without scoring: %v", err)
	}
}

func TestScannerScalarIndexSegment(t *testing.T) {
	requireNative(t)
	dataset := newSingleFragmentDataset(t, []int64{1, 2, 3, 4, 5, 6})
	indexed, err := dataset.CreateScalarIndex(
		context.Background(),
		"id",
		index.ScalarBTree,
		nil,
		index.ScalarOptions{},
		lance.WithIndexName("id_btree"),
	)
	if err != nil {
		t.Fatalf("CreateScalarIndex: %v", err)
	}
	defer indexed.Close()

	fragments, err := indexed.FragmentIDs()
	if err != nil {
		t.Fatalf("FragmentIDs: %v", err)
	}
	segments, err := indexed.IndexSegmentUUIDs("id_btree")
	if err != nil {
		t.Fatalf("IndexSegmentUUIDs: %v", err)
	}
	if len(fragments) == 0 || len(segments) == 0 {
		t.Fatalf("index fixture fragments=%v segments=%v, want both nonempty", fragments, segments)
	}

	for _, useIndex := range []bool{true, false} {
		ids := scanIDs(t, indexed,
			lance.WithSQLFilter("id >= 4"),
			lance.WithFragmentIDs(fragments...),
			lance.WithScalarIndexSegment(segments[0]),
			lance.WithUseScalarIndex(useIndex),
		)
		if !equalIDs(ids, 4, 5, 6) {
			t.Fatalf("scalar index useIndex=%v ids = %v, want [4 5 6]", useIndex, ids)
		}
	}

	for _, useIndex := range []bool{true, false} {
		scanner, err := indexed.Scan(
			lance.WithColumns("id"),
			lance.WithRowID(true),
			lance.WithIncludeDeletedRows(true),
			lance.WithSQLFilter("id >= 4"),
			lance.WithFragmentIDs(fragments...),
			lance.WithScalarIndexSegment(segments[0]),
			lance.WithUseScalarIndex(useIndex),
		)
		if scanner != nil {
			scanner.Close()
		}
		if !errors.Is(err, lance.ErrInvalidArgument) {
			t.Fatalf("scalar index segment with deleted rows useIndex=%v: %v, want ErrInvalidArgument", useIndex, err)
		}
	}

	if scanner, err := indexed.Scan(
		lance.WithSQLFilter("id >= 4"),
		lance.WithScalarIndexSegment(segments[0]),
	); err == nil {
		scanner.Close()
		t.Fatal("scalar index segment without explicit fragment ids succeeded")
	}
}

func TestScannerSubstraitFilterAndAggregate(t *testing.T) {
	requireNative(t)
	dataset := newSingleFragmentDataset(t, []int64{1, 2, 3, 4, 5})
	filter := mustHexBytes(t, "120c1a0a1a02677420ffffffff0f1a290a1f1a1d1a040a021001220a1a0812060a021200220022091a070a0538039003011a06726573756c74221a0a0269640a046e616d65120e0a043a0210010a046202100118013a12103f2a0e6c616e63652d676f2d7465737473")

	ids := scanIDs(t, dataset, lance.WithSubstraitFilter(filter))
	if !equalIDs(ids, 4, 5) {
		t.Fatalf("Substrait filter ids = %v, want [4 5]", ids)
	}
	ids = scanIDs(t, dataset,
		lance.WithSQLFilter("id = 1"),
		lance.WithSubstraitFilter(filter),
	)
	if !equalIDs(ids, 4, 5) {
		t.Fatalf("Substrait filter with SQL filter ids = %v, want [4 5]", ids)
	}

	aggregate := mustHexBytes(t, "120b1a091a05636f756e7420011a1312110a08220622040a0230011205636f756e743212103f2a0e6c616e63652d676f2d746573747342630801125f68747470733a2f2f6769746875622e636f6d2f7375627374726169742d696f2f7375627374726169742f626c6f622f6d61696e2f657874656e73696f6e732f7375627374726169745f6167677265676174655f67656e657269632e79616d6c")
	scanner, err := dataset.Scan(lance.WithSubstraitAggregate(aggregate))
	if err != nil {
		t.Fatalf("Substrait aggregate Scan: %v", err)
	}
	var counts []int64
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			scanner.Close()
			t.Fatalf("Substrait aggregate records: %v", err)
		}
		counts = append(counts, collectInt64(t, batch, "count")...)
	}
	if err := scanner.Close(); err != nil {
		t.Fatalf("Close aggregate scanner: %v", err)
	}
	if !slices.Equal(counts, []int64{5}) {
		t.Fatalf("Substrait count = %v, want [5]", counts)
	}

	if scanner, err := dataset.Scan(lance.WithSubstraitFilter([]byte{0x0a, 0x05})); err == nil {
		scanner.Close()
		t.Fatal("malformed Substrait filter succeeded")
	}
	if scanner, err := dataset.Scan(
		lance.WithSubstraitAggregate(aggregate),
		lance.WithLimit(1),
	); err == nil {
		scanner.Close()
		t.Fatal("Substrait aggregate with limit succeeded")
	}
}

func TestScannerControlValidation(t *testing.T) {
	requireNative(t)
	dataset := newSingleFragmentDataset(t, []int64{1, 2, 3})

	for _, option := range []lance.ScanOption{
		lance.WithIOBufferSize(0),
		lance.WithIOBufferSize(^uint64(0)),
		lance.WithBatchReadahead(0),
		lance.WithFragmentReadahead(0),
		lance.WithTargetParallelism(0),
		lance.WithBlobHandling(lance.BlobHandling(3)),
	} {
		if scanner, err := dataset.Scan(option); err == nil {
			scanner.Close()
			t.Fatalf("invalid scanner control succeeded")
		}
	}

	scanner, err := dataset.Scan(lance.WithBatchSize(1), lance.WithUseStats(false))
	if err != nil {
		t.Fatalf("Scan with controlled batch size: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var canceled error
	for _, err := range scanner.Records(ctx) {
		canceled = err
		break
	}
	if !errors.Is(canceled, context.Canceled) {
		t.Fatalf("canceled controlled scan error = %v, want context.Canceled", canceled)
	}
	if err := scanner.Close(); err != nil {
		t.Fatalf("Close canceled scanner: %v", err)
	}

	closedScanner, err := dataset.Scan(lance.WithUseStats(false), lance.WithScanInOrder(false))
	if err != nil {
		t.Fatalf("Scan closed fixture: %v", err)
	}
	if err := closedScanner.Close(); err != nil {
		t.Fatalf("Close closed fixture scanner: %v", err)
	}
	var closedErr error
	for _, err := range closedScanner.Records(context.Background()) {
		closedErr = err
		break
	}
	if !errors.Is(closedErr, lance.ErrClosed) {
		t.Fatalf("closed controlled scan error = %v, want ErrClosed", closedErr)
	}
}

func newScannerControlDataset(t *testing.T) (*lance.Dataset, []uint64, [][]int64) {
	t.Helper()
	rows := [][]int64{{1, 2}, {3, 4}, {5, 6, 7}}
	schema := testSchema()
	uri := filepath.Join(t.TempDir(), "data.lance")
	dataset, err := lance.Create(
		context.Background(),
		uri,
		makeReader(t, schema, makeControlRecord(t, schema, rows[0])),
		lance.WithWriteParams(lance.WriteParams{EnableStableRowIDs: true}),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if dataset != nil {
			if err := dataset.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}
	})
	for _, row := range rows[1:] {
		next, err := lance.Append(
			context.Background(),
			uri,
			makeReader(t, schema, makeControlRecord(t, schema, row)),
			lance.WithWriteParams(lance.WriteParams{EnableStableRowIDs: true}),
		)
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
		if err := dataset.Close(); err != nil {
			t.Fatalf("Close before append: %v", err)
		}
		dataset = next
	}
	fragments, err := dataset.FragmentIDs()
	if err != nil {
		t.Fatalf("FragmentIDs: %v", err)
	}
	if len(fragments) != len(rows) {
		t.Fatalf("fragment count = %d, want %d", len(fragments), len(rows))
	}
	return dataset, fragments, rows
}

func newSingleFragmentDataset(t *testing.T, ids []int64) *lance.Dataset {
	t.Helper()
	schema := testSchema()
	uri := filepath.Join(t.TempDir(), "data.lance")
	dataset, err := lance.Create(
		context.Background(),
		uri,
		makeReader(t, schema, makeControlRecord(t, schema, ids)),
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

func makeControlRecord(t *testing.T, schema *arrow.Schema, ids []int64) arrow.RecordBatch {
	t.Helper()
	names := make([]string, len(ids))
	valid := make([]bool, len(ids))
	for i := range names {
		names[i] = "row"
		valid[i] = true
	}
	return makeRecord(t, schema, ids, names, valid)
}

func scanScannerIDs(t *testing.T, scanner *lance.Scanner) []int64 {
	t.Helper()
	defer scanner.Close()
	var ids []int64
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("scanner records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	slices.Sort(ids)
	return ids
}

func scanIDsAndDeleted(t *testing.T, dataset *lance.Dataset, opts ...lance.ScanOption) ([]int64, []bool) {
	t.Helper()
	options := []lance.ScanOption{lance.WithColumns("id"), lance.WithRowID(true)}
	options = append(options, opts...)
	scanner, err := dataset.Scan(options...)
	if err != nil {
		t.Fatalf("Scan with row IDs: %v", err)
	}
	defer scanner.Close()
	var ids []int64
	var deleted []bool
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("row ID records: %v", err)
		}
		batchIDs := collectInt64(t, batch, "id")
		rowIDs := findColumn(t, batch, "_rowid").(*array.Uint64)
		ids = append(ids, batchIDs...)
		for i := range batchIDs {
			deleted = append(deleted, rowIDs.IsNull(i))
		}
	}
	return ids, deleted
}

func mustHexBytes(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return decoded
}
