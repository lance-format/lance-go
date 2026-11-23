package reporoot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindFromMarkerDirectory(t *testing.T) {
	root := t.TempDir()
	markerPath := filepath.Join(root, "internal", "ffi", "upstream.go")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("create marker directory: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte("package ffi\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	got, err := findFrom(root)
	if err != nil {
		t.Fatalf("findFrom(root): %v", err)
	}
	if got != root {
		t.Fatalf("findFrom(root) = %q, want %q", got, root)
	}
}

func TestFindFromNestedModuleAndPackageDirectories(t *testing.T) {
	root := t.TempDir()
	markerPath := filepath.Join(root, "internal", "ffi", "upstream.go")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("create marker directory: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte("package ffi\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	for _, dir := range []string{
		filepath.Join(root, "internal", "tools"),
		filepath.Join(root, "internal", "tools", "fetch-lance-c-release"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
		got, err := findFrom(dir)
		if err != nil {
			t.Fatalf("findFrom(%s): %v", dir, err)
		}
		if got != root {
			t.Fatalf("findFrom(%s) = %q, want %q", dir, got, root)
		}
	}
}

func TestFindFromWithoutMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "tools"), 0o755); err != nil {
		t.Fatalf("create tools directory: %v", err)
	}

	_, err := findFrom(filepath.Join(dir, "internal", "tools"))
	if err == nil {
		t.Fatal("findFrom succeeded without the repository marker")
	}
	if !strings.Contains(err.Error(), marker) {
		t.Fatalf("error %q does not name the missing marker %q", err, marker)
	}
}
