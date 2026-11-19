// Package artifactmeta reads the packaging metadata that the artifact
// generator writes into a data-only lance-c companion module's artifact.go.
//
// The same metadata contract drives artifact verification and the CI
// acquisition check that pins the library bytes to the companion module for
// the current upstream commit.
package artifactmeta

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
)

// Metadata is the packaging record embedded in artifact.go.
type Metadata struct {
	Target             string
	LibraryName        string
	CompressedSize     int64
	CompressedSHA256   string
	UncompressedSize   int64
	UncompressedSHA256 string
	UpstreamRepo       string
	UpstreamCommit     string
	HeaderSHA256       string
}

var constantLine = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z][A-Za-z0-9]*)[ \t]*(?:int64[ \t]*)?=[ \t]*(?:"([^"]*)"|([0-9]+))[ \t]*$`)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Read parses one generated artifact.go.
func Read(path string) (Metadata, error) {
	var md Metadata
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

	md.Target = values["Target"]
	md.LibraryName = values["LibraryName"]
	md.CompressedSHA256 = values["CompressedSHA256"]
	md.UncompressedSHA256 = values["UncompressedSHA256"]
	md.UpstreamRepo = values["UpstreamRepo"]
	md.UpstreamCommit = values["UpstreamCommit"]
	md.HeaderSHA256 = values["HeaderSHA256"]

	if md.CompressedSize, err = strconv.ParseInt(values["CompressedSize"], 10, 64); err != nil {
		return md, fmt.Errorf("artifact CompressedSize: %w", err)
	}
	if md.UncompressedSize, err = strconv.ParseInt(values["UncompressedSize"], 10, 64); err != nil {
		return md, fmt.Errorf("artifact UncompressedSize: %w", err)
	}
	if !sha256Pattern.MatchString(md.CompressedSHA256) {
		return md, fmt.Errorf("artifact CompressedSHA256 is not a lowercase SHA-256: %q", md.CompressedSHA256)
	}
	if !sha256Pattern.MatchString(md.UncompressedSHA256) {
		return md, fmt.Errorf("artifact UncompressedSHA256 is not a lowercase SHA-256: %q", md.UncompressedSHA256)
	}
	if !sha256Pattern.MatchString(md.HeaderSHA256) {
		return md, fmt.Errorf("artifact HeaderSHA256 is not a lowercase SHA-256: %q", md.HeaderSHA256)
	}
	return md, nil
}
