package ffi

import "testing"

func TestCreateFragmentFromFilePhysicalRowsPresence(t *testing.T) {
	zero, explicit := uint64(0), uint64(17)
	for _, tc := range []struct {
		name    string
		rows    *uint64
		present bool
		value   uint64
	}{
		{"infer", nil, false, 0},
		{"explicit zero", &zero, true, 0},
		{"explicit rows", &explicit, true, 17},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called, closed := false, false
			b := &bindings{
				fragmentCreateFromFile: func(dataset uintptr, filename string, id uint64, rows *uint64, out *uintptr) int32 {
					called = true
					if dataset != 3 || filename != "part.lance" || id != 42 {
						t.Fatal("fragment file identity was not forwarded")
					}
					if (rows != nil) != tc.present {
						t.Fatalf("physicalRows presence = %v, want %v", rows != nil, tc.present)
					}
					if rows != nil && *rows != tc.value {
						t.Fatalf("physicalRows = %d, want %d", *rows, tc.value)
					}
					*out = 9
					return 0
				},
				fragmentClose: func(handle uintptr) {
					if handle != 9 || closed {
						t.Fatal("returned fragment must be released exactly once")
					}
					closed = true
				},
			}
			dataset := &Dataset{b: b}
			dataset.handle.Store(3)
			fragment, err := CreateFragmentFromFile(dataset, "part.lance", 42, tc.rows)
			if err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("native constructor was not called")
			}
			fragment.Close()
			if !closed {
				t.Fatal("returned fragment was not released")
			}
		})
	}
}
