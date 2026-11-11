// Package artifactgen builds a data-only Go module and a platform selector
// from an already-built lance-c shared library and its matching header.
//
// The generated module embeds a zstd-compressed copy of the library. It does
// not import the root module, link native code, or download anything. The
// generated selector is normally copied into internal/bundled, which is the
// only package that imports internal/loader for compiled-in activation.
package artifactgen

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/klauspost/compress/zstd"
)

const goModVersion = "1.23.0"

var supportedTargets = []targetSpec{
	{goos: "linux", goarch: "amd64", libraryName: "liblance_c.so"},
	{goos: "linux", goarch: "arm64", libraryName: "liblance_c.so"},
	{goos: "darwin", goarch: "amd64", libraryName: "liblance_c.dylib"},
	{goos: "darwin", goarch: "arm64", libraryName: "liblance_c.dylib"},
	{goos: "windows", goarch: "amd64", libraryName: "lance_c.dll"},
	{goos: "windows", goarch: "arm64", libraryName: "lance_c.dll"},
}

type targetSpec struct {
	goos        string
	goarch      string
	libraryName string
}

// Options configures one generated artifact module.
type Options struct {
	LibraryPath     string
	HeaderPath      string
	Target          string
	ModulePath      string
	OutDir          string
	SelectorPackage string
	UpstreamRepo    string
	UpstreamCommit  string
	HeaderSHA256    string
}

// Metadata records the checksums and provenance written into the generated
// module.
type Metadata struct {
	Target             string
	LibraryName        string
	CompressedSize     int64
	CompressedSHA256   string
	UncompressedSize   int64
	UncompressedSHA256 string
	UpstreamCommit     string
	HeaderSHA256       string
}

// Result describes a successfully published output directory.
type Result struct {
	ModuleDir    string
	SelectorPath string
	Metadata     Metadata
}

// Generate verifies the inputs, compresses the library, and publishes a new
// output directory. It never overwrites an existing output directory.
func Generate(opts Options) (Result, error) {
	var zero Result

	libPath, err := existingRegularFile(opts.LibraryPath, "library")
	if err != nil {
		return zero, err
	}
	headerPath, err := existingRegularFile(opts.HeaderPath, "header")
	if err != nil {
		return zero, err
	}
	if opts.OutDir == "" {
		return zero, errors.New("output directory is required")
	}
	opts.OutDir = filepath.Clean(opts.OutDir)

	spec, err := parseTarget(opts.Target)
	if err != nil {
		return zero, err
	}
	if filepath.Base(libPath) != spec.libraryName {
		return zero, fmt.Errorf("library basename is %q, want %q for %s", filepath.Base(libPath), spec.libraryName, opts.Target)
	}
	if err := validateModulePath(opts.ModulePath); err != nil {
		return zero, err
	}
	if err := validateSelectorPackage(opts.SelectorPackage); err != nil {
		return zero, err
	}
	if strings.TrimSpace(opts.UpstreamRepo) == "" {
		return zero, errors.New("upstream repository is required")
	}
	if !isLowerHex(opts.UpstreamCommit, 40) {
		return zero, fmt.Errorf("upstream commit %q is not a 40-character lowercase hex commit", opts.UpstreamCommit)
	}
	if !isLowerHex(opts.HeaderSHA256, 64) {
		return zero, fmt.Errorf("header SHA-256 %q is not 64 lowercase hex characters", opts.HeaderSHA256)
	}

	if err := rejectExistingOutput(opts.OutDir); err != nil {
		return zero, err
	}
	if err := inspectLibrary(libPath, spec); err != nil {
		return zero, err
	}

	headerHash, _, err := hashFile(headerPath)
	if err != nil {
		return zero, fmt.Errorf("hash header %s: %w", headerPath, err)
	}
	if headerHash != opts.HeaderSHA256 {
		return zero, fmt.Errorf("header %s has sha256 %s, want pinned %s", headerPath, headerHash, opts.HeaderSHA256)
	}

	parent := filepath.Dir(opts.OutDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return zero, fmt.Errorf("create output parent %s: %w", parent, err)
	}
	staging, err := os.MkdirTemp(parent, ".sync-lance-c-*")
	if err != nil {
		return zero, fmt.Errorf("create staging directory in %s: %w", parent, err)
	}
	published := false
	defer func() {
		if !published {
			os.RemoveAll(staging)
		}
	}()

	moduleDir := filepath.Join(staging, "module")
	selectorDir := filepath.Join(staging, "selector")
	if err := os.Mkdir(moduleDir, 0o755); err != nil {
		return zero, fmt.Errorf("create staging module directory: %w", err)
	}
	if err := os.Mkdir(selectorDir, 0o755); err != nil {
		return zero, fmt.Errorf("create staging selector directory: %w", err)
	}

	payloadName := spec.libraryName + ".zst"
	payloadPath := filepath.Join(moduleDir, payloadName)
	compressedSize, uncompressedSize, uncompressedSHA, compressedSHA, err := compressLibrary(libPath, payloadPath)
	if err != nil {
		return zero, err
	}
	meta := Metadata{
		Target:             opts.Target,
		LibraryName:        spec.libraryName,
		CompressedSize:     compressedSize,
		CompressedSHA256:   compressedSHA,
		UncompressedSize:   uncompressedSize,
		UncompressedSHA256: uncompressedSHA,
		UpstreamCommit:     opts.UpstreamCommit,
		HeaderSHA256:       opts.HeaderSHA256,
	}

	artifactPath := filepath.Join(moduleDir, "artifact.go")
	if err := writeTemplate(artifactPath, artifactTemplate, struct {
		Metadata
		PayloadName  string
		UpstreamRepo string
	}{
		Metadata:     meta,
		PayloadName:  payloadName,
		UpstreamRepo: opts.UpstreamRepo,
	}); err != nil {
		return zero, err
	}

	goMod := "module " + opts.ModulePath + "\n\ngo " + goModVersion + "\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return zero, fmt.Errorf("write generated go.mod: %w", err)
	}

	selectorPath := filepath.Join(selectorDir, fmt.Sprintf("lance_bundle_%s_%s.go", spec.goos, spec.goarch))
	if err := writeTemplate(selectorPath, selectorTemplate, struct {
		GOOS            string
		GOARCH          string
		SelectorPackage string
		ModulePath      string
	}{
		GOOS:            spec.goos,
		GOARCH:          spec.goarch,
		SelectorPackage: opts.SelectorPackage,
		ModulePath:      opts.ModulePath,
	}); err != nil {
		return zero, err
	}

	if err := rejectExistingOutput(opts.OutDir); err != nil {
		return zero, err
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		return zero, fmt.Errorf("set output directory permissions: %w", err)
	}
	if err := os.Rename(staging, opts.OutDir); err != nil {
		return zero, fmt.Errorf("publish output directory %s: %w", opts.OutDir, err)
	}
	published = true

	return Result{
		ModuleDir:    filepath.Join(opts.OutDir, "module"),
		SelectorPath: filepath.Join(opts.OutDir, "selector", filepath.Base(selectorPath)),
		Metadata:     meta,
	}, nil
}

func existingRegularFile(path, kind string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%s path is required", kind)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s %s: %w", kind, path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s %s is not a regular file (mode %s)", kind, path, info.Mode())
	}
	if info.Size() == 0 {
		return "", fmt.Errorf("%s %s is empty", kind, path)
	}
	return filepath.Clean(path), nil
}

func parseTarget(value string) (targetSpec, error) {
	targets := make([]string, 0, len(supportedTargets))
	for _, spec := range supportedTargets {
		target := spec.goos + "/" + spec.goarch
		if value == target {
			return spec, nil
		}
		targets = append(targets, target)
	}
	return targetSpec{}, fmt.Errorf("unsupported target %q; supported targets are %s", value, strings.Join(targets, ", "))
}

func rejectExistingOutput(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		return fmt.Errorf("output %s already exists (mode %s); choose a new output directory", path, info.Mode())
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output %s: %w", path, err)
	}
	return nil
}

func inspectLibrary(path string, spec targetSpec) error {
	switch spec.goos {
	case "linux":
		return inspectELF(path, spec)
	case "darwin":
		return inspectMachO(path, spec)
	case "windows":
		return inspectPE(path, spec)
	default:
		return fmt.Errorf("unsupported target %q", spec.goos+"/"+spec.goarch)
	}
}

func inspectELF(path string, spec targetSpec) error {
	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("parse ELF library %s: %w", path, err)
	}
	defer f.Close()

	if f.Class != elf.ELFCLASS64 {
		return fmt.Errorf("ELF library %s has class %s, want ELFCLASS64", path, f.Class)
	}
	if f.Type != elf.ET_DYN {
		return fmt.Errorf("ELF library %s has type %s, want ET_DYN shared object", path, f.Type)
	}
	want := elf.EM_X86_64
	if spec.goarch == "arm64" {
		want = elf.EM_AARCH64
	}
	if f.Machine != want {
		return fmt.Errorf("ELF library %s has machine %s, want %s", path, f.Machine, want)
	}
	return nil
}

func inspectMachO(path string, spec targetSpec) error {
	f, err := macho.Open(path)
	if err != nil {
		if fat, fatErr := macho.OpenFat(path); fatErr == nil {
			fat.Close()
			return fmt.Errorf("Mach-O library %s is a universal binary; provide a thin %s %s dylib", path, spec.goos, spec.goarch)
		}
		return fmt.Errorf("parse Mach-O library %s: %w", path, err)
	}
	defer f.Close()

	if f.Type != macho.TypeDylib {
		return fmt.Errorf("Mach-O library %s has type %s, want dylib", path, f.Type)
	}
	want := macho.CpuAmd64
	if spec.goarch == "arm64" {
		want = macho.CpuArm64
	}
	if f.Cpu != want {
		return fmt.Errorf("Mach-O library %s has CPU %s, want %s", path, f.Cpu, want)
	}
	return nil
}

func inspectPE(path string, spec targetSpec) error {
	f, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("parse PE library %s: %w", path, err)
	}
	defer f.Close()

	if f.Characteristics&pe.IMAGE_FILE_DLL == 0 {
		return fmt.Errorf("PE library %s is not a DLL (characteristics %#x)", path, f.Characteristics)
	}
	if _, ok := f.OptionalHeader.(*pe.OptionalHeader64); !ok {
		return fmt.Errorf("PE library %s is not a 64-bit PE image", path)
	}
	want := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
	if spec.goarch == "arm64" {
		want = pe.IMAGE_FILE_MACHINE_ARM64
	}
	if f.Machine != want {
		return fmt.Errorf("PE library %s has machine %#x, want %#x", path, f.Machine, want)
	}
	return nil
}

func compressLibrary(src, dst string) (compressedSize, uncompressedSize int64, uncompressedSHA, compressedSHA string, err error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("open library %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("create payload %s: %w", dst, err)
	}

	compressedHash := sha256.New()
	zw, err := zstd.NewWriter(
		io.MultiWriter(out, compressedHash),
		zstd.WithEncoderLevel(zstd.SpeedBetterCompression),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		out.Close()
		return 0, 0, "", "", fmt.Errorf("create zstd encoder: %w", err)
	}

	uncompressedHash := sha256.New()
	written, copyErr := io.Copy(zw, io.TeeReader(in, uncompressedHash))
	closeErr := zw.Close()
	fileCloseErr := out.Close()
	if copyErr != nil {
		return 0, 0, "", "", fmt.Errorf("compress library %s: %w", src, copyErr)
	}
	if closeErr != nil {
		return 0, 0, "", "", fmt.Errorf("finish zstd payload %s: %w", dst, closeErr)
	}
	if fileCloseErr != nil {
		return 0, 0, "", "", fmt.Errorf("close payload %s: %w", dst, fileCloseErr)
	}

	info, err := os.Stat(dst)
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("stat payload %s: %w", dst, err)
	}
	return info.Size(), written, hex.EncodeToString(uncompressedHash.Sum(nil)), hex.EncodeToString(compressedHash.Sum(nil)), nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func writeTemplate(path string, tmpl *template.Template, data any) error {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render %s: %w", filepath.Base(path), err)
	}
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("format %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, src, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func validateModulePath(path string) error {
	if path == "" {
		return errors.New("module path is required")
	}
	if strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Contains(path, "//") || strings.Contains(path, "..") {
		return fmt.Errorf("module path %q is not a clean import path", path)
	}
	for _, r := range path {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '-', '_', '~', '/':
			continue
		default:
			return fmt.Errorf("module path %q contains unsupported character %q", path, r)
		}
	}
	return nil
}

func validateSelectorPackage(name string) error {
	if name == "" {
		return errors.New("selector package is required")
	}
	if !token.IsIdentifier(name) || token.Lookup(name).IsKeyword() || name == "_" || name == "main" {
		return fmt.Errorf("selector package %q is not a valid importable Go package name", name)
	}
	return nil
}

func isLowerHex(s string, length int) bool {
	if len(s) != length {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

var artifactTemplate = template.Must(template.New("artifact").Parse(`// Code generated by scripts/sync-lance-c.sh; DO NOT EDIT.

// Package artifact embeds one compressed lance-c shared library as data.
// Importing this module does not link, load, or execute native code.
package artifact

import _ "embed"

// Payload is the zstd-compressed {{.LibraryName}} for {{.Target}}.
//
//go:embed {{printf "%q" .PayloadName}}
var Payload []byte

// Packaging metadata. UpstreamCommit and HeaderSHA256 are provenance claims
// recorded by the packaging process. lance-c exposes no runtime version
// symbol, so the loader verifies the target and payload, not the library's
// build commit or the header's provenance.
const (
	Target              = {{printf "%q" .Target}}
	LibraryName         = {{printf "%q" .LibraryName}}
	CompressedSize    int64 = {{.CompressedSize}}
	CompressedSHA256    = {{printf "%q" .CompressedSHA256}}
	UncompressedSize  int64 = {{.UncompressedSize}}
	UncompressedSHA256  = {{printf "%q" .UncompressedSHA256}}
	UpstreamRepo        = {{printf "%q" .UpstreamRepo}}
	UpstreamCommit      = {{printf "%q" .UpstreamCommit}}
	HeaderSHA256        = {{printf "%q" .HeaderSHA256}}
)
`))

var selectorTemplate = template.Must(template.New("selector").Parse(`// Code generated by scripts/sync-lance-c.sh; DO NOT EDIT.

//go:build !lance_external_lib && {{.GOOS}} && {{.GOARCH}}

package {{.SelectorPackage}}

import (
	"github.com/lance-format/lance-go/internal/loader"

	artifact {{printf "%q" .ModulePath}}
)

func init() {
	loader.RegisterBundle(loader.Bundle{
		Module:             {{printf "%q" .ModulePath}},
		Target:             artifact.Target,
		LibraryName:        artifact.LibraryName,
		Compressed:         artifact.Payload,
		CompressedSize:     artifact.CompressedSize,
		CompressedSHA256:   artifact.CompressedSHA256,
		UncompressedSize:   artifact.UncompressedSize,
		UncompressedSHA256: artifact.UncompressedSHA256,
		UpstreamCommit:     artifact.UpstreamCommit,
		HeaderSHA256:       artifact.HeaderSHA256,
	})
}
`))
