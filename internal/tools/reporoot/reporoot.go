// Package reporoot locates the lance-go repository root for maintainer tools.
//
// The maintainer tools live in a nested module at internal/tools and are run
// with `go -C internal/tools run ./...`, so their working directory is usually
// the tools module instead of the repository root. The marker file is the
// upstream pin that fetch-lance-c-release, check-abi-symbols and
// verify-lance-c-artifact all read.
package reporoot

import (
	"fmt"
	"os"
	"path/filepath"
)

const marker = "internal/ffi/upstream.go"

// Find returns the absolute path of the repository root that contains the
// upstream pin, searching the current working directory and its parents.
func Find() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("locate repository root: %w", err)
	}
	return findFrom(start)
}

// findFrom searches dir and its parents for the repository marker.
func findFrom(dir string) (string, error) {
	start := dir
	for {
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(marker)))
		if err == nil && !info.IsDir() {
			return dir, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("locate repository root: stat %s: %w", filepath.Join(dir, marker), err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repository root not found: %s is missing from %s and its parents; pass -root or -pin explicitly", marker, start)
		}
		dir = parent
	}
}
