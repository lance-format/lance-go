package file_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/file"
)

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
