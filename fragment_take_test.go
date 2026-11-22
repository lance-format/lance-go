package lance_test

import (
	"context"
	"slices"
	"testing"
)

// TestFragmentTakeExplicitEmptyProjection pins the native contract from
// lance-c v0.2.1-beta.1 / Lance v15.0.0-beta.26: an explicit empty fragment
// projection returns one zero-column batch with the requested row count.
// Earlier native builds rejected the same call with LANCE_ERR_NOT_FOUND, so a
// regression here would silently change the documented Go projection contract.
func TestFragmentTakeExplicitEmptyProjection(t *testing.T) {
	dataset, _, _ := newMutationDataset(t)
	ctx := context.Background()
	fragment := mustOpenFragment(t, dataset, 0)

	indices := []uint32{0, 2}
	batches, err := fragment.Take(ctx, indices, []string{}...)
	if err != nil {
		t.Fatalf("Take(explicit empty projection): %v", err)
	}
	defer func() {
		for _, batch := range batches {
			batch.Release()
		}
	}()
	if len(batches) != 1 {
		t.Fatalf("explicit empty projection returned %d batches, want 1", len(batches))
	}
	if got := batches[0].NumCols(); got != 0 {
		t.Fatalf("explicit empty projection columns = %d, want 0", got)
	}
	if got := batches[0].NumRows(); got != int64(len(indices)) {
		t.Fatalf("explicit empty projection rows = %d, want %d", got, len(indices))
	}
	if got := batches[0].Schema().NumFields(); got != 0 {
		t.Fatalf("explicit empty projection schema fields = %d, want 0", got)
	}

	full, err := fragment.Take(ctx, indices)
	if err != nil {
		t.Fatalf("Take(nil columns): %v", err)
	}
	defer func() {
		for _, batch := range full {
			batch.Release()
		}
	}()
	if len(full) != 1 {
		t.Fatalf("Take(nil columns) returned %d batches, want 1", len(full))
	}
	if got := full[0].NumCols(); got != 2 {
		t.Fatalf("Take(nil columns) columns = %d, want the full 2-column schema", got)
	}
	if got, want := collectInt64(t, full[0], "id"), []int64{1, 3}; !slices.Equal(got, want) {
		t.Fatalf("Take(nil columns) ids = %v, want %v", got, want)
	}
	if got := full[0].Schema().NumFields(); got != 2 {
		t.Fatalf("Take(nil columns) schema fields = %d, want 2", got)
	}

	// A caller that builds an empty slice still requests the explicit empty
	// projection; nil continues to mean the full schema.
	var nilColumns []string
	nilBatches, err := fragment.Take(ctx, indices[:1], nilColumns...)
	if err != nil {
		t.Fatalf("Take(nil slice): %v", err)
	}
	defer func() {
		for _, batch := range nilBatches {
			batch.Release()
		}
	}()
	if len(nilBatches) != 1 || nilBatches[0].NumCols() != 2 || nilBatches[0].NumRows() != 1 {
		t.Fatalf("Take(nil slice) returned %d batches with the wrong shape, want one 2-column 1-row batch", len(nilBatches))
	}
	if !nilBatches[0].Schema().Equal(full[0].Schema()) {
		t.Fatalf("Take(nil slice) schema = %v, want %v", nilBatches[0].Schema(), full[0].Schema())
	}
}
