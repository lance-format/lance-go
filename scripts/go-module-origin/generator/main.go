// Command go-module-origin builds the static Go module origin tree for the
// Lance-C artifact modules.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"
)

const (
	defaultModulePrefix = "go.lance.org/liblance"
	defaultBaseURL      = "https://go.lance.org"
	defaultVersion      = "v0.2.0-preview.1"
)

var (
	targets  = []string{"linux-amd64", "linux-arm64", "darwin-arm64", "windows-amd64", "windows-arm64"}
	targetOS = map[string]string{
		"linux-amd64":   "linux/amd64",
		"linux-arm64":   "linux/arm64",
		"darwin-arm64":  "darwin/arm64",
		"windows-amd64": "windows/amd64",
		"windows-arm64": "windows/arm64",
	}
)

type object struct {
	Key          string `json:"key"`
	Kind         string `json:"kind"`
	Module       string `json:"module,omitempty"`
	Version      string `json:"version,omitempty"`
	Target       string `json:"target,omitempty"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	ContentType  string `json:"content_type"`
	CacheControl string `json:"cache_control"`
	Immutable    bool   `json:"immutable"`
	ZIPH1        string `json:"zip_h1,omitempty"`
	GoModH1      string `json:"gomod_h1,omitempty"`
}

type manifest struct {
	Version        string   `json:"version"`
	ModulePrefix   string   `json:"module_prefix"`
	BaseURL        string   `json:"base_url"`
	SourceCommit   string   `json:"source_commit"`
	SourceRelease  string   `json:"source_release"`
	Time           string   `json:"time"`
	Objects        []object `json:"objects"`
	PublicKeyOrder []string `json:"public_key_order"`
}

func main() {
	var (
		candidates    = flag.String("candidates", "", "directory containing <target>/module candidate directories")
		out           = flag.String("out", "", "output origin tree")
		version       = flag.String("version", defaultVersion, "artifact module version")
		timeValue     = flag.String("time", "", "RFC3339 .info time")
		prefix        = flag.String("module-prefix", defaultModulePrefix, "module path prefix")
		baseURL       = flag.String("base-url", defaultBaseURL, "public origin base URL")
		license       = flag.String("license", "LICENSE", "license file to add to each module")
		sourceCommit  = flag.String("source-commit", "", "source commit recorded in the manifest")
		sourceRelease = flag.String("source-release", "", "Lance-C release recorded in the manifest")
	)
	flag.Parse()
	if *candidates == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	if *timeValue == "" {
		fmt.Fprintln(os.Stderr, "-time is required")
		os.Exit(2)
	}
	if _, err := time.Parse(time.RFC3339, *timeValue); err != nil {
		fmt.Fprintf(os.Stderr, "invalid -time: %v\n", err)
		os.Exit(2)
	}
	licenseBytes, err := os.ReadFile(*license)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read license: %v\n", err)
		os.Exit(1)
	}
	if err := os.RemoveAll(*out); err != nil {
		fmt.Fprintf(os.Stderr, "clean output: %v\n", err)
		os.Exit(1)
	}
	m := manifest{
		Version:       *version,
		ModulePrefix:  strings.TrimSuffix(*prefix, "/"),
		BaseURL:       strings.TrimSuffix(*baseURL, "/"),
		SourceCommit:  *sourceCommit,
		SourceRelease: *sourceRelease,
		Time:          *timeValue,
	}
	for _, target := range targets {
		modulePath := m.ModulePrefix + "/" + target
		candidateDir := filepath.Join(*candidates, target, "module")
		if err := buildModule(*out, candidateDir, modulePath, target, *version, *timeValue, licenseBytes, &m); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", target, err)
			os.Exit(1)
		}
	}
	sort.Slice(m.Objects, func(i, j int) bool { return m.Objects[i].Key < m.Objects[j].Key })
	m.PublicKeyOrder = make([]string, len(m.Objects))
	for i, object := range m.Objects {
		m.PublicKeyOrder[i] = object.Key
	}
	if err := writeManifest(*out, m); err != nil {
		fmt.Fprintf(os.Stderr, "write manifest: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d objects)\n", *out, len(m.Objects))
}

func buildModule(out, candidateDir, modulePath, target, version, timeValue string, license []byte, m *manifest) error {
	files, err := os.ReadDir(candidateDir)
	if err != nil {
		return fmt.Errorf("read candidate module: %w", err)
	}
	var payloadName string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		if name != "artifact.go" && name != "go.mod" && !strings.HasSuffix(name, ".zst") {
			continue
		}
		switch {
		case name == "artifact.go", name == "go.mod":
		case strings.HasSuffix(name, ".zst"):
			payloadName = name
		default:
			return fmt.Errorf("unexpected candidate file %q", name)
		}
	}
	if payloadName == "" {
		return fmt.Errorf("no .zst payload in %s", candidateDir)
	}
	payloadPath := filepath.Join(candidateDir, payloadName)
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		return err
	}
	artifactBytes, err := os.ReadFile(filepath.Join(candidateDir, "artifact.go"))
	if err != nil {
		return err
	}
	if err := verifyArtifact(artifactBytes, payload, targetOS[target]); err != nil {
		return err
	}

	moduleDir := filepath.Join(out, "modules", modulePath, "@v")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		return err
	}
	goMod := []byte("module " + modulePath + "\n\ngo 1.26.0\n")
	if err := os.WriteFile(filepath.Join(moduleDir, version+".mod"), goMod, 0o644); err != nil {
		return err
	}
	info, err := json.Marshal(struct {
		Version string `json:"Version"`
		Time    string `json:"Time"`
	}{Version: version, Time: timeValue})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(moduleDir, version+".info"), info, 0o644); err != nil {
		return err
	}

	stage, err := os.MkdirTemp("", "lance-go-module-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.WriteFile(filepath.Join(stage, "go.mod"), goMod, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "artifact.go"), artifactBytes, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "LICENSE"), license, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, payloadName), payload, 0o644); err != nil {
		return err
	}
	zipPath := filepath.Join(moduleDir, version+".zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	if err := modzip.CreateFromDir(zipFile, module.Version{Path: modulePath, Version: version}, stage); err != nil {
		zipFile.Close()
		return err
	}
	if err := zipFile.Close(); err != nil {
		return err
	}
	zipH1, err := dirhash.HashZip(zipPath, dirhash.Hash1)
	if err != nil {
		return err
	}
	goModH1, err := dirhash.Hash1([]string{"go.mod"}, func(string) (io.ReadCloser, error) {
		return os.Open(filepath.Join(stage, "go.mod"))
	})
	if err != nil {
		return err
	}
	list := []byte(version + "\n")
	if err := os.WriteFile(filepath.Join(moduleDir, "list"), list, 0o644); err != nil {
		return err
	}

	discovery := []byte(fmt.Sprintf("<!doctype html>\n<html><head><meta name=\"go-import\" content=\"%s mod %s/modules\"></head><body>%s</body></html>\n", modulePath, m.BaseURL, modulePath))
	discoveryPath, err := safeJoin(out, "liblance", target)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(discoveryPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(discoveryPath, discovery, 0o644); err != nil {
		return err
	}

	add := func(key, kind, path, contentType, cacheControl string, immutable bool, h1, gomodh1 string) error {
		bytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(bytes)
		m.Objects = append(m.Objects, object{
			Key:          key,
			Kind:         kind,
			Module:       modulePath,
			Version:      version,
			Target:       target,
			SHA256:       hex.EncodeToString(digest[:]),
			Size:         int64(len(bytes)),
			ContentType:  contentType,
			CacheControl: cacheControl,
			Immutable:    immutable,
			ZIPH1:        h1,
			GoModH1:      gomodh1,
		})
		return nil
	}
	if err := add(filepath.Join("liblance", target), "discovery", discoveryPath, "text/html; charset=utf-8", "public, max-age=300", true, "", ""); err != nil {
		return err
	}
	for _, suffix := range []string{"list", version + ".info", version + ".mod", version + ".zip"} {
		contentType, cacheControl, immutable := contentTypeFor(suffix)
		path := filepath.Join(moduleDir, suffix)
		key := filepath.ToSlash(filepath.Join("modules", modulePath, "@v", suffix))
		h1, gomodh1 := "", ""
		if suffix == version+".zip" {
			h1 = zipH1
		}
		if suffix == version+".mod" {
			gomodh1 = goModH1
		}
		if err := add(key, strings.TrimSuffix(suffix, filepath.Ext(suffix)), path, contentType, cacheControl, immutable, h1, gomodh1); err != nil {
			return err
		}
	}
	return nil
}

func contentTypeFor(suffix string) (string, string, bool) {
	switch {
	case suffix == "list":
		return "text/plain; charset=utf-8", "public, max-age=60", false
	case strings.HasSuffix(suffix, ".info"):
		return "application/json", "public, max-age=31536000, immutable", true
	case strings.HasSuffix(suffix, ".mod"):
		return "text/plain; charset=utf-8", "public, max-age=31536000, immutable", true
	case strings.HasSuffix(suffix, ".zip"):
		return "application/zip", "public, max-age=31536000, immutable", true
	default:
		return "application/octet-stream", "public, max-age=31536000, immutable", true
	}
}

func verifyArtifact(artifact, payload []byte, target string) error {
	compressedSize, compressedSHA, uncompressedSize, uncompressedSHA, artifactTarget, err := artifactClaims(artifact)
	if err != nil {
		return err
	}
	if artifactTarget != target {
		return fmt.Errorf("artifact target %q, want %q", artifactTarget, target)
	}
	if int64(len(payload)) != compressedSize {
		return fmt.Errorf("payload size %d, claim %d", len(payload), compressedSize)
	}
	compressed := sha256.Sum256(payload)
	if hex.EncodeToString(compressed[:]) != compressedSHA {
		return fmt.Errorf("payload SHA-256 %x, claim %s", compressed, compressedSHA)
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return err
	}
	defer decoder.Close()
	stream, err := decoder.DecodeAll(payload, nil)
	if err != nil {
		return fmt.Errorf("decompress payload: %w", err)
	}
	if int64(len(stream)) != uncompressedSize {
		return fmt.Errorf("uncompressed size %d, claim %d", len(stream), uncompressedSize)
	}
	uncompressed := sha256.Sum256(stream)
	if hex.EncodeToString(uncompressed[:]) != uncompressedSHA {
		return fmt.Errorf("uncompressed SHA-256 %x, claim %s", uncompressed, uncompressedSHA)
	}
	return nil
}

func artifactClaims(artifact []byte) (int64, string, int64, string, string, error) {
	text := string(artifact)
	parseInt := func(name string) (int64, error) {
		re := regexp.MustCompile(name + `\s+int64\s*=\s*(\d+)`)
		match := re.FindStringSubmatch(text)
		if len(match) != 2 {
			return 0, fmt.Errorf("missing %s", name)
		}
		return strconv.ParseInt(match[1], 10, 64)
	}
	parseString := func(name string) (string, error) {
		re := regexp.MustCompile(name + `\s*=\s*"([^"]+)"`)
		match := re.FindStringSubmatch(text)
		if len(match) != 2 {
			return "", fmt.Errorf("missing %s", name)
		}
		return match[1], nil
	}
	compressedSize, err := parseInt("CompressedSize")
	if err != nil {
		return 0, "", 0, "", "", err
	}
	compressedSHA, err := parseString("CompressedSHA256")
	if err != nil {
		return 0, "", 0, "", "", err
	}
	uncompressedSize, err := parseInt("UncompressedSize")
	if err != nil {
		return 0, "", 0, "", "", err
	}
	uncompressedSHA, err := parseString("UncompressedSHA256")
	if err != nil {
		return 0, "", 0, "", "", err
	}
	target, err := parseString("Target")
	if err != nil {
		return 0, "", 0, "", "", err
	}
	return compressedSize, compressedSHA, uncompressedSize, uncompressedSHA, target, nil
}

func safeJoin(root string, elements ...string) (string, error) {
	for _, element := range elements {
		if strings.Contains(element, "..") || strings.ContainsAny(element, "/\\") {
			return "", fmt.Errorf("unsafe path element %q", element)
		}
	}
	parts := append([]string{root}, elements...)
	return filepath.Join(parts...), nil
}

func writeManifest(out string, m manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(out, "manifest.json"), data, 0o644); err != nil {
		return err
	}
	var tsv strings.Builder
	tsv.WriteString("key\tsha256\tsize\tkind\timmutable\tcontent_type\tmodule\tversion\ttarget\tzip_h1\tgomod_h1\n")
	for _, object := range m.Objects {
		fmt.Fprintf(&tsv, "%s\t%s\t%d\t%s\t%t\t%s\t%s\t%s\t%s\t%s\t%s\n",
			object.Key, object.SHA256, object.Size, object.Kind, object.Immutable,
			object.ContentType, object.Module, object.Version, object.Target,
			object.ZIPH1, object.GoModH1)
	}
	return os.WriteFile(filepath.Join(out, "manifest.tsv"), []byte(tsv.String()), 0o644)
}
