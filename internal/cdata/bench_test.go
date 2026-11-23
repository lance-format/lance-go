package cdata

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// benchmarkBatchRows is large enough that per-call overhead does not dominate
// the measured import or export work.
const benchmarkBatchRows = 1024

// newBenchmarkRecordBatch returns a record batch built with the Arrow builder
// API. Every source is released through the exporter's own callback, so the
// benchmarks keep producer and consumer ownership balanced.
func newBenchmarkRecordBatch(b *testing.B) (arrow.RecordBatch, *arrow.Schema) {
	b.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "score", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		{Name: "tags", Type: arrow.ListOf(arrow.PrimitiveTypes.Int32), Nullable: true},
	}, nil)

	builder := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	b.Cleanup(builder.Release)

	idBuilder := builder.Field(0).(*array.Int64Builder)
	nameBuilder := builder.Field(1).(*array.StringBuilder)
	scoreBuilder := builder.Field(2).(*array.Float64Builder)
	tagBuilder := builder.Field(3).(*array.ListBuilder)
	tagValues := tagBuilder.ValueBuilder().(*array.Int32Builder)

	for i := 0; i < benchmarkBatchRows; i++ {
		idBuilder.Append(int64(i))
		if i%7 == 0 {
			nameBuilder.AppendNull()
		} else {
			nameBuilder.Append("row")
		}
		if i%5 == 0 {
			scoreBuilder.AppendNull()
		} else {
			scoreBuilder.Append(float64(i) / 3)
		}
		if i%11 == 0 {
			tagBuilder.AppendNull()
			continue
		}
		tagBuilder.Append(true)
		tagValues.Append(int32(i))
		tagValues.Append(int32(i + 1))
	}

	record := builder.NewRecordBatch()
	b.Cleanup(record.Release)
	return record, schema
}

// BenchmarkExportRecordBatch measures exporting a valid record batch to the C
// data interface and releasing the exported structure. Release is part of the
// timed phase because a caller must always pair it with the export.
func BenchmarkExportRecordBatch(b *testing.B) {
	batch, _ := newBenchmarkRecordBatch(b)
	b.ReportAllocs()

	for b.Loop() {
		var exported ArrowArray
		if err := ExportRecordBatch(batch, &exported); err != nil {
			b.Fatalf("ExportRecordBatch: %v", err)
		}
		ReleaseArray(&exported)
		if exported.Release != 0 {
			b.Fatal("exported record batch was not released")
		}
	}
	b.ReportMetric(benchmarkBatchRows, "rows/op")
}

// BenchmarkImportRecordBatch measures importing a C record batch produced from
// a valid Arrow batch and releasing the imported columns. The export that
// prepares each source structure runs outside the timed region, so the results
// describe the import phase only.
func BenchmarkImportRecordBatch(b *testing.B) {
	batch, schema := newBenchmarkRecordBatch(b)
	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()
		var source ArrowArray
		if err := ExportRecordBatch(batch, &source); err != nil {
			b.Fatalf("ExportRecordBatch: %v", err)
		}
		b.StartTimer()

		imported, err := ImportRecordBatch(&source, schema)
		if err != nil {
			b.Fatalf("ImportRecordBatch: %v", err)
		}
		rows := imported.NumRows()
		imported.Release()
		if rows != benchmarkBatchRows {
			b.Fatalf("imported rows = %d, want %d", rows, benchmarkBatchRows)
		}
	}
	b.ReportMetric(benchmarkBatchRows, "rows/op")
}

// BenchmarkCopyBorrowedRecordBatch measures the copy path used when a borrowed
// producer batch must become Go-owned, as in batch UDF input. Exporting the
// producer structure and schema runs outside the timed region; the copy and
// the caller's releases are measured.
func BenchmarkCopyBorrowedRecordBatch(b *testing.B) {
	batch, schema := newBenchmarkRecordBatch(b)
	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()
		var source ArrowArray
		if err := ExportRecordBatch(batch, &source); err != nil {
			b.Fatalf("ExportRecordBatch: %v", err)
		}
		var sourceSchema ArrowSchema
		if err := ExportSchema(schema, &sourceSchema); err != nil {
			ReleaseArray(&source)
			b.Fatalf("ExportSchema: %v", err)
		}
		b.StartTimer()

		copied, err := CopyBorrowedRecordBatch(&source, &sourceSchema)
		ReleaseSchema(&sourceSchema)
		if err != nil {
			ReleaseArray(&source)
			b.Fatalf("CopyBorrowedRecordBatch: %v", err)
		}
		rows := copied.NumRows()
		copied.Release()
		ReleaseArray(&source)
		if rows != benchmarkBatchRows {
			b.Fatalf("copied rows = %d, want %d", rows, benchmarkBatchRows)
		}
	}
	b.ReportMetric(benchmarkBatchRows, "rows/op")
}
