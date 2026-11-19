package lance

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lance-format/lance-go/internal/ffi"
)

const runtimeInfoHelperEnv = "LANCE_RUNTIME_INFO_HELPER"

func TestRuntimeInfoBeforeFirstNativeUse(t *testing.T) {
	if os.Getenv(runtimeInfoHelperEnv) == "1" {
		info := RuntimeInfo()
		if info.Loaded || info.FFIReady {
			t.Fatalf("RuntimeInfo loaded native code: %+v", info)
		}
		if info.Library.Source != RuntimeSourceUnknown || info.Library.Path != "" || info.Library.Bundle != nil {
			t.Fatalf("RuntimeInfo reported a library before load: %+v", info.Library)
		}

		cacheRoot, err := os.UserCacheDir()
		if err != nil {
			t.Fatalf("locate helper cache root: %v", err)
		}
		if _, err := os.Stat(filepath.Join(cacheRoot, "lance-go")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("RuntimeInfo touched the bundle cache: %v", err)
		}
		return
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}
	root := t.TempDir()
	cmd := exec.Command(exe, "-test.run=^TestRuntimeInfoBeforeFirstNativeUse$")
	cmd.Env = append(os.Environ(),
		runtimeInfoHelperEnv+"=1",
		"HOME="+root,
		"XDG_CACHE_HOME="+filepath.Join(root, "cache"),
		"LOCALAPPDATA="+filepath.Join(root, "local-app-data"),
		"LANCE_LIB_PATH=",
		"LANCE_CACHE_DIR=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("RuntimeInfo helper failed: %v\n%s", err, out)
	}
}

func TestRuntimeInfoReportsExpectedPin(t *testing.T) {
	info := RuntimeInfo()
	expected := ffi.Pinned()
	got := fmt.Sprintf("%s %s %s %s", info.Expected.Repository, info.Expected.Release, info.Expected.Commit, info.Expected.HeaderSHA256)
	want := fmt.Sprintf("%s %s %s %s", expected.Repository, expected.Release, expected.Commit, expected.HeaderSHA256)
	if got != want {
		t.Fatalf("Expected = %q, want %q", got, want)
	}
	if info.Loaded && info.Library.Source == RuntimeSourceUnknown {
		t.Fatalf("loaded runtime has unknown source: %+v", info)
	}
	if !info.Loaded && info.FFIReady {
		t.Fatalf("unloaded runtime is FFI-ready: %+v", info)
	}
}

func TestRuntimeSourceString(t *testing.T) {
	tests := []struct {
		source RuntimeSource
		want   string
	}{
		{RuntimeSourceUnknown, "unknown"},
		{RuntimeSourceLibPath, "LANCE_LIB_PATH"},
		{RuntimeSourceBundle, "bundle"},
		{RuntimeSourceSystem, "system"},
		{RuntimeSource(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.source.String(); got != tt.want {
			t.Errorf("RuntimeSource(%d).String() = %q, want %q", tt.source, got, tt.want)
		}
	}
}
