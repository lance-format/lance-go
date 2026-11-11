//go:build !lance_external_lib && (linux || darwin || windows)

package loader

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

const cacheDirName = "lance-go"

// errCacheMismatch marks an existing cache entry that does not match the
// bundle metadata. The entry is safe to remove and replace.
var errCacheMismatch = errors.New("cached library does not match bundle metadata")

// cachedLibrary verifies or creates the private cache file for b and returns
// its path. All processes publishing the same cache entry coordinate through
// an advisory lock in the entry directory.
func (b *Bundle) cachedLibrary() (string, error) {
	if err := b.validate(); err != nil {
		return "", err
	}

	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache directory: %w", err)
	}

	baseDir := filepath.Join(root, cacheDirName)
	if err := makePrivateDir(baseDir); err != nil {
		return "", err
	}
	dir := filepath.Join(baseDir, b.UncompressedSHA256)
	if err := makePrivateDir(dir); err != nil {
		return "", err
	}

	lockPath := filepath.Join(dir, ".lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", fmt.Errorf("open cache lock %s: %w", lockPath, err)
	}
	defer lock.Close()

	if err := lockExclusive(lock); err != nil {
		return "", fmt.Errorf("lock cache directory %s: %w", dir, err)
	}

	target := filepath.Join(dir, b.LibraryName)
	err = verifyCachedLibrary(target, b)
	switch {
	case err == nil:
		return target, nil
	case errors.Is(err, os.ErrNotExist):
		// Nothing was published yet; extract below.
	case errors.Is(err, errCacheMismatch):
		if err := os.Remove(target); err != nil {
			return "", fmt.Errorf("remove corrupt cached library %s: %w", target, err)
		}
	default:
		return "", fmt.Errorf("verify cached library %s: %w", target, err)
	}

	if err := extractBundle(b, dir, target); err != nil {
		return "", err
	}
	return target, nil
}

func makePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create cache directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("set cache directory permissions on %s: %w", dir, err)
	}
	return nil
}

// verifyCachedLibrary checks the exact bytes that will be passed to the
// platform loader.
// It returns errCacheMismatch for a replaceable entry and os.ErrNotExist when
// no entry exists yet.
func verifyCachedLibrary(path string, b *Bundle) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file (mode %s)", errCacheMismatch, path, info.Mode())
	}
	if info.Size() != b.UncompressedSize {
		return fmt.Errorf("%w: %s has size %d, want %d", errCacheMismatch, path, info.Size(), b.UncompressedSize)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != b.UncompressedSHA256 {
		return fmt.Errorf("%w: %s has sha256 %s, want %s", errCacheMismatch, path, got, b.UncompressedSHA256)
	}
	return nil
}

func verifyCompressedPayload(b *Bundle) error {
	sum := sha256.Sum256(b.Compressed)
	if got := hex.EncodeToString(sum[:]); got != b.CompressedSHA256 {
		return fmt.Errorf("embedded payload sha256 is %s, want %s", got, b.CompressedSHA256)
	}
	return nil
}

func extractBundle(b *Bundle, dir, target string) error {
	if err := verifyCompressedPayload(b); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-"+b.LibraryName+"-*")
	if err != nil {
		return fmt.Errorf("create temporary cache file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	published := false
	defer func() {
		if published {
			return
		}
		tmp.Close()
		os.Remove(tmpName)
	}()

	dec, err := zstd.NewReader(bytes.NewReader(b.Compressed))
	if err != nil {
		return fmt.Errorf("open zstd payload for %s: %w", b.Module, err)
	}
	defer dec.Close()

	h := sha256.New()
	written, err := io.Copy(
		io.MultiWriter(tmp, h),
		io.LimitReader(dec, b.UncompressedSize+1),
	)
	if err != nil {
		return fmt.Errorf("decompress bundled %s: %w", b.Module, err)
	}
	if written != b.UncompressedSize {
		return fmt.Errorf("decompressed %d bytes, want %d", written, b.UncompressedSize)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != b.UncompressedSHA256 {
		return fmt.Errorf("decompressed sha256 is %s, want %s", got, b.UncompressedSHA256)
	}

	if err := protectCachedLibrary(tmp); err != nil {
		return fmt.Errorf("protect cached library: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync cached library: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cached library: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("publish cached library %s: %w", target, err)
	}
	published = true
	return nil
}
