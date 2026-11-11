package lance

import (
	"context"
	"io"
	"math"
	"sync"

	"github.com/lance-format/lance-go/internal/ffi"
)

// BlobFile is one blob value returned by TakeBlobs. It stays readable after
// the Dataset that produced it is closed and is safe for concurrent use.
//
// A nil *BlobFile represents a null blob. Reading it reports an error, while
// Close is a no-op so callers can close every slot uniformly.
//
// BlobFile implements io.Reader, io.ReaderAt, io.Seeker and io.Closer. Reads
// after the cursor reaches the end return io.EOF; seeking past the end is
// allowed and a later read returns io.EOF.
type BlobFile struct {
	native *ffi.BlobFile
	uri    string
	res    *resource
	mu     sync.Mutex
}

var (
	_ io.Reader   = (*BlobFile)(nil)
	_ io.ReaderAt = (*BlobFile)(nil)
	_ io.Seeker   = (*BlobFile)(nil)
	_ io.Closer   = (*BlobFile)(nil)
)

// TakeBlobs reads blob handles by Lance row ID, preserving input order and
// duplicates. A NULL native slot becomes a nil *BlobFile. The caller owns the
// returned handles and must close each non-nil handle.
func (d *Dataset) TakeBlobs(ctx context.Context, rowIDs []uint64, column string) ([]*BlobFile, error) {
	return d.takeBlobs(ctx, "take blobs", column, func() ([]*ffi.BlobFile, error) {
		return d.native.TakeBlobs(rowIDs, column)
	})
}

// TakeBlobsByIndices reads blob handles by zero-based row offset, preserving
// input order and duplicates. The caller owns the returned handles.
func (d *Dataset) TakeBlobsByIndices(ctx context.Context, indices []uint64, column string) ([]*BlobFile, error) {
	return d.takeBlobs(ctx, "take blobs by indices", column, func() ([]*ffi.BlobFile, error) {
		return d.native.TakeBlobsByIndices(indices, column)
	})
}

// TakeBlobsByAddresses reads blob handles by row address, preserving input
// order and duplicates. The caller owns the returned handles.
func (d *Dataset) TakeBlobsByAddresses(ctx context.Context, rowAddresses []uint64, column string) ([]*BlobFile, error) {
	return d.takeBlobs(ctx, "take blobs by addresses", column, func() ([]*ffi.BlobFile, error) {
		return d.native.TakeBlobsByAddresses(rowAddresses, column)
	})
}

func (d *Dataset) takeBlobs(ctx context.Context, op, column string, call func() ([]*ffi.BlobFile, error)) ([]*BlobFile, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateCString(column); err != nil {
		return nil, invalidError(op, d.uri, "%v", err)
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()

	native, err := call()
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	files := wrapBlobFiles(native, d.uri)
	if err := checkContext(ctx); err != nil {
		closeBlobFiles(files)
		return nil, err
	}
	return files, nil
}

func wrapBlobFiles(native []*ffi.BlobFile, uri string) []*BlobFile {
	files := make([]*BlobFile, len(native))
	for i, file := range native {
		if file == nil {
			continue
		}
		files[i] = &BlobFile{
			native: file,
			uri:    uri,
			res:    newResource(file.Close),
		}
	}
	return files
}

func closeBlobFiles(files []*BlobFile) {
	for _, file := range files {
		_ = file.Close()
	}
}

func (f *BlobFile) acquire(op string) error {
	if f == nil {
		return invalidError(op, "", "nil blob file")
	}
	if f.res == nil {
		return closedError(op, f.uri)
	}
	if !f.res.acquire() {
		return closedError(op, f.uri)
	}
	f.mu.Lock()
	return nil
}

func (f *BlobFile) release() {
	f.mu.Unlock()
	f.res.release()
}

// Size returns the blob length in bytes. Zero is valid for an empty blob.
func (f *BlobFile) Size() (uint64, error) {
	if err := f.acquire("blob size"); err != nil {
		return 0, err
	}
	defer f.release()
	size, err := f.native.Size()
	if err != nil {
		return 0, wrapError("blob size", f.uri, err)
	}
	return size, nil
}

// Read reads from the cursor into p. It returns io.EOF when no bytes remain.
func (f *BlobFile) Read(p []byte) (int, error) {
	if err := f.acquire("blob read"); err != nil {
		return 0, err
	}
	defer f.release()
	if len(p) == 0 {
		return 0, nil
	}
	n, err := f.native.ReadUpTo(p)
	if err != nil {
		return 0, wrapError("blob read", f.uri, err)
	}
	if n == 0 {
		return 0, io.EOF
	}
	return int(n), nil
}

// ReadAt reads up to len(p) bytes at off without moving the cursor. When the
// blob ends inside p, it returns the available prefix with io.EOF. A
// zero-length read succeeds without checking off against the blob size.
func (f *BlobFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, invalidError("blob read at", blobURI(f), "negative offset %d", off)
	}
	if err := f.acquire("blob read at"); err != nil {
		return 0, err
	}
	defer f.release()
	if len(p) == 0 {
		return 0, nil
	}
	size, err := f.native.Size()
	if err != nil {
		return 0, wrapError("blob read at", f.uri, err)
	}
	start := uint64(off)
	if start >= size {
		return 0, io.EOF
	}
	n := len(p)
	if remaining := size - start; uint64(n) > remaining {
		n = int(remaining)
	}
	if err := f.native.ReadRange(start, p[:n]); err != nil {
		return 0, wrapError("blob read at", f.uri, err)
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// ReadRanges reads several blob-local byte ranges without moving the cursor.
// ranges and destinations must have the same length, and each destination must
// have capacity for its range. Native code validates the complete request
// before writing any destination, so an error leaves every buffer unchanged.
func (f *BlobFile) ReadRanges(ranges []BlobRange, destinations [][]byte) error {
	if len(ranges) != len(destinations) {
		return invalidError("blob read ranges", blobURI(f), "range count %d does not match destination count %d", len(ranges), len(destinations))
	}
	for i, r := range ranges {
		if r.Length > uint64(len(destinations[i])) {
			return invalidError("blob read ranges", blobURI(f), "range %d length %d exceeds destination capacity %d", i, r.Length, len(destinations[i]))
		}
	}
	if err := f.acquire("blob read ranges"); err != nil {
		return err
	}
	defer f.release()
	nativeRanges := make([]ffi.BlobRange, len(ranges))
	for i, r := range ranges {
		nativeRanges[i] = ffi.BlobRange{Offset: r.Offset, Length: r.Length}
	}
	if err := f.native.ReadRanges(nativeRanges, destinations); err != nil {
		return wrapError("blob read ranges", f.uri, err)
	}
	return nil
}

// Seek moves the cursor and returns the new absolute position. Seeking past
// the end is allowed; a subsequent read returns io.EOF.
func (f *BlobFile) Seek(offset int64, whence int) (int64, error) {
	if err := f.acquire("blob seek"); err != nil {
		return 0, err
	}
	defer f.release()

	var base uint64
	switch whence {
	case io.SeekStart:
		base = 0
	case io.SeekCurrent:
		current, err := f.native.Tell()
		if err != nil {
			return 0, wrapError("blob seek", f.uri, err)
		}
		base = current
	case io.SeekEnd:
		size, err := f.native.Size()
		if err != nil {
			return 0, wrapError("blob seek", f.uri, err)
		}
		base = size
	default:
		return 0, invalidError("blob seek", f.uri, "invalid whence %d", whence)
	}

	var pos uint64
	if offset < 0 {
		magnitude := uint64(-(offset + 1)) + 1
		if magnitude > base {
			return 0, invalidError("blob seek", f.uri, "negative position for offset %d", offset)
		}
		pos = base - magnitude
	} else {
		add := uint64(offset)
		if base > math.MaxInt64 || add > uint64(math.MaxInt64)-base {
			return 0, invalidError("blob seek", f.uri, "position overflows int64")
		}
		pos = base + add
	}
	if pos > math.MaxInt64 {
		return 0, invalidError("blob seek", f.uri, "position overflows int64")
	}
	if err := f.native.Seek(pos); err != nil {
		return 0, wrapError("blob seek", f.uri, err)
	}
	return int64(pos), nil
}

// Tell returns the current cursor position.
func (f *BlobFile) Tell() (uint64, error) {
	if err := f.acquire("blob tell"); err != nil {
		return 0, err
	}
	defer f.release()
	pos, err := f.native.Tell()
	if err != nil {
		return 0, wrapError("blob tell", f.uri, err)
	}
	return pos, nil
}

// SetReadAhead sets the native read-ahead buffer size in bytes. Zero disables
// read-ahead. The setting does not change visible bytes or the cursor.
func (f *BlobFile) SetReadAhead(size int) error {
	if size < 0 {
		return invalidError("blob set read-ahead", blobURI(f), "negative buffer size %d", size)
	}
	if err := f.acquire("blob set read-ahead"); err != nil {
		return err
	}
	defer f.release()
	if err := f.native.SetBufferSize(uint64(size)); err != nil {
		return wrapError("blob set read-ahead", f.uri, err)
	}
	return nil
}

// Close releases the native blob handle. It is idempotent, waits for in-flight
// operations, and is a no-op for a nil *BlobFile.
func (f *BlobFile) Close() error {
	if f == nil || f.res == nil {
		return nil
	}
	f.res.close()
	return nil
}

func blobURI(f *BlobFile) string {
	if f == nil {
		return ""
	}
	return f.uri
}

// BlobRange is a half-open byte range within one blob.
type BlobRange struct {
	Offset uint64
	Length uint64
}
