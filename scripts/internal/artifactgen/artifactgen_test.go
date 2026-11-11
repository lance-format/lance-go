package artifactgen

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestGenerateELFModule(t *testing.T) {
	dir := t.TempDir()
	lib := syntheticELF(elf.EM_X86_64)
	libPath := writeFixture(t, dir, "liblance_c.so", lib)
	header := []byte("#define LANCE_TEST 1\n")
	headerPath := writeFixture(t, dir, "lance.h", header)
	outDir := filepath.Join(dir, "out")

	result, err := Generate(Options{
		LibraryPath:     libPath,
		HeaderPath:      headerPath,
		Target:          "linux/amd64",
		ModulePath:      "example.com/artifact",
		OutDir:          outDir,
		SelectorPackage: "lance",
		UpstreamRepo:    "https://example.com/lance-c",
		UpstreamCommit:  strings.Repeat("a", 40),
		HeaderSHA256:    sha256Hex(header),
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if result.ModuleDir != filepath.Join(outDir, "module") {
		t.Fatalf("ModuleDir = %q", result.ModuleDir)
	}
	selectorName := "lance_bundle_linux_amd64.go"
	if filepath.Base(result.SelectorPath) != selectorName {
		t.Fatalf("SelectorPath = %q, want basename %q", result.SelectorPath, selectorName)
	}

	payload, err := os.ReadFile(filepath.Join(result.ModuleDir, "liblance_c.so.zst"))
	if err != nil {
		t.Fatalf("read generated payload: %v", err)
	}
	dec, err := zstd.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("open generated payload: %v", err)
	}
	decompressed, err := io.ReadAll(dec)
	dec.Close()
	if err != nil {
		t.Fatalf("decompress generated payload: %v", err)
	}
	if !bytes.Equal(decompressed, lib) {
		t.Fatal("generated payload does not round-trip the input library")
	}

	if result.Metadata.UncompressedSize != int64(len(lib)) {
		t.Fatalf("uncompressed size = %d, want %d", result.Metadata.UncompressedSize, len(lib))
	}
	if result.Metadata.UncompressedSHA256 != sha256Hex(lib) {
		t.Fatalf("uncompressed sha256 = %s", result.Metadata.UncompressedSHA256)
	}
	if result.Metadata.CompressedSize != int64(len(payload)) {
		t.Fatalf("compressed size = %d, want %d", result.Metadata.CompressedSize, len(payload))
	}
	if result.Metadata.CompressedSHA256 != sha256Hex(payload) {
		t.Fatalf("compressed sha256 = %s", result.Metadata.CompressedSHA256)
	}

	goMod, err := os.ReadFile(filepath.Join(result.ModuleDir, "go.mod"))
	if err != nil {
		t.Fatalf("read generated go.mod: %v", err)
	}
	if !strings.Contains(string(goMod), "module example.com/artifact\n") {
		t.Fatalf("generated go.mod does not declare the module path:\n%s", goMod)
	}
	if !strings.Contains(string(goMod), "\ngo 1.23.0\n") {
		t.Fatalf("generated go.mod does not target Go 1.23:\n%s", goMod)
	}

	if got := parsePackageName(t, filepath.Join(result.ModuleDir, "artifact.go")); got != "artifact" {
		t.Fatalf("artifact package = %q, want artifact", got)
	}
	if got := parsePackageName(t, result.SelectorPath); got != "lance" {
		t.Fatalf("selector package = %q, want lance", got)
	}
	imports := parseImports(t, result.SelectorPath)
	for _, want := range []string{
		"github.com/lance-format/lance-go/internal/loader",
		"example.com/artifact",
	} {
		if _, ok := imports[want]; !ok {
			t.Fatalf("selector imports %v, missing %q", imports, want)
		}
	}

	ctx := build.Default
	ctx.BuildTags = nil
	ctx.GOOS = "linux"
	ctx.GOARCH = "amd64"
	match, err := ctx.MatchFile(filepath.Dir(result.SelectorPath), selectorName)
	if err != nil {
		t.Fatalf("match linux/amd64 selector: %v", err)
	}
	if !match {
		t.Fatal("selector does not match linux/amd64")
	}
	ctx.GOOS = "linux"
	ctx.GOARCH = "arm64"
	match, err = ctx.MatchFile(filepath.Dir(result.SelectorPath), selectorName)
	if err != nil {
		t.Fatalf("match linux/arm64 selector: %v", err)
	}
	if match {
		t.Fatal("selector matches linux/arm64 but was generated for linux/amd64")
	}
	ctx.GOOS = "linux"
	ctx.GOARCH = "amd64"
	ctx.BuildTags = []string{"lance_external_lib"}
	match, err = ctx.MatchFile(filepath.Dir(result.SelectorPath), selectorName)
	if err != nil {
		t.Fatalf("match external selector: %v", err)
	}
	if match {
		t.Fatal("selector matches the lance_external_lib build tag")
	}

	if _, err := Generate(Options{
		LibraryPath:     libPath,
		HeaderPath:      headerPath,
		Target:          "linux/amd64",
		ModulePath:      "example.com/artifact",
		OutDir:          outDir,
		SelectorPackage: "lance",
		UpstreamRepo:    "https://example.com/lance-c",
		UpstreamCommit:  strings.Repeat("a", 40),
		HeaderSHA256:    sha256Hex(header),
	}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second Generate error = %v, want refusal to overwrite", err)
	}
}

func TestGenerateMachOSelector(t *testing.T) {
	dir := t.TempDir()
	libPath := writeFixture(t, dir, "liblance_c.dylib", syntheticMachO(macho.CpuArm64))
	headerPath := writeFixture(t, dir, "lance.h", []byte("#define LANCE_TEST 1\n"))
	outDir := filepath.Join(dir, "out")

	result, err := Generate(Options{
		LibraryPath:     libPath,
		HeaderPath:      headerPath,
		Target:          "darwin/arm64",
		ModulePath:      "example.com/artifact",
		OutDir:          outDir,
		SelectorPackage: "lance",
		UpstreamRepo:    "https://example.com/lance-c",
		UpstreamCommit:  strings.Repeat("b", 40),
		HeaderSHA256:    sha256Hex([]byte("#define LANCE_TEST 1\n")),
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	ctx := build.Default
	ctx.BuildTags = nil
	ctx.GOOS = "darwin"
	ctx.GOARCH = "arm64"
	match, err := ctx.MatchFile(filepath.Dir(result.SelectorPath), filepath.Base(result.SelectorPath))
	if err != nil {
		t.Fatalf("match darwin/arm64 selector: %v", err)
	}
	if !match {
		t.Fatal("selector does not match darwin/arm64")
	}
}

func TestGeneratePESelector(t *testing.T) {
	tests := []struct {
		arch    string
		machine uint16
		commit  string
	}{
		{arch: "amd64", machine: pe.IMAGE_FILE_MACHINE_AMD64, commit: strings.Repeat("c", 40)},
		{arch: "arm64", machine: pe.IMAGE_FILE_MACHINE_ARM64, commit: strings.Repeat("d", 40)},
	}

	for _, tt := range tests {
		t.Run(tt.arch, func(t *testing.T) {
			dir := t.TempDir()
			lib := syntheticPE(tt.machine, true)
			libPath := writeFixture(t, dir, "lance_c.dll", lib)
			header := []byte("#define LANCE_TEST 1\n")
			headerPath := writeFixture(t, dir, "lance.h", header)
			outDir := filepath.Join(dir, "out")

			result, err := Generate(Options{
				LibraryPath:     libPath,
				HeaderPath:      headerPath,
				Target:          "windows/" + tt.arch,
				ModulePath:      "example.com/artifact",
				OutDir:          outDir,
				SelectorPackage: "lance",
				UpstreamRepo:    "https://example.com/lance-c",
				UpstreamCommit:  tt.commit,
				HeaderSHA256:    sha256Hex(header),
			})
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}

			selectorName := "lance_bundle_windows_" + tt.arch + ".go"
			if filepath.Base(result.SelectorPath) != selectorName {
				t.Fatalf("SelectorPath = %q, want basename %q", result.SelectorPath, selectorName)
			}
			payload, err := os.ReadFile(filepath.Join(result.ModuleDir, "lance_c.dll.zst"))
			if err != nil {
				t.Fatalf("read generated payload: %v", err)
			}
			dec, err := zstd.NewReader(bytes.NewReader(payload))
			if err != nil {
				t.Fatalf("open generated payload: %v", err)
			}
			decompressed, err := io.ReadAll(dec)
			dec.Close()
			if err != nil {
				t.Fatalf("decompress generated payload: %v", err)
			}
			if !bytes.Equal(decompressed, lib) {
				t.Fatal("generated payload does not round-trip the input DLL")
			}

			ctx := build.Default
			ctx.BuildTags = nil
			ctx.GOOS = "windows"
			ctx.GOARCH = tt.arch
			match, err := ctx.MatchFile(filepath.Dir(result.SelectorPath), selectorName)
			if err != nil {
				t.Fatalf("match windows/%s selector: %v", tt.arch, err)
			}
			if !match {
				t.Fatalf("selector does not match windows/%s", tt.arch)
			}
			otherArch := "arm64"
			if tt.arch == "arm64" {
				otherArch = "amd64"
			}
			ctx.GOARCH = otherArch
			match, err = ctx.MatchFile(filepath.Dir(result.SelectorPath), selectorName)
			if err != nil {
				t.Fatalf("match windows/%s selector: %v", otherArch, err)
			}
			if match {
				t.Fatalf("selector matches windows/%s but was generated for windows/%s", otherArch, tt.arch)
			}
			ctx.GOARCH = tt.arch
			ctx.BuildTags = []string{"lance_external_lib"}
			match, err = ctx.MatchFile(filepath.Dir(result.SelectorPath), selectorName)
			if err != nil {
				t.Fatalf("match external selector: %v", err)
			}
			if match {
				t.Fatal("selector matches the lance_external_lib build tag")
			}
		})
	}
}

func TestGenerateRejectsBadInputs(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string) Options
		want  string
	}{
		{
			name: "unsupported target",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, syntheticELF(elf.EM_X86_64), "liblance_c.so")
				opts.Target = "windows/386"
				return opts
			},
			want: "unsupported target",
		},
		{
			name: "library basename",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, syntheticELF(elf.EM_X86_64), "wrong.so")
				return opts
			},
			want: "basename",
		},
		{
			name: "ELF architecture",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, syntheticELF(elf.EM_X86_64), "liblance_c.so")
				opts.Target = "linux/arm64"
				return opts
			},
			want: "machine",
		},
		{
			name: "ELF type",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, syntheticELFType(elf.EM_X86_64, elf.ET_EXEC), "liblance_c.so")
				return opts
			},
			want: "ET_DYN",
		},
		{
			name: "Mach-O architecture",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, syntheticMachO(macho.CpuAmd64), "liblance_c.dylib")
				opts.Target = "darwin/arm64"
				return opts
			},
			want: "CPU",
		},
		{
			name: "PE not a DLL",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, syntheticPE(pe.IMAGE_FILE_MACHINE_AMD64, false), "lance_c.dll")
				opts.Target = "windows/amd64"
				return opts
			},
			want: "not a DLL",
		},
		{
			name: "PE 32-bit optional header",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, syntheticPEWithOptionalHeader(pe.IMAGE_FILE_MACHINE_AMD64, true, 0x10b), "lance_c.dll")
				opts.Target = "windows/amd64"
				return opts
			},
			want: "64-bit",
		},
		{
			name: "PE architecture",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, syntheticPE(pe.IMAGE_FILE_MACHINE_ARM64, true), "lance_c.dll")
				opts.Target = "windows/amd64"
				return opts
			},
			want: "machine",
		},
		{
			name: "PE corrupt",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, []byte("not a PE image"), "lance_c.dll")
				opts.Target = "windows/amd64"
				return opts
			},
			want: "parse PE library",
		},
		{
			name: "header hash",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, syntheticELF(elf.EM_X86_64), "liblance_c.so")
				opts.HeaderSHA256 = strings.Repeat("0", 64)
				return opts
			},
			want: "header",
		},
		{
			name: "module path",
			setup: func(t *testing.T, dir string) Options {
				opts := validOptions(t, dir, syntheticELF(elf.EM_X86_64), "liblance_c.so")
				opts.ModulePath = "example.com/artifact\n"
				return opts
			},
			want: "module path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			opts := tt.setup(t, dir)
			_, err := Generate(opts)
			if err == nil {
				t.Fatal("Generate succeeded with invalid input")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
			if _, statErr := os.Stat(opts.OutDir); !os.IsNotExist(statErr) {
				t.Fatalf("failed Generate published output %s (stat error %v)", opts.OutDir, statErr)
			}
			assertNoStaging(t, filepath.Dir(opts.OutDir))
		})
	}
}

func validOptions(t *testing.T, dir string, lib []byte, name string) Options {
	t.Helper()
	header := []byte("#define LANCE_TEST 1\n")
	return Options{
		LibraryPath:     writeFixture(t, dir, name, lib),
		HeaderPath:      writeFixture(t, dir, "lance.h", header),
		Target:          "linux/amd64",
		ModulePath:      "example.com/artifact",
		OutDir:          filepath.Join(dir, "out"),
		SelectorPackage: "lance",
		UpstreamRepo:    "https://example.com/lance-c",
		UpstreamCommit:  strings.Repeat("a", 40),
		HeaderSHA256:    sha256Hex(header),
	}
}

func syntheticELF(machine elf.Machine) []byte {
	return syntheticELFType(machine, elf.ET_DYN)
}

func syntheticELFType(machine elf.Machine, typ elf.Type) []byte {
	hdr := make([]byte, 64)
	copy(hdr, "\x7fELF")
	hdr[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	hdr[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	hdr[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	binary.LittleEndian.PutUint16(hdr[16:], uint16(typ))
	binary.LittleEndian.PutUint16(hdr[18:], uint16(machine))
	binary.LittleEndian.PutUint32(hdr[20:], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint16(hdr[52:], 64)
	return hdr
}

func syntheticPE(machine uint16, dll bool) []byte {
	return syntheticPEWithOptionalHeader(machine, dll, 0x20b)
}

func syntheticPEWithOptionalHeader(machine uint16, dll bool, magic uint16) []byte {
	const peOffset = 0x40
	optionalSize := 240
	if magic == 0x10b {
		optionalSize = 224
	}
	buf := make([]byte, peOffset+4+20+optionalSize)

	copy(buf, "MZ")
	binary.LittleEndian.PutUint32(buf[0x3c:], peOffset)
	copy(buf[peOffset:], "PE\x00\x00")

	fileHeader := peOffset + 4
	binary.LittleEndian.PutUint16(buf[fileHeader:], machine)
	binary.LittleEndian.PutUint16(buf[fileHeader+16:], uint16(optionalSize))
	characteristics := uint16(pe.IMAGE_FILE_EXECUTABLE_IMAGE)
	if dll {
		characteristics |= pe.IMAGE_FILE_DLL
	}
	binary.LittleEndian.PutUint16(buf[fileHeader+18:], characteristics)

	optionalHeader := fileHeader + 20
	binary.LittleEndian.PutUint16(buf[optionalHeader:], magic)
	if magic == 0x20b {
		binary.LittleEndian.PutUint32(buf[optionalHeader+108:], 16)
	} else {
		binary.LittleEndian.PutUint32(buf[optionalHeader+92:], 16)
	}
	return buf
}

func syntheticMachO(cpu macho.Cpu) []byte {
	hdr := make([]byte, 32)
	binary.LittleEndian.PutUint32(hdr[0:], macho.Magic64)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(cpu))
	binary.LittleEndian.PutUint32(hdr[8:], 0)
	binary.LittleEndian.PutUint32(hdr[12:], uint32(macho.TypeDylib))
	return hdr
}

func writeFixture(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

func parsePackageName(t *testing.T, path string) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return f.Name.Name
}

func parseImports(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse imports in %s: %v", path, err)
	}
	imports := make(map[string]struct{}, len(f.Imports))
	for _, imp := range f.Imports {
		value, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("unquote import path %s: %v", imp.Path.Value, err)
		}
		imports[value] = struct{}{}
	}
	return imports
}

func assertNoStaging(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read output parent %s: %v", parent, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".sync-lance-c-") {
			t.Fatalf("staging directory %q was left behind", entry.Name())
		}
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
