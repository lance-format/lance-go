//go:build linux || darwin || windows

package loader

import (
	"path/filepath"
	"testing"
)

func TestObserveBeforeLoadDoesNotOpenOrCache(t *testing.T) {
	resetForTest()
	t.Cleanup(resetForTest)

	t.Setenv(EnvLibPath, filepath.Join(t.TempDir(), "missing-liblance_c.so"))

	state := Observe()
	if state.Loaded {
		t.Fatalf("Observe reported a loaded library before Load: %+v", state)
	}
	if state.Source != SourceUnknown || state.Path != "" || state.Bundle != nil {
		t.Fatalf("Observe reported source metadata before Load: %+v", state)
	}

	if _, err := Load(); err == nil {
		t.Fatal("Load succeeded with a missing LANCE_LIB_PATH")
	}
	state = Observe()
	if state.Loaded {
		t.Fatalf("Observe cached a failed load: %+v", state)
	}
}

func TestObserveReportsSuccessfulSourceMetadata(t *testing.T) {
	resetForTest()
	t.Cleanup(resetForTest)

	mu.Lock()
	cached = &Library{
		path:   "/cache/lance-go/liblance_c.so",
		source: SourceBundle,
		bundle: &BundleInfo{
			Module:         "example.com/artifact",
			Target:         "linux/amd64",
			UpstreamCommit: "e2564b89be36bb18f5beff6d673daf9dfff8c986",
			HeaderSHA256:   "90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d",
		},
	}
	mu.Unlock()

	state := Observe()
	if !state.Loaded {
		t.Fatal("Observe did not report the cached library as loaded")
	}
	if state.Source != SourceBundle {
		t.Fatalf("Source = %v, want %v", state.Source, SourceBundle)
	}
	if state.Path != "/cache/lance-go/liblance_c.so" {
		t.Fatalf("Path = %q", state.Path)
	}
	if state.Bundle == nil {
		t.Fatal("Observe did not report bundle metadata")
	}
	if state.Bundle.Module != "example.com/artifact" || state.Bundle.Target != "linux/amd64" {
		t.Fatalf("Bundle = %+v", *state.Bundle)
	}

	state.Bundle.Module = "mutated"
	if got := Observe(); got.Bundle == nil || got.Bundle.Module != "example.com/artifact" {
		t.Fatalf("Observe returned shared bundle metadata: %+v", got.Bundle)
	}
}

func TestSourceString(t *testing.T) {
	tests := []struct {
		source Source
		want   string
	}{
		{SourceUnknown, "unknown"},
		{SourceEnv, EnvLibPath},
		{SourceBundle, "bundle"},
		{SourceSystem, "system"},
		{Source(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.source.String(); got != tt.want {
			t.Errorf("Source(%d).String() = %q, want %q", tt.source, got, tt.want)
		}
	}
}
