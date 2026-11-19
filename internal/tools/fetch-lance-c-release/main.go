// Command fetch-lance-c-release acquires the pinned lance-c shared library and
// header from the official GitHub release for one target.
//
// It downloads the archive for the target triple named after the pinned
// release, verifies it against the checksum published with the release,
// extracts only the header and shared library, and verifies that the header
// bytes match the pin in internal/ffi/upstream.go and that the library's
// object format and machine match the requested target. It never builds
// lance-c from source, and it never substitutes a different release or target.
//
// Usage:
//
//	go run ./internal/tools/fetch-lance-c-release \
//	  -target linux/amd64 \
//	  -out /tmp/lance-c-release
//
// The output directory must not exist. The command writes:
//
//	acquisition.txt                 verified provenance for the rest of the lane
//	lance-c-<tag>-<triple>.tar.xz   the verified published archive
//	...tar.xz.sha256                the published per-asset checksum
//	include/lance/lance.h           canonical header (LF line endings)
//	lib/<library>                   the published shared library, byte for byte
//
// Linux and macOS archives ship an LF header; the Windows archive ships the
// same header with CRLF line endings. The command writes the canonical LF form
// recorded by upstreamHeaderSHA256 and records both hashes.
//
// When the checked-in companion metadata at artifacts/<target>/artifact.go
// records the same upstream repository, commit, and header as the pin, the
// acquired library must also equal that module's UncompressedSHA256. That
// check keeps CI on the bytes a published companion ships even if a release
// archive and its published checksum were replaced together. A pin bump whose
// companions have not been republished yet records the independent check as
// inactive; candidate packaging still uses the acquired bytes.
//
// lance-c exposes no runtime version symbol, so the library bytes cannot prove
// the commit that built them. The published per-asset checksum, the pinned
// companion identity, the pinned header hash, and the target machine check are
// the available identity chain.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lance-format/lance-go/internal/tools/artifactmeta"
)

const pinPath = "internal/ffi/upstream.go"

func main() {
	targetFlag := flag.String("target", "", "target to acquire as GOOS/GOARCH: linux/amd64, linux/arm64, darwin/arm64, windows/amd64, or windows/arm64")
	outFlag := flag.String("out", "", "new output directory; must not exist")
	pinFlag := flag.String("pin", pinPath, "pin file with the upstream provenance constants")
	artifactsFlag := flag.String("artifacts", "artifacts", "root of the checked-in companion metadata")
	flag.Parse()

	if flag.NArg() != 0 {
		fail(fmt.Errorf("unexpected arguments: %s", strings.Join(flag.Args(), " ")))
	}
	if *targetFlag == "" || *outFlag == "" {
		flag.Usage()
		fail(errors.New("-target and -out are required"))
	}

	if err := run(*targetFlag, *outFlag, *pinFlag, *artifactsFlag); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "fetch-lance-c-release: %v\n", err)
	os.Exit(1)
}

type pin struct {
	repo      string
	release   string
	commit    string
	headerSHA string
}

type pinCheck struct {
	name string
	ok   func(string) bool
	want string
}

func run(targetName, outDir, pinFile, artifactsRoot string) error {
	pinned, err := readPin(pinFile)
	if err != nil {
		return err
	}
	spec, err := resolveTarget(targetName)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(outDir); err == nil {
		return fmt.Errorf("output already exists: %s; choose a new -out directory", outDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output directory %s: %w", outDir, err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory %s: %w", outDir, err)
	}
	if err := acquire(pinned, spec, outDir, artifactsRoot); err != nil {
		return err
	}

	record, err := readRecord(filepath.Join(outDir, recordName))
	if err != nil {
		return err
	}
	fmt.Print(record)
	return nil
}

const recordName = "acquisition.txt"

// acquire downloads, verifies, extracts, and records one release archive.
func acquire(pinned pin, spec targetSpec, outDir, artifactsRoot string) error {
	assetName := fmt.Sprintf("lance-c-%s-%s.tar.xz", pinned.release, spec.triple)
	baseURL := strings.TrimSuffix(pinned.repo, "/") + "/releases/download/" + url.PathEscape(pinned.release)
	assetURL := baseURL + "/" + assetName
	checksumName := assetName + ".sha256"
	checksumURL := baseURL + "/" + checksumName

	fmt.Printf("download: %s\n", assetURL)
	archivePath := filepath.Join(outDir, assetName)
	archiveSHA, archiveSize, err := download(assetURL, archivePath)
	if err != nil {
		return fmt.Errorf("download %s: %w; lance-c %s must publish the %s archive %s, and this command will not fall back to a source build",
			assetURL, err, pinned.release, spec.name, assetName)
	}

	fmt.Printf("download: %s\n", checksumURL)
	checksumPath := filepath.Join(outDir, checksumName)
	if err := downloadToFile(checksumURL, checksumPath); err != nil {
		return fmt.Errorf("download published checksum %s: %w; the release must publish %s next to the archive",
			checksumURL, err, checksumName)
	}
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("read published checksum %s: %w", checksumPath, err)
	}
	publishedSHA, err := parsePublishedSHA256(checksumData, assetName)
	if err != nil {
		return fmt.Errorf("published checksum %s: %w", checksumPath, err)
	}
	if err := verifyArchiveChecksum(archiveSHA, publishedSHA, checksumName); err != nil {
		return err
	}

	members, err := listArchiveMembers(archivePath)
	if err != nil {
		return err
	}
	expected := []string{headerMember, spec.libraryMember}
	if spec.importLibraryMember != "" {
		expected = append(expected, spec.importLibraryMember)
	}
	for _, member := range expected {
		if !members[member] {
			return fmt.Errorf("archive %s does not contain %s; published members: %s", assetName, member, memberSummary(members))
		}
	}
	if err := extractMembers(archivePath, outDir, expected); err != nil {
		return err
	}

	headerPath := filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(headerMember, "./")))
	headerSHA, headerArchiveSHA, lineEndings, err := canonicalHeader(headerPath, pinned.headerSHA)
	if err != nil {
		return fmt.Errorf("header from %s: %w", assetName, err)
	}
	if lineEndings != "lf" {
		fmt.Printf("note: %s ships the pinned header with %s line endings; wrote the canonical LF form (archive sha256 %s)\n",
			assetName, strings.ToUpper(lineEndings), headerArchiveSHA)
	}

	libraryPath := filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(spec.libraryMember, "./")))
	librarySHA, librarySize, format, machine, err := identifyLibrary(libraryPath, spec)
	if err != nil {
		return fmt.Errorf("library from %s: %w", assetName, err)
	}

	var importLibraryPath, importLibrarySHA string
	var importLibrarySize int64
	if spec.importLibraryMember != "" {
		importLibraryPath = filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(spec.importLibraryMember, "./")))
		importLibrarySHA, importLibrarySize, err = fileSHA256(importLibraryPath)
		if err != nil {
			return fmt.Errorf("import library from %s: %w", assetName, err)
		}
	}

	independent, err := verifyIndependentIdentity(artifactsRoot, spec, pinned, librarySHA, librarySize)
	if err != nil {
		return err
	}
	if independent.status == "verified" {
		fmt.Printf("independent identity: %s matches the companion module %s\n", librarySHA, independent.path)
	} else {
		fmt.Printf("independent identity: inactive for %s (%s)\n", spec.name, independent.reason)
	}

	record := []string{
		"target=" + spec.name,
		"upstream_repo=" + pinned.repo,
		"upstream_release=" + pinned.release,
		"upstream_commit=" + pinned.commit,
		"upstream_header_sha256=" + pinned.headerSHA,
		"archive=" + archivePath,
		"archive_name=" + assetName,
		"archive_url=" + assetURL,
		"archive_size=" + strconv.FormatInt(archiveSize, 10),
		"archive_sha256=" + archiveSHA,
		"published_checksum=" + checksumPath,
		"published_checksum_name=" + checksumName,
		"published_checksum_url=" + checksumURL,
		"published_sha256=" + publishedSHA,
		"header=" + headerPath,
		"header_sha256=" + headerSHA,
		"header_archive_sha256=" + headerArchiveSHA,
		"header_archive_line_endings=" + lineEndings,
		"library=" + libraryPath,
		"library_bytes=" + strconv.FormatInt(librarySize, 10),
		"library_sha256=" + librarySHA,
		"library_format=" + format,
		"library_machine=" + machine,
		"independent_identity=" + independent.path,
		"independent_identity_status=" + independent.status,
	}
	if independent.status == "verified" {
		record = append(record,
			"independent_identity_library_bytes="+strconv.FormatInt(independent.expectedSize, 10),
			"independent_identity_library_sha256="+independent.expectedSHA,
		)
	} else {
		record = append(record, "independent_identity_reason="+independent.reason)
	}
	if importLibraryPath != "" {
		record = append(record,
			"import_library="+importLibraryPath,
			"import_library_bytes="+strconv.FormatInt(importLibrarySize, 10),
			"import_library_sha256="+importLibrarySHA,
		)
	}
	record = append(record, "fetch_lance_c_release=ok")

	if err := os.WriteFile(filepath.Join(outDir, recordName), []byte(strings.Join(record, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", recordName, err)
	}
	return nil
}

func readRecord(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read acquisition record: %w", err)
	}
	return string(data), nil
}

// readPin reads the four provenance constants from internal/ffi/upstream.go.
func readPin(path string) (pin, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return pin{}, fmt.Errorf("parse pin %s: %w", path, err)
	}
	values := map[string]string{}
	for _, declaration := range file.Decls {
		decl, ok := declaration.(*ast.GenDecl)
		if !ok || decl.Tok != token.CONST {
			continue
		}
		for _, specification := range decl.Specs {
			spec, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range spec.Names {
				if i >= len(spec.Values) {
					continue
				}
				literal, ok := spec.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				if _, exists := values[name.Name]; exists {
					return pin{}, fmt.Errorf("pin %s declares %s more than once", path, name.Name)
				}
				values[name.Name] = value
			}
		}
	}

	pinned := pin{
		repo:      values["upstreamRepo"],
		release:   values["upstreamRelease"],
		commit:    values["upstreamCommit"],
		headerSHA: values["upstreamHeaderSHA256"],
	}
	checks := []pinCheck{
		{"upstreamRepo", func(v string) bool {
			return strings.HasPrefix(v, "https://") && urlPattern.MatchString(v)
		}, "an https repository URL"},
		{"upstreamRelease", releasePattern.MatchString, "a release tag such as v0.2.0-beta.1"},
		{"upstreamCommit", commitPattern.MatchString, "a full 40-character lowercase commit SHA"},
		{"upstreamHeaderSHA256", sha256Pattern.MatchString, "a lowercase SHA-256"},
	}
	for _, check := range checks {
		value := values[check.name]
		if value == "" {
			return pin{}, fmt.Errorf("pin %s is missing %s", path, check.name)
		}
		if !check.ok(value) {
			return pin{}, fmt.Errorf("pin %s has %s = %q, want %s", path, check.name, value, check.want)
		}
	}
	return pinned, nil
}

var (
	urlPattern     = regexp.MustCompile(`^https://[A-Za-z0-9.-]+/[\w./-]+$`)
	releasePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

const headerMember = "./include/lance/lance.h"

type targetSpec struct {
	name                string
	triple              string
	libraryMember       string
	importLibraryMember string
	format              string
	machine             string
}

var targets = []targetSpec{
	{
		name:          "linux/amd64",
		triple:        "x86_64-unknown-linux-gnu",
		libraryMember: "./lib/liblance_c.so",
		format:        "ELF64",
		machine:       "x86-64",
	},
	{
		name:          "linux/arm64",
		triple:        "aarch64-unknown-linux-gnu",
		libraryMember: "./lib/liblance_c.so",
		format:        "ELF64",
		machine:       "AArch64",
	},
	{
		name:          "darwin/arm64",
		triple:        "aarch64-apple-darwin",
		libraryMember: "./lib/liblance_c.dylib",
		format:        "Mach-O64",
		machine:       "arm64",
	},
	{
		name:                "windows/amd64",
		triple:              "x86_64-pc-windows-msvc",
		libraryMember:       "./lib/lance_c.dll",
		importLibraryMember: "./lib/lance_c.dll.lib",
		format:              "PE32+",
		machine:             "x86-64",
	},
	{
		name:                "windows/arm64",
		triple:              "aarch64-pc-windows-msvc",
		libraryMember:       "./lib/lance_c.dll",
		importLibraryMember: "./lib/lance_c.dll.lib",
		format:              "PE32+",
		machine:             "ARM64",
	},
}

func resolveTarget(name string) (targetSpec, error) {
	for _, spec := range targets {
		if spec.name == name {
			return spec, nil
		}
	}
	supported := make([]string, 0, len(targets))
	for _, spec := range targets {
		supported = append(supported, spec.name)
	}
	return targetSpec{}, fmt.Errorf("unsupported -target %q; supported targets: %s", name, strings.Join(supported, ", "))
}

// parsePublishedSHA256 accepts the single-line per-asset checksum that lance-c
// publishes, in either text ("hash  name") or binary ("hash *name") form.
func parsePublishedSHA256(data []byte, assetName string) (string, error) {
	content := strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
	if content == "" {
		return "", errors.New("checksum file is empty")
	}
	lines := strings.Split(content, "\n")
	if len(lines) != 1 {
		return "", fmt.Errorf("expected one checksum line, found %d", len(lines))
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 2 {
		return "", fmt.Errorf("malformed checksum line %q; want \"<sha256>  <asset>\"", lines[0])
	}
	digest := fields[0]
	if !sha256Pattern.MatchString(digest) {
		return "", fmt.Errorf("checksum %q is not a lowercase SHA-256", digest)
	}
	name := strings.TrimPrefix(fields[1], "*")
	if name != assetName {
		return "", fmt.Errorf("checksum names %q, want %q; the archive and checksum do not belong together", name, assetName)
	}
	return digest, nil
}

const (
	downloadAttempts = 3
	downloadTimeout  = 20 * time.Minute
)

type statusError struct {
	code   int
	status string
}

func (e statusError) Error() string {
	return "HTTP " + e.status
}

// download fetches url and returns the SHA-256 and size of the bytes written.
func download(source, destination string) (string, int64, error) {
	client := &http.Client{Timeout: downloadTimeout}
	var lastErr error
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		digest, size, err := downloadOnce(client, source, destination)
		if err == nil {
			return digest, size, nil
		}
		lastErr = err
		if attempt < downloadAttempts && retryable(err) {
			fmt.Printf("retry download after transient failure (attempt %d of %d): %v\n", attempt, downloadAttempts, err)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}
		break
	}
	return "", 0, lastErr
}

func downloadToFile(source, destination string) error {
	_, _, err := download(source, destination)
	return err
}

func downloadOnce(client *http.Client, source, destination string) (string, int64, error) {
	request, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return "", 0, err
	}
	request.Header.Set("User-Agent", "lance-go-fetch-lance-c-release")
	response, err := client.Do(request)
	if err != nil {
		return "", 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", 0, statusError{code: response.StatusCode, status: response.Status}
	}

	file, err := os.Create(destination)
	if err != nil {
		return "", 0, err
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(destination)
		return "", 0, copyErr
	}
	if closeErr != nil {
		os.Remove(destination)
		return "", 0, closeErr
	}
	if size == 0 {
		os.Remove(destination)
		return "", 0, errors.New("downloaded file is empty")
	}
	return hex.EncodeToString(digest.Sum(nil)), size, nil
}

func retryable(err error) bool {
	var failure statusError
	if errors.As(err, &failure) {
		return failure.code == http.StatusTooManyRequests || failure.code >= http.StatusInternalServerError
	}
	return true
}

// canonicalHeader verifies the extracted header against the pin. A header that
// differs only by CRLF line endings is accepted and rewritten in canonical LF
// form; both hashes are recorded.
func canonicalHeader(path, pinSHA string) (canonicalSHA, archiveSHA, lineEndings string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", err
	}
	archiveSHA = sha256Hex(raw)
	canonical := raw
	lineEndings = "lf"
	if archiveSHA != pinSHA {
		normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
		normalizedSHA := sha256Hex(normalized)
		if normalizedSHA != pinSHA {
			return "", "", "", fmt.Errorf("sha256 is %s as published and %s with LF line endings, pin says %s; the release does not contain the pinned header",
				archiveSHA, normalizedSHA, pinSHA)
		}
		canonical = normalized
		lineEndings = "crlf"
		if err := os.WriteFile(path, canonical, 0o644); err != nil {
			return "", "", "", fmt.Errorf("write canonical header: %w", err)
		}
	}
	canonicalSHA = sha256Hex(canonical)
	return canonicalSHA, archiveSHA, lineEndings, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileSHA256(path string) (string, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("%s is not a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(digest.Sum(nil)), info.Size(), nil
}

// identifyLibrary verifies that the extracted library is a shared object for
// the requested target and returns its SHA-256, size, format, and machine.
func identifyLibrary(path string, spec targetSpec) (string, int64, string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, "", "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, "", "", err
	}
	if !info.Mode().IsRegular() {
		return "", 0, "", "", fmt.Errorf("%s is not a regular file", path)
	}
	head := make([]byte, 64*1024)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", 0, "", "", err
	}
	head = head[:n]

	format, machine, err := classify(head)
	if err != nil {
		return "", 0, "", "", fmt.Errorf("%s: %w", path, err)
	}
	if format != spec.format || machine != spec.machine {
		return "", 0, "", "", fmt.Errorf("identity is %s/%s, want %s/%s for %s", format, machine, spec.format, spec.machine, spec.name)
	}
	// Re-read the whole file for the digest; identify only needs the header.
	fullSHA, size, err := fileSHA256(path)
	if err != nil {
		return "", 0, "", "", err
	}
	return fullSHA, size, format, machine, nil
}

// classify returns the object format and machine of a shared library header.
func classify(head []byte) (string, string, error) {
	switch {
	case bytes.HasPrefix(head, []byte{0x7f, 'E', 'L', 'F'}):
		return classifyELF(head)
	case bytes.HasPrefix(head, []byte{'M', 'Z'}):
		return classifyPE(head)
	case bytes.HasPrefix(head, []byte{0xcf, 0xfa, 0xed, 0xfe}):
		return classifyMachO(head)
	default:
		return "", "", errors.New("not an ELF, Mach-O, or PE shared library")
	}
}

func classifyELF(head []byte) (string, string, error) {
	if len(head) < 20 {
		return "", "", errors.New("ELF header is truncated")
	}
	if head[4] != 2 {
		return "ELF32", "", errors.New("ELF object is not 64-bit")
	}
	var order binary.ByteOrder
	switch head[5] {
	case 1:
		order = binary.LittleEndian
	case 2:
		order = binary.BigEndian
	default:
		return "", "", fmt.Errorf("ELF data encoding %d is unknown", head[5])
	}
	if objectType := order.Uint16(head[16:18]); objectType != 3 {
		return "", "", fmt.Errorf("ELF object type is %d, want a shared object (3)", objectType)
	}
	machine := order.Uint16(head[18:20])
	for _, candidate := range []struct {
		id    uint16
		label string
	}{{62, "x86-64"}, {183, "AArch64"}} {
		if machine == candidate.id {
			return "ELF64", candidate.label, nil
		}
	}
	return "", "", fmt.Errorf("ELF machine %d is not a supported target", machine)
}

func classifyMachO(head []byte) (string, string, error) {
	if len(head) < 16 {
		return "", "", errors.New("Mach-O header is truncated")
	}
	cpuType := binary.LittleEndian.Uint32(head[4:8])
	if fileType := binary.LittleEndian.Uint32(head[12:16]); fileType != 6 {
		return "", "", fmt.Errorf("Mach-O file type is %d, want a dylib (6)", fileType)
	}
	switch cpuType {
	case 0x0100000c:
		return "Mach-O64", "arm64", nil
	case 0x01000007:
		return "Mach-O64", "x86-64", nil
	default:
		return "", "", fmt.Errorf("Mach-O CPU type 0x%x is not a supported target", cpuType)
	}
}

func classifyPE(head []byte) (string, string, error) {
	if len(head) < 0x40 {
		return "", "", errors.New("DOS header is truncated")
	}
	offset := int(binary.LittleEndian.Uint32(head[0x3c:0x40]))
	if offset < 0x40 || offset+24 > len(head) {
		return "", "", fmt.Errorf("PE header offset %d is outside the file header", offset)
	}
	if !bytes.Equal(head[offset:offset+4], []byte{'P', 'E', 0, 0}) {
		return "", "", errors.New("missing PE signature")
	}
	characteristics := binary.LittleEndian.Uint16(head[offset+22 : offset+24])
	if characteristics&0x2000 == 0 {
		return "", "", errors.New("PE image is not a DLL")
	}
	machine := binary.LittleEndian.Uint16(head[offset+4 : offset+6])
	switch machine {
	case 0x8664:
		return "PE32+", "x86-64", nil
	case 0xaa64:
		return "PE32+", "ARM64", nil
	default:
		return "", "", fmt.Errorf("PE machine 0x%x is not a supported target", machine)
	}
}

// verifyArchiveChecksum binds the downloaded archive to the checksum published
// next to it. It cannot detect a release whose archive and checksum were
// replaced together; verifyIndependentIdentity adds the checked-in companion
// identity for that case.
func verifyArchiveChecksum(observed, published, checksumName string) error {
	if observed != published {
		return fmt.Errorf("archive sha256 is %s, published %s says %s; the download is corrupt or the published asset changed",
			observed, checksumName, published)
	}
	return nil
}

type independentIdentity struct {
	path         string
	status       string
	expectedSHA  string
	expectedSize int64
	reason       string
}

// verifyIndependentIdentity ties the acquired library to the companion module
// metadata recorded in the SDK tree for this target. The check is inactive
// when that metadata describes a different upstream revision, which is the
// normal state between a pin bump and companion republication. Candidate
// packaging therefore depends only on the acquired bytes and the pin.
func verifyIndependentIdentity(artifactsRoot string, spec targetSpec, pinned pin, librarySHA string, librarySize int64) (independentIdentity, error) {
	slug := strings.ReplaceAll(spec.name, "/", "-")
	path := filepath.Join(artifactsRoot, slug, "artifact.go")
	result := independentIdentity{path: path}

	md, err := artifactmeta.Read(path)
	if errors.Is(err, fs.ErrNotExist) {
		result.status = "inactive"
		result.reason = fmt.Sprintf("no companion metadata at %s", path)
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read independently pinned companion %s: %w", path, err)
	}
	if md.UpstreamRepo == pinned.repo && md.UpstreamCommit == pinned.commit && md.HeaderSHA256 == pinned.headerSHA {
		if md.Target != spec.name || md.LibraryName != filepath.Base(spec.libraryMember) {
			return result, fmt.Errorf("independently pinned companion %s records %s/%s, want %s/%s",
				path, md.Target, md.LibraryName, spec.name, filepath.Base(spec.libraryMember))
		}
		result.status = "verified"
		result.expectedSHA = md.UncompressedSHA256
		result.expectedSize = md.UncompressedSize
		if md.UncompressedSHA256 != librarySHA || md.UncompressedSize != librarySize {
			return result, fmt.Errorf("library sha256 is %s (%d bytes), but the independently pinned companion %s records %s (%d bytes) for %s@%s; the release archive does not carry the bytes this pin publishes",
				librarySHA, librarySize, path, md.UncompressedSHA256, md.UncompressedSize, pinned.repo, pinned.commit)
		}
		return result, nil
	}
	result.status = "inactive"
	result.reason = fmt.Sprintf("companion metadata records %s@%s with header sha256 %s; the pin records %s@%s with header sha256 %s",
		md.UpstreamRepo, md.UpstreamCommit, md.HeaderSHA256, pinned.repo, pinned.commit, pinned.headerSHA)
	return result, nil
}

// listArchiveMembers returns the exact member names in the archive.
func listArchiveMembers(archivePath string) (map[string]bool, error) {
	output, err := runTar("-tJf", archivePath)
	if err != nil {
		return nil, fmt.Errorf("list archive %s: %w", archivePath, err)
	}
	members := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		members[line] = true
	}
	return members, nil
}

func extractMembers(archivePath, outDir string, members []string) error {
	args := append([]string{"-xJf", archivePath, "-C", outDir}, members...)
	if _, err := runTar(args...); err != nil {
		return fmt.Errorf("extract %s from %s: %w", strings.Join(members, ", "), archivePath, err)
	}
	for _, member := range members {
		path := filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(member, "./")))
		// Reject symlinks: a published archive must contain the header and
		// library as regular files, and a link could point outside the output
		// directory.
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("extract %s from %s: %w", member, archivePath, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("extract %s from %s: extracted member is empty, not a regular file, or a symlink", member, archivePath)
		}
	}
	return nil
}

func runTar(args ...string) (string, error) {
	if _, err := exec.LookPath("tar"); err != nil {
		return "", errors.New("tar is required to read the .tar.xz release archive; install a tar with xz support")
	}
	command := exec.Command("tar", args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", errors.New(message)
	}
	return stdout.String(), nil
}

func memberSummary(members map[string]bool) string {
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 12 {
		names = append(names[:12], "...")
	}
	return strings.Join(names, ", ")
}
