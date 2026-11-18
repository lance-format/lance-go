//go:build lance_rtree_legacy

package lance_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
)

// TestRTreeRejectedByLegacyNative verifies that a pre-RTree library still loads
// through the generic scalar-index entry point and rejects enum value 9. Symbol
// presence alone would not show that RTree is supported.
func TestRTreeRejectedByLegacyNative(t *testing.T) {
	requireNative(t)

	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "legacy-rtree.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1}, []string{"one"}, []bool{true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()

	if _, err := dataset.CreateScalarIndex(
		ctx,
		"id",
		index.ScalarRTree,
		nil,
		index.ScalarOptions{},
		lance.WithIndexName("legacy_rtree"),
	); !errors.Is(err, lance.ErrInvalidArgument) || !strings.Contains(err.Error(), "invalid scalar index type") {
		t.Fatalf("RTree on legacy native = %v, want native ErrInvalidArgument", err)
	}
	if count, err := dataset.IndexCount(); err != nil || count != 0 {
		t.Fatalf("legacy failed create changed IndexCount = %d, %v; want 0, nil", count, err)
	}
}
