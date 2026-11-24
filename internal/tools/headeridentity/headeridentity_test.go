package headeridentity

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHeader(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func fixtureHeaders(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeHeader(t, dir, "lance.h", "/* umbrella */\n#include \"error.h\"\n")
	writeHeader(t, dir, "error.h", "int lance_last_error_code(void);\n")
	writeHeader(t, dir, "fwd.h", "typedef struct LanceDataset LanceDataset;\n")
	writeHeader(t, dir, "lance.hpp", "// C++ only\n")
	writeHeader(t, dir, "notes.txt", "not part of the identity\n")
	return dir
}

func TestScanIncludesTypeOnlyHeadersAndIgnoresHPP(t *testing.T) {
	dir := fixtureHeaders(t)
	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := []string{"include/lance/error.h", "include/lance/fwd.h", "include/lance/lance.h"}
	if len(files) != len(want) {
		t.Fatalf("Scan returned %d files, want %d: %+v", len(files), len(want), files)
	}
	for i, path := range want {
		if files[i].Path != path {
			t.Fatalf("Scan[%d].Path = %q, want %q", i, files[i].Path, path)
		}
	}
	if files[1].SHA256 != sha256Hex("typedef struct LanceDataset LanceDataset;\n") {
		t.Fatalf("type-only header digest is %q", files[1].SHA256)
	}
}

func TestScanRequiresUmbrellaAndIgnoresSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeHeader(t, dir, "error.h", "int lance_last_error_code(void);\n")
	if _, err := Scan(dir); err == nil || !strings.Contains(err.Error(), "does not contain include/lance/lance.h") {
		t.Fatalf("Scan without lance.h error = %v", err)
	}

	dir = fixtureHeaders(t)
	if err := os.MkdirAll(filepath.Join(dir, "detail", "inline"), 0o755); err != nil {
		t.Fatalf("create detail directory: %v", err)
	}
	writeHeader(t, filepath.Join(dir, "detail"), "declarations.hpp", "// C++ only\n")
	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan with detail/: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("Scan with detail/ returned %d files, want 3: %+v", len(files), files)
	}
	for _, file := range files {
		if strings.Contains(file.Path, "detail/") {
			t.Fatalf("Scan included an implementation header: %s", file.Path)
		}
	}
}

func TestEncodeAndDigestAreCanonical(t *testing.T) {
	files := []File{
		{Path: "include/lance/lance.h", SHA256: sha256Hex("umbrella")},
		{Path: "include/lance/error.h", SHA256: sha256Hex("error")},
	}
	text, err := Encode(files)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := "include/lance/error.h " + sha256Hex("error") + "\n" +
		"include/lance/lance.h " + sha256Hex("umbrella") + "\n"
	if string(text) != want {
		t.Fatalf("Encode = %q, want %q", text, want)
	}
	digest, err := Digest(files)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if digest != sha256Hex(want) {
		t.Fatalf("Digest = %q, want %q", digest, sha256Hex(want))
	}
}

func TestParseIsStrict(t *testing.T) {
	files, err := Scan(fixtureHeaders(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	text, err := Encode(files)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := Parse(text); err != nil {
		t.Fatalf("Parse(valid): %v", err)
	}

	cases := map[string]struct {
		text string
		want string
	}{
		"empty":               {"", "empty"},
		"no trailing newline": {strings.TrimSuffix(string(text), "\n"), "end with a newline"},
		"crlf":                {strings.ReplaceAll(string(text), "\n", "\r\n"), "LF line endings"},
		"tab":                 {strings.Replace(string(text), " ", "\t", 1), "one space"},
		"extra space":         {strings.Replace(string(text), " ", "  ", 1), "not \"<path> <sha256>\""},
		"blank line":          {"\n" + string(text), "line 1 is empty"},
		"not sorted":          {reverseLines(string(text)), "not sorted"},
		"missing umbrella":    {strings.Replace(string(text), "include/lance/lance.h "+sha256Hex("/* umbrella */\n#include \"error.h\"\n")+"\n", "", 1), "does not contain"},
		"hpp path":            {string(text) + "include/lance/lance.hpp " + strings.Repeat("a", 64) + "\n", "not a C header"},
		"absolute path":       {string(text) + "/include/lance/extra.h " + strings.Repeat("a", 64) + "\n", "not under"},
		"nested path":         {string(text) + "include/lance/sub/extra.h " + strings.Repeat("a", 64) + "\n", "not a direct child"},
		"bad digest":          {strings.Replace(string(text), files[0].SHA256, strings.Repeat("A", 64), 1), "lowercase SHA-256"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.text)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func reverseLines(text string) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return strings.Join(lines, "\n") + "\n"
}

func TestVerifyDetectsMissingChangedAndUnexpected(t *testing.T) {
	dir := fixtureHeaders(t)
	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	manifest, err := Encode(files)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	identity, err := Digest(files)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	if _, got, err := Verify(dir, manifest, identity); err != nil {
		t.Fatalf("Verify(valid): %v", err)
	} else if got != identity {
		t.Fatalf("Verify identity = %q, want %q", got, identity)
	}

	writeHeader(t, dir, "error.h", "int lance_last_error_code(int changed);\n")
	writeHeader(t, dir, "unexpected.h", "int lance_unexpected(void);\n")
	if err := os.Remove(filepath.Join(dir, "fwd.h")); err != nil {
		t.Fatalf("remove fwd.h: %v", err)
	}
	_, _, err = Verify(dir, manifest, identity)
	if err == nil {
		t.Fatal("Verify accepted a changed header set")
	}
	for _, want := range []string{
		"missing include/lance/fwd.h",
		"unexpected include/lance/unexpected.h",
		"changed include/lance/error.h",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Verify error %q does not mention %q", err, want)
		}
	}
}

func TestVerifyIdentityOnly(t *testing.T) {
	dir := fixtureHeaders(t)
	files, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	identity, err := Digest(files)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if _, _, err := Verify(dir, nil, identity); err != nil {
		t.Fatalf("Verify identity only: %v", err)
	}
	if _, _, err := Verify(dir, nil, strings.Repeat("0", 64)); err == nil ||
		!strings.Contains(err.Error(), "header set identity") {
		t.Fatalf("Verify accepted a wrong identity: %v", err)
	}
	if _, _, err := Verify(dir, nil, ""); err == nil || !strings.Contains(err.Error(), "no expected") {
		t.Fatalf("Verify without an expectation error = %v", err)
	}
	writeHeader(t, dir, "extra.h", "int lance_extra(void);\n")
	if _, _, err := Verify(dir, nil, identity); err == nil {
		t.Fatal("a digest-only check accepted an unexpected header")
	}
}
