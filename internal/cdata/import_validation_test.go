package cdata

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func TestImportSlicedChildren(t *testing.T) {
	for _, tc := range []struct {
		name        string
		dt          arrow.DataType
		childLength int
		valid       bool
	}{
		{"struct-short", arrow.StructOf(arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Int32}), 3, false},
		{"struct-valid", arrow.StructOf(arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Int32}), 4, true},
		{"fixed-list-short", arrow.FixedSizeListOf(2, arrow.PrimitiveTypes.Int32), 6, false},
		{"fixed-list-valid", arrow.FixedSizeListOf(2, arrow.PrimitiveTypes.Int32), 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetFixtureRelease()
			f := newFixture(t)
			values := make([]int32, tc.childLength)
			for i := range values {
				values[i] = int32(i)
			}
			root := f.array()
			root.Length, root.Offset = 2, 2
			root.NBuffers, root.NChildren = 1, 1
			root.Buffers = f.pointers(nil)
			root.Children = f.arrayPtrs(f.int32Array(values, nil, 0))
			root.Release = fixtureRelease
			arr, err := ImportArray(root, tc.dt)
			if tc.valid {
				if err != nil {
					t.Fatalf("valid sliced import: %v", err)
				}
				switch v := arr.(type) {
				case *array.Struct:
					if got := v.Field(0).(*array.Int32).Value(0); got != 2 {
						t.Errorf("first value = %d, want 2", got)
					}
				case *array.FixedSizeList:
					start, end := v.ValueOffsets(0)
					if start != 4 || end != 6 {
						t.Errorf("first range = [%d,%d), want [4,6)", start, end)
					}
					if got := v.ListValues().(*array.Int32).Value(int(start)); got != 4 {
						t.Errorf("first child value = %d, want 4", got)
					}
				}
			} else if !errors.Is(err, ErrInvalid) {
				t.Errorf("short child error = %v, want ErrInvalid", err)
			}
			if arr != nil {
				arr.Release()
			}
			if got := fixtureReleaseCalls.Load(); got != 1 {
				t.Errorf("producer release calls = %d, want 1", got)
			}
		})
	}
}

func TestImportInvalidBinaryOffsets(t *testing.T) {
	for _, dt := range []arrow.DataType{arrow.BinaryTypes.String, arrow.BinaryTypes.LargeString} {
		for _, tc := range []struct {
			name    string
			offsets []int64
		}{
			{"negative-first", []int64{-1, 1}},
			{"negative-last", []int64{0, -1}},
			{"decreasing", []int64{0, 2, 1}},
		} {
			t.Run(dt.Name()+"/"+tc.name, func(t *testing.T) {
				resetFixtureRelease()
				f := newFixture(t)
				width := 4
				if dt.ID() == arrow.LARGE_STRING {
					width = 8
				}
				offsets := make([]byte, len(tc.offsets)*width)
				for i, off := range tc.offsets {
					if width == 4 {
						binary.NativeEndian.PutUint32(offsets[i*width:], uint32(int32(off)))
					} else {
						binary.NativeEndian.PutUint64(offsets[i*width:], uint64(off))
					}
				}
				src := f.array()
				src.Length, src.NBuffers = int64(len(tc.offsets)-1), 3
				src.Buffers = f.pointers(nil, pinSlice(f, offsets), pinSlice(f, []byte{'a', 'b'}))
				src.Release = fixtureRelease
				arr, err := ImportArray(src, dt)
				if arr != nil {
					arr.Release()
				}
				if !errors.Is(err, ErrInvalid) {
					t.Errorf("offset error = %v, want ErrInvalid", err)
				}
				if got := fixtureReleaseCalls.Load(); got != 1 {
					t.Errorf("producer release calls = %d, want 1", got)
				}
			})
		}
	}
}
