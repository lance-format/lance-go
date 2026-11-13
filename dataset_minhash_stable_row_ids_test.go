package lance_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
)

type minHashRowCoordinate struct {
	rowID      uint64
	rowAddress uint64
}

func TestMinHashStableRowIDQueryAfterReopen(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "minhash-stable-row-ids.lance")
	schema := testSchema()

	dataset, err := lance.Create(
		ctx,
		uri,
		makeReader(t, schema, makeRecord(t, schema,
			[]int64{1, 2},
			[]string{"ordinary fragment one", "ordinary fragment two"},
			[]bool{true, true},
		)),
		lance.WithWriteParams(lance.WriteParams{EnableStableRowIDs: true}),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("close created dataset: %v", err)
	}

	dataset, err = lance.Append(
		ctx,
		uri,
		makeReader(t, schema, makeRecord(t, schema,
			[]int64{3, 4},
			[]string{"ordinary fragment three", "ordinary fragment four"},
			[]bool{true, true},
		)),
		lance.WithWriteParams(lance.WriteParams{EnableStableRowIDs: true}),
	)
	if err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("close first appended dataset: %v", err)
	}

	dataset, err = lance.Append(
		ctx,
		uri,
		makeReader(t, schema, makeRecord(t, schema,
			[]int64{5, 6},
			[]string{"ordinary fragment five", "minhash stable target phrase"},
			[]bool{true, true},
		)),
		lance.WithWriteParams(lance.WriteParams{EnableStableRowIDs: true}),
	)
	if err != nil {
		t.Fatalf("second Append: %v", err)
	}
	defer dataset.Close()

	fragments, err := dataset.Fragments()
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	defer func() {
		for _, fragment := range fragments {
			if err := fragment.Close(); err != nil {
				t.Errorf("close fragment: %v", err)
			}
		}
	}()
	if len(fragments) != 3 {
		t.Fatalf("fragments = %d, want 3", len(fragments))
	}
	for i, fragment := range fragments {
		rows, err := fragment.PhysicalRows()
		if err != nil {
			t.Fatalf("fragment %d PhysicalRows: %v", i, err)
		}
		if rows != 2 {
			t.Fatalf("fragment %d physical rows = %d, want 2", i, rows)
		}
	}

	fragmentID := fragments[2].ID()
	coordinates := scanMinHashRowCoordinates(t, dataset, fragmentID)
	wantCoordinates := map[int64]minHashRowCoordinate{
		5: {rowID: 4, rowAddress: fragmentID << 32},
		6: {rowID: 5, rowAddress: (fragmentID << 32) | 1},
	}
	if len(coordinates) != len(wantCoordinates) {
		t.Fatalf("fragment %d coordinates = %+v, want %+v", fragmentID, coordinates, wantCoordinates)
	}
	for applicationID, want := range wantCoordinates {
		got, ok := coordinates[applicationID]
		if !ok {
			t.Fatalf("fragment %d has no row for application id %d", fragmentID, applicationID)
		}
		if got != want || got.rowID == got.rowAddress {
			t.Fatalf("application id %d coordinate = %+v, want %+v with distinct row id/address", applicationID, got, want)
		}
	}

	indexed, err := dataset.CreateScalarIndex(
		ctx,
		"name",
		index.ScalarMinHashLSH,
		json.RawMessage(`{}`),
		index.ScalarOptions{},
		lance.WithIndexName("name_minhash"),
	)
	if err != nil {
		t.Fatalf("CreateScalarIndex MinHash: %v", err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("close indexed source dataset: %v", err)
	}

	const query = "minhash stable target phrase"
	if id, distance := scanSingleMinHashHit(t, indexed, query); id != 6 || distance != 0 {
		t.Fatalf("MinHash hit = (%d, %v), want application id 6 at distance 0", id, distance)
	}
	if err := indexed.Close(); err != nil {
		t.Fatalf("close indexed dataset: %v", err)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("reopen indexed dataset: %v", err)
	}
	defer reopened.Close()
	if id, distance := scanSingleMinHashHit(t, reopened, query); id != 6 || distance != 0 {
		t.Fatalf("reopened MinHash hit = (%d, %v), want application id 6 at distance 0", id, distance)
	}
}

func scanMinHashRowCoordinates(t *testing.T, dataset *lance.Dataset, fragmentID uint64) map[int64]minHashRowCoordinate {
	t.Helper()
	scanner, err := dataset.Scan(
		lance.WithColumns("id"),
		lance.WithRowID(true),
		lance.WithRowAddress(true),
		lance.WithFragmentIDs(fragmentID),
	)
	if err != nil {
		t.Fatalf("scan fragment %d row coordinates: %v", fragmentID, err)
	}
	defer scanner.Close()

	coordinates := make(map[int64]minHashRowCoordinate)
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("scan fragment %d records: %v", fragmentID, err)
		}
		ids := collectInt64(t, batch, "id")
		rowIDColumn := findColumn(t, batch, "_rowid")
		rowIDs, ok := rowIDColumn.(*array.Uint64)
		if !ok {
			t.Fatalf("_rowid type = %T, want *array.Uint64", rowIDColumn)
		}
		rowAddressColumn := findColumn(t, batch, "_rowaddr")
		rowAddresses, ok := rowAddressColumn.(*array.Uint64)
		if !ok {
			t.Fatalf("_rowaddr type = %T, want *array.Uint64", rowAddressColumn)
		}
		for i, id := range ids {
			if rowIDs.IsNull(i) || rowAddresses.IsNull(i) {
				t.Fatalf("application id %d has null row id or row address", id)
			}
			coordinates[id] = minHashRowCoordinate{
				rowID:      rowIDs.Value(i),
				rowAddress: rowAddresses.Value(i),
			}
		}
	}
	return coordinates
}

func scanSingleMinHashHit(t *testing.T, dataset *lance.Dataset, text string) (int64, float32) {
	t.Helper()
	scanner, err := dataset.Scan(
		lance.WithColumns("id", "_distance"),
		lance.WithMinHashQuery("name", text),
		lance.WithLimit(1),
	)
	if err != nil {
		t.Fatalf("MinHash scan: %v", err)
	}
	defer scanner.Close()

	var ids []int64
	var distances []float32
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("MinHash records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
		distanceArray := findColumn(t, batch, "_distance")
		distanceColumn, ok := distanceArray.(*array.Float32)
		if !ok {
			t.Fatalf("_distance type = %T, want *array.Float32", distanceArray)
		}
		for i := 0; i < distanceColumn.Len(); i++ {
			if distanceColumn.IsNull(i) {
				t.Fatal("_distance is null")
			}
			distances = append(distances, distanceColumn.Value(i))
		}
	}
	if len(ids) != 1 || len(distances) != 1 {
		t.Fatalf("MinHash returned ids %v and distances %v, want exactly one hit", ids, distances)
	}
	return ids[0], distances[0]
}
