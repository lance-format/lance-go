package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestContentTypeFor(t *testing.T) {
	cases := []struct {
		suffix      string
		contentType string
		immutable   bool
	}{
		{"list", "text/plain; charset=utf-8", false},
		{"v0.0.0.info", "application/json", true},
		{"v0.0.0.mod", "text/plain; charset=utf-8", true},
		{"v0.0.0.zip", "application/zip", true},
	}
	for _, tc := range cases {
		contentType, _, immutable := contentTypeFor(tc.suffix)
		if contentType != tc.contentType || immutable != tc.immutable {
			t.Fatalf("%s: got %q immutable=%v", tc.suffix, contentType, immutable)
		}
	}
}

func TestSafeJoin(t *testing.T) {
	if _, err := safeJoin("/tmp/root", "liblance", "linux-amd64"); err != nil {
		t.Fatal(err)
	}
	for _, element := range []string{"../escape", "a/b", "a\\b"} {
		if _, err := safeJoin("/tmp/root", element); err == nil {
			t.Fatalf("safeJoin accepted %q", element)
		}
	}
}

func TestVerifyArtifact(t *testing.T) {
	raw := []byte("fixture native payload")
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := encoder.EncodeAll(raw, nil)
	encoder.Close()
	compressed := sha256.Sum256(payload)
	uncompressed := sha256.Sum256(raw)
	artifact := fmt.Sprintf(`package artifact
const (
	Target = "linux/amd64"
	CompressedSize int64 = %d
	CompressedSHA256 = %q
	UncompressedSize int64 = %d
	UncompressedSHA256 = %q
)
`, len(payload), hex.EncodeToString(compressed[:]), len(raw), hex.EncodeToString(uncompressed[:]))
	if err := verifyArtifact([]byte(artifact), payload, "linux/amd64"); err != nil {
		t.Fatal(err)
	}
	if err := verifyArtifact([]byte(artifact), append(payload, 0), "linux/amd64"); err == nil {
		t.Fatal("verifyArtifact accepted a changed payload")
	}
	if err := verifyArtifact([]byte(strings.Replace(artifact, "linux/amd64", "linux/arm64", 1)), payload, "linux/amd64"); err == nil {
		t.Fatal("verifyArtifact accepted a changed target")
	}
}
