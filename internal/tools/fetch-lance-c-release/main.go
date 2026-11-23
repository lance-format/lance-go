// Command fetch-lance-c-release acquires the pinned lance-c shared library and
// header from the official GitHub release for one target.
//
// It downloads the archive for the target triple named after the pinned
// release, verifies it against the checksum published with the release, and
// extracts the shared library together with every public include/lance/*.h
// header. The C++ header lance.hpp is not part of the Go header set. Legacy
// archives that ship only include/lance/lance.h are still accepted, with the
// single header verified against upstreamHeaderSHA256. A split header set is
// verified against an independently supplied HEADER-MANIFEST or composite
// identity, because the umbrella header alone is not a complete identity. The
// command never builds lance-c from source, and it never substitutes a
// different release or target.
//
// Usage:
//
//	go -C internal/tools run ./fetch-lance-c-release \
//	  -target linux/amd64 \
//	  -out /tmp/lance-c-release
//
// The command runs from the internal/tools module directory. Pass absolute
// paths for -out, -pin and -root; relative values resolve under internal/tools
// rather than the caller's directory.
//
// Extraction uses in-process xz and tar readers, so acquisition does not depend
// on the platform tar implementation.
//
// The output directory must not exist. The command writes:
//
//	acquisition.txt                 verified provenance for the rest of the lane
//	lance-c-<tag>-<triple>.tar.xz   the verified published archive
//	...tar.xz.sha256                the published per-asset checksum
//	include/lance/*.h               canonical public C headers (LF line endings)
//	HEADER-MANIFEST                 split header manifest, when applicable
//	COMPOSITE-SHA256                split header identity, when applicable
//	lib/<library>                   the published shared library, byte for byte
//
// Linux and macOS archives ship LF headers; the Windows archive ships the same
// headers with CRLF line endings. The command writes the canonical LF form and
// records both hashes. A split manifest lists sorted "include/lance/<name>
// <sha256>" lines, and its identity is the SHA-256 of that exact text.
//
// When the published artifact module that this target's generated selector
// imports records the same upstream repository, commit, and header as the pin,
// the acquired library must also equal that module's UncompressedSHA256. That
// check keeps CI on the bytes the published module ships even if a release
// archive and its published checksum were replaced together: the module is an
// immutable version resolved through the Go proxy and checksum database, not
// bytes published with the archive. A pin bump whose modules have not been
// republished yet records the independent check as inactive; candidate
// packaging still uses the acquired bytes.
//
// lance-c reports its binding release through lance_c_version(), but that
// string cannot prove the commit that built the library bytes. The published
// per-asset checksum, the adopted module identity, the pinned header hash, and
// the target machine check are the available identity chain.
package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lance-format/lance-go/internal/tools/artifactmeta"
	"github.com/lance-format/lance-go/internal/tools/headeridentity"
	"github.com/lance-format/lance-go/internal/tools/reporoot"
	"github.com/ulikunitz/xz"
)

func main() {
	targetFlag := flag.String("target", "", "target to acquire as GOOS/GOARCH: linux/amd64, linux/arm64, darwin/arm64, windows/amd64, or windows/arm64")
	outFlag := flag.String("out", "", "new output directory; must not exist (absolute path; relative values resolve from the internal/tools module directory)")
	pinFlag := flag.String("pin", "", "pin file with the upstream provenance constants (absolute path; relative values resolve from the internal/tools module directory; default: <root>/internal/ffi/upstream.go)")
	rootFlag := flag.String("root", "", "repository root that provides go.mod and internal/bundled (absolute path; relative values resolve from the internal/tools module directory; default: discovered from the current directory)")
	headerIdentityFlag := flag.String("expected-header-identity", "", "expected composite SHA-256 of a split HEADER-MANIFEST; defaults to upstreamHeaderIdentitySHA256 when the pin records one")
	headerManifestFlag := flag.String("expected-header-manifest", "", "expected split HEADER-MANIFEST file (absolute path; relative values resolve from the internal/tools module directory); its exact text digest is used as the identity")
	flag.Parse()

	if flag.NArg() != 0 {
		fail(fmt.Errorf("unexpected arguments: %s", strings.Join(flag.Args(), " ")))
	}
	if *targetFlag == "" || *outFlag == "" {
		flag.Usage()
		fail(errors.New("-target and -out are required"))
	}

	root := *rootFlag
	if root == "" {
		discovered, err := reporoot.Find()
		if err != nil {
			fail(err)
		}
		root = discovered
	}
	pin := *pinFlag
	if pin == "" {
		pin = filepath.Join(root, "internal", "ffi", "upstream.go")
	}

	if err := run(*targetFlag, *outFlag, pin, root, *headerIdentityFlag, *headerManifestFlag); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "fetch-lance-c-release: %v\n", err)
	os.Exit(1)
}

type pin struct {
	repo              string
	release           string
	commit            string
	headerSHA         string
	headerIdentitySHA string
}

type pinCheck struct {
	name string
	ok   func(string) bool
	want string
}

func run(targetName, outDir, pinFile, root, expectedIdentity, expectedManifestPath string) error {
	pinned, err := readPin(pinFile)
	if err != nil {
		return err
	}
	spec, err := resolveTarget(targetName)
	if err != nil {
		return err
	}
	expected, err := loadHeaderExpectation(expectedManifestPath, expectedIdentity, pinned.headerIdentitySHA)
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
	if err := acquire(pinned, spec, outDir, root, expected); err != nil {
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

// headerExpectation is the independently supplied identity for a split header
// set. manifest is nil when the caller supplied only a composite digest.
type headerExpectation struct {
	manifest []byte
	identity string
}

// loadHeaderExpectation combines the command-line expectation with the
// optional identity recorded in the pin. A supplied manifest is authoritative:
// its exact text digest must agree with any additional identity.
func loadHeaderExpectation(manifestPath, identity, pinIdentity string) (headerExpectation, error) {
	expected := headerExpectation{identity: identity}
	if expected.identity == "" {
		expected.identity = pinIdentity
	}
	if manifestPath != "" {
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return headerExpectation{}, fmt.Errorf("read expected header manifest %s: %w", manifestPath, err)
		}
		if _, err := headeridentity.Parse(data); err != nil {
			return headerExpectation{}, fmt.Errorf("expected header manifest %s: %w", manifestPath, err)
		}
		manifestIdentity := sha256Hex(data)
		if expected.identity != "" && expected.identity != manifestIdentity {
			return headerExpectation{}, fmt.Errorf("expected header manifest %s has sha256 %s, but the supplied identity is %s", manifestPath, manifestIdentity, expected.identity)
		}
		expected.manifest = data
		expected.identity = manifestIdentity
	}
	if expected.identity != "" && !headeridentity.ValidDigest(expected.identity) {
		return headerExpectation{}, fmt.Errorf("expected header identity %q is not a lowercase SHA-256", expected.identity)
	}
	return expected, nil
}

// acquire downloads, verifies, extracts, and records one release archive.
func acquire(pinned pin, spec targetSpec, outDir, root string, expected headerExpectation) error {
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

	expectedMembers := []string{headeridentity.Umbrella, spec.libraryMember}
	if spec.importLibraryMember != "" {
		expectedMembers = append(expectedMembers, spec.importLibraryMember)
	}
	fmt.Printf("extract: %s (public C headers and shared library only)\n", assetName)
	if _, err := extractMembers(archivePath, outDir, expectedMembers); err != nil {
		return fmt.Errorf("extract from %s: %w", assetName, err)
	}

	headers, err := verifyAcquiredHeaders(outDir, pinned, expected)
	if err != nil {
		return fmt.Errorf("headers from %s: %w", assetName, err)
	}
	headerPath, headerSHA := headers.umbrellaPath, headers.umbrellaSHA
	headerArchiveSHA, lineEndings := headers.umbrellaArchiveSHA, headers.umbrellaLineEndings
	if lineEndings != "lf" {
		fmt.Printf("note: %s ships the umbrella header with %s line endings; wrote the canonical LF form (archive sha256 %s)\n",
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

	independent, err := verifyIndependentIdentity(root, spec, pinned, librarySHA, librarySize)
	if err != nil {
		return err
	}
	if independent.status == "verified" {
		fmt.Printf("independent identity: %s matches %s\n", librarySHA, independent.module)
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
		"header_count=" + strconv.Itoa(len(headers.files)),
		"abi_header=" + headers.abiHeader,
		"library=" + libraryPath,
		"library_bytes=" + strconv.FormatInt(librarySize, 10),
		"library_sha256=" + librarySHA,
		"library_format=" + format,
		"library_machine=" + machine,
		"independent_identity_source=" + independent.source,
		"independent_identity_module=" + independent.module,
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
	if headers.split {
		record = append(record,
			"headers="+headers.dir,
			"header_manifest="+headers.manifestPath,
			"header_manifest_sha256="+headers.identity,
			"header_identity_sha256="+headers.identity,
		)
	}
	if pinned.headerIdentitySHA != "" {
		record = append(record, "upstream_header_identity_sha256="+pinned.headerIdentitySHA)
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

// readPin reads the provenance constants from internal/ffi/upstream.go.
// upstreamHeaderIdentitySHA256 is optional and is used for a split header set.
// acquiredHeaderSet is the verified public C header set extracted from one
// release archive. split distinguishes the eight-header layout from a legacy
// archive that ships only include/lance/lance.h.
type acquiredHeaderSet struct {
	dir                 string
	files               []headeridentity.File
	identity            string
	split               bool
	manifestPath        string
	abiHeader           string
	umbrellaPath        string
	umbrellaSHA         string
	umbrellaArchiveSHA  string
	umbrellaLineEndings string
}

// verifyAcquiredHeaders canonicalizes the extracted headers, verifies them
// against the independent expectation, and writes the manifest evidence for a
// split set. A legacy single header keeps the published upstreamHeaderSHA256
// check and output shape.
func verifyAcquiredHeaders(outDir string, pinned pin, expected headerExpectation) (acquiredHeaderSet, error) {
	dir := filepath.Join(outDir, "include", "lance")
	raw, err := headeridentity.Scan(dir)
	if err != nil {
		return acquiredHeaderSet{}, err
	}
	split := len(raw) > 1 || raw[0].Path != headeridentity.Umbrella
	result := acquiredHeaderSet{
		dir:          dir,
		split:        split,
		abiHeader:    dir,
		umbrellaPath: filepath.Join(dir, "lance.h"),
	}

	if !split {
		result.abiHeader = result.umbrellaPath
		canonicalSHA, archiveSHA, lineEndings, err := canonicalHeader(result.umbrellaPath, pinned.headerSHA)
		if err != nil {
			return acquiredHeaderSet{}, fmt.Errorf("%s: %w", headeridentity.Umbrella, err)
		}
		result.umbrellaSHA = canonicalSHA
		result.umbrellaArchiveSHA = archiveSHA
		result.umbrellaLineEndings = lineEndings
		if expected.manifest != nil || expected.identity != "" {
			files, identity, err := headeridentity.Verify(dir, expected.manifest, expected.identity)
			if err != nil {
				return acquiredHeaderSet{}, err
			}
			result.files = files
			result.identity = identity
		} else {
			files, err := headeridentity.Scan(dir)
			if err != nil {
				return acquiredHeaderSet{}, err
			}
			result.files = files
		}
		return result, nil
	}

	expectedHashes := map[string]string{}
	if expected.manifest != nil {
		manifest, err := headeridentity.Parse(expected.manifest)
		if err != nil {
			return acquiredHeaderSet{}, fmt.Errorf("parse expected %s: %w", headeridentity.ManifestName, err)
		}
		for _, file := range manifest {
			expectedHashes[file.Path] = file.SHA256
		}
	}

	for _, file := range raw {
		path := filepath.Join(dir, filepath.Base(file.Path))
		var canonicalSHA, archiveSHA, lineEndings string
		if want, ok := expectedHashes[file.Path]; ok {
			canonicalSHA, archiveSHA, lineEndings, err = canonicalHeaderAgainst(path, want,
				"expected "+headeridentity.ManifestName, "the release does not contain the expected header")
		} else {
			canonicalSHA, archiveSHA, lineEndings, err = canonicalHeaderLoose(path)
		}
		if err != nil {
			return acquiredHeaderSet{}, fmt.Errorf("%s: %w", file.Path, err)
		}
		if file.Path == headeridentity.Umbrella {
			result.umbrellaSHA = canonicalSHA
			result.umbrellaArchiveSHA = archiveSHA
			result.umbrellaLineEndings = lineEndings
		}
	}

	if expected.manifest == nil && expected.identity == "" {
		return acquiredHeaderSet{}, fmt.Errorf("split header set requires an expected identity; pass -expected-header-identity or -expected-header-manifest, or record upstreamHeaderIdentitySHA256 in the pin")
	}
	files, identity, err := headeridentity.Verify(dir, expected.manifest, expected.identity)
	if err != nil {
		return acquiredHeaderSet{}, err
	}
	result.files = files
	result.identity = identity

	manifest, err := headeridentity.Encode(result.files)
	if err != nil {
		return acquiredHeaderSet{}, err
	}
	result.manifestPath = filepath.Join(outDir, headeridentity.ManifestName)
	if err := os.WriteFile(result.manifestPath, manifest, 0o644); err != nil {
		return acquiredHeaderSet{}, fmt.Errorf("write %s: %w", result.manifestPath, err)
	}
	compositePath := filepath.Join(outDir, headeridentity.CompositeName)
	if err := os.WriteFile(compositePath, []byte(result.identity+"\n"), 0o644); err != nil {
		return acquiredHeaderSet{}, fmt.Errorf("write %s: %w", compositePath, err)
	}
	return result, nil
}

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
		repo:              values["upstreamRepo"],
		release:           values["upstreamRelease"],
		commit:            values["upstreamCommit"],
		headerSHA:         values["upstreamHeaderSHA256"],
		headerIdentitySHA: values["upstreamHeaderIdentitySHA256"],
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
	if value := pinned.headerIdentitySHA; value != "" && !sha256Pattern.MatchString(value) {
		return pin{}, fmt.Errorf("pin %s has upstreamHeaderIdentitySHA256 = %q, want a lowercase SHA-256", path, value)
	}
	return pinned, nil
}

var (
	urlPattern     = regexp.MustCompile(`^https://[A-Za-z0-9.-]+/[\w./-]+$`)
	releasePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

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

// canonicalHeader verifies the extracted legacy header against the pin. A
// header that differs only by CRLF line endings is accepted and rewritten in
// canonical LF form; both hashes are recorded.
func canonicalHeader(path, pinSHA string) (canonicalSHA, archiveSHA, lineEndings string, err error) {
	return canonicalHeaderAgainst(path, pinSHA, "pin", "the release does not contain the pinned header")
}

// canonicalHeaderAgainst verifies one header against an expected SHA-256. A
// header that differs only by CRLF line endings is accepted and rewritten in
// canonical LF form. expectation and failure describe the source of the
// expected digest in error messages.
func canonicalHeaderAgainst(path, expectedSHA, expectation, failure string) (canonicalSHA, archiveSHA, lineEndings string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", err
	}
	archiveSHA = sha256Hex(raw)
	canonical := raw
	lineEndings = "lf"
	if archiveSHA != expectedSHA {
		normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
		normalizedSHA := sha256Hex(normalized)
		if normalizedSHA != expectedSHA {
			return "", "", "", fmt.Errorf("sha256 is %s as published and %s with LF line endings, %s says %s; %s",
				archiveSHA, normalizedSHA, expectation, expectedSHA, failure)
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

// canonicalHeaderLoose normalizes CRLF line endings for a split header that
// has no per-file expected digest yet. The identity check then binds the
// canonical LF manifest.
func canonicalHeaderLoose(path string) (canonicalSHA, archiveSHA, lineEndings string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", err
	}
	archiveSHA = sha256Hex(raw)
	canonical := raw
	lineEndings = "lf"
	if bytes.Contains(raw, []byte("\r\n")) {
		canonical = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
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
// replaced together; verifyIndependentIdentity adds the adopted module identity
// for that case.
func verifyArchiveChecksum(observed, published, checksumName string) error {
	if observed != published {
		return fmt.Errorf("archive sha256 is %s, published %s says %s; the download is corrupt or the published asset changed",
			observed, checksumName, published)
	}
	return nil
}

type independentIdentity struct {
	source       string
	module       string
	status       string
	expectedSHA  string
	expectedSize int64
	reason       string
}

// adoptedArtifact identifies the artifact module that a default build for one
// target embeds. An empty reason means the module was found.
type adoptedArtifact struct {
	selector string
	module   string
	version  string
	reason   string
}

// verifyIndependentIdentity ties the acquired library to the immutable artifact
// module that the target's generated selector registers. The module is
// resolved through Go module resolution, so its bytes are pinned by go.sum and
// the checksum database, independently of the release archive and its published
// checksum. The check is inactive when the adopted module describes a different
// upstream revision, which is the normal state between a pin bump and module
// republication, or when the target has no adopted module yet. Candidate
// packaging therefore depends only on the acquired bytes and the pin.
func verifyIndependentIdentity(root string, spec targetSpec, pinned pin, librarySHA string, librarySize int64) (independentIdentity, error) {
	adopted, err := readAdoptedModule(root, spec)
	if err != nil {
		return independentIdentity{}, err
	}
	result := independentIdentity{source: adopted.selector, module: adopted.module}
	if adopted.reason != "" {
		result.status = "inactive"
		result.reason = adopted.reason
		return result, nil
	}
	result.module = adopted.module + "@" + adopted.version

	moduleDir, err := resolveAdoptedModuleDir(adopted.module, adopted.version)
	if err != nil {
		return result, err
	}
	metadataPath := filepath.Join(moduleDir, "artifact.go")
	md, err := artifactmeta.Read(metadataPath)
	if err != nil {
		return result, fmt.Errorf("read adopted artifact metadata %s: %w", metadataPath, err)
	}
	if md.Target != spec.name || md.LibraryName != filepath.Base(spec.libraryMember) {
		return result, fmt.Errorf("adopted artifact metadata %s records %s/%s, want %s/%s",
			metadataPath, md.Target, md.LibraryName, spec.name, filepath.Base(spec.libraryMember))
	}
	if md.UpstreamRepo != pinned.repo || md.UpstreamCommit != pinned.commit || md.HeaderSHA256 != pinned.headerSHA {
		result.status = "inactive"
		result.reason = fmt.Sprintf("%s records %s@%s with header sha256 %s; the pin records %s@%s with header sha256 %s",
			result.module, md.UpstreamRepo, md.UpstreamCommit, md.HeaderSHA256, pinned.repo, pinned.commit, pinned.headerSHA)
		return result, nil
	}
	result.status = "verified"
	result.expectedSHA = md.UncompressedSHA256
	result.expectedSize = md.UncompressedSize
	if md.UncompressedSHA256 != librarySHA || md.UncompressedSize != librarySize {
		return result, fmt.Errorf("library sha256 is %s (%d bytes), but %s records %s (%d bytes) for %s@%s; the release archive does not carry the bytes this module publishes",
			librarySHA, librarySize, result.module, md.UncompressedSHA256, md.UncompressedSize, pinned.repo, pinned.commit)
	}
	return result, nil
}

// readAdoptedModule finds the artifact module that internal/bundled selects for
// this target. The generated selector is the canonical statement of which
// module a default build embeds, and go.mod records the immutable version it
// requires. A missing selector or requirement is reported as an inactive check
// rather than an error so a target can be acquired before its module is
// published.
func readAdoptedModule(root string, spec targetSpec) (adoptedArtifact, error) {
	goos, goarch, ok := strings.Cut(spec.name, "/")
	if !ok {
		return adoptedArtifact{}, fmt.Errorf("target %q is not GOOS/GOARCH", spec.name)
	}
	identity, err := readGoModIdentity(filepath.Join(root, "go.mod"))
	if err != nil {
		return adoptedArtifact{}, err
	}
	result := adoptedArtifact{selector: filepath.Join(root, "internal", "bundled", "lance_bundle_"+goos+"_"+goarch+".go")}
	module, err := selectorArtifactModule(result.selector, identity.module)
	if errors.Is(err, fs.ErrNotExist) {
		result.reason = fmt.Sprintf("no generated selector at %s", result.selector)
		return result, nil
	}
	if err != nil {
		return adoptedArtifact{}, err
	}
	if module == "" {
		result.reason = fmt.Sprintf("selector %s does not import an artifact module", result.selector)
		return result, nil
	}
	version, ok := identity.requirements[module]
	if !ok {
		result.reason = fmt.Sprintf("%s requires no version of %s", identity.path, module)
		return result, nil
	}
	result.module = module
	result.version = version
	return result, nil
}

// goModIdentity is the part of go.mod that identifies the modules a default
// build embeds.
type goModIdentity struct {
	path         string
	module       string
	requirements map[string]string
}

// readGoModIdentity reads the module path and every required version from a
// go.mod file. It deliberately accepts only the single-line requirement form
// that go.mod uses for released module versions.
func readGoModIdentity(path string) (goModIdentity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return goModIdentity{}, fmt.Errorf("read %s: %w", path, err)
	}
	identity := goModIdentity{requirements: map[string]string{}}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		switch {
		case fields[0] == "module":
			identity.module = fields[1]
		case strings.HasPrefix(fields[1], "v"):
			if _, exists := identity.requirements[fields[0]]; !exists {
				identity.requirements[fields[0]] = fields[1]
			}
		}
	}
	if identity.module == "" {
		return goModIdentity{}, fmt.Errorf("%s declares no module path", path)
	}
	identity.path = path
	return identity, nil
}

// selectorArtifactModule returns the module that a generated selector imports
// besides packages of the repository itself.
func selectorArtifactModule(path, rootModule string) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return "", fmt.Errorf("parse selector %s: %w", path, err)
	}
	var modules []string
	for _, specification := range file.Imports {
		importPath, err := strconv.Unquote(specification.Path.Value)
		if err != nil {
			return "", fmt.Errorf("selector %s has an unreadable import: %w", path, err)
		}
		if importPath == rootModule || strings.HasPrefix(importPath, rootModule+"/") {
			continue
		}
		modules = append(modules, importPath)
	}
	switch len(modules) {
	case 0:
		return "", nil
	case 1:
		return modules[0], nil
	default:
		return "", fmt.Errorf("selector %s imports more than one artifact module: %s", path, strings.Join(modules, ", "))
	}
}

// resolveAdoptedModuleDir resolves the adopted module version to a directory.
// It is a variable so tests can substitute a module tree without a toolchain.
var resolveAdoptedModuleDir = resolveModuleDir

// resolveModuleDir asks the Go toolchain for the directory of one released
// module version. go.sum and the checksum database therefore verify the
// metadata this check reads.
func resolveModuleDir(modulePath, version string) (string, error) {
	command := exec.Command("go", "mod", "download", "-json", modulePath+"@"+version)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("resolve %s@%s: %w: %s", modulePath, version, err, strings.TrimSpace(stderr.String()))
	}
	var result struct {
		Dir   string
		Error string
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return "", fmt.Errorf("parse go mod download output for %s@%s: %w", modulePath, version, err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("resolve %s@%s: %s", modulePath, version, result.Error)
	}
	if result.Dir == "" {
		return "", fmt.Errorf("resolve %s@%s: go mod download reported no directory", modulePath, version)
	}
	return result.Dir, nil
}

// extractMembers streams the compressed tar archive once and writes the
// requested regular-file members plus every public include/lance/*.h header.
// Archive names with or without a leading "./" are accepted. It returns every
// member name it saw so a missing expected member can be reported against the
// published layout.
func extractMembers(archivePath, outDir string, members []string) ([]string, error) {
	wanted := make(map[string]bool, len(members))
	canonicalMembers := make([]string, 0, len(members))
	for _, member := range members {
		clean, err := canonicalArchiveMember(member)
		if err != nil {
			return nil, fmt.Errorf("expected member %q: %w", member, err)
		}
		wanted[clean] = true
		canonicalMembers = append(canonicalMembers, clean)
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	xzReader, err := xz.NewReader(archive)
	if err != nil {
		return nil, fmt.Errorf("open xz stream: %w", err)
	}
	reader := tar.NewReader(xzReader)
	extracted := make(map[string]bool, len(members))
	var seen []string
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return seen, fmt.Errorf("read tar stream: %w", err)
		}
		clean, err := canonicalArchiveMember(header.Name)
		if err != nil {
			return seen, fmt.Errorf("archive member %q: %w", header.Name, err)
		}
		seen = append(seen, clean)
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			return seen, fmt.Errorf("archive member %s has unsupported type %q; the release archive must contain only regular files and directories", header.Name, header.Typeflag)
		}
		if !wanted[clean] && !isPublicHeader(clean) {
			continue
		}
		if clean == "." {
			return seen, fmt.Errorf("archive member %q escapes the output directory", header.Name)
		}
		if extracted[clean] {
			return seen, fmt.Errorf("archive member %s appears more than once", clean)
		}
		if header.Size <= 0 {
			return seen, fmt.Errorf("archive member %s is empty", clean)
		}
		destination := filepath.Join(outDir, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return seen, fmt.Errorf("extract %s: %w", clean, err)
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return seen, fmt.Errorf("extract %s: %w", clean, err)
		}
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		if copyErr != nil {
			return seen, fmt.Errorf("extract %s: %w", clean, copyErr)
		}
		if closeErr != nil {
			return seen, fmt.Errorf("extract %s: %w", clean, closeErr)
		}
		extracted[clean] = true
	}
	for _, member := range canonicalMembers {
		if !extracted[member] {
			return seen, fmt.Errorf("archive does not contain %s; published members: %s", member, memberSummary(seen))
		}
	}
	return seen, nil
}

// canonicalArchiveMember normalizes an archive member name so the legacy
// "./include/..." form and the split "include/..." form match the same entry.
func canonicalArchiveMember(name string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", fmt.Errorf("member escapes the output directory")
	}
	return clean, nil
}

// isPublicHeader reports whether name is a public C header directly under
// include/lance. lance.hpp is deliberately excluded.
func isPublicHeader(name string) bool {
	return path.Dir(name) == headeridentity.Dir && strings.HasSuffix(name, ".h")
}

func memberSummary(members []string) string {
	names := append([]string(nil), members...)
	sort.Strings(names)
	if len(names) > 12 {
		names = append(names[:12], "...")
	}
	return strings.Join(names, ", ")
}
