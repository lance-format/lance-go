package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Validate fixture construction and canonical output against the independent
// reports before any SDK writes a dataset. This does not replace the matrix.
func TestFixtureReports(t *testing.T) {
	for _, name := range []string{"base", "latest"} {
		t.Run(name, func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			constructors := []func(memory.Allocator) (arrow.RecordBatch, error){baseRecord}
			version := uint64(1)
			if name == "latest" {
				constructors = append(constructors, appendRecord)
				version = 2
			}
			data := report{version: version, values: make(map[string]map[string]string)}
			for _, construct := range constructors {
				func() {
					batch, err := construct(mem)
					if err != nil {
						t.Fatal(err)
					}
					defer batch.Release()
					if err := validateSchema(batch.Schema()); err != nil {
						t.Fatal(err)
					}
					for row := 0; row < int(batch.NumRows()); row++ {
						key, values, err := canonicalRow(batch, row)
						if err != nil {
							t.Fatal(err)
						}
						if _, exists := data.values[key]; exists {
							t.Fatalf("duplicate key %q", key)
						}
						data.values[key] = values
						data.rows++
					}
				}()
			}
			want, err := os.ReadFile(filepath.Join("..", "expected", name+".report"))
			if err != nil {
				t.Fatal(err)
			}
			if got := canonicalReport(data); got != string(want) {
				t.Fatalf("canonical report mismatch\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}
