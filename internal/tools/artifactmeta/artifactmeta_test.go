package artifactmeta

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validMetadata = `package artifact

const (
	Target                   = "linux/amd64"
	LibraryName              = "liblance_c.so"
	CompressedSize     int64 = 87177503
	CompressedSHA256         = "1de588c93d23b5ecbe0288ce67e7ba236089d3b66e37d55f565812937b26eba0"
	UncompressedSize   int64 = 342441416
	UncompressedSHA256       = "8e00a4c9fd401d0e99723222d6dfeea47855c57a2e442409b1c32dfed0559c13"
	UpstreamRepo             = "https://github.com/lancedb/lance-c"
	UpstreamCommit           = "e2564b89be36bb18f5beff6d673daf9dfff8c986"
	HeaderSHA256             = "90e52d19f7cedb6cd52a83024927453f0ded5f13e24ed2adae8684cd9d4b0d8d"
)
`

func writeMetadata(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	return path
}

func TestRead(t *testing.T) {
	md, err := Read(writeMetadata(t, validMetadata))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if md.Target != "linux/amd64" || md.LibraryName != "liblance_c.so" ||
		md.CompressedSize != 87177503 || md.UncompressedSize != 342441416 ||
		md.UncompressedSHA256 != "8e00a4c9fd401d0e99723222d6dfeea47855c57a2e442409b1c32dfed0559c13" ||
		md.UpstreamCommit != "e2564b89be36bb18f5beff6d673daf9dfff8c986" {
		t.Fatalf("unexpected metadata: %+v", md)
	}

	cases := map[string]struct {
		content string
		want    string
	}{
		"missing uncompressed hash": {
			strings.Replace(validMetadata, "\tUncompressedSHA256       = \"8e00a4c9fd401d0e99723222d6dfeea47855c57a2e442409b1c32dfed0559c13\"\n", "", 1),
			"missing UncompressedSHA256",
		},
		"duplicate target": {
			validMetadata + "\nconst (\n\tTarget = \"windows/arm64\"\n)\n",
			"duplicate Target",
		},
		"uppercase hash": {
			strings.Replace(validMetadata, "8e00a4c9fd401d0e99723222d6dfeea47855c57a2e442409b1c32dfed0559c13", "8E00A4C9FD401D0E99723222D6DFEEA47855C57A2E442409B1C32DFED0559C13", 1),
			"UncompressedSHA256",
		},
		"malformed size": {
			strings.Replace(validMetadata, "UncompressedSize   int64 = 342441416", "UncompressedSize   int64 = three", 1),
			"missing UncompressedSize",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Read(writeMetadata(t, testCase.content))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("Read error = %v, want one mentioning %q", err, testCase.want)
			}
		})
	}
}

func TestReadMissingFile(t *testing.T) {
	_, err := Read(filepath.Join(t.TempDir(), "artifact.go"))
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read error = %v, want a wrapped fs.ErrNotExist", err)
	}
}
