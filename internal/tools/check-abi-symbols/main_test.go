package main

import (
	"slices"
	"testing"
)

func TestParseHeaderIgnoresDeclarationAttributes(t *testing.T) {
	header := []byte(`
#define LANCE_API __attribute__((visibility("default")))
#define LANCE_NODISCARD __attribute__((warn_unused_result))
#define LANCE_DEPRECATED(message) __attribute__((deprecated(message)))

LANCE_NODISCARD LANCE_API const char* lance_last_error_message(void);
LANCE_API LanceErrorCode lance_last_error_code(void);
LANCE_DEPRECATED("Use lance_dataset_prepare_fts_match_query() instead, it is (slightly) safer") LANCE_NODISCARD LANCE_API LanceFtsQueryContext* lance_dataset_prepare_fts_query(
    const char* query,
    int32_t limit);
`)

	funcs, err := parseHeader(header)
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}

	tests := []struct {
		name   string
		result string
		class  string
		params []string
	}{
		{"lance_last_error_message", "const char*", "pointer", nil},
		{"lance_last_error_code", "LanceErrorCode", "int32", nil},
		{"lance_dataset_prepare_fts_query", "LanceFtsQueryContext*", "pointer", []string{"const char* query", "int32_t limit"}},
	}
	for _, tc := range tests {
		got, ok := funcs[tc.name]
		if !ok {
			t.Fatalf("%s was not parsed as a declaration", tc.name)
		}
		if got.result != tc.result {
			t.Errorf("%s result = %q, want %q", tc.name, got.result, tc.result)
		}
		if class := cClass(got.result); class != tc.class {
			t.Errorf("%s class = %q, want %q", tc.name, class, tc.class)
		}
		if !slices.Equal(got.params, tc.params) {
			t.Errorf("%s params = %q, want %q", tc.name, got.params, tc.params)
		}
	}
}

func TestParseHeaderWithoutAttributesIsUnchanged(t *testing.T) {
	funcs, err := parseHeader([]byte("LanceErrorCode lance_last_error_code(void);\n"))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	got, ok := funcs["lance_last_error_code"]
	if !ok {
		t.Fatal("lance_last_error_code was not parsed as a declaration")
	}
	if got.result != "LanceErrorCode" || len(got.params) != 0 {
		t.Fatalf("declaration = %+v, want result LanceErrorCode and no parameters", got)
	}
}

func TestParseHeaderKeepsUnknownAttributesVisible(t *testing.T) {
	funcs, err := parseHeader([]byte("LANCE_ALWAYS_INLINE int32_t lance_foo(void);\n"))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	got, ok := funcs["lance_foo"]
	if !ok {
		t.Fatal("lance_foo was not parsed as a declaration")
	}
	if got.result != "LANCE_ALWAYS_INLINE int32_t" {
		t.Fatalf("result = %q, want the unknown attribute left in place", got.result)
	}
	if class := cClass(got.result); class != "unknown(LANCE_ALWAYS_INLINE int32_t)" {
		t.Fatalf("class = %q, want an unknown class so the check fails loudly", class)
	}
}

func TestStripDeclarationAttributes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"int32_t", "int32_t"},
		{"LANCE_API int32_t", "int32_t"},
		{"LANCE_NODISCARD LANCE_API const char*", "const char*"},
		{`LANCE_DEPRECATED("use lance_x() instead") LANCE_API int32_t`, "int32_t"},
		{`LANCE_DEPRECATED("close ) paren ( and \"quoted\"") int32_t`, "int32_t"},
		{"LANCE_APIX int32_t", "LANCE_APIX int32_t"},
		{"LANCE_DEPRECATED int32_t", "LANCE_DEPRECATED int32_t"},
		{`LANCE_DEPRECATED("unterminated int32_t`, `LANCE_DEPRECATED("unterminated int32_t`},
	}
	for _, tc := range tests {
		if got := stripDeclarationAttributes(tc.in); got != tc.want {
			t.Errorf("stripDeclarationAttributes(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
