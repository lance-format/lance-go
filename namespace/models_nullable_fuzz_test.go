package namespace_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// FuzzNullableModelRoundTrip checks the serialization contract for every
// document that decodes into a model with Nullable fields: re-encoding the
// decoded model is stable, every Nullable field keeps its presence, and a
// present field keeps its null marker or value.
//
// Documents that do not decode into a model are skipped. This target covers
// valid modeling behavior, not parser hardening.
func FuzzNullableModelRoundTrip(f *testing.F) {
	for _, seed := range nullableRoundTripSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, document []byte) {
		for _, model := range nullableModels() {
			if err := json.Unmarshal(document, model); err != nil {
				continue
			}
			checkNullableModelRoundTrip(t, model)
		}
	})
}

// nullableRoundTripSeeds reuses the encoding and decoding cases already pinned
// for the Nullable models, so the fuzz corpus does not maintain a second set of
// fixtures.
func nullableRoundTripSeeds() [][]byte {
	var seeds [][]byte
	for _, testCase := range nullableModelEncodingCases {
		seeds = append(seeds, []byte(testCase.want))
	}
	for _, testCase := range refreshMaterializedViewDecodingCases {
		seeds = append(seeds, []byte(testCase.input))
	}
	for _, testCase := range alterVirtualColumnEntryDecodingCases {
		seeds = append(seeds, []byte(testCase.input))
	}
	return seeds
}

func checkNullableModelRoundTrip(t *testing.T, model any) {
	t.Helper()
	first, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("marshal %T: %v", model, err)
	}
	decoded := reflect.New(reflect.TypeOf(model).Elem()).Interface()
	if err := json.Unmarshal(first, decoded); err != nil {
		t.Fatalf("unmarshal %T round trip: %v", model, err)
	}
	second, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal %T after round trip: %v", model, err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("%T wire changed after round trip:\nfirst:  %s\nsecond: %s", model, first, second)
	}

	var wire map[string]json.RawMessage
	if err := json.Unmarshal(first, &wire); err != nil {
		t.Fatalf("%T did not encode as a JSON object: %v", model, err)
	}
	source := reflect.ValueOf(model).Elem()
	target := reflect.ValueOf(decoded).Elem()
	for _, index := range nullableFields(t, source) {
		name := strings.Split(source.Type().Field(index).Tag.Get("json"), ",")[0]
		sourceField := source.Field(index)
		if got := target.Field(index).Interface(); !reflect.DeepEqual(sourceField.Interface(), got) {
			t.Fatalf("%T.%s changed after round trip: %#v -> %#v", model, name, sourceField.Interface(), got)
		}

		raw, encoded := wire[name]
		if !present(t, sourceField) {
			if encoded {
				t.Fatalf("%T.%s is absent but encoded as %s", model, name, raw)
			}
			continue
		}
		if !encoded {
			t.Fatalf("%T.%s is present but was omitted from %s", model, name, first)
		}
		isNull := sourceField.FieldByName("Value").IsNil()
		if isNull != bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			t.Fatalf("%T.%s null marker does not match the decoded value: %s", model, name, raw)
		}
	}
}
