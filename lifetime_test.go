package lance_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lance-format/lance-go"
)

// TestDerivedHandlesOutliveOwners exercises the ownership contracts that are
// easy to lose when a native handle is borrowed rather than cloned. A fragment
// retains its dataset snapshot, NativeSchema/DataFile are independent clones,
// and batches returned by Take belong to the caller.
func TestDerivedHandlesOutliveOwners(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "lifetime.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})

	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	fragments, err := dataset.Fragments()
	if err != nil {
		dataset.Close()
		t.Fatalf("Fragments: %v", err)
	}
	if len(fragments) != 1 {
		dataset.Close()
		t.Fatalf("fragments = %d, want 1", len(fragments))
	}
	fragment := fragments[0]
	dataFile, err := fragment.DataFileAt(0)
	if err != nil {
		fragment.Close()
		dataset.Close()
		t.Fatalf("DataFileAt: %v", err)
	}
	dataFileClone, err := dataFile.Clone()
	if err != nil {
		dataFile.Close()
		fragment.Close()
		dataset.Close()
		t.Fatalf("DataFile.Clone: %v", err)
	}
	schemaClone, err := dataset.NativeSchema()
	if err != nil {
		dataFileClone.Close()
		dataFile.Close()
		fragment.Close()
		dataset.Close()
		t.Fatalf("NativeSchema: %v", err)
	}
	batches, err := dataset.Take(ctx, []uint32{0, 1})
	if err != nil {
		schemaClone.Close()
		dataFileClone.Close()
		dataFile.Close()
		fragment.Close()
		dataset.Close()
		t.Fatalf("Take: %v", err)
	}
	defer func() {
		for _, batch := range batches {
			batch.Release()
		}
	}()

	dataFile.Close()
	dataset.Close()

	// The fragment keeps the dataset version alive.
	ids := fragmentTakeIDs(t, fragment, []uint32{0})
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("fragment take after dataset close = %v, want [1]", ids)
	}
	if count, err := fragment.CountRows(nil); err != nil || count != 2 {
		t.Fatalf("fragment CountRows after dataset close = %d, %v; want 2", count, err)
	}
	scanner, err := fragment.Scan()
	if err != nil {
		t.Fatalf("fragment Scan after dataset close: %v", err)
	}
	if ids := scanIDs(t, scanner); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("fragment scan after dataset close = %v, want [1 2]", ids)
	}
	fragment.Close()

	// Caller-owned batches survive the dataset that produced them.
	var taken []int64
	for _, batch := range batches {
		taken = append(taken, collectInt64(t, batch, "id")...)
	}
	if len(taken) != 2 || taken[0] != 1 || taken[1] != 2 {
		t.Fatalf("retained batches = %v, want [1 2]", taken)
	}

	// Cloned metadata outlives both the dataset and the fragment.
	if fieldID, err := schemaClone.FieldID("id"); err != nil || fieldID < 0 {
		t.Fatalf("schema FieldID after dataset close = %d, %v", fieldID, err)
	}
	arrowSchema, err := schemaClone.Arrow()
	if err != nil || len(arrowSchema.Fields()) != 2 {
		t.Fatalf("schema Arrow after dataset close = %v, %v", arrowSchema, err)
	}
	if path, err := dataFileClone.Path(); err != nil || path == "" {
		t.Fatalf("data file Path after dataset close = %q, %v", path, err)
	}
	schemaClone.Close()
	dataFileClone.Close()
}

// TestReaderOwnedBatchSurvivesWrite checks that exporting a caller's reader to
// native code retains the reader only for the duration of the export. The
// caller's own record batch must remain readable afterwards.
func TestReaderOwnedBatchSurvivesWrite(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "reader-ownership.lance")
	schema := testSchema()
	batch := makeRecord(t, schema, []int64{9}, []string{"kept"}, []bool{true})

	tx, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, batch), lance.WithWriteMode(lance.WriteModeCreate))
	if err != nil {
		t.Fatalf("WriteUncommitted: %v", err)
	}
	if err := tx.Close(); err != nil {
		t.Fatalf("transaction Close: %v", err)
	}

	ids := collectInt64(t, batch, "id")
	if len(ids) != 1 || ids[0] != 9 {
		t.Fatalf("caller-owned batch after export = %v, want [9]", ids)
	}
}
