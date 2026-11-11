package ffi

import (
	"reflect"
	"runtime"
	"testing"
	"unsafe"
)

func TestFileReaderColumnPointers(t *testing.T) {
	for _, columns := range [][]string{nil, {}, {"id", "name"}} {
		t.Run("columns", func(t *testing.T) {
			raw, keep, err := buildFileReaderOptions(&FileReaderOptions{Columns: columns})
			if err != nil {
				t.Fatal(err)
			}
			defer keep.keepAlive()
			runtime.GC()
			if (raw.HasColumns != 0) != (columns != nil) || raw.ColumnCount != uint64(len(columns)) {
				t.Fatalf("projection presence/count = %d/%d, input = %#v", raw.HasColumns, raw.ColumnCount, columns)
			}
			// Consume const char* const* exactly as the native reader does.
			for i, ptr := range unsafe.Slice(raw.Columns, raw.ColumnCount) {
				if got := goString(ptr); got != columns[i] {
					t.Fatalf("column %d = %q, want %q", i, got, columns[i])
				}
			}
		})
	}
}

func TestStorageOptionsInputPointers(t *testing.T) {
	for _, values := range []map[string]string{nil, {}, {"region": "west", "endpoint": "https://example.invalid"}} {
		t.Run("options", func(t *testing.T) {
			var got map[string]string
			b := &bindings{
				storageAccessorNew: func(input *storageOptionsInput, provider uintptr) uintptr {
					if input == nil || provider != 0 {
						t.Fatal("static accessor must pass a present input and no provider")
					}
					runtime.GC()
					got = make(map[string]string)
					keys := unsafe.Slice(input.Keys, input.Count)
					vals := unsafe.Slice(input.Values, input.Count)
					for i := range keys {
						got[goString(keys[i])] = goString(vals[i])
					}
					return 1
				},
				storageAccessorClose: func(uintptr) {},
			}
			accessor, err := newStorageOptionsAccessor(b, values)
			if err != nil {
				t.Fatal(err)
			}
			accessor.Close()
			if values == nil {
				values = map[string]string{}
			}
			if !reflect.DeepEqual(got, values) {
				t.Fatalf("native input = %v, want %v", got, values)
			}
		})
	}
}

func TestStorageOptionsInputNilPresence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		initial    map[string]string
		hasInitial bool
		wantInput  bool
	}{
		{name: "absent", initial: nil, hasInitial: false, wantInput: false},
		{name: "present empty", initial: map[string]string{}, hasInitial: true, wantInput: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input *storageOptionsInput
			b := &bindings{
				storageAccessorNew: func(got *storageOptionsInput, provider uintptr) uintptr {
					if provider != 0 {
						t.Fatal("static accessor passed a provider")
					}
					input = got
					return 1
				},
				storageAccessorClose: func(uintptr) {},
			}
			accessor, err := newStorageOptionsAccessorWithPresence(b, tc.initial, tc.hasInitial, 0)
			if err != nil {
				t.Fatal(err)
			}
			accessor.Close()
			if (input != nil) != tc.wantInput {
				t.Fatalf("input presence = %v, want %v", input != nil, tc.wantInput)
			}
			if input != nil && input.Count != 0 {
				t.Fatalf("input count = %d, want 0", input.Count)
			}
		})
	}
}
