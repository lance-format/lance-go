//go:build !lance_external_lib && (linux || darwin || windows)

package loader

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

const cacheHelperEnv = "LANCE_LOADER_CACHE_HELPER"

func TestBundleReusesVerifiedCache(t *testing.T) {
	cacheBase := testCacheBase(t)
	raw := []byte("test payload: not a native library")
	b := testBundle(t, raw)

	first, err := b.cachedLibrary()
	if err != nil {
		t.Fatalf("first cachedLibrary: %v", err)
	}
	wantPath := filepath.Join(cacheBase, b.UncompressedSHA256, libraryName)
	if first != wantPath {
		t.Fatalf("cache path = %q, want %q", first, wantPath)
	}

	got, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read cached library: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("cached bytes = %q, want %q", got, raw)
	}

	info, err := os.Stat(first)
	if err != nil {
		t.Fatalf("stat cached library: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Dir(first))
	if err != nil {
		t.Fatalf("stat cache directory: %v", err)
	}
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode != 0o500 {
			t.Fatalf("cached library mode = %o, want 500", mode)
		}
		if mode := dirInfo.Mode().Perm(); mode != 0o700 {
			t.Fatalf("cache directory mode = %o, want 700", mode)
		}
	}

	second, err := b.cachedLibrary()
	if err != nil {
		t.Fatalf("second cachedLibrary: %v", err)
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		t.Fatalf("stat reused cache file: %v", err)
	}
	if !os.SameFile(info, secondInfo) {
		t.Fatal("second call replaced a cache entry that already matched the metadata")
	}
}

func TestBundleReplacesCorruptCache(t *testing.T) {
	testCacheBase(t)
	raw := []byte("verified payload contents")
	b := testBundle(t, raw)

	path, err := b.cachedLibrary()
	if err != nil {
		t.Fatalf("first cachedLibrary: %v", err)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("make cached file writable: %v", err)
	}
	corrupt := bytes.Repeat([]byte{0x5a}, len(raw))
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatalf("corrupt cached file: %v", err)
	}

	replaced, err := b.cachedLibrary()
	if err != nil {
		t.Fatalf("replace corrupt cache: %v", err)
	}
	if replaced != path {
		t.Fatalf("replacement path = %q, want %q", replaced, path)
	}
	got, err := os.ReadFile(replaced)
	if err != nil {
		t.Fatalf("read replacement: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("replacement bytes = %q, want %q", got, raw)
	}
}

func TestBundleFailedExtractionPublishesNothing(t *testing.T) {
	validRaw := []byte("correct payload")
	tests := []struct {
		name   string
		mutate func(*Bundle)
		want   string
	}{
		{
			name: "compressed payload hash mismatch",
			mutate: func(b *Bundle) {
				b.CompressedSHA256 = strings.Repeat("0", 64)
			},
			want: "embedded payload sha256",
		},
		{
			name: "invalid zstd payload",
			mutate: func(b *Bundle) {
				b.Compressed = []byte("this is not a zstd frame")
				b.CompressedSize = int64(len(b.Compressed))
				b.CompressedSHA256 = sha256Hex(b.Compressed)
			},
			want: "decompress bundled",
		},
		{
			name: "uncompressed hash mismatch",
			mutate: func(b *Bundle) {
				b.UncompressedSHA256 = strings.Repeat("0", 64)
			},
			want: "decompressed sha256",
		},
		{
			name: "uncompressed size mismatch",
			mutate: func(b *Bundle) {
				b.UncompressedSize = int64(len(validRaw) - 1)
			},
			want: "decompressed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cacheBase := testCacheBase(t)
			b := testBundle(t, validRaw)
			tt.mutate(&b)

			_, err := b.cachedLibrary()
			if err == nil {
				t.Fatal("cachedLibrary succeeded with an invalid payload")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}

			cacheDir := filepath.Join(cacheBase, b.UncompressedSHA256)
			target := filepath.Join(cacheDir, libraryName)
			if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("failed extraction left a loadable target at %s (stat error %v)", target, statErr)
			}
			assertNoTemporaryFiles(t, cacheDir)
		})
	}
}

func TestBundleValidatesDescriptor(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Bundle)
		want   string
	}{
		{
			name: "empty module",
			mutate: func(b *Bundle) {
				b.Module = ""
			},
			want: "no module",
		},
		{
			name: "target mismatch",
			mutate: func(b *Bundle) {
				b.Target = "plan9/386"
			},
			want: "target",
		},
		{
			name: "library name mismatch",
			mutate: func(b *Bundle) {
				b.LibraryName = "libwrong.so"
			},
			want: "library name",
		},
		{
			name: "compressed size mismatch",
			mutate: func(b *Bundle) {
				b.CompressedSize++
			},
			want: "compressed size",
		},
		{
			name: "bad compressed hash",
			mutate: func(b *Bundle) {
				b.CompressedSHA256 = "not-a-hash"
			},
			want: "compressed SHA-256",
		},
		{
			name: "zero uncompressed size",
			mutate: func(b *Bundle) {
				b.UncompressedSize = 0
			},
			want: "uncompressed size",
		},
		{
			name: "bad uncompressed hash",
			mutate: func(b *Bundle) {
				b.UncompressedSHA256 = strings.Repeat("Z", 64)
			},
			want: "uncompressed SHA-256",
		},
		{
			name: "bad upstream commit",
			mutate: func(b *Bundle) {
				b.UpstreamCommit = strings.Repeat("a", 39)
			},
			want: "upstream commit",
		},
		{
			name: "bad header hash",
			mutate: func(b *Bundle) {
				b.HeaderSHA256 = ""
			},
			want: "header SHA-256",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCacheBase(t)
			b := testBundle(t, []byte("payload"))
			tt.mutate(&b)
			_, err := b.cachedLibrary()
			if err == nil {
				t.Fatal("cachedLibrary accepted an invalid descriptor")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadPrefersEnvOverRegisteredBundle(t *testing.T) {
	cacheBase := testCacheBase(t)
	b := testBundle(t, []byte("payload"))
	installTestBundle(t, b)

	missing := filepath.Join(t.TempDir(), "missing-liblance_c.so")
	t.Setenv(EnvLibPath, missing)

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded with a missing LANCE_LIB_PATH")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error does not wrap os.ErrNotExist: %v", err)
	}
	if strings.Contains(err.Error(), b.Module) {
		t.Fatalf("error came from the registered bundle instead of LANCE_LIB_PATH: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cacheBase, b.UncompressedSHA256)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("bundle cache was created even though LANCE_LIB_PATH was set: %v", statErr)
	}
}

func TestLoadUsesRegisteredBundleBeforeSystemLoader(t *testing.T) {
	testCacheBase(t)
	raw := []byte("payload that the platform loader must reject")
	b := testBundle(t, raw)
	installTestBundle(t, b)
	t.Setenv(EnvLibPath, "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded with a payload that is not a native library")
	}
	if !strings.Contains(err.Error(), b.Module) {
		t.Fatalf("Load error does not identify the registered bundle: %v", err)
	}

	path, err := b.cachedLibrary()
	if err != nil {
		t.Fatalf("bundle cache was not usable after Load failed to open the library: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bundled cache: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("bundled cache bytes = %q, want %q", got, raw)
	}
}

func TestBundleCacheSharedByProcesses(t *testing.T) {
	cacheBase := testCacheBase(t)
	raw := bytes.Repeat([]byte("cross-process cache payload\n"), 4096)
	b := testBundle(t, raw)

	payloadPath := filepath.Join(t.TempDir(), "payload.zst")
	if err := os.WriteFile(payloadPath, b.Compressed, 0o600); err != nil {
		t.Fatalf("write helper payload: %v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	type child struct {
		cmd    *exec.Cmd
		stdin  io.WriteCloser
		stdout *bufio.Reader
		stderr *bytes.Buffer
	}
	const workers = 4
	children := make([]*child, 0, workers)
	for i := 0; i < workers; i++ {
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestBundleCacheHelper$")
		cmd.Env = append(os.Environ(),
			cacheHelperEnv+"=1",
			"LANCE_HELPER_PAYLOAD="+payloadPath,
			"LANCE_HELPER_MODULE="+b.Module,
			"LANCE_HELPER_TARGET="+b.Target,
			"LANCE_HELPER_LIBRARY="+b.LibraryName,
			"LANCE_HELPER_COMPRESSED_SIZE="+strconv.FormatInt(b.CompressedSize, 10),
			"LANCE_HELPER_COMPRESSED_SHA="+b.CompressedSHA256,
			"LANCE_HELPER_UNCOMPRESSED_SIZE="+strconv.FormatInt(b.UncompressedSize, 10),
			"LANCE_HELPER_UNCOMPRESSED_SHA="+b.UncompressedSHA256,
			"LANCE_HELPER_UPSTREAM_COMMIT="+b.UpstreamCommit,
			"LANCE_HELPER_HEADER_SHA="+b.HeaderSHA256,
		)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatalf("stdin pipe for worker %d: %v", i, err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatalf("stdout pipe for worker %d: %v", i, err)
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start worker %d: %v", i, err)
		}
		children = append(children, &child{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: &stderr})
	}
	t.Cleanup(func() {
		for _, c := range children {
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
			}
		}
	})

	for i, c := range children {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			t.Fatalf("worker %d did not become ready: %v; stderr: %s", i, err, c.stderr.String())
		}
		if line != "ready\n" {
			t.Fatalf("worker %d ready line = %q; stderr: %s", i, line, c.stderr.String())
		}
	}

	for _, c := range children {
		if _, err := io.WriteString(c.stdin, "go\n"); err != nil {
			t.Fatalf("release worker: %v", err)
		}
	}

	wantPath := filepath.Join(cacheBase, b.UncompressedSHA256, libraryName)
	for i, c := range children {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			t.Fatalf("worker %d result: %v; stderr: %s", i, err, c.stderr.String())
		}
		if !strings.HasPrefix(line, "ok ") {
			t.Fatalf("worker %d result = %q; stderr: %s", i, line, c.stderr.String())
		}
		if got := strings.TrimSpace(strings.TrimPrefix(line, "ok ")); got != wantPath {
			t.Fatalf("worker %d cache path = %q, want %q", i, got, wantPath)
		}
		if err := c.cmd.Wait(); err != nil {
			t.Fatalf("worker %d exit: %v; stderr: %s", i, err, c.stderr.String())
		}
	}

	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read shared cache entry: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("shared cache entry does not contain the original payload")
	}
	assertNoTemporaryFiles(t, filepath.Dir(wantPath))
}

// TestBundleCacheHelper is a subprocess entry point for
// TestBundleCacheSharedByProcesses. It does nothing in a normal test run.
func TestBundleCacheHelper(t *testing.T) {
	if os.Getenv(cacheHelperEnv) != "1" {
		return
	}

	payload, err := os.ReadFile(os.Getenv("LANCE_HELPER_PAYLOAD"))
	if err != nil {
		failCacheHelper("read payload: %v", err)
	}
	compressedSize, err := strconv.ParseInt(os.Getenv("LANCE_HELPER_COMPRESSED_SIZE"), 10, 64)
	if err != nil {
		failCacheHelper("parse compressed size: %v", err)
	}
	uncompressedSize, err := strconv.ParseInt(os.Getenv("LANCE_HELPER_UNCOMPRESSED_SIZE"), 10, 64)
	if err != nil {
		failCacheHelper("parse uncompressed size: %v", err)
	}

	b := Bundle{
		Module:             os.Getenv("LANCE_HELPER_MODULE"),
		Target:             os.Getenv("LANCE_HELPER_TARGET"),
		LibraryName:        os.Getenv("LANCE_HELPER_LIBRARY"),
		Compressed:         payload,
		CompressedSize:     compressedSize,
		CompressedSHA256:   os.Getenv("LANCE_HELPER_COMPRESSED_SHA"),
		UncompressedSize:   uncompressedSize,
		UncompressedSHA256: os.Getenv("LANCE_HELPER_UNCOMPRESSED_SHA"),
		UpstreamCommit:     os.Getenv("LANCE_HELPER_UPSTREAM_COMMIT"),
		HeaderSHA256:       os.Getenv("LANCE_HELPER_HEADER_SHA"),
	}

	fmt.Println("ready")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		failCacheHelper("wait for release: %v", err)
	}
	if strings.TrimSpace(line) != "go" {
		failCacheHelper("unexpected release line %q", line)
	}

	path, err := b.cachedLibrary()
	if err != nil {
		failCacheHelper("cachedLibrary: %v", err)
	}
	fmt.Printf("ok %s\n", path)
	os.Exit(0)
}

func failCacheHelper(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func testBundle(t *testing.T, raw []byte) Bundle {
	t.Helper()

	var compressed bytes.Buffer
	zw, err := zstd.NewWriter(&compressed, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatalf("create zstd encoder: %v", err)
	}
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("compress test payload: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zstd encoder: %v", err)
	}
	payload := compressed.Bytes()

	return Bundle{
		Module:             "example.com/test/artifact",
		Target:             runtime.GOOS + "/" + runtime.GOARCH,
		LibraryName:        libraryName,
		Compressed:         payload,
		CompressedSize:     int64(len(payload)),
		CompressedSHA256:   sha256Hex(payload),
		UncompressedSize:   int64(len(raw)),
		UncompressedSHA256: sha256Hex(raw),
		UpstreamCommit:     strings.Repeat("a", 40),
		HeaderSHA256:       strings.Repeat("b", 64),
	}
}

func testCacheBase(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "xdg-cache"))
	// os.UserCacheDir uses LocalAppData on Windows, not HOME/XDG_CACHE_HOME.
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "local-app-data"))

	root, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("resolve test cache directory: %v", err)
	}
	return filepath.Join(root, cacheDirName)
}

func installTestBundle(t *testing.T, b Bundle) {
	t.Helper()
	resetForTest()
	mu.Lock()
	bundle = nil
	mu.Unlock()
	RegisterBundle(b)
	t.Cleanup(func() {
		resetForTest()
		mu.Lock()
		bundle = nil
		mu.Unlock()
	})
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read cache directory %s: %v", dir, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("temporary cache file %q was left behind", entry.Name())
		}
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
