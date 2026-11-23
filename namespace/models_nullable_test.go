package namespace_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lance-format/lance-go/namespace"
)

// The generated models carry Nullable[T] fields for the Option<Option<T>>
// fields that lance-namespace-reqwest-client 0.13.0 marks with
// serde_with::rust::double_option. A JSON value has three states that must
// survive both directions: absent (the zero Nullable, omitted when encoding),
// explicit null (present with a nil Value), and a concrete value, including
// zero values and empty collections.

func TestNullableConstructors(t *testing.T) {
	absent := namespace.Absent[int32]()
	if absent.Present || !absent.IsZero() {
		t.Fatalf("Absent = %#v, want an absent value", absent)
	}

	null := namespace.Null[int32]()
	if !null.Present || null.IsZero() || null.Value != nil {
		t.Fatalf("Null = %#v, want present with a nil Value", null)
	}

	zero := namespace.Some(int32(0))
	if !zero.Present || zero.IsZero() || zero.Value == nil || *zero.Value != 0 {
		t.Fatalf("Some(0) = %#v, want present with a zero Value", zero)
	}
}

func TestNullableHelperEncoding(t *testing.T) {
	// serde_json cannot tell a present JSON null apart from an absent inner
	// value, so the reference client also encodes a nil any as null.
	nilAny := any(nil)

	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"explicit null", namespace.Null[string](), `null`},
		{"empty string", namespace.Some(""), `""`},
		{"zero int32", namespace.Some(int32(0)), `0`},
		{"zero float64", namespace.Some(0.0), `0`},
		{"false", namespace.Some(false), `false`},
		{"empty list", namespace.Some([]string{}), `[]`},
		{"nil list", namespace.Some([]string(nil)), `[]`},
		{"empty map", namespace.Some(map[string]string{}), `{}`},
		{"nil map", namespace.Some(map[string]string(nil)), `{}`},
		{"zero nested model", namespace.Some(namespace.AlterVirtualColumnEntry{}), `{}`},
		{"nil any", namespace.Some[any](nilAny), `null`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := json.Marshal(testCase.value)
			if err != nil {
				t.Fatalf("marshal %#v: %v", testCase.value, err)
			}
			if string(encoded) != testCase.want {
				t.Fatalf("json.Marshal(%#v) = %s, want %s", testCase.value, encoded, testCase.want)
			}
		})
	}
}

type nullableModelEncodingCase struct {
	name  string
	model any
	want  string
}

var nullableModelEncodingCases = []nullableModelEncodingCase{
	{"absent", namespace.RefreshMaterializedViewRequest{}, `{}`},
	{
		"explicit null",
		namespace.RefreshMaterializedViewRequest{SrcVersion: namespace.Null[int32]()},
		`{"src_version":null}`,
	},
	{
		"zero int32",
		namespace.RefreshMaterializedViewRequest{SrcVersion: namespace.Some(int32(0))},
		`{"src_version":0}`,
	},
	{
		"empty string",
		namespace.RefreshMaterializedViewRequest{Manifest: namespace.Some("")},
		`{"manifest":""}`,
	},
	{
		"zero float64",
		namespace.MaterializedViewUdtfEntry{NumCpus: namespace.Some(0.0)},
		`{"kind":"","udtf":"","udtf_sha":"","udtf_name":"","udtf_version":"","num_cpus":0}`,
	},
	{
		"false",
		namespace.AlterColumnsEntry{Path: "c", Nullable: namespace.Some(false)},
		`{"path":"c","nullable":false}`,
	},
	{
		"empty list",
		namespace.AlterVirtualColumnEntry{InputColumns: namespace.Some([]string{})},
		`{"input_columns":[]}`,
	},
	{
		"nil list",
		namespace.AlterVirtualColumnEntry{InputColumns: namespace.Some([]string(nil))},
		`{"input_columns":[]}`,
	},
	{
		"zero nested model",
		namespace.AlterColumnsEntry{
			Path:          "c",
			VirtualColumn: namespace.Some(namespace.AlterVirtualColumnEntry{}),
		},
		`{"path":"c","virtual_column":{}}`,
	},
	{
		"null nested model",
		namespace.AlterColumnsEntry{
			Path:          "c",
			VirtualColumn: namespace.Null[namespace.AlterVirtualColumnEntry](),
		},
		`{"path":"c","virtual_column":null}`,
	},
}

func TestNullableModelEncoding(t *testing.T) {
	for _, testCase := range nullableModelEncodingCases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := json.Marshal(testCase.model)
			if err != nil {
				t.Fatalf("marshal %#v: %v", testCase.model, err)
			}
			if string(encoded) != testCase.want {
				t.Fatalf("json.Marshal(%#v) = %s, want %s", testCase.model, encoded, testCase.want)
			}
		})
	}
}

type nullableModelDecodingCase struct {
	name    string
	input   string
	present bool
	want    string
}

var refreshMaterializedViewDecodingCases = []nullableModelDecodingCase{
	{"absent", `{}`, false, `{}`},
	{"explicit null", `{"src_version":null}`, true, `{"src_version":null}`},
	{"zero", `{"src_version":0}`, true, `{"src_version":0}`},
	{"positive", `{"src_version":12}`, true, `{"src_version":12}`},
	{"negative", `{"src_version":-3}`, true, `{"src_version":-3}`},
}

func TestRefreshMaterializedViewRequestDecoding(t *testing.T) {
	for _, testCase := range refreshMaterializedViewDecodingCases {
		t.Run(testCase.name, func(t *testing.T) {
			var request namespace.RefreshMaterializedViewRequest
			if err := json.Unmarshal([]byte(testCase.input), &request); err != nil {
				t.Fatalf("unmarshal %s: %v", testCase.input, err)
			}
			if present := request.SrcVersion.Present; present != testCase.present {
				t.Fatalf("SrcVersion present = %t, want %t for %s", present, testCase.present, testCase.input)
			}
			encoded, err := json.Marshal(request)
			if err != nil {
				t.Fatalf("marshal decoded request: %v", err)
			}
			if string(encoded) != testCase.want {
				t.Fatalf("round trip of %s = %s, want %s", testCase.input, encoded, testCase.want)
			}
		})
	}
}

var alterVirtualColumnEntryDecodingCases = []nullableModelDecodingCase{
	{"absent", `{}`, false, `{}`},
	{"explicit null", `{"input_columns":null}`, true, `{"input_columns":null}`},
	{"empty list", `{"input_columns":[]}`, true, `{"input_columns":[]}`},
	{"one element", `{"input_columns":["a"]}`, true, `{"input_columns":["a"]}`},
}

func TestAlterVirtualColumnEntryDecoding(t *testing.T) {
	for _, testCase := range alterVirtualColumnEntryDecodingCases {
		t.Run(testCase.name, func(t *testing.T) {
			var entry namespace.AlterVirtualColumnEntry
			if err := json.Unmarshal([]byte(testCase.input), &entry); err != nil {
				t.Fatalf("unmarshal %s: %v", testCase.input, err)
			}
			if present := entry.InputColumns.Present; present != testCase.present {
				t.Fatalf("InputColumns present = %t, want %t for %s", present, testCase.present, testCase.input)
			}
			if testCase.name == "empty list" {
				columns := entry.InputColumns.Value
				if columns == nil || len(*columns) != 0 {
					t.Fatalf("empty list decoded as %#v, want a present empty list", columns)
				}
			}
			encoded, err := json.Marshal(entry)
			if err != nil {
				t.Fatalf("marshal decoded entry: %v", err)
			}
			if string(encoded) != testCase.want {
				t.Fatalf("round trip of %s = %s, want %s", testCase.input, encoded, testCase.want)
			}
		})
	}
}

// TestNullableDecodingResetsReusedDestinations pins the behavior that a model
// without its wrapper would lose: absent keys are not visited by
// encoding/json, so the model must decode into a fresh destination to clear
// values that the previous JSON document set.
func TestNullableDecodingResetsReusedDestinations(t *testing.T) {
	var request namespace.RefreshMaterializedViewRequest
	steps := []struct {
		input   string
		present bool
		value   *int32
	}{
		{`{"src_version":12}`, true, nullableValueOf(int32(12))},
		{`{"src_version":null}`, true, nil},
		{`{}`, false, nil},
		{`{"src_version":0}`, true, nullableValueOf(int32(0))},
		{`{}`, false, nil},
		{`{"src_version":-3}`, true, nullableValueOf(int32(-3))},
		{`{"src_version":null}`, true, nil},
	}
	for _, step := range steps {
		if err := json.Unmarshal([]byte(step.input), &request); err != nil {
			t.Fatalf("unmarshal %s: %v", step.input, err)
		}
		if present := request.SrcVersion.Present; present != step.present {
			t.Fatalf("after %s SrcVersion present = %t, want %t", step.input, present, step.present)
		}
		if !step.present {
			continue
		}
		switch {
		case step.value == nil && request.SrcVersion.Value != nil:
			t.Fatalf("after %s SrcVersion.Value = %v, want nil", step.input, *request.SrcVersion.Value)
		case step.value != nil && (request.SrcVersion.Value == nil || *request.SrcVersion.Value != *step.value):
			t.Fatalf("after %s SrcVersion.Value = %v, want %d", step.input, request.SrcVersion.Value, *step.value)
		}
	}

	entry := namespace.AlterColumnsEntry{Path: "c"}
	if err := json.Unmarshal([]byte(`{"path":"c","virtual_column":{"input_columns":["a"]}}`), &entry); err != nil {
		t.Fatalf("unmarshal nested value: %v", err)
	}
	if entry.VirtualColumn.IsZero() || entry.VirtualColumn.Value == nil {
		t.Fatalf("nested value = %#v, want a present nested model", entry.VirtualColumn)
	}
	if err := json.Unmarshal([]byte(`{"path":"c"}`), &entry); err != nil {
		t.Fatalf("unmarshal absent nested value: %v", err)
	}
	if !entry.VirtualColumn.IsZero() {
		t.Fatalf("nested value = %#v after decoding an absent field, want absent", entry.VirtualColumn)
	}
}

// TestReusedDestinationsResetScope pins which fields a reused destination
// clears: a model that carries a Nullable field replaces the whole value, while
// a model without one keeps encoding/json's behavior of leaving omitted fields
// untouched.
func TestReusedDestinationsResetScope(t *testing.T) {
	entry := namespace.AddColumnsEntry{}
	if err := json.Unmarshal([]byte(`{"name":"old","computed":"c"}`), &entry); err != nil {
		t.Fatalf("unmarshal first document: %v", err)
	}
	if entry.Name != "old" {
		t.Fatalf("Name = %q after the first document, want old", entry.Name)
	}
	if err := json.Unmarshal([]byte(`{"expression":"e"}`), &entry); err != nil {
		t.Fatalf("unmarshal second document: %v", err)
	}
	if entry.Name != "" {
		t.Fatalf("Name = %q after a document without name, want the zero value", entry.Name)
	}
	if !entry.Computed.IsZero() {
		t.Fatalf("Computed = %#v after a document without computed, want absent", entry.Computed)
	}

	versionRange := namespace.VersionRange{StartVersion: 3, EndVersion: 9}
	if err := json.Unmarshal([]byte(`{"start_version":1}`), &versionRange); err != nil {
		t.Fatalf("unmarshal version range: %v", err)
	}
	if versionRange.EndVersion != 9 {
		t.Fatalf("EndVersion = %d after a document without end_version, want 9", versionRange.EndVersion)
	}
}

// nullableModels returns every model that carries a Nullable field. Each call
// returns fresh pointers so a test or fuzz case never observes another one's
// decoded state.
func nullableModels() []any {
	return []any{
		&namespace.AddColumnsEntry{},
		&namespace.AddVirtualColumnEntry{},
		&namespace.AlterColumnsEntry{},
		&namespace.AlterTableBackfillColumnsRequest{},
		&namespace.AlterVirtualColumnEntry{},
		&namespace.CreateMaterializedViewRequest{},
		&namespace.CreateMaterializedViewResponse{},
		&namespace.MaterializedViewUdtfEntry{},
		&namespace.RefreshMaterializedViewRequest{},
		&namespace.UpdateFieldMetadataEntry{},
	}
}

// TestNullableModelsResetReusedDestinations runs the reuse contract over every
// model that carries a Nullable field: every Nullable field is present after
// decoding an object that names each one, and absent after decoding an empty
// object into the same destination.
func TestNullableModelsResetReusedDestinations(t *testing.T) {
	models := nullableModels()

	var modelNames []string
	for _, model := range models {
		modelName := reflect.TypeOf(model).Elem().Name()
		modelNames = append(modelNames, modelName)

		fields := nullableFields(reflect.ValueOf(model).Elem())
		if len(fields) == 0 {
			t.Fatalf("%s has no Nullable field", modelName)
		}
		document := map[string]any{}
		for _, index := range fields {
			name := strings.Split(reflect.TypeOf(model).Elem().Field(index).Tag.Get("json"), ",")[0]
			if name == "" {
				t.Fatalf("%s has a Nullable field without a JSON name", modelName)
			}
			document[name] = nil
		}
		encoded, err := json.Marshal(document)
		if err != nil {
			t.Fatalf("marshal %s document: %v", modelName, err)
		}
		if err := json.Unmarshal(encoded, model); err != nil {
			t.Fatalf("unmarshal %s: %v", modelName, err)
		}
		for _, index := range fields {
			if !present(t, reflect.ValueOf(model).Elem().Field(index)) {
				t.Fatalf("%s field %d not present after decoding %s", modelName, index, encoded)
			}
		}
		if err := json.Unmarshal([]byte(`{}`), model); err != nil {
			t.Fatalf("unmarshal empty %s: %v", modelName, err)
		}
		for _, index := range fields {
			if present(t, reflect.ValueOf(model).Elem().Field(index)) {
				t.Fatalf("%s field %d stayed present after decoding an empty object", modelName, index)
			}
		}
	}

	if declared := parseNullableModels(t, "models.go"); !slices.Equal(slices.Sorted(slices.Values(modelNames)), declared) {
		t.Fatalf("tested models = %v, models.go declares %v", modelNames, declared)
	}
}

func nullableValueOf[T any](value T) *T {
	return &value
}

func present(t *testing.T, field reflect.Value) bool {
	t.Helper()
	zeroer, ok := field.Interface().(interface{ IsZero() bool })
	if !ok {
		t.Fatalf("field %s does not implement IsZero", field.Type())
	}
	return !zeroer.IsZero()
}

func nullableFields(model reflect.Value) []int {
	var indexes []int
	for i := 0; i < model.NumField(); i++ {
		if isNullableField(model.Type().Field(i).Type) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

// TestNullableModelsKeepPresenceDecoders guards the manual pairing between the
// generated models and the hand-written wrappers. A model that gains a Nullable
// field without a wrapper, or keeps a wrapper after losing its last Nullable
// field, would change how reused destinations decode.
func TestNullableModelsKeepPresenceDecoders(t *testing.T) {
	models := parseNullableModels(t, "models.go")
	decoders := parsePresenceDecoders(t, "models_nullable.go")
	if !slices.Equal(models, decoders) {
		t.Fatalf("models with Nullable fields = %v, presence decoders = %v", models, decoders)
	}
	if len(models) == 0 {
		t.Fatal("no model with a Nullable field was found; the parser no longer matches models.go")
	}
}

func parseNullableModels(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var names []string
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range structType.Fields.List {
			if isNullableExpr(field.Type) {
				names = append(names, spec.Name.Name)
				break
			}
		}
		return true
	})
	return slices.Sorted(slices.Values(names))
}

func parsePresenceDecoders(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var names []string
	ast.Inspect(file, func(node ast.Node) bool {
		decl, ok := node.(*ast.FuncDecl)
		if !ok || decl.Name.Name != "UnmarshalJSON" || decl.Recv == nil || len(decl.Recv.List) != 1 {
			return true
		}
		pointer, ok := decl.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			return true
		}
		if receiver, ok := pointer.X.(*ast.Ident); ok {
			names = append(names, receiver.Name)
		}
		return true
	})
	return slices.Sorted(slices.Values(names))
}

func isNullableField(typ reflect.Type) bool {
	return typ.PkgPath() == "github.com/lance-format/lance-go/namespace" && strings.HasPrefix(typ.Name(), "Nullable[")
}

func isNullableExpr(expr ast.Expr) bool {
	instantiation, ok := expr.(*ast.IndexExpr)
	if !ok {
		return false
	}
	identifier, ok := instantiation.X.(*ast.Ident)
	return ok && identifier.Name == "Nullable"
}
