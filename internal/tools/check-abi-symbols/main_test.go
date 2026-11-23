package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lance-format/lance-go/internal/tools/headeridentity"
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

func testSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// testCheckerRoot writes a minimal repository whose bindings bind one symbol.
func testCheckerRoot(t *testing.T, headerSHA, identity string) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, root, "internal/ffi/upstream.go", fmt.Sprintf(`package ffi

const (
	upstreamRepo         = "https://github.com/lance-format/lance-c"
	upstreamRelease      = "v0.2.1-beta.3"
	upstreamCommit       = "a800fd5f28636bfff34778e3ad6663f3cf099a38"
	upstreamHeaderSHA256 = %q
	upstreamHeaderIdentitySHA256 = %q
)
`, headerSHA, identity))
	writeTestFile(t, root, "internal/ffi/bindings.go", `package ffi

type symbol struct {
	name string
	addr any
}

type bindings struct {
	lastErrorCode func() int32
}

func (b *bindings) symbols() []symbol {
	return []symbol{
		{"lance_last_error_code", &b.lastErrorCode},
	}
}
`)
	return root
}

func headerSetFixture(t *testing.T) (string, []byte, string) {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, dir, "lance.h", "/* umbrella */\n#include \"error.h\"\n")
	writeTestFile(t, dir, "error.h", "#include \"fwd.h\"\nLanceErrorCode lance_last_error_code(void);\n")
	writeTestFile(t, dir, "fwd.h", "typedef struct LanceDataset LanceDataset;\n")
	writeTestFile(t, dir, "lance.hpp", "// C++ only\n")

	files, err := headeridentity.Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	manifest, err := headeridentity.Encode(files)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	identity, err := headeridentity.Digest(files)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	return dir, manifest, identity
}

func TestRunAcceptsSplitHeaderDirectoryWithTypeOnlyFile(t *testing.T) {
	dir, manifest, identity := headerSetFixture(t)
	manifestPath := writeTestFile(t, t.TempDir(), "HEADER-MANIFEST", string(manifest))
	root := testCheckerRoot(t, strings.Repeat("0", 64), identity)

	if err := run(root, headerInput{path: dir, manifestPath: manifestPath}); err != nil {
		t.Fatalf("run split directory: %v", err)
	}
	if err := run(root, headerInput{path: dir, identity: identity}); err != nil {
		t.Fatalf("run split directory with identity only: %v", err)
	}
	if _, _, err := headeridentity.Verify(dir, manifest, identity); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestRunRejectsSplitHeaderSetProblems(t *testing.T) {
	dir, manifest, identity := headerSetFixture(t)

	t.Run("missing identity", func(t *testing.T) {
		root := testCheckerRoot(t, strings.Repeat("0", 64), "")
		if err := run(root, headerInput{path: dir}); err == nil || !strings.Contains(err.Error(), "requires an expected identity") {
			t.Fatalf("run error = %v", err)
		}
	})

	t.Run("missing header", func(t *testing.T) {
		if err := os.Remove(filepath.Join(dir, "fwd.h")); err != nil {
			t.Fatalf("remove fwd.h: %v", err)
		}
		t.Cleanup(func() { writeTestFile(t, dir, "fwd.h", "typedef struct LanceDataset LanceDataset;\n") })
		root := testCheckerRoot(t, strings.Repeat("0", 64), identity)
		manifestPath := writeTestFile(t, t.TempDir(), "HEADER-MANIFEST", string(manifest))
		if err := run(root, headerInput{path: dir, manifestPath: manifestPath}); err == nil || !strings.Contains(err.Error(), "missing include/lance/fwd.h") {
			t.Fatalf("run error = %v", err)
		}
	})

	t.Run("unexpected header", func(t *testing.T) {
		writeTestFile(t, dir, "extra.h", "int lance_extra(void);\n")
		t.Cleanup(func() { os.Remove(filepath.Join(dir, "extra.h")) })
		root := testCheckerRoot(t, strings.Repeat("0", 64), identity)
		if err := run(root, headerInput{path: dir, identity: identity}); err == nil || !strings.Contains(err.Error(), "header set identity") {
			t.Fatalf("run error = %v", err)
		}
	})

	t.Run("signature drift", func(t *testing.T) {
		changed := "LanceErrorCode lance_last_error_code(uint64_t changed);\n"
		writeTestFile(t, dir, "error.h", changed)
		t.Cleanup(func() {
			writeTestFile(t, dir, "error.h", "#include \"fwd.h\"\nLanceErrorCode lance_last_error_code(void);\n")
		})
		changedFiles := []headeridentity.File{
			{Path: "include/lance/error.h", SHA256: testSHA256([]byte(changed))},
			{Path: "include/lance/fwd.h", SHA256: testSHA256([]byte("typedef struct LanceDataset LanceDataset;\n"))},
			{Path: "include/lance/lance.h", SHA256: testSHA256([]byte("/* umbrella */\n#include \"error.h\"\n"))},
		}
		changedManifest, err := headeridentity.Encode(changedFiles)
		if err != nil {
			t.Fatalf("Encode changed: %v", err)
		}
		changedIdentity, err := headeridentity.Digest(changedFiles)
		if err != nil {
			t.Fatalf("Digest changed: %v", err)
		}
		root := testCheckerRoot(t, strings.Repeat("0", 64), changedIdentity)
		if err := run(root, headerInput{path: dir, manifestPath: writeTestFile(t, t.TempDir(), "HEADER-MANIFEST", string(changedManifest))}); err == nil ||
			!strings.Contains(err.Error(), "C takes 1 parameter(s), Go binding takes 0") {
			t.Fatalf("run error = %v, want a signature drift error", err)
		}
	})

	t.Run("duplicate declaration across files", func(t *testing.T) {
		writeTestFile(t, dir, "lance.h", "/* umbrella */\n#include \"error.h\"\nLanceErrorCode lance_last_error_code(void);\n")
		t.Cleanup(func() { writeTestFile(t, dir, "lance.h", "/* umbrella */\n#include \"error.h\"\n") })
		files, err := headeridentity.Scan(dir)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		duplicateManifest, err := headeridentity.Encode(files)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		duplicateIdentity, err := headeridentity.Digest(files)
		if err != nil {
			t.Fatalf("Digest: %v", err)
		}
		root := testCheckerRoot(t, strings.Repeat("0", 64), duplicateIdentity)
		if err := run(root, headerInput{path: dir, manifestPath: writeTestFile(t, t.TempDir(), "HEADER-MANIFEST", string(duplicateManifest))}); err == nil ||
			!strings.Contains(err.Error(), "duplicate declaration of lance_last_error_code") {
			t.Fatalf("run error = %v, want a duplicate declaration error", err)
		}
	})
}

func TestRunKeepsLegacySingleHeaderPath(t *testing.T) {
	header := []byte("/* legacy */\nLanceErrorCode lance_last_error_code(void);\n")
	dir := t.TempDir()
	headerPath := writeTestFile(t, dir, "lance.h", string(header))
	root := testCheckerRoot(t, testSHA256(header), "")
	if err := run(root, headerInput{path: headerPath}); err != nil {
		t.Fatalf("run legacy header: %v", err)
	}
	if err := run(root, headerInput{path: headerPath, identity: strings.Repeat("a", 64)}); err == nil ||
		!strings.Contains(err.Error(), "require -header to name an include/lance directory") {
		t.Fatalf("run legacy header with identity error = %v", err)
	}
	mismatched := testCheckerRoot(t, strings.Repeat("b", 64), "")
	if err := run(mismatched, headerInput{path: headerPath}); err == nil || !strings.Contains(err.Error(), "header sha256 is") {
		t.Fatalf("run legacy header with wrong pin error = %v", err)
	}
}
