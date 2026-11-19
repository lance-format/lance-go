package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lance-format/lance-go/internal/tools/artifactmeta"
	"github.com/ulikunitz/xz"
)

const testPinFile = `package ffi

const (
	upstreamRepo         = "https://github.com/lancedb/lance-c"
	upstreamRelease      = "v0.2.0-beta.1"
	upstreamCommit       = "e2564b89be36bb18f5beff6d673daf9dfff8c986"
	upstreamHeaderSHA256 = "90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d"
)
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestReadPin(t *testing.T) {
	dir := t.TempDir()
	pinned, err := readPin(writeFile(t, dir, "upstream.go", testPinFile))
	if err != nil {
		t.Fatalf("readPin: %v", err)
	}
	if pinned.repo != "https://github.com/lancedb/lance-c" ||
		pinned.release != "v0.2.0-beta.1" ||
		pinned.commit != "e2564b89be36bb18f5beff6d673daf9dfff8c986" ||
		pinned.headerSHA != "90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d" {
		t.Fatalf("unexpected pin: %+v", pinned)
	}

	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "missing release",
			content: strings.Replace(testPinFile, "\tupstreamRelease      = \"v0.2.0-beta.1\"\n", "", 1),
			want:    "missing upstreamRelease",
		},
		{
			name:    "duplicate commit",
			content: testPinFile + "\nconst upstreamCommit = \"e2564b89be36bb18f5beff6d673daf9dfff8c986\"\n",
			want:    "declares upstreamCommit more than once",
		},
		{
			name:    "short commit",
			content: strings.Replace(testPinFile, "e2564b89be36bb18f5beff6d673daf9dfff8c986", "e2564b89", 1),
			want:    "upstreamCommit",
		},
		{
			name:    "plain http repo",
			content: strings.Replace(testPinFile, "https://github.com", "http://github.com", 1),
			want:    "upstreamRepo",
		},
		{
			name:    "tag with a path separator",
			content: strings.Replace(testPinFile, `"v0.2.0-beta.1"`, `"release/v0.2.0-beta.1"`, 1),
			want:    "upstreamRelease",
		},
		{
			name:    "uppercase header hash",
			content: strings.Replace(testPinFile, "90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d", "90E52D19F7CEDB6CD52A83024927453F0DED5F13E24ED2ADAEA8684CD9D4B0D8D", 1),
			want:    "upstreamHeaderSHA256",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := readPin(writeFile(t, t.TempDir(), "upstream.go", testCase.content))
			if err == nil {
				t.Fatal("readPin accepted an invalid pin")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %q does not mention %q", err, testCase.want)
			}
		})
	}
}

func TestResolveTarget(t *testing.T) {
	cases := map[string]struct {
		triple  string
		library string
		format  string
		machine string
		imports string
	}{
		"linux/amd64":   {"x86_64-unknown-linux-gnu", "./lib/liblance_c.so", "ELF64", "x86-64", ""},
		"linux/arm64":   {"aarch64-unknown-linux-gnu", "./lib/liblance_c.so", "ELF64", "AArch64", ""},
		"darwin/arm64":  {"aarch64-apple-darwin", "./lib/liblance_c.dylib", "Mach-O64", "arm64", ""},
		"windows/amd64": {"x86_64-pc-windows-msvc", "./lib/lance_c.dll", "PE32+", "x86-64", "./lib/lance_c.dll.lib"},
		"windows/arm64": {"aarch64-pc-windows-msvc", "./lib/lance_c.dll", "PE32+", "ARM64", "./lib/lance_c.dll.lib"},
	}
	for name, want := range cases {
		spec, err := resolveTarget(name)
		if err != nil {
			t.Fatalf("resolveTarget(%q): %v", name, err)
		}
		if spec.triple != want.triple || spec.libraryMember != want.library ||
			spec.format != want.format || spec.machine != want.machine || spec.importLibraryMember != want.imports {
			t.Fatalf("resolveTarget(%q) = %+v, want %+v", name, spec, want)
		}
	}
	if _, err := resolveTarget("darwin/amd64"); err == nil || !strings.Contains(err.Error(), "supported targets") {
		t.Fatalf("resolveTarget accepted a retired target: %v", err)
	}
}

func TestParsePublishedSHA256(t *testing.T) {
	const asset = "lance-c-v0.2.0-beta.1-x86_64-unknown-linux-gnu.tar.xz"
	const digest = "76df5338b16e02627e715a3bbe6df554ce7e280e2c2d7497fa01ff76bea7ebf5"

	for name, content := range map[string]string{
		"text form":                digest + "  " + asset + "\n",
		"binary form":              digest + " *" + asset + "\n",
		"crlf line endings":        digest + "  " + asset + "\r\n",
		"missing trailing newline": digest + "  " + asset,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parsePublishedSHA256([]byte(content), asset)
			if err != nil {
				t.Fatalf("parsePublishedSHA256: %v", err)
			}
			if got != digest {
				t.Fatalf("got %q, want %q", got, digest)
			}
		})
	}

	cases := map[string]struct {
		content string
		want    string
	}{
		"other asset":   {digest + "  lance-c-v0.2.0-beta.1-aarch64-apple-darwin.tar.xz\n", "do not belong together"},
		"two lines":     {digest + "  " + asset + "\n" + digest + "  other.tar.xz\n", "one checksum line"},
		"short digest":  {"abcd  " + asset + "\n", "lowercase SHA-256"},
		"single field":  {digest + "\n", "malformed checksum line"},
		"empty content": {"", "checksum file is empty"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parsePublishedSHA256([]byte(testCase.content), asset)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("parsePublishedSHA256 error = %v, want one mentioning %q", err, testCase.want)
			}
		})
	}
}

func TestCanonicalHeader(t *testing.T) {
	canonical := []byte("/* header */\nint lance_open(void);\nint lance_close(void);\n")
	pinSHA := sha256Hex(canonical)

	t.Run("canonical lf header", func(t *testing.T) {
		path := writeFile(t, t.TempDir(), "lance.h", string(canonical))
		gotSHA, archiveSHA, lineEndings, err := canonicalHeader(path, pinSHA)
		if err != nil {
			t.Fatalf("canonicalHeader: %v", err)
		}
		if gotSHA != pinSHA || archiveSHA != pinSHA || lineEndings != "lf" {
			t.Fatalf("got sha=%s archive=%s endings=%s", gotSHA, archiveSHA, lineEndings)
		}
	})

	t.Run("windows crlf header", func(t *testing.T) {
		crlf := bytes.ReplaceAll(canonical, []byte("\n"), []byte("\r\n"))
		path := writeFile(t, t.TempDir(), "lance.h", string(crlf))
		gotSHA, archiveSHA, lineEndings, err := canonicalHeader(path, pinSHA)
		if err != nil {
			t.Fatalf("canonicalHeader: %v", err)
		}
		if gotSHA != pinSHA {
			t.Fatalf("canonical sha is %s, want %s", gotSHA, pinSHA)
		}
		if archiveSHA != sha256Hex(crlf) {
			t.Fatalf("archive sha is %s, want %s", archiveSHA, sha256Hex(crlf))
		}
		if lineEndings != "crlf" {
			t.Fatalf("line endings are %q, want crlf", lineEndings)
		}
		written, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read rewritten header: %v", err)
		}
		if !bytes.Equal(written, canonical) {
			t.Fatal("header was not rewritten in canonical LF form")
		}
	})

	t.Run("different header", func(t *testing.T) {
		path := writeFile(t, t.TempDir(), "lance.h", string(canonical)+"int lance_extra(void);\n")
		_, _, _, err := canonicalHeader(path, pinSHA)
		if err == nil || !strings.Contains(err.Error(), "does not contain the pinned header") {
			t.Fatalf("canonicalHeader error = %v", err)
		}
	})
}

func syntheticELF(machine uint16) []byte {
	header := make([]byte, 64)
	copy(header, []byte{0x7f, 'E', 'L', 'F'})
	header[4] = 2 // 64-bit
	header[5] = 1 // little endian
	binary.LittleEndian.PutUint16(header[16:], 3)
	binary.LittleEndian.PutUint16(header[18:], machine)
	return header
}

func syntheticMachO(cpuType uint32) []byte {
	header := make([]byte, 32)
	copy(header, []byte{0xcf, 0xfa, 0xed, 0xfe})
	binary.LittleEndian.PutUint32(header[4:], cpuType)
	binary.LittleEndian.PutUint32(header[12:], 6)
	return header
}

func syntheticPE(machine uint16) []byte {
	header := make([]byte, 0x200)
	header[0] = 'M'
	header[1] = 'Z'
	binary.LittleEndian.PutUint32(header[0x3c:], 0x80)
	copy(header[0x80:], []byte{'P', 'E', 0, 0})
	binary.LittleEndian.PutUint16(header[0x80+4:], machine)
	binary.LittleEndian.PutUint16(header[0x80+22:], 0x2000) // IMAGE_FILE_DLL
	return header
}

func TestClassify(t *testing.T) {
	cases := map[string]struct {
		header  []byte
		format  string
		machine string
	}{
		"elf amd64":   {syntheticELF(62), "ELF64", "x86-64"},
		"elf arm64":   {syntheticELF(183), "ELF64", "AArch64"},
		"macho arm64": {syntheticMachO(0x0100000c), "Mach-O64", "arm64"},
		"pe amd64":    {syntheticPE(0x8664), "PE32+", "x86-64"},
		"pe arm64":    {syntheticPE(0xaa64), "PE32+", "ARM64"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			format, machine, err := classify(testCase.header)
			if err != nil {
				t.Fatalf("classify: %v", err)
			}
			if format != testCase.format || machine != testCase.machine {
				t.Fatalf("classify = %s/%s, want %s/%s", format, machine, testCase.format, testCase.machine)
			}
		})
	}

	badPE := syntheticPE(0x8664)
	binary.LittleEndian.PutUint16(badPE[0x80+22:], 0)
	badELFType := syntheticELF(62)
	binary.LittleEndian.PutUint16(badELFType[16:], 1)
	badCases := map[string][]byte{
		"not a library":    []byte("plain text"),
		"elf relocatable":  badELFType,
		"pe executable":    badPE,
		"unsupported risk": syntheticELF(243),
	}
	for name, header := range badCases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := classify(header); err == nil {
				t.Fatal("classify accepted an unsupported library")
			}
		})
	}
}

func TestIdentifyLibrary(t *testing.T) {
	dir := t.TempDir()
	library := append(syntheticELF(62), bytes.Repeat([]byte{0}, 128)...)
	path := writeFile(t, dir, "liblance_c.so", string(library))

	amd64, err := resolveTarget("linux/amd64")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	sha, size, format, machine, err := identifyLibrary(path, amd64)
	if err != nil {
		t.Fatalf("identifyLibrary: %v", err)
	}
	sum := sha256.Sum256(library)
	if sha != hex.EncodeToString(sum[:]) || size != int64(len(library)) || format != "ELF64" || machine != "x86-64" {
		t.Fatalf("identifyLibrary = %s %d %s/%s", sha, size, format, machine)
	}

	arm64, err := resolveTarget("linux/arm64")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if _, _, _, _, err := identifyLibrary(path, arm64); err == nil || !strings.Contains(err.Error(), "want ELF64/AArch64") {
		t.Fatalf("identifyLibrary accepted the wrong target: %v", err)
	}
}

func TestRetryable(t *testing.T) {
	if retryable(statusError{code: 404, status: "404 Not Found"}) {
		t.Fatal("404 must not be retried")
	}
	if !retryable(statusError{code: 500, status: "500 Internal Server Error"}) {
		t.Fatal("500 must be retried")
	}
	if !retryable(statusError{code: 429, status: "429 Too Many Requests"}) {
		t.Fatal("429 must be retried")
	}
}

func TestDownloadOnce(t *testing.T) {
	content := []byte("archive bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/asset" {
			http.NotFound(w, r)
			return
		}
		w.Write(content)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	destination := filepath.Join(t.TempDir(), "asset")
	digest, size, err := downloadOnce(client, server.URL+"/asset", destination)
	if err != nil {
		t.Fatalf("downloadOnce: %v", err)
	}
	sum := sha256.Sum256(content)
	if digest != hex.EncodeToString(sum[:]) || size != int64(len(content)) {
		t.Fatalf("downloadOnce = %s, %d bytes", digest, size)
	}
	written, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if !bytes.Equal(written, content) {
		t.Fatal("downloaded bytes differ from the response")
	}

	_, _, err = downloadOnce(client, server.URL+"/missing", filepath.Join(t.TempDir(), "missing"))
	var failure statusError
	if !errors.As(err, &failure) || failure.code != http.StatusNotFound || retryable(err) {
		t.Fatalf("missing asset returned %v, want a non-retryable HTTP 404", err)
	}
}

const companionLibrarySHA = "8e00a4c9fd401d0e99723222d6dfeea47855c57a2e442409b1c32dfed0559c13"

func testPin(t *testing.T) pin {
	t.Helper()
	pinned, err := readPin(writeFile(t, t.TempDir(), "upstream.go", testPinFile))
	if err != nil {
		t.Fatalf("readPin: %v", err)
	}
	return pinned
}

func writeCompanion(t *testing.T, root string, spec targetSpec, pinned pin, librarySHA string, librarySize int64) {
	t.Helper()
	dir := filepath.Join(root, strings.ReplaceAll(spec.name, "/", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create companion directory: %v", err)
	}
	source := fmt.Sprintf(`package artifact

const (
	Target                   = %q
	LibraryName              = %q
	CompressedSize     int64 = 1
	CompressedSHA256         = %q
	UncompressedSize   int64 = %d
	UncompressedSHA256       = %q
	UpstreamRepo             = %q
	UpstreamCommit           = %q
	HeaderSHA256             = %q
)
`, spec.name, filepath.Base(spec.libraryMember), strings.Repeat("a", 64), librarySize,
		librarySHA, pinned.repo, pinned.commit, pinned.headerSHA)
	if err := os.WriteFile(filepath.Join(dir, "artifact.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write companion metadata: %v", err)
	}
}

func TestVerifyArchiveChecksum(t *testing.T) {
	const digest = "76df5338b16e02627e715a3bbe6df554ce7e280e2c2d7497fa01ff76bea7ebf5"
	if err := verifyArchiveChecksum(digest, digest, "asset.tar.xz.sha256"); err != nil {
		t.Fatalf("verifyArchiveChecksum: %v", err)
	}
	if err := verifyArchiveChecksum(digest, strings.Repeat("b", 64), "asset.tar.xz.sha256"); err == nil {
		t.Fatal("verifyArchiveChecksum accepted a mismatched checksum")
	}
}

// TestReplacedArchiveWithMatchingChecksumIsRejected covers the contract that a
// replaced archive with a replacement same-source checksum cannot pass: the
// published-checksum comparison succeeds, and the independently pinned
// companion identity rejects the different library bytes.
func TestReplacedArchiveWithMatchingChecksumIsRejected(t *testing.T) {
	pinned := testPin(t)
	spec, err := resolveTarget("linux/amd64")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	root := t.TempDir()
	writeCompanion(t, root, spec, pinned, companionLibrarySHA, 342441416)

	replacementArchiveSHA := strings.Repeat("c", 64)
	if err := verifyArchiveChecksum(replacementArchiveSHA, replacementArchiveSHA, "asset.tar.xz.sha256"); err != nil {
		t.Fatalf("a self-consistent replacement checksum must pass the published-checksum check: %v", err)
	}
	if _, err := verifyIndependentIdentity(root, spec, pinned, strings.Repeat("d", 64), 342441416); err == nil ||
		!strings.Contains(err.Error(), "independently pinned companion") {
		t.Fatalf("verifyIndependentIdentity accepted replaced library bytes: %v", err)
	}
}

func TestVerifyIndependentIdentity(t *testing.T) {
	pinned := testPin(t)
	spec, err := resolveTarget("linux/amd64")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}

	t.Run("valid pinned companion", func(t *testing.T) {
		root := t.TempDir()
		writeCompanion(t, root, spec, pinned, companionLibrarySHA, 342441416)
		got, err := verifyIndependentIdentity(root, spec, pinned, companionLibrarySHA, 342441416)
		if err != nil {
			t.Fatalf("verifyIndependentIdentity: %v", err)
		}
		if got.status != "verified" || got.expectedSHA != companionLibrarySHA || got.expectedSize != 342441416 {
			t.Fatalf("unexpected identity result: %+v", got)
		}
	})

	t.Run("different library size", func(t *testing.T) {
		root := t.TempDir()
		writeCompanion(t, root, spec, pinned, companionLibrarySHA, 342441416)
		if _, err := verifyIndependentIdentity(root, spec, pinned, companionLibrarySHA, 1); err == nil {
			t.Fatal("verifyIndependentIdentity accepted a different library size")
		}
	})

	t.Run("stale companion after a pin bump", func(t *testing.T) {
		root := t.TempDir()
		stale := pinned
		stale.commit = strings.Repeat("a", 40)
		writeCompanion(t, root, spec, stale, companionLibrarySHA, 342441416)
		got, err := verifyIndependentIdentity(root, spec, pinned, strings.Repeat("d", 64), 1)
		if err != nil {
			t.Fatalf("a stale companion must not block acquisition: %v", err)
		}
		if got.status != "inactive" || !strings.Contains(got.reason, "companion metadata records") {
			t.Fatalf("unexpected identity result: %+v", got)
		}
	})

	t.Run("missing companion", func(t *testing.T) {
		got, err := verifyIndependentIdentity(t.TempDir(), spec, pinned, companionLibrarySHA, 342441416)
		if err != nil {
			t.Fatalf("missing companion must not block acquisition: %v", err)
		}
		if got.status != "inactive" || !strings.Contains(got.reason, "no companion metadata") {
			t.Fatalf("unexpected identity result: %+v", got)
		}
	})

	t.Run("companion for another target", func(t *testing.T) {
		root := t.TempDir()
		other, err := resolveTarget("linux/arm64")
		if err != nil {
			t.Fatalf("resolveTarget: %v", err)
		}
		writeCompanion(t, root, other, pinned, companionLibrarySHA, 342441416)
		// Place the other target's metadata where linux/amd64 is expected.
		if err := os.Rename(filepath.Join(root, "linux-arm64"), filepath.Join(root, "linux-amd64")); err != nil {
			t.Fatalf("rename companion: %v", err)
		}
		if _, err := verifyIndependentIdentity(root, spec, pinned, companionLibrarySHA, 342441416); err == nil ||
			!strings.Contains(err.Error(), "records linux/arm64") {
			t.Fatalf("verifyIndependentIdentity accepted metadata for another target: %v", err)
		}
	})
}

// TestCheckedInCompanionsMatchTargets keeps the metadata mapping every
// supported target already relies on. It deliberately does not require the
// companions to describe the current pin: after a pin bump they are updated
// only once the new modules are published.
func TestCheckedInCompanionsMatchTargets(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..")
	for _, spec := range targets {
		path := filepath.Join(repoRoot, "artifacts", strings.ReplaceAll(spec.name, "/", "-"), "artifact.go")
		md, err := artifactmeta.Read(path)
		if err != nil {
			t.Fatalf("%s: %v", spec.name, err)
		}
		if md.Target != spec.name {
			t.Fatalf("%s metadata target is %q", spec.name, md.Target)
		}
		if want := filepath.Base(spec.libraryMember); md.LibraryName != want {
			t.Fatalf("%s metadata library is %q, want %q", spec.name, md.LibraryName, want)
		}
	}
}

type tarEntry struct {
	name     string
	data     []byte
	typeflag byte
}

func writeTarXZ(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer file.Close()
	xzWriter, err := xz.NewWriter(file)
	if err != nil {
		t.Fatalf("create xz writer: %v", err)
	}
	tarWriter := tar.NewWriter(xzWriter)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.data)), Typeflag: typeflag}
		if typeflag == tar.TypeDir {
			header.Mode = 0o755
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header %s: %v", entry.name, err)
		}
		if typeflag == tar.TypeReg {
			if _, err := tarWriter.Write(entry.data); err != nil {
				t.Fatalf("write tar entry %s: %v", entry.name, err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := xzWriter.Close(); err != nil {
		t.Fatalf("close xz writer: %v", err)
	}
}

func TestExtractMembers(t *testing.T) {
	lib := bytes.Repeat([]byte("library bytes"), 1024)
	archive := filepath.Join(t.TempDir(), "release.tar.xz")
	writeTarXZ(t, archive, []tarEntry{
		{name: "./", typeflag: tar.TypeDir},
		{name: "./include/", typeflag: tar.TypeDir},
		{name: "./include/lance/", typeflag: tar.TypeDir},
		{name: "./include/lance/lance.h", data: []byte("header bytes")},
		{name: "./lib/", typeflag: tar.TypeDir},
		{name: "./lib/liblance_c.so", data: lib},
		{name: "./lib/liblance_c.so.zst", data: []byte("not wanted")},
	})

	out := t.TempDir()
	seen, err := extractMembers(archive, out, []string{"./include/lance/lance.h", "./lib/liblance_c.so"})
	if err != nil {
		t.Fatalf("extractMembers: %v", err)
	}
	if len(seen) != 7 {
		t.Fatalf("seen %d members, want 7: %v", len(seen), seen)
	}
	header, err := os.ReadFile(filepath.Join(out, "include", "lance", "lance.h"))
	if err != nil || string(header) != "header bytes" {
		t.Fatalf("header = %q, %v", header, err)
	}
	library, err := os.ReadFile(filepath.Join(out, "lib", "liblance_c.so"))
	if err != nil || !bytes.Equal(library, lib) {
		t.Fatalf("library mismatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "lib", "liblance_c.so.zst")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unwanted member was extracted: %v", err)
	}

	_, err = extractMembers(archive, t.TempDir(), []string{"./lib/lance_c.dll"})
	if err == nil || !strings.Contains(err.Error(), "does not contain ./lib/lance_c.dll") {
		t.Fatalf("missing member error = %v", err)
	}

	linkArchive := filepath.Join(t.TempDir(), "link.tar.xz")
	writeTarXZ(t, linkArchive, []tarEntry{
		{name: "./lib/liblance_c.so", data: []byte("target")},
		{name: "./include/lance/lance.h", typeflag: tar.TypeSymlink},
	})
	if _, err := extractMembers(linkArchive, t.TempDir(), []string{"./include/lance/lance.h", "./lib/liblance_c.so"}); err == nil ||
		!strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("symlink member error = %v", err)
	}

	duplicateArchive := filepath.Join(t.TempDir(), "duplicate.tar.xz")
	writeTarXZ(t, duplicateArchive, []tarEntry{
		{name: "./lib/liblance_c.so", data: []byte("one")},
		{name: "./lib/liblance_c.so", data: []byte("two")},
	})
	if _, err := extractMembers(duplicateArchive, t.TempDir(), []string{"./lib/liblance_c.so"}); err == nil ||
		!strings.Contains(err.Error(), "appears more than once") {
		t.Fatalf("duplicate member error = %v", err)
	}
}
