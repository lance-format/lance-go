package lance_test

import (
	"context"
	"path/filepath"
	"testing"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
)

func TestListIndicesPreservesEscapedNames(t *testing.T) {
	requireNative(t)
	for _, tc := range []struct {
		label string
		name  string
	}{
		{"backslash", `index\q`},
		{"literal_escape", `index\n`},
		{"quote", `index"quote`},
		{"control_characters", "index\nline\ttab"},
		{"unicode", "index_索引"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			ctx := context.Background()
			uri := filepath.Join(t.TempDir(), "data.lance")
			schema := testSchema()
			record := makeRecord(t, schema, []int64{1, 2}, []string{"one", "two"}, []bool{true, true})
			dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			t.Cleanup(func() { _ = dataset.Close() })

			indexed, err := dataset.CreateScalarIndex(ctx, "id", index.ScalarBTree, nil, index.ScalarOptions{}, lance.WithIndexName(tc.name))
			if err != nil {
				t.Fatalf("CreateScalarIndex(%q): %v", tc.name, err)
			}
			t.Cleanup(func() { _ = indexed.Close() })
			check := func(d *lance.Dataset) {
				t.Helper()
				indices, err := d.ListIndices()
				if err != nil {
					t.Fatalf("ListIndices(%q): %v", tc.name, err)
				}
				if len(indices) != 1 || indices[0].Name != tc.name {
					t.Fatalf("ListIndices = %+v, want one index named %q", indices, tc.name)
				}
				if len(indices[0].Columns) != 1 || indices[0].Columns[0] != "id" {
					t.Fatalf("index columns = %q, want [id]", indices[0].Columns)
				}
			}
			check(indexed)
			if err := indexed.Close(); err != nil {
				t.Fatalf("Close indexed dataset: %v", err)
			}
			if err := dataset.Close(); err != nil {
				t.Fatalf("Close source dataset: %v", err)
			}
			reopened, err := lance.Open(ctx, uri)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			check(reopened)
		})
	}
}
