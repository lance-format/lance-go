// Command verify-lance-c-artifact verifies one generated lance-c artifact
// module against the native library it was packaged from and the pin in
// internal/ffi/upstream.go.
//
// It checks the compressed payload hash and size, decompresses the payload,
// compares it byte for byte with the library, and checks the uncompressed hash
// and size. It never loads or executes the library.
//
// Usage:
//
//	go run ./scripts/verify-lance-c-artifact \
//	  -module /tmp/staging/linux-amd64/module \
//	  -library /path/to/liblance_c.so \
//	  -target linux/amd64 \
//	  -module-path github.com/lance-format/lance-go/artifacts/staging/linux-amd64
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
)

type metadata struct {
	target             string
	libraryName        string
	compressedSize     int64
	compressedSHA256   string
	uncompressedSize   int64
	uncompressedSHA256 string
	upstreamRepo       string
	upstreamCommit     string
	headerSHA256       string
}

var constantLine = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z][A-Za-z0-9]*)[ \t]*(?:int64[ \t]*)?=[ \t]*(?:"([^"]*)"|([0-9]+))[ \t]*$`)

func main() {
	moduleDir := flag.String("module", "", "generated module directory to verify")
	libraryPath := flag.String("library", "", "native library the module was packaged from")
	pinPath := flag.String("pin", "internal/ffi/upstream.go", "pin file with the upstream provenance constants")
	target := flag.String("target", "", "expected target as GOOS/GOARCH")
	modulePath := flag.String("module-path", "", "expected module path in module/go.mod")
	flag.Parse()

	if flag.NArg() != 0 {
		fail(fmt.Errorf("unexpected arguments: %s", strings.Join(flag.Args(), " ")))
	}
	if *moduleDir == "" || *libraryPath == "" || *target == "" {
		flag.Usage()
		fail(errors.New("-module, -library, and -target are required"))
	}

	md, err := readMetadata(filepath.Join(*moduleDir, "artifact.go"))
	if err != nil {
		fail(err)
	}
	if err := verifyModuleLayout(*moduleDir, md.libraryName+".zst"); err != nil {
		fail(err)
	}
	if md.target != *target {
		fail(fmt.Errorf("artifact target is %q, want %q", md.target, *target))
	}

	libraryInfo, err := regularFile(*libraryPath)
	if err != nil {
		fail(fmt.Errorf("library: %w", err))
	}
	if filepath.Base(*libraryPath) != md.libraryName {
		fail(fmt.Errorf("library basename is %q, want %q", filepath.Base(*libraryPath), md.libraryName))
	}

	payloadPath := filepath.Join(*moduleDir, md.libraryName+".zst")
	payloadInfo, err := regularFile(payloadPath)
	if err != nil {
		fail(fmt.Errorf("payload: %w", err))
	}
	if payloadInfo.Size() != md.compressedSize {
		fail(fmt.Errorf("payload is %d bytes, artifact says %d", payloadInfo.Size(), md.compressedSize))
	}
	payloadSHA, err := sha256File(payloadPath)
	if err != nil {
		fail(err)
	}
	if payloadSHA != md.compressedSHA256 {
		fail(fmt.Errorf("payload sha256 is %s, artifact says %s", payloadSHA, md.compressedSHA256))
	}

	pin, err := os.ReadFile(*pinPath)
	if err != nil {
		fail(fmt.Errorf("read pin: %w", err))
	}
	for _, check := range []struct {
		name string
		want string
	}{
		{"upstreamRepo", md.upstreamRepo},
		{"upstreamCommit", md.upstreamCommit},
		{"upstreamHeaderSHA256", md.headerSHA256},
	} {
		got, err := pinConstant(string(pin), check.name)
		if err != nil {
			fail(err)
		}
		if got != check.want {
			fail(fmt.Errorf("artifact %s is %s, pin says %s", check.name, check.want, got))
		}
	}

	gotModulePath, goVersion, err := readGoMod(filepath.Join(*moduleDir, "go.mod"))
	if err != nil {
		fail(err)
	}
	if *modulePath != "" && gotModulePath != *modulePath {
		fail(fmt.Errorf("module path is %q, want %q", gotModulePath, *modulePath))
	}

	librarySHA, err := sha256File(*libraryPath)
	if err != nil {
		fail(err)
	}
	libraryFile, err := os.Open(*libraryPath)
	if err != nil {
		fail(fmt.Errorf("open library: %w", err))
	}
	defer libraryFile.Close()
	payloadFile, err := os.Open(payloadPath)
	if err != nil {
		fail(fmt.Errorf("open payload: %w", err))
	}
	defer payloadFile.Close()

	decoder, err := zstd.NewReader(payloadFile, zstd.WithDecoderConcurrency(1))
	if err != nil {
		fail(fmt.Errorf("open zstd payload: %w", err))
	}
	defer decoder.Close()

	decompressedHash := sha256.New()
	compared, err := equalStreams(libraryFile, io.TeeReader(decoder, decompressedHash))
	if err != nil {
		fail(err)
	}
	if compared != libraryInfo.Size() {
		fail(fmt.Errorf("decompressed payload is %d bytes, library is %d", compared, libraryInfo.Size()))
	}
	if compared != md.uncompressedSize {
		fail(fmt.Errorf("decompressed payload is %d bytes, artifact says %d", compared, md.uncompressedSize))
	}
	decompressedSHA := hex.EncodeToString(decompressedHash.Sum(nil))
	if decompressedSHA != md.uncompressedSHA256 {
		fail(fmt.Errorf("decompressed payload sha256 is %s, artifact says %s", decompressedSHA, md.uncompressedSHA256))
	}
	if librarySHA != md.uncompressedSHA256 {
		fail(fmt.Errorf("library sha256 is %s, artifact says %s", librarySHA, md.uncompressedSHA256))
	}

	fmt.Printf("artifact_module=%s\n", *moduleDir)
	fmt.Printf("module_path=%s\n", gotModulePath)
	fmt.Printf("go_version=%s\n", goVersion)
	fmt.Printf("target=%s\n", md.target)
	fmt.Printf("library=%s\n", *libraryPath)
	fmt.Printf("library_bytes=%d\n", libraryInfo.Size())
	fmt.Printf("library_sha256=%s\n", librarySHA)
	fmt.Printf("payload=%s\n", payloadPath)
	fmt.Printf("payload_bytes=%d\n", payloadInfo.Size())
	fmt.Printf("payload_sha256=%s\n", payloadSHA)
	fmt.Printf("decompressed_bytes=%d\n", compared)
	fmt.Printf("decompressed_sha256=%s\n", decompressedSHA)
	fmt.Printf("byte_equality=identical\n")
	fmt.Printf("upstream_repo=%s\n", md.upstreamRepo)
	fmt.Printf("upstream_commit=%s\n", md.upstreamCommit)
	fmt.Printf("header_sha256=%s\n", md.headerSHA256)
	fmt.Printf("artifact_verification=ok\n")
}

// verifyModuleLayout connects the verified bytes to the loader's Payload
// variable. Only the generated module files and an optional license belong in
// a data-only companion module.
func verifyModuleLayout(dir, payloadName string) error {
	required := map[string]bool{"artifact.go": false, "go.mod": false, payloadName: false}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if _, ok := required[name]; !ok && name != "LICENSE" {
			return fmt.Errorf("unexpected artifact module entry %q", name)
		}
		if _, err := regularFile(filepath.Join(dir, name)); err != nil {
			return err
		}
		required[name] = true
	}
	for name, present := range required {
		if !present {
			return fmt.Errorf("artifact module is missing %q", name)
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "artifact.go"), nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse artifact source: %w", err)
	}
	if file.Name.Name != "artifact" || len(file.Imports) != 1 {
		return errors.New("artifact module must use package artifact and import only embed")
	}
	imp := file.Imports[0]
	imported, err := strconv.Unquote(imp.Path.Value)
	if err != nil || imported != "embed" || imp.Name == nil || imp.Name.Name != "_" {
		return errors.New("artifact module must blank-import embed")
	}
	payloads := 0
	for _, declaration := range file.Decls {
		decl, ok := declaration.(*ast.GenDecl)
		if !ok {
			return errors.New("artifact module must contain only data declarations")
		}
		if decl.Tok == token.IMPORT || decl.Tok == token.CONST {
			continue
		}
		if decl.Tok != token.VAR || len(decl.Specs) != 1 {
			return errors.New("artifact module must declare only the Payload variable")
		}
		spec, ok := decl.Specs[0].(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "Payload" || len(spec.Values) != 0 {
			return errors.New("artifact module must declare var Payload []byte")
		}
		slice, ok := spec.Type.(*ast.ArrayType)
		if !ok || slice.Len != nil {
			return errors.New("artifact Payload must have type []byte")
		}
		element, ok := slice.Elt.(*ast.Ident)
		if !ok || element.Name != "byte" {
			return errors.New("artifact Payload must have type []byte")
		}
		doc := spec.Doc
		if doc == nil {
			doc = decl.Doc
		}
		directives := 0
		if doc != nil {
			for _, comment := range doc.List {
				if !strings.HasPrefix(comment.Text, "//go:embed ") {
					continue
				}
				// The generator emits one quoted filename, never a glob or directory.
				name, err := strconv.Unquote(strings.TrimSpace(strings.TrimPrefix(comment.Text, "//go:embed ")))
				if err != nil || name != payloadName {
					return errors.New("artifact Payload must embed the verified payload file")
				}
				directives++
			}
		}
		if directives != 1 {
			return errors.New("artifact Payload must have exactly one embed directive")
		}
		payloads++
	}
	if payloads != 1 {
		return errors.New("artifact module must declare exactly one Payload variable")
	}
	return nil
}

func readMetadata(path string) (metadata, error) {
	var md metadata
	data, err := os.ReadFile(path)
	if err != nil {
		return md, fmt.Errorf("read artifact metadata: %w", err)
	}
	values := make(map[string]string)
	for _, match := range constantLine.FindAllStringSubmatch(string(data), -1) {
		name := match[1]
		if _, exists := values[name]; exists {
			return md, fmt.Errorf("artifact metadata has duplicate %s", name)
		}
		value := match[2]
		if value == "" {
			value = match[3]
		}
		values[name] = value
	}

	required := []string{
		"Target", "LibraryName", "CompressedSize", "CompressedSHA256",
		"UncompressedSize", "UncompressedSHA256", "UpstreamRepo",
		"UpstreamCommit", "HeaderSHA256",
	}
	for _, name := range required {
		if _, ok := values[name]; !ok {
			return md, fmt.Errorf("artifact metadata is missing %s", name)
		}
	}

	md.target = values["Target"]
	md.libraryName = values["LibraryName"]
	md.compressedSHA256 = values["CompressedSHA256"]
	md.uncompressedSHA256 = values["UncompressedSHA256"]
	md.upstreamRepo = values["UpstreamRepo"]
	md.upstreamCommit = values["UpstreamCommit"]
	md.headerSHA256 = values["HeaderSHA256"]

	if md.compressedSize, err = strconv.ParseInt(values["CompressedSize"], 10, 64); err != nil {
		return md, fmt.Errorf("artifact CompressedSize: %w", err)
	}
	if md.uncompressedSize, err = strconv.ParseInt(values["UncompressedSize"], 10, 64); err != nil {
		return md, fmt.Errorf("artifact UncompressedSize: %w", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(md.compressedSHA256) {
		return md, fmt.Errorf("artifact CompressedSHA256 is not a lowercase SHA-256: %q", md.compressedSHA256)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(md.uncompressedSHA256) {
		return md, fmt.Errorf("artifact UncompressedSHA256 is not a lowercase SHA-256: %q", md.uncompressedSHA256)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(md.headerSHA256) {
		return md, fmt.Errorf("artifact HeaderSHA256 is not a lowercase SHA-256: %q", md.headerSHA256)
	}
	return md, nil
}

func readGoMod(path string) (modulePath, goVersion string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read module go.mod: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "module "):
			modulePath = strings.TrimSpace(strings.TrimPrefix(line, "module "))
		case strings.HasPrefix(line, "go "):
			goVersion = strings.TrimSpace(strings.TrimPrefix(line, "go "))
		case line == "require (" || strings.HasPrefix(line, "require "):
			return "", "", errors.New("artifact module must not declare requirements")
		}
	}
	if modulePath == "" {
		return "", "", errors.New("artifact module has no module path")
	}
	if goVersion == "" {
		return "", "", errors.New("artifact module has no go directive")
	}
	return modulePath, goVersion, nil
}

func pinConstant(src, name string) (string, error) {
	pattern := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(name) + `[ \t]*=[ \t]*"([^"]*)"[ \t]*$`)
	matches := pattern.FindAllStringSubmatch(src, -1)
	if len(matches) != 1 {
		return "", fmt.Errorf("expected exactly one %s in pin file, found %d", name, len(matches))
	}
	return matches[0][1], nil
}

func regularFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return info, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func equalStreams(a, b io.Reader) (int64, error) {
	const chunk = 1 << 20
	bufA := make([]byte, chunk)
	bufB := make([]byte, chunk)
	var total int64
	for {
		nA, errA := io.ReadFull(a, bufA)
		nB, errB := io.ReadFull(b, bufB)
		if nA != nB || !bytes.Equal(bufA[:nA], bufB[:nB]) {
			return total, fmt.Errorf("library and decompressed payload differ at byte %d", total)
		}
		total += int64(nA)
		doneA := errors.Is(errA, io.EOF) || errors.Is(errA, io.ErrUnexpectedEOF)
		doneB := errors.Is(errB, io.EOF) || errors.Is(errB, io.ErrUnexpectedEOF)
		if doneA || doneB {
			if doneA && doneB {
				return total, nil
			}
			return total, fmt.Errorf("library and decompressed payload have different sizes at byte %d", total)
		}
		if errA != nil {
			return total, fmt.Errorf("read library: %w", errA)
		}
		if errB != nil {
			return total, fmt.Errorf("read decompressed payload: %w", errB)
		}
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "verify-lance-c-artifact: %v\n", err)
	os.Exit(1)
}
