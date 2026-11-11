//go:build linux || darwin

package loader

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoadFromEnvRequiresExistingFile(t *testing.T) {
	resetForTest()
	t.Cleanup(resetForTest)

	missing := filepath.Join(t.TempDir(), "liblance_c.so")
	t.Setenv(EnvLibPath, missing)

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded with a nonexistent LANCE_LIB_PATH")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error does not wrap fs.ErrNotExist: %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("explicitly configured missing file must not be reported as ErrNotFound: %v", err)
	}
	for _, want := range []string{EnvLibPath, missing} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error does not mention %q: %v", want, err)
		}
	}
}

func TestLoadFromEnvRejectsDirectory(t *testing.T) {
	resetForTest()
	t.Cleanup(resetForTest)

	dir := t.TempDir()
	t.Setenv(EnvLibPath, dir)

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded with a directory as LANCE_LIB_PATH")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error does not explain the file type requirement: %v", err)
	}
}

func TestLoadWithoutEnvReportsDiscoveryGuidance(t *testing.T) {
	resetForTest()
	t.Cleanup(resetForTest)

	t.Setenv(EnvLibPath, "")

	_, err := Load()
	if err == nil {
		t.Skip("a system liblance_c is installed; default discovery succeeded and cannot be exercised here")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("default discovery error does not wrap ErrNotFound: %v", err)
	}
	if !strings.Contains(err.Error(), EnvLibPath) {
		t.Fatalf("default discovery error does not point at %s: %v", EnvLibPath, err)
	}
	if !strings.Contains(err.Error(), libraryName) {
		t.Fatalf("default discovery error does not mention the soname %q: %v", libraryName, err)
	}
}

func TestLoadRealLibrary(t *testing.T) {
	path := os.Getenv(EnvLibPath)
	if path == "" {
		t.Skipf("set %s to a built liblance_c to run the native loader tests", EnvLibPath)
	}

	resetForTest()
	t.Cleanup(resetForTest)

	lib, err := Load()
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	if got := lib.Path(); got != path {
		t.Fatalf("Path() = %q, want %q", got, path)
	}

	for _, name := range []string{
		"lance_last_error_code",
		"lance_last_error_message",
		"lance_free_string",
		"lance_dataset_open",
		"lance_dataset_close",
	} {
		if _, err := lib.Lookup(name); err != nil {
			t.Fatalf("Lookup(%q): %v", name, err)
		}
	}

	if _, err := lib.Lookup("lance_symbol_that_does_not_exist"); err == nil {
		t.Fatal("Lookup succeeded for a symbol that is not in lance-c")
	}
}

func TestLoadConcurrentSingleInstance(t *testing.T) {
	if os.Getenv(EnvLibPath) == "" {
		t.Skipf("set %s to a built liblance_c to run the native loader tests", EnvLibPath)
	}

	resetForTest()
	t.Cleanup(resetForTest)

	const goroutines = 8
	libs := make([]*Library, goroutines)
	var wg sync.WaitGroup
	for i := range libs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lib, err := Load()
			if err != nil {
				t.Errorf("Load in goroutine %d: %v", i, err)
				return
			}
			libs[i] = lib
		}(i)
	}
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}

	for i, lib := range libs {
		if lib != libs[0] {
			t.Fatalf("goroutine %d received a different *Library than goroutine 0", i)
		}
	}
}
