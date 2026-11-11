package lance_test

import (
	"context"
	"path/filepath"
	"testing"

	lance "github.com/lance-format/lance-go"
)

func TestSessionSharedCachesAndDatasetLifetime(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3}, []string{"a", "b", "c"}, []bool{true, true, true})

	writer, err := lance.Open(ctx, uri)
	if err == nil {
		writer.Close()
		t.Fatal("Open unexpectedly succeeded for a missing dataset")
	}
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	dataset.Close()

	session, err := lance.NewSession(64<<20, 16<<20)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	first, err := lance.Open(ctx, uri, lance.WithSession(session))
	if err != nil {
		session.Close()
		t.Fatalf("Open with session: %v", err)
	}
	rows, err := first.CountRows(ctx)
	if err != nil {
		first.Close()
		session.Close()
		t.Fatalf("CountRows: %v", err)
	}
	if rows != 3 {
		first.Close()
		session.Close()
		t.Fatalf("CountRows = %d, want 3", rows)
	}
	if _, err := session.SizeBytes(); err != nil {
		first.Close()
		session.Close()
		t.Fatalf("SizeBytes: %v", err)
	}
	if _, err := session.ApproxNumItems(); err != nil {
		first.Close()
		session.Close()
		t.Fatalf("ApproxNumItems: %v", err)
	}
	stats, err := session.CacheStats()
	if err != nil {
		first.Close()
		session.Close()
		t.Fatalf("CacheStats: %v", err)
	}
	if stats.MetadataCacheEntries == 0 && stats.IndexCacheEntries == 0 {
		t.Log("session cache stats are empty immediately after open")
	}

	if err := session.Close(); err != nil {
		t.Fatalf("session Close: %v", err)
	}
	rows, err = first.CountRows(ctx)
	if err != nil {
		t.Fatalf("CountRows after session close: %v", err)
	}
	if rows != 3 {
		t.Fatalf("CountRows after session close = %d, want 3", rows)
	}
	first.Close()

	second, err := lance.NewSessionWithCacheSpecs(
		lance.CacheSpec{Kind: lance.CacheSpecConfig, BackendKind: "moka", Options: map[string]string{"capacity": "1048576"}},
		lance.CacheSpec{Kind: lance.CacheSpecDefault},
	)
	if err != nil {
		t.Fatalf("NewSessionWithCacheSpecs: %v", err)
	}
	defer second.Close()
	dataset, err = lance.Open(ctx, uri, lance.WithSession(second))
	if err != nil {
		t.Fatalf("Open with configured session: %v", err)
	}
	defer dataset.Close()
	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan with configured session: %v", err)
	}
	scanner.Close()
}
