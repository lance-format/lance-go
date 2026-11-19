package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validArtifactSource = `package artifact
import _ "embed"
const Target = "linux/amd64"
// Payload is the compressed library.
//go:embed "liblance_c.so.zst"
var Payload []byte
`

func TestVerifyModuleLayout(t *testing.T) {
	tests := []struct {
		name   string
		source string
		extra  string
		remove string
		want   string
	}{
		{name: "generated module", source: validArtifactSource},
		{name: "published license", source: validArtifactSource, extra: "LICENSE"},
		{name: "different embed target", source: strings.Replace(validArtifactSource, `"liblance_c.so.zst"`, `"other.zst"`, 1), want: "must embed the verified payload file"},
		{name: "multiple embed targets", source: strings.Replace(validArtifactSource, `"liblance_c.so.zst"`, `"liblance_c.so.zst" "other.zst"`, 1), want: "must embed the verified payload file"},
		{name: "renamed payload", source: strings.Replace(validArtifactSource, "var Payload", "var Other", 1), want: "must declare var Payload []byte"},
		{name: "missing payload", source: strings.Replace(validArtifactSource, "var Payload []byte", "", 1), want: "exactly one Payload variable"},
		{name: "wrong payload type", source: strings.Replace(validArtifactSource, "[]byte", "string", 1), want: "Payload must have type []byte"},
		{name: "missing embed directive", source: strings.Replace(validArtifactSource, "//go:embed", "//embed", 1), want: "exactly one embed directive"},
		{name: "unattached embed directive", source: strings.Replace(validArtifactSource, "var Payload", "\nvar Payload", 1), want: "exactly one embed directive"},
		{name: "missing embed import", source: strings.Replace(validArtifactSource, `import _ "embed"`, "", 1), want: "import only embed"},
		{name: "initializer replaces bytes", source: validArtifactSource + "func init() { Payload = nil }\n", want: "only data declarations"},
		{name: "extra source", source: validArtifactSource, extra: "extra.go", want: `unexpected artifact module entry "extra.go"`},
		{name: "extra payload", source: validArtifactSource, extra: "other.zst", want: `unexpected artifact module entry "other.zst"`},
		{name: "missing module file", source: validArtifactSource, remove: "go.mod", want: `missing "go.mod"`},
		{name: "missing payload file", source: validArtifactSource, remove: "liblance_c.so.zst", want: `missing "liblance_c.so.zst"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{
				"artifact.go":       tt.source,
				"go.mod":            "module example.com/artifact\n\ngo 1.23.0\n",
				"liblance_c.so.zst": "payload bytes checked separately",
			}
			if tt.extra != "" {
				files[tt.extra] = "extra"
			}
			delete(files, tt.remove)
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := verifyModuleLayout(dir, "liblance_c.so.zst")
			if tt.want == "" {
				if err != nil {
					t.Fatalf("valid module: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want diagnostic containing %q", err, tt.want)
			}
		})
	}
}

func TestVerifyModuleRejectsPayloadDirectory(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{"artifact.go": validArtifactSource, "go.mod": "module example.com/artifact\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "liblance_c.so.zst"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := verifyModuleLayout(dir, "liblance_c.so.zst"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error = %v, want nonregular payload rejection", err)
	}
}
