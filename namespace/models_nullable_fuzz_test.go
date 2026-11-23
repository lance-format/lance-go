package namespace_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzNullableModelRoundTrip builds a canonical document for a model, a
// Nullable field and one of the three documented states, decodes it, and checks
// the result against that independent input.
//
// The parameters only select supported model shapes, fields and values; the
// document itself is constructed with encoding/json. A decoder that drops or
// alters the input therefore fails the first-decode assertions instead of
// agreeing with itself on a later round trip.
func FuzzNullableModelRoundTrip(f *testing.F) {
	for _, seed := range nullableRoundTripSeeds() {
		f.Add(seed.modelIndex, seed.fieldIndex, seed.state, seed.value)
	}

	f.Fuzz(func(t *testing.T, modelIndex, fieldIndex, state uint8, value string) {
		models := nullableModels()
		if int(modelIndex) >= len(models) || state > 2 {
			t.Skip()
		}
		model := models[modelIndex]
		modelValue := reflect.ValueOf(model).Elem()
		fields := nullableFields(modelValue)
		if int(fieldIndex) >= len(fields) {
			t.Skip()
		}
		field := modelValue.Field(fields[fieldIndex])
		name := nullableJSONName(modelValue.Type().Field(fields[fieldIndex]))
		modelName := modelValue.Type().Name()

		document := map[string]json.RawMessage{}
		var wantValue any
		switch state {
		case 1:
			document[name] = json.RawMessage("null")
		case 2:
			raw, wanted, ok := nullableValueJSON(field, value)
			if !ok {
				t.Skipf("unsupported value %q for %s.%s", value, modelName, name)
			}
			document[name] = raw
			wantValue = wanted
		}

		encoded, err := json.Marshal(document)
		if err != nil {
			t.Fatalf("marshal canonical document: %v", err)
		}
		decoded := reflect.New(modelValue.Type()).Interface()
		if err := json.Unmarshal(encoded, decoded); err != nil {
			t.Fatalf("%s did not decode canonical document %s: %v", modelName, encoded, err)
		}
		target := reflect.ValueOf(decoded).Elem().Field(fields[fieldIndex])
		targetValue := target.FieldByName("Value")

		wantPresent := state != 0
		if got := present(t, target); got != wantPresent {
			t.Fatalf("%s.%s present = %t after decoding %s, want %t", modelName, name, got, encoded, wantPresent)
		}
		switch state {
		case 1:
			if !targetValue.IsNil() {
				t.Fatalf("%s.%s value = %v after decoding %s, want null", modelName, name, targetValue.Interface(), encoded)
			}
		case 2:
			if targetValue.IsNil() {
				t.Fatalf("%s.%s value is nil after decoding %s, want %#v", modelName, name, encoded, wantValue)
			}
			if got := targetValue.Elem().Interface(); !reflect.DeepEqual(got, wantValue) {
				t.Fatalf("%s.%s value = %#v after decoding %s, want %#v", modelName, name, got, encoded, wantValue)
			}
		}

		reencoded, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("marshal decoded %s: %v", modelName, err)
		}
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(reencoded, &wire); err != nil {
			t.Fatalf("%s did not encode as a JSON object: %v", modelName, err)
		}
		raw, encodedField := wire[name]
		if !wantPresent {
			if encodedField {
				t.Fatalf("%s.%s is absent but encoded as %s", modelName, name, raw)
			}
			return
		}
		if !encodedField {
			t.Fatalf("%s.%s is present but was omitted from %s", modelName, name, reencoded)
		}
		if state == 1 {
			if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				t.Fatalf("%s.%s null encoded as %s", modelName, name, raw)
			}
			return
		}
		wantRaw, err := json.Marshal(wantValue)
		if err != nil {
			t.Fatalf("marshal expected %s.%s value: %v", modelName, name, err)
		}
		if !bytes.Equal(bytes.TrimSpace(raw), wantRaw) {
			t.Fatalf("%s.%s encoded as %s, want %s", modelName, name, raw, wantRaw)
		}
	})
}

type nullableRoundTripSeed struct {
	modelIndex uint8
	fieldIndex uint8
	state      uint8
	value      string
}

// nullableRoundTripSeeds covers every model and every Nullable field in the
// absent, null and populated states.
func nullableRoundTripSeeds() []nullableRoundTripSeed {
	var seeds []nullableRoundTripSeed
	for modelIndex, model := range nullableModels() {
		modelValue := reflect.ValueOf(model).Elem()
		for fieldIndex, index := range nullableFields(modelValue) {
			valueType := modelValue.Field(index).FieldByName("Value").Type().Elem()
			value := nullableSeedValue(valueType)
			for state := uint8(0); state <= 2; state++ {
				seeds = append(seeds, nullableRoundTripSeed{
					modelIndex: uint8(modelIndex),
					fieldIndex: uint8(fieldIndex),
					state:      state,
					value:      value,
				})
			}
		}
	}
	return seeds
}

// nullableSeedValue returns a supported value for the field's element type.
func nullableSeedValue(valueType reflect.Type) string {
	switch valueType.Kind() {
	case reflect.Bool:
		return "true"
	case reflect.Int32, reflect.Int64, reflect.Float64:
		return "12"
	case reflect.String:
		return "seed"
	case reflect.Slice:
		return "a,b"
	default:
		return ""
	}
}

// nullableValueJSON turns value into the JSON encoding of a supported value for
// the Nullable field's element type, returning both the wire form and the value
// the first decode must produce.
func nullableValueJSON(field reflect.Value, value string) (json.RawMessage, any, bool) {
	valueType := field.FieldByName("Value").Type().Elem()
	var typed any
	switch valueType.Kind() {
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, nil, false
		}
		typed = parsed
	case reflect.Int32:
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return nil, nil, false
		}
		typed = int32(parsed)
	case reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, nil, false
		}
		typed = parsed
	case reflect.Float64:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, nil, false
		}
		typed = parsed
	case reflect.String:
		if !utf8.ValidString(value) {
			return nil, nil, false
		}
		typed = value
	case reflect.Slice:
		if valueType.Elem().Kind() != reflect.String {
			return nil, nil, false
		}
		items := []string{}
		if value != "" {
			items = strings.Split(value, ",")
		}
		for _, item := range items {
			if !utf8.ValidString(item) {
				return nil, nil, false
			}
		}
		typed = items
	case reflect.Struct:
		typed = reflect.New(valueType).Elem().Interface()
	case reflect.Interface:
		typed = map[string]any{}
	default:
		return nil, nil, false
	}

	raw, err := json.Marshal(typed)
	if err != nil {
		return nil, nil, false
	}
	return raw, typed, true
}

func nullableJSONName(field reflect.StructField) string {
	return strings.Split(field.Tag.Get("json"), ",")[0]
}
