package cdata

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go/internal/handle"
)

// TestRecordBatchRoundTripSupportedTypes walks representative valid Arrow
// record batches through the C data interface and back. Each case checks the
// schema, row count, values and nulls, and that both sides release their own
// references: the checked allocator returns to zero and the exporter handle is
// gone.
func TestRecordBatchRoundTripSupportedTypes(t *testing.T) {
	cases := []struct {
		name   string
		schema *arrow.Schema
		build  func(*testing.T, *array.RecordBuilder)
	}{
		{
			name: "primitives",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: "id", Type: arrow.PrimitiveTypes.Int64},
				{Name: "score", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
				{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
				{Name: "at", Type: arrow.FixedWidthTypes.Timestamp_ms, Nullable: true},
			}, nil),
			build: func(t *testing.T, builder *array.RecordBuilder) {
				t.Helper()
				id := builder.Field(0).(*array.Int64Builder)
				score := builder.Field(1).(*array.Float64Builder)
				name := builder.Field(2).(*array.StringBuilder)
				at := builder.Field(3).(*array.TimestampBuilder)

				id.AppendValues([]int64{10, 11, 12}, nil)
				score.AppendValues([]float64{1.5, 0, -2.25}, []bool{true, false, true})
				name.AppendValues([]string{"alpha", "", "beta"}, []bool{true, true, false})
				at.AppendValues([]arrow.Timestamp{1764000000000, 0, 1764000001000}, []bool{true, false, true})
			},
		},
		{
			name: "binary",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: "payload", Type: arrow.BinaryTypes.Binary, Nullable: true},
			}, nil),
			build: func(t *testing.T, builder *array.RecordBuilder) {
				t.Helper()
				payload := builder.Field(0).(*array.BinaryBuilder)
				payload.Append([]byte{1, 2, 3})
				payload.Append([]byte{})
				payload.AppendNull()
			},
		},
		{
			name: "nested",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: "tags", Type: arrow.ListOf(arrow.PrimitiveTypes.Int32), Nullable: true},
				{Name: "point", Type: arrow.StructOf(arrow.Field{
					Name: "x", Type: arrow.PrimitiveTypes.Int32, Nullable: true,
				}), Nullable: true},
			}, nil),
			build: func(t *testing.T, builder *array.RecordBuilder) {
				t.Helper()
				tags := builder.Field(0).(*array.ListBuilder)
				tagValues := tags.ValueBuilder().(*array.Int32Builder)
				tags.Append(true)
				tagValues.AppendValues([]int32{1, 2}, nil)
				tags.AppendNull()
				tags.Append(true)

				point := builder.Field(1).(*array.StructBuilder)
				x := point.FieldBuilder(0).(*array.Int32Builder)
				point.Append(true)
				x.Append(7)
				point.Append(true)
				x.AppendNull()
				point.Append(false)
				x.Append(9)
			},
		},
		{
			name: "dictionary",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: "category", Type: &arrow.DictionaryType{
					IndexType: arrow.PrimitiveTypes.Int32,
					ValueType: arrow.BinaryTypes.String,
				}, Nullable: true},
			}, nil),
			build: func(t *testing.T, builder *array.RecordBuilder) {
				t.Helper()
				category := builder.Field(0).(*array.BinaryDictionaryBuilder)
				if err := category.AppendString("red"); err != nil {
					t.Fatalf("append dictionary value: %v", err)
				}
				category.AppendNull()
				if err := category.AppendString("red"); err != nil {
					t.Fatalf("append repeated dictionary value: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
			defer mem.AssertSize(t, 0)

			builder := array.NewRecordBuilder(mem, tc.schema)
			tc.build(t, builder)
			batch := builder.NewRecordBatch()
			builder.Release()
			defer batch.Release()

			before := handle.Len()
			var exported ArrowArray
			if err := ExportRecordBatch(batch, &exported); err != nil {
				t.Fatalf("ExportRecordBatch: %v", err)
			}
			imported, err := ImportRecordBatch(&exported, tc.schema)
			if err != nil {
				t.Fatalf("ImportRecordBatch: %v", err)
			}
			importedReleased := false
			defer func() {
				if !importedReleased {
					imported.Release()
				}
			}()

			if got, want := imported.NumRows(), batch.NumRows(); got != want {
				t.Errorf("imported rows = %d, want %d", got, want)
			}
			if !imported.Schema().Equal(tc.schema) {
				t.Errorf("imported schema = %v, want %v", imported.Schema(), tc.schema)
			}
			for i := 0; i < int(batch.NumCols()); i++ {
				column := tc.schema.Field(i).Name
				if !array.Equal(batch.Column(i), imported.Column(i)) {
					t.Errorf("column %q values differ after round trip", column)
				}
				if got, want := imported.Column(i).NullN(), batch.Column(i).NullN(); got != want {
					t.Errorf("column %q nulls = %d, want %d", column, got, want)
				}
			}

			imported.Release()
			importedReleased = true
			if got := handle.Len(); got != before {
				t.Errorf("round trip left %d exporter handles registered, want %d", got, before)
			}
		})
	}
}
