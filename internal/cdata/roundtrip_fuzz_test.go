package cdata

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go/internal/handle"
)

const fuzzBatchMaxRows = 16

// FuzzRecordBatchRoundTrip builds a bounded batch with the supported Arrow Go
// builders from logical values, nulls and a row count, then runs the ordinary
// ExportRecordBatch and ImportRecordBatch path. The decoded schema, row count,
// null positions and values are compared with the original logical input.
//
// The fuzz parameters only supply logical values. Export and import build every
// descriptor, buffer, offset and pointer through the normal library path; the
// target does not mutate or construct descriptors itself.
func FuzzRecordBatchRoundTrip(f *testing.F) {
	f.Add(uint8(0), uint16(0), uint16(0), uint16(0), uint16(0), int64(0), "")
	f.Add(uint8(1), uint16(0), uint16(0), uint16(0), uint16(0), int64(7), "row")
	f.Add(uint8(3), uint16(0b101), uint16(0b010), uint16(0b001), uint16(0b100), int64(11), "héllo")
	f.Add(uint8(fuzzBatchMaxRows), uint16(0xffff), uint16(0xffff), uint16(0xffff), uint16(0xffff), int64(-3), "seed")
	f.Add(uint8(4), uint16(0b0101), uint16(0b1010), uint16(0b0011), uint16(0b1100), int64(1000), "mixed")

	f.Fuzz(func(t *testing.T, rows uint8, idMask, nameMask, scoreMask, tagsMask uint16, base int64, text string) {
		rowCount := int(rows) % (fuzzBatchMaxRows + 1)
		text = boundedFuzzText(text)

		mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
		defer mem.AssertSize(t, 0)

		batch := buildFuzzRecordBatch(t, mem, rowCount, idMask, nameMask, scoreMask, tagsMask, base, text)
		defer batch.Release()

		before := handle.Len()
		var exported ArrowArray
		if err := ExportRecordBatch(batch, &exported); err != nil {
			t.Fatalf("ExportRecordBatch: %v", err)
		}
		var exportedSchema ArrowSchema
		if err := ExportSchema(batch.Schema(), &exportedSchema); err != nil {
			ReleaseArray(&exported)
			t.Fatalf("ExportSchema: %v", err)
		}
		schema, err := ImportSchema(&exportedSchema)
		ReleaseSchema(&exportedSchema)
		if err != nil {
			ReleaseArray(&exported)
			t.Fatalf("ImportSchema: %v", err)
		}

		imported, err := ImportRecordBatch(&exported, schema)
		if err != nil {
			t.Fatalf("ImportRecordBatch: %v", err)
		}
		if got, want := imported.NumRows(), batch.NumRows(); got != want {
			t.Errorf("imported rows = %d, want %d", got, want)
		}
		if !imported.Schema().Equal(batch.Schema()) {
			t.Errorf("imported schema = %v, want %v", imported.Schema(), batch.Schema())
		}
		for i := 0; i < int(batch.NumCols()); i++ {
			assertFuzzColumnEqual(t, batch.Schema().Field(i).Name, batch.Column(i), imported.Column(i))
		}

		imported.Release()
		if got := handle.Len(); got != before {
			t.Errorf("round trip left %d exporter handles registered, want %d", got, before)
		}
	})
}

func fuzzRecordBatchSchema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "score", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
		{Name: "tags", Type: arrow.ListOf(arrow.PrimitiveTypes.Int32), Nullable: true},
	}, nil)
}

func buildFuzzRecordBatch(
	t *testing.T,
	mem memory.Allocator,
	rows int,
	idMask, nameMask, scoreMask, tagsMask uint16,
	base int64,
	text string,
) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(mem, fuzzRecordBatchSchema())
	defer builder.Release()

	id := builder.Field(0).(*array.Int64Builder)
	name := builder.Field(1).(*array.StringBuilder)
	score := builder.Field(2).(*array.Float64Builder)
	tags := builder.Field(3).(*array.ListBuilder)
	tagValues := tags.ValueBuilder().(*array.Int32Builder)

	for i := 0; i < rows; i++ {
		if idMask&(1<<i) != 0 {
			id.AppendNull()
		} else {
			id.Append(base + int64(i))
		}
		if nameMask&(1<<i) != 0 {
			name.AppendNull()
		} else {
			name.Append(fmt.Sprintf("%s-%d", text, i))
		}
		if scoreMask&(1<<i) != 0 {
			score.AppendNull()
		} else {
			score.Append(float64(base%1000)/8 + float64(i))
		}
		if tagsMask&(1<<i) != 0 {
			tags.AppendNull()
			continue
		}
		tags.Append(true)
		switch i % 3 {
		case 0:
			// An empty list: valid and distinct from null.
		case 1:
			tagValues.Append(int32(base%1000) + int32(i))
		case 2:
			tagValues.Append(int32(i))
			tagValues.Append(int32(i) + 1)
		}
	}

	return builder.NewRecordBatch()
}

// assertFuzzColumnEqual checks null positions and values against the original
// built column.
func assertFuzzColumnEqual(t *testing.T, name string, want, got arrow.Array) {
	t.Helper()
	if want.Len() != got.Len() {
		t.Fatalf("%s length = %d, want %d", name, got.Len(), want.Len())
	}
	for i := 0; i < want.Len(); i++ {
		if want.IsNull(i) != got.IsNull(i) {
			t.Fatalf("%s row %d null = %t, want %t", name, i, got.IsNull(i), want.IsNull(i))
		}
	}
	if !array.Equal(want, got) {
		t.Fatalf("%s values differ after round trip", name)
	}
}

// boundedFuzzText normalizes fuzz text to valid UTF-8 and a small size so the
// built strings stay bounded; the normalized value is the logical input.
func boundedFuzzText(text string) string {
	text = strings.ToValidUTF8(text, "\uFFFD")
	if len(text) > 32 {
		text = text[:32]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return text
}
