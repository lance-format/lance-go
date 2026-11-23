package lance_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
)

const (
	benchmarkScanRows         = 32768
	benchmarkScanWriteRows    = 8192
	benchmarkScanWriteBatches = benchmarkScanRows / benchmarkScanWriteRows
)

// BenchmarkScannerRecords measures the Go streaming path over a warm local
// dataset at fixed target batch sizes. The dataset is written once before the
// first iteration; each iteration scans every row and closes the scanner.
func BenchmarkScannerRecords(b *testing.B) {
	dataset := newBenchmarkDataset(b)
	ctx := context.Background()

	for _, targetBatchSize := range []int64{64, 1024, 8192} {
		b.Run(fmt.Sprintf("batch=%d", targetBatchSize), func(b *testing.B) {
			b.ReportAllocs()

			var rows int64
			for b.Loop() {
				rows = scanBenchmarkDataset(b, ctx, dataset, targetBatchSize)
			}
			b.ReportMetric(float64(rows), "rows/op")
		})
	}
}

func scanBenchmarkDataset(b *testing.B, ctx context.Context, dataset *lance.Dataset, targetBatchSize int64) int64 {
	b.Helper()
	scanner, err := dataset.Scan(lance.WithBatchSize(targetBatchSize))
	if err != nil {
		b.Fatalf("Scan: %v", err)
	}

	var rows int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			b.Fatalf("Records: %v", err)
		}
		rows += batch.NumRows()
	}
	if rows != benchmarkScanRows {
		b.Fatalf("scan rows = %d, want %d", rows, benchmarkScanRows)
	}
	return rows
}

func newBenchmarkDataset(b *testing.B) *lance.Dataset {
	b.Helper()
	requireNative(b)

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)

	batches := make([]arrow.RecordBatch, 0, benchmarkScanWriteBatches)
	for start := 0; start < benchmarkScanRows; start += benchmarkScanWriteRows {
		batches = append(batches, newBenchmarkScanBatch(b, schema, start, benchmarkScanWriteRows))
	}
	reader, err := array.NewRecordReader(schema, batches)
	if err != nil {
		b.Fatalf("NewRecordReader: %v", err)
	}
	for _, batch := range batches {
		batch.Release()
	}
	b.Cleanup(reader.Release)

	dataset, err := lance.Create(context.Background(), filepath.Join(b.TempDir(), "data.lance"), reader)
	if err != nil {
		b.Fatalf("Create: %v", err)
	}
	b.Cleanup(func() {
		if err := dataset.Close(); err != nil {
			b.Errorf("Close: %v", err)
		}
	})
	return dataset
}

func newBenchmarkScanBatch(b *testing.B, schema *arrow.Schema, start, rows int) arrow.RecordBatch {
	b.Helper()
	builder := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer builder.Release()

	idBuilder := builder.Field(0).(*array.Int64Builder)
	nameBuilder := builder.Field(1).(*array.StringBuilder)
	for i := 0; i < rows; i++ {
		idBuilder.Append(int64(start + i))
		nameBuilder.Append("row")
	}
	return builder.NewRecordBatch()
}
