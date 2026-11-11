package ffi

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

func TestAddBasesMarshalsBasePaths(t *testing.T) {
	emptyName := ""
	tests := []struct {
		name         string
		bases        []BasePath
		properties   map[string]string
		wantNilProps bool
		wantProps    map[string]string
	}{
		{
			name: "nil name and nil properties",
			bases: []BasePath{
				{ID: 0, Path: "s3://bucket/auto"},
				{ID: 42, Path: "s3://bucket/named", Name: ptrTo("named"), IsDatasetRoot: true},
			},
			properties:   nil,
			wantNilProps: true,
		},
		{
			name: "explicit empty name and sorted present properties",
			bases: []BasePath{
				{ID: 7, Path: "s3://bucket/empty", Name: &emptyName},
			},
			properties: map[string]string{"z": "last", "a": "first"},
			wantProps:  map[string]string{"a": "first", "z": "last"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			var gotBases []BasePath
			var gotProperties map[string]string
			propertiesNil := false
			b := &bindings{
				datasetAddBases: func(handle uintptr, bases []basePath, baseCount uint64, properties *transactionProperties) int32 {
					calls++
					runtime.GC()
					if handle != 7 {
						t.Errorf("handle = %d, want 7", handle)
					}
					if baseCount != uint64(len(tc.bases)) {
						t.Errorf("base count = %d, want %d", baseCount, len(tc.bases))
					}
					gotBases = make([]BasePath, len(bases))
					for i, raw := range bases {
						gotBases[i].ID = raw.ID
						gotBases[i].IsDatasetRoot = raw.IsDatasetRoot
						if raw.Path == 0 {
							t.Errorf("bases[%d].path is NULL", i)
						} else {
							gotBases[i].Path = goString((*byte)(unsafe.Pointer(raw.Path)))
						}
						if raw.Name != 0 {
							name := goString((*byte)(unsafe.Pointer(raw.Name)))
							gotBases[i].Name = &name
						}
					}
					if properties == nil {
						propertiesNil = true
						return 0
					}
					gotProperties = make(map[string]string, properties.Count)
					keys := unsafe.Slice(properties.Keys, properties.Count)
					values := unsafe.Slice(properties.Values, properties.Count)
					for i := range keys {
						gotProperties[goString(keys[i])] = goString(values[i])
					}
					return 0
				},
			}
			dataset := &Dataset{b: b}
			dataset.handle.Store(7)

			if err := dataset.AddBases(tc.bases, tc.properties); err != nil {
				t.Fatalf("AddBases: %v", err)
			}
			if calls != 1 {
				t.Fatalf("native calls = %d, want 1", calls)
			}
			if !reflect.DeepEqual(gotBases, tc.bases) {
				t.Fatalf("native bases = %+v, want %+v", gotBases, tc.bases)
			}
			if tc.wantNilProps {
				if !propertiesNil {
					t.Fatalf("native properties = %v, want NULL", gotProperties)
				}
				return
			}
			if propertiesNil {
				t.Fatalf("native properties were NULL, want %v", tc.wantProps)
			}
			if !reflect.DeepEqual(gotProperties, tc.wantProps) {
				t.Fatalf("native properties = %v, want %v", gotProperties, tc.wantProps)
			}
		})
	}
}

func TestAddBasesAndBasePathsClosed(t *testing.T) {
	dataset := &Dataset{b: &bindings{}}
	if _, err := dataset.BasePaths(); err != ErrClosed {
		t.Fatalf("BasePaths closed error = %v, want ErrClosed", err)
	}
	if err := dataset.AddBases(nil, nil); err != ErrClosed {
		t.Fatalf("AddBases closed error = %v, want ErrClosed", err)
	}
}

func TestBasePathsDecodesRegistryJSON(t *testing.T) {
	payload := append([]byte(`{"0":{"id":0,"name":null,"path":"s3://bucket/root","is_dataset_root":true},"7":{"id":7,"name":"","path":"s3://bucket/alt","is_dataset_root":false}}`), 0)
	frees := 0
	b := &bindings{
		datasetBasePathsJSON: func(uintptr) *byte {
			runtime.GC()
			return &payload[0]
		},
		freeString: func(*byte) { frees++ },
	}
	dataset := &Dataset{b: b}
	dataset.handle.Store(9)

	bases, err := dataset.BasePaths()
	runtime.KeepAlive(payload)
	if err != nil {
		t.Fatalf("BasePaths: %v", err)
	}
	if frees != 1 {
		t.Fatalf("native JSON frees = %d, want 1", frees)
	}
	emptyName := ""
	want := map[uint32]BasePath{
		0: {ID: 0, Path: "s3://bucket/root", IsDatasetRoot: true},
		7: {ID: 7, Path: "s3://bucket/alt", Name: &emptyName},
	}
	if !reflect.DeepEqual(bases, want) {
		t.Fatalf("base paths = %+v, want %+v", bases, want)
	}
}

func TestDecodeBasePathsRejectsInvalidRegistry(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{"malformed JSON", `{`, "decode dataset base paths"},
		{"non-numeric key", `{"base":{"id":0,"path":"s3://bucket/root","is_dataset_root":false}}`, "not a uint32 base ID"},
		{"mismatched id", `{"7":{"id":8,"path":"s3://bucket/alt","is_dataset_root":false}}`, "disagrees with entry id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeBasePaths([]byte(tc.payload))
			if err == nil {
				t.Fatalf("decodeBasePaths accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func ptrTo(value string) *string { return &value }
