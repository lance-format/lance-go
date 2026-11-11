//go:build windows

package loader

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestWindowsSystemDLLPlumbing checks the Windows loader wrapper against a DLL
// that is already part of the operating system. It deliberately does not claim
// anything about lance-c itself.
func TestWindowsSystemDLLPlumbing(t *testing.T) {
	handle, err := openLibrary("kernel32.dll")
	if err != nil {
		t.Fatalf("openLibrary(kernel32.dll): %v", err)
	}
	if _, err := lookupSymbol(handle, "GetCurrentProcessId"); err != nil {
		t.Fatalf("lookupSymbol(GetCurrentProcessId): %v", err)
	}
	if _, err := lookupSymbol(handle, "lance_symbol_that_does_not_exist"); err == nil {
		t.Fatal("lookupSymbol succeeded for a symbol that is not in kernel32.dll")
	}
}

// TestLoadFromEnvRejectsInvalidWindowsLibrary exercises the configured-file
// path with a regular file that Windows cannot load as a DLL. The error must
// name the configured candidate rather than falling back to ErrNotFound.
func TestLoadFromEnvRejectsInvalidWindowsLibrary(t *testing.T) {
	resetForTest()
	t.Cleanup(resetForTest)

	path := filepath.Join(t.TempDir(), "not-a-dll.bin")
	if err := os.WriteFile(path, []byte("not a PE image"), 0o600); err != nil {
		t.Fatalf("write invalid library: %v", err)
	}
	t.Setenv(EnvLibPath, path)

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded with an invalid DLL")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("explicitly configured invalid DLL must not be reported as ErrNotFound: %v", err)
	}
	for _, want := range []string{EnvLibPath, strconv.Quote(path)} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error does not mention %q: %v", want, err)
		}
	}
}
