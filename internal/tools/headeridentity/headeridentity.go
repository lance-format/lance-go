// Package headeridentity defines and verifies the identity of a lance-c public
// C header set.
//
// A header set is every regular *.h file directly under include/lance. The C++
// header lance.hpp, the implementation-only detail/ tree and all other files are
// outside the identity. A manifest lists the files sorted by path, one
// "<path> <sha256>\n" line each, and the identity is the SHA-256 of that exact
// manifest text.
//
// The same manifest is usable for a legacy single-file release, which contains
// only include/lance/lance.h, and for a split release with several headers.
// Type-only headers such as fwd.h remain part of the identity even though they
// contain no lance_* function declarations.
package headeridentity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// Dir is the repository-relative directory that holds the public C headers.
	Dir = "include/lance"
	// ManifestName is the conventional manifest file name for release evidence.
	ManifestName = "HEADER-MANIFEST"
	// CompositeName is the conventional identity file name.
	CompositeName = "COMPOSITE-SHA256"
	// Umbrella is the header every public C header set must contain.
	Umbrella = Dir + "/lance.h"
)

// File is one public C header in a manifest.
type File struct {
	Path   string
	SHA256 string
}

// Scan reads every regular *.h file directly under dir and returns the sorted
// manifest entries. Paths use the include/lance/<name> form. Non-.h files,
// including lance.hpp, are ignored. Subdirectories, such as the C++ detail/
// tree, are outside the public C identity and are ignored as well; the public
// C headers stay flat.
func Scan(dir string) ([]File, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("inspect header directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("header path %s is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read header directory %s: %w", dir, err)
	}

	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		full := filepath.Join(dir, name)
		entryInfo, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", full, err)
		}
		if entryInfo.IsDir() {
			// The C++ detail/ tree ships beside the flat C headers and is
			// outside the public C identity.
			continue
		}
		if !entryInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("header %s is not a regular file", full)
		}
		if !strings.HasSuffix(name, ".h") {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("read header %s: %w", full, err)
		}
		files = append(files, File{Path: Dir + "/" + name, SHA256: digestBytes(data)})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if len(files) == 0 {
		return nil, fmt.Errorf("no public C headers (*.h) found in %s", dir)
	}
	if !contains(files, Umbrella) {
		return nil, fmt.Errorf("header set in %s does not contain %s", dir, Umbrella)
	}
	return files, nil
}

// Encode returns the exact manifest text for files. Entries are sorted by path
// so callers can pass them in any order.
func Encode(files []File) ([]byte, error) {
	if len(files) == 0 {
		return nil, errors.New("header manifest has no files")
	}
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	if !contains(sorted, Umbrella) {
		return nil, fmt.Errorf("header manifest does not contain %s", Umbrella)
	}

	var text strings.Builder
	for i, file := range sorted {
		if err := validateFile(file); err != nil {
			return nil, err
		}
		if i > 0 && sorted[i-1].Path == file.Path {
			return nil, fmt.Errorf("header manifest declares %s more than once", file.Path)
		}
		text.WriteString(file.Path)
		text.WriteByte(' ')
		text.WriteString(file.SHA256)
		text.WriteByte('\n')
	}
	return []byte(text.String()), nil
}

// Digest returns the identity of a header set: the SHA-256 of its exact
// manifest text.
func Digest(files []File) (string, error) {
	text, err := Encode(files)
	if err != nil {
		return "", err
	}
	return digestBytes(text), nil
}

// Parse reads an exact header manifest. It rejects CRLF line endings, blank
// lines, extra spaces, unsorted entries, duplicate paths, non-.h files and
// missing umbrella headers so a malformed identity cannot pass silently.
func Parse(data []byte) ([]File, error) {
	if len(data) == 0 {
		return nil, errors.New("header manifest is empty")
	}
	if data[len(data)-1] != '\n' {
		return nil, errors.New("header manifest must end with a newline")
	}
	if bytes.ContainsRune(data, '\r') {
		return nil, errors.New("header manifest must use LF line endings")
	}
	if bytes.ContainsRune(data, '\t') {
		return nil, errors.New("header manifest must separate path and digest with one space")
	}

	lines := strings.Split(string(data[:len(data)-1]), "\n")
	files := make([]File, 0, len(lines))
	for i, line := range lines {
		if line == "" {
			return nil, fmt.Errorf("header manifest line %d is empty", i+1)
		}
		if strings.Count(line, " ") != 1 {
			return nil, fmt.Errorf("header manifest line %d is not \"<path> <sha256>\"", i+1)
		}
		parts := strings.SplitN(line, " ", 2)
		file := File{Path: parts[0], SHA256: parts[1]}
		if err := validateFile(file); err != nil {
			return nil, fmt.Errorf("header manifest line %d: %w", i+1, err)
		}
		if i > 0 && files[i-1].Path >= file.Path {
			return nil, fmt.Errorf("header manifest is not sorted by path at line %d", i+1)
		}
		files = append(files, file)
	}
	if !contains(files, Umbrella) {
		return nil, fmt.Errorf("header manifest does not contain %s", Umbrella)
	}
	return files, nil
}

// Verify scans dir and checks it against an expected manifest, an expected
// identity, or both. When a manifest is supplied, every missing, changed and
// unexpected header is reported. When only an identity is supplied, any change
// to the file set or its contents changes the identity.
func Verify(dir string, expectedManifest []byte, expectedIdentity string) ([]File, string, error) {
	if expectedIdentity != "" && !ValidDigest(expectedIdentity) {
		return nil, "", fmt.Errorf("expected header identity %q is not a lowercase SHA-256", expectedIdentity)
	}
	actual, err := Scan(dir)
	if err != nil {
		return nil, "", err
	}
	actualIdentity, err := Digest(actual)
	if err != nil {
		return nil, "", err
	}

	if expectedManifest != nil {
		expected, err := Parse(expectedManifest)
		if err != nil {
			return nil, "", fmt.Errorf("parse expected %s: %w", ManifestName, err)
		}
		manifestIdentity := digestBytes(expectedManifest)
		if expectedIdentity != "" && expectedIdentity != manifestIdentity {
			return nil, "", fmt.Errorf("expected %s sha256 is %s, but the supplied identity is %s", ManifestName, manifestIdentity, expectedIdentity)
		}
		expectedIdentity = manifestIdentity
		if err := compare(expected, actual); err != nil {
			return nil, "", fmt.Errorf("header set in %s does not match %s: %w", dir, ManifestName, err)
		}
	} else if expectedIdentity == "" {
		return nil, "", errors.New("no expected header identity or manifest supplied")
	}

	if actualIdentity != expectedIdentity {
		return nil, actualIdentity, fmt.Errorf("header set identity is %s, want %s", actualIdentity, expectedIdentity)
	}
	return actual, actualIdentity, nil
}

// ValidDigest reports whether value is a lowercase SHA-256.
func ValidDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validateFile(file File) error {
	name, ok := strings.CutPrefix(file.Path, Dir+"/")
	if !ok || name == "" {
		return fmt.Errorf("header path %q is not under %s/", file.Path, Dir)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("header path %q is not a direct child of %s", file.Path, Dir)
	}
	if !strings.HasSuffix(name, ".h") || strings.HasSuffix(name, ".hpp") {
		return fmt.Errorf("header path %q is not a C header (*.h)", file.Path)
	}
	if !ValidDigest(file.SHA256) {
		return fmt.Errorf("header %s digest %q is not a lowercase SHA-256", file.Path, file.SHA256)
	}
	return nil
}

func compare(expected, actual []File) error {
	expectedByPath := make(map[string]string, len(expected))
	for _, file := range expected {
		expectedByPath[file.Path] = file.SHA256
	}
	actualByPath := make(map[string]string, len(actual))
	for _, file := range actual {
		actualByPath[file.Path] = file.SHA256
	}

	var missing, unexpected, changed []string
	for path, want := range expectedByPath {
		got, ok := actualByPath[path]
		switch {
		case !ok:
			missing = append(missing, path)
		case got != want:
			changed = append(changed, fmt.Sprintf("%s (got %s, want %s)", path, got, want))
		}
	}
	for path := range actualByPath {
		if _, ok := expectedByPath[path]; !ok {
			unexpected = append(unexpected, path)
		}
	}
	if len(missing) == 0 && len(unexpected) == 0 && len(changed) == 0 {
		return nil
	}

	sort.Strings(missing)
	sort.Strings(unexpected)
	sort.Strings(changed)
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing "+strings.Join(missing, ", "))
	}
	if len(unexpected) > 0 {
		parts = append(parts, "unexpected "+strings.Join(unexpected, ", "))
	}
	if len(changed) > 0 {
		parts = append(parts, "changed "+strings.Join(changed, ", "))
	}
	return errors.New(strings.Join(parts, "; "))
}

func contains(files []File, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
