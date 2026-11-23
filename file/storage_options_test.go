package file_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/file"
)

var _ io.Closer = (*file.StorageOptions)(nil)

type fileAccessorProvider struct {
	id string
}

func (p fileAccessorProvider) StorageOptionsProviderID() string { return p.id }

func (p fileAccessorProvider) FetchStorageOptions(context.Context) (map[string]string, error) {
	return map[string]string{"allow_http": "true"}, nil
}

func TestRawFileStorageOptionsAccessorRetainedAfterClose(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accessor.lance")
	schema := testSchema()
	batch := testBatch(t, schema)
	defer batch.Release()

	storage, err := file.NewStorageOptionsWithProvider(nil, fileAccessorProvider{id: "test/file-writer"})
	if err != nil {
		t.Fatalf("new storage options: %v", err)
	}
	writer, err := file.Create(ctx, path, schema, &file.WriterOptions{StorageOptions: storage})
	if err != nil {
		storage.Close()
		t.Fatalf("create writer: %v", err)
	}
	if err := writer.Write(ctx, batch); err != nil {
		writer.Close()
		storage.Close()
		t.Fatalf("write: %v", err)
	}
	if _, err := writer.Finish(ctx); err != nil {
		writer.Close()
		storage.Close()
		t.Fatalf("finish: %v", err)
	}
	if err := writer.Close(); err != nil {
		storage.Close()
		t.Fatalf("close writer: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage options: %v", err)
	}

	readStorage, err := file.NewStorageOptionsWithProvider(nil, fileAccessorProvider{id: "test/file-reader"})
	if err != nil {
		t.Fatalf("new read storage options: %v", err)
	}
	reader, err := file.Open(ctx, path, &file.ReaderOptions{StorageOptions: readStorage})
	if err != nil {
		readStorage.Close()
		t.Fatalf("open reader: %v", err)
	}
	if err := readStorage.Close(); err != nil {
		reader.Close()
		t.Fatalf("close read storage options: %v", err)
	}
	rows, err := reader.ReadAll(ctx, file.ReadOptions{})
	if err != nil {
		reader.Close()
		t.Fatalf("read all: %v", err)
	}
	defer rows.Release()
	if ids := collectIDs(t, rows); len(ids) != 4 {
		t.Fatalf("read ids = %v, want 4 rows", ids)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
}

func TestRawFileSessionStorageOptionsAccessorAndOverrides(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	base := t.TempDir()
	schema := testSchema()
	batch := testBatch(t, schema)
	defer batch.Release()

	storage, err := file.NewStorageOptionsWithProvider(map[string]string{"region": "initial"}, fileAccessorProvider{id: "test/file-session"})
	if err != nil {
		t.Fatalf("new storage options: %v", err)
	}
	session, err := file.OpenSession(ctx, base, &file.SessionOptions{StorageOptions: storage})
	if err != nil {
		storage.Close()
		t.Fatalf("open session: %v", err)
	}
	defer session.Close()

	override := func() *file.StorageOptions {
		options, err := file.NewStorageOptions(map[string]string{"allow_http": "true"})
		if err != nil {
			t.Fatalf("new override storage options: %v", err)
		}
		return options
	}()
	defer override.Close()
	if _, err := session.OpenWriter(ctx, "rejected.lance", schema, &file.WriterOptions{StorageOptions: override}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("session open writer with override = %v, want ErrInvalidArgument", err)
	}

	writer, err := session.OpenWriter(ctx, "session.lance", schema, nil)
	if err != nil {
		t.Fatalf("session open writer: %v", err)
	}
	if err := writer.Write(ctx, batch); err != nil {
		t.Fatalf("session write: %v", err)
	}
	if _, err := writer.Finish(ctx); err != nil {
		t.Fatalf("session finish: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close session writer: %v", err)
	}

	// The file session retains its accessor for reads after the caller-owned
	// StorageOptions is closed.
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage options: %v", err)
	}
	reader, err := session.OpenReader(ctx, "session.lance", nil)
	if err != nil {
		t.Fatalf("session open reader after storage close: %v", err)
	}
	defer reader.Close()
	rows, err := reader.ReadAll(ctx, file.ReadOptions{})
	if err != nil {
		t.Fatalf("session read all: %v", err)
	}
	defer rows.Release()
	if ids := collectIDs(t, rows); len(ids) != 4 {
		t.Fatalf("session read ids = %v, want 4 rows", ids)
	}
}

func TestRawFileStorageOptionsStaticPresence(t *testing.T) {
	requireNative(t)
	for _, values := range []map[string]string{nil, {}} {
		storage, err := file.NewStorageOptions(values)
		if err != nil {
			t.Fatalf("new storage options %v: %v", values, err)
		}
		snapshot, has, err := storage.Snapshot()
		if err != nil {
			storage.Close()
			t.Fatalf("snapshot %v: %v", values, err)
		}
		if !has || snapshot == nil || len(snapshot) != 0 {
			storage.Close()
			t.Fatalf("NewStorageOptions(%v) snapshot = %v, has=%v; want explicit empty", values, snapshot, has)
		}
		if err := storage.Close(); err != nil {
			t.Fatalf("close storage options %v: %v", values, err)
		}
	}
}

func TestRawFileStorageOptionsFromRootAccessor(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "root-accessor.lance")
	schema := testSchema()
	batch := testBatch(t, schema)
	defer batch.Release()

	owned, err := lance.NewStorageOptionsAccessor(nil, fileAccessorProvider{id: "test/file-root-accessor"})
	if err != nil {
		t.Fatalf("new root accessor: %v", err)
	}
	storage, err := file.NewStorageOptionsFromAccessor(owned)
	if err != nil {
		owned.Close()
		t.Fatalf("adopt root accessor: %v", err)
	}
	if storage != owned {
		t.Fatalf("adopted accessor = %p, want the root accessor %p", storage, owned)
	}
	writer, err := file.Create(ctx, path, schema, &file.WriterOptions{StorageOptions: storage})
	if err != nil {
		storage.Close()
		t.Fatalf("create writer: %v", err)
	}
	if err := writer.Write(ctx, batch); err != nil {
		writer.Close()
		storage.Close()
		t.Fatalf("write: %v", err)
	}
	if _, err := writer.Finish(ctx); err != nil {
		writer.Close()
		storage.Close()
		t.Fatalf("finish: %v", err)
	}
	if err := writer.Close(); err != nil {
		storage.Close()
		t.Fatalf("close writer: %v", err)
	}

	reader, err := file.Open(ctx, path, &file.ReaderOptions{StorageOptions: owned})
	if err != nil {
		storage.Close()
		t.Fatalf("open reader: %v", err)
	}
	// Closing the adopted accessor closes the root handle, because they are the
	// same native accessor.
	if err := storage.Close(); err != nil {
		reader.Close()
		t.Fatalf("close adopted storage options: %v", err)
	}
	if _, _, err := owned.Snapshot(); !errors.Is(err, lance.ErrClosed) {
		reader.Close()
		t.Fatalf("root accessor Snapshot after close = %v, want ErrClosed", err)
	}
	if _, err := file.Open(ctx, path, &file.ReaderOptions{StorageOptions: storage}); !errors.Is(err, lance.ErrClosed) {
		reader.Close()
		t.Fatalf("open with closed accessor = %v, want ErrClosed", err)
	}
	// The open reader retained its own native reference.
	rows := mustReadAll(t, reader)
	defer rows.Release()
	if ids := collectIDs(t, rows); len(ids) != 4 {
		reader.Close()
		t.Fatalf("read ids after accessor close = %v, want 4 rows", ids)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("second adopted close: %v", err)
	}
	if err := owned.Close(); err != nil {
		t.Fatalf("root close: %v", err)
	}
}

type callerOwnedFileProvider struct {
	id string

	mu      sync.Mutex
	fetches int
	closed  bool
}

func (p *callerOwnedFileProvider) StorageOptionsProviderID() string { return p.id }

func (p *callerOwnedFileProvider) FetchStorageOptions(context.Context) (map[string]string, error) {
	p.mu.Lock()
	p.fetches++
	p.mu.Unlock()
	return map[string]string{"allow_http": "true"}, nil
}

// Close is not part of StorageOptionsProvider. It proves the SDK never calls a
// provider Close method.
func (p *callerOwnedFileProvider) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
}

func (p *callerOwnedFileProvider) wasClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *callerOwnedFileProvider) fetchCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fetches
}

func TestRawFileStorageOptionsProviderCallerOwned(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	path := writeTestFile(t, ctx, "provider-owned.lance")

	provider := &callerOwnedFileProvider{id: "test/file-owned-provider"}
	storage, err := file.NewStorageOptionsWithProvider(nil, provider)
	if err != nil {
		t.Fatalf("new storage options: %v", err)
	}
	reader, err := file.Open(ctx, path, &file.ReaderOptions{StorageOptions: storage})
	if err != nil {
		storage.Close()
		t.Fatalf("open reader: %v", err)
	}
	if err := storage.Close(); err != nil {
		reader.Close()
		t.Fatalf("close storage options: %v", err)
	}
	rows := mustReadAll(t, reader)
	if ids := collectIDs(t, rows); len(ids) != 4 {
		rows.Release()
		reader.Close()
		t.Fatalf("read ids after storage close = %v, want 4 rows", ids)
	}
	rows.Release()
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	if provider.wasClosed() {
		t.Fatal("SDK closed the caller-owned provider")
	}

	// The provider value remains usable for another accessor after the first
	// one is closed.
	reused, err := lance.NewStorageOptionsAccessor(nil, provider)
	if err != nil {
		t.Fatalf("reuse provider: %v", err)
	}
	snapshot, has, err := reused.Snapshot()
	if err != nil {
		reused.Close()
		t.Fatalf("reuse provider snapshot: %v", err)
	}
	if !has || snapshot["allow_http"] != "true" {
		reused.Close()
		t.Fatalf("reuse provider snapshot = %v, has=%v", snapshot, has)
	}
	if provider.fetchCount() == 0 {
		reused.Close()
		t.Fatal("reused provider was not fetched")
	}
	if err := reused.Close(); err != nil {
		t.Fatalf("close reused accessor: %v", err)
	}
	if provider.wasClosed() {
		t.Fatal("SDK closed the caller-owned provider after the final owner")
	}
}

func TestRawFileStorageOptionsFromZeroRootAccessor(t *testing.T) {
	requireNative(t)
	ctx := context.Background()

	if _, err := file.NewStorageOptionsFromAccessor(nil); err == nil {
		t.Fatal("nil root accessor was accepted")
	}
	zero := &lance.StorageOptionsAccessor{}
	storage, err := file.NewStorageOptionsFromAccessor(zero)
	if err != nil {
		t.Fatalf("zero root accessor: %v", err)
	}
	if _, err := file.Open(ctx, filepath.Join(t.TempDir(), "zero.lance"), &file.ReaderOptions{StorageOptions: storage}); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("open with zero root accessor = %v, want ErrClosed", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	zeroSession := &lance.Session{}
	fileSession, err := file.OpenSession(ctx, t.TempDir(), &file.SessionOptions{Session: zeroSession})
	if err != nil {
		t.Fatalf("zero root session: %v", err)
	}
	if err := fileSession.Close(); err != nil {
		t.Fatalf("close zero root session: %v", err)
	}
}
