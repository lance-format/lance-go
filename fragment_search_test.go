package lance_test

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	lance "github.com/lance-format/lance-go"
)

func TestFragmentNearestSearch(t *testing.T) {
	requireNative(t)
	schema := vectorSchema(2, arrow.PrimitiveTypes.Float32)
	record := makeVectorRecord(t, schema, []int64{1, 2, 3}, [][]float64{{0, 0}, {3, 4}, {6, 8}}, lance.VectorDataTypeFloat32)
	dataset := createVectorDataset(t, schema, record)
	fragments, err := dataset.Fragments()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range fragments {
		defer fragment.Close()
	}
	if len(fragments) != 1 {
		t.Fatalf("fragments = %d, want 1", len(fragments))
	}
	scanner, err := fragments[0].Scan(lance.WithNearest("vector", lance.NewFloat32Vector([]float32{3, 4}), 1))
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()
	rows := collectNearest(t, scanner)
	requireNearestIDs(t, rows, []int64{2})
	requireNearestDistances(t, rows, []float32{0})
}

func TestFragmentFullTextSearch(t *testing.T) {
	requireNative(t)
	dataset, _ := ftsTestDataset(t)
	fragments, err := dataset.Fragments()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range fragments {
		defer fragment.Close()
	}
	if len(fragments) != 1 {
		t.Fatalf("fragments = %d, want 1", len(fragments))
	}
	query, err := lance.NewFTSMatchQuery("delta", "name", lance.FTSMatchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer query.Close()
	scanner, err := fragments[0].Scan(lance.WithFTSQuery(query))
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()
	query.Close()
	if got := scannerIDs(t, scanner); !equalIDs(got, 5) {
		t.Fatalf("matching fragment rows = %v, want [5]", got)
	}
}
