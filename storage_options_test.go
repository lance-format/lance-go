package lance_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	lance "github.com/lance-format/lance-go"
)

type accessorTestProvider struct {
	id string

	mu      sync.Mutex
	options map[string]string
	err     error
	hook    func(context.Context) (map[string]string, error)
	fetches int
}

func (p *accessorTestProvider) StorageOptionsProviderID() string { return p.id }

func (p *accessorTestProvider) FetchStorageOptions(ctx context.Context) (map[string]string, error) {
	p.mu.Lock()
	p.fetches++
	options, fetchErr, hook := p.options, p.err, p.hook
	p.mu.Unlock()
	if hook != nil {
		return hook(ctx)
	}
	return options, fetchErr
}

func (p *accessorTestProvider) fetchCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fetches
}

func TestStorageOptionsAccessorSnapshotRefreshAndPresence(t *testing.T) {
	requireNative(t)

	t.Run("expired options refetch", func(t *testing.T) {
		past := strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10)
		provider := &accessorTestProvider{
			id:      "test/expired",
			options: map[string]string{"allow_http": "true", "expires_at_millis": past},
		}
		accessor, err := lance.NewStorageOptionsAccessor(nil, provider)
		if err != nil {
			t.Fatal(err)
		}
		defer accessor.Close()

		first, has, err := accessor.Snapshot()
		if err != nil {
			t.Fatalf("first snapshot: %v", err)
		}
		if !has || first["allow_http"] != "true" {
			t.Fatalf("first snapshot = %v, has=%v", first, has)
		}
		second, has, err := accessor.Snapshot()
		if err != nil {
			t.Fatalf("second snapshot: %v", err)
		}
		if !has || second["allow_http"] != "true" {
			t.Fatalf("second snapshot = %v, has=%v", second, has)
		}
		if got := provider.fetchCount(); got != 2 {
			t.Fatalf("expired provider fetches = %d, want 2", got)
		}
	})

	t.Run("stable options stay cached", func(t *testing.T) {
		provider := &accessorTestProvider{
			id:      "test/stable",
			options: map[string]string{"allow_http": "true"},
		}
		accessor, err := lance.NewStorageOptionsAccessor(nil, provider)
		if err != nil {
			t.Fatal(err)
		}
		defer accessor.Close()

		for i := 0; i < 2; i++ {
			snapshot, has, err := accessor.Snapshot()
			if err != nil {
				t.Fatalf("snapshot %d: %v", i, err)
			}
			if !has || snapshot["allow_http"] != "true" {
				t.Fatalf("snapshot %d = %v, has=%v", i, snapshot, has)
			}
		}
		if got := provider.fetchCount(); got != 1 {
			t.Fatalf("stable provider fetches = %d, want 1", got)
		}
	})

	t.Run("initial and provider merge", func(t *testing.T) {
		provider := &accessorTestProvider{
			id:      "test/initial-merge",
			options: map[string]string{"allow_http": "true"},
		}
		accessor, err := lance.NewStorageOptionsAccessor(map[string]string{"region": "initial"}, provider)
		if err != nil {
			t.Fatal(err)
		}
		defer accessor.Close()

		snapshot, has, err := accessor.Snapshot()
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if !has || snapshot["region"] != "initial" || snapshot["allow_http"] != "true" {
			t.Fatalf("merged snapshot = %v, has=%v", snapshot, has)
		}
	})
}

func TestStorageOptionsAccessorCallbackErrors(t *testing.T) {
	requireNative(t)

	t.Run("typed error", func(t *testing.T) {
		provider := &accessorTestProvider{id: "test/error", err: errors.New("no credentials")}
		accessor, err := lance.NewStorageOptionsAccessor(nil, provider)
		if err != nil {
			t.Fatal(err)
		}
		defer accessor.Close()

		_, _, err = accessor.Snapshot()
		if err == nil {
			t.Fatal("snapshot succeeded, want provider error")
		}
		var nativeErr *lance.Error
		if !errors.As(err, &nativeErr) {
			t.Fatalf("snapshot error = %T %v, want *lance.Error", err, err)
		}
		if !errors.Is(err, lance.ErrInternal) {
			t.Fatalf("snapshot error = %v, want ErrInternal", err)
		}
		if nativeErr.Code != lance.CodeInternal {
			t.Fatalf("snapshot error code = %v, want internal", nativeErr.Code)
		}
		if !strings.Contains(err.Error(), "no credentials") {
			t.Fatalf("snapshot error lost provider message: %v", err)
		}
	})

	t.Run("panic", func(t *testing.T) {
		provider := &accessorTestProvider{
			id: "test/panic",
			hook: func(context.Context) (map[string]string, error) {
				panic("provider exploded")
			},
		}
		accessor, err := lance.NewStorageOptionsAccessor(nil, provider)
		if err != nil {
			t.Fatal(err)
		}
		defer accessor.Close()

		_, _, err = accessor.Snapshot()
		if err == nil || !strings.Contains(err.Error(), "panic") {
			t.Fatalf("panic snapshot error = %v, want panic", err)
		}
	})
}

func TestStorageOptionsSnapshotAfterClose(t *testing.T) {
	requireNative(t)
	owned, err := lance.NewStorageOptionsAccessor(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	owned.Close()
	if _, _, err := owned.Snapshot(); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("Snapshot after Close = %v, want ErrClosed", err)
	}
}

func TestDatasetStorageOptionsAccessorLifecycle(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "accessor.lance")
	schema := testSchema()
	provider := &accessorTestProvider{
		id:      "test/dataset-lifecycle",
		options: map[string]string{"allow_http": "true"},
	}
	accessor, err := lance.NewStorageOptionsAccessor(nil, provider)
	if err != nil {
		t.Fatal(err)
	}

	record := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})
	dataset, err := lance.Write(ctx, uri, makeReader(t, schema, record), lance.WithWriteStorageOptionsAccessor(accessor))
	if err != nil {
		t.Fatalf("Write with accessor: %v", err)
	}
	defer dataset.Close()

	accessor.Close()
	runtime.GC()
	if ids := scanAccessorIDs(t, ctx, dataset); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("retained dataset ids after accessor close = %v", ids)
	}

	openAccessor, err := lance.NewStorageOptionsAccessor(nil, provider)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := lance.Open(ctx, uri, lance.WithStorageOptionsAccessor(openAccessor))
	if err != nil {
		openAccessor.Close()
		t.Fatalf("Open with accessor: %v", err)
	}
	openAccessor.Close()
	if ids := scanAccessorIDs(t, ctx, opened); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("opened dataset ids after accessor close = %v", ids)
	}
	opened.Close()

	if _, err := lance.Open(ctx, uri, lance.WithStorageOptionsAccessor(accessor)); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("Open with closed accessor = %v, want ErrClosed", err)
	}
}

func TestStorageOptionsStaticAndAccessorAreExclusive(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "exclusive.lance")
	accessor, err := lance.NewStorageOptionsAccessor(map[string]string{"allow_http": "true"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer accessor.Close()

	for _, static := range []map[string]string{{"allow_http": "true"}, {}} {
		for _, opts := range [][]lance.OpenOption{
			{lance.WithStorageOptions(static), lance.WithStorageOptionsAccessor(accessor)},
			{lance.WithStorageOptionsAccessor(accessor), lance.WithStorageOptions(static)},
		} {
			if _, err := lance.Open(ctx, uri, opts...); !errors.Is(err, lance.ErrInvalidArgument) {
				t.Fatalf("Open with static+accessor = %v, want ErrInvalidArgument", err)
			}
		}
	}
}

func TestDatasetAccessorStagedAndBatchCommit(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	schema := testSchema()
	provider := &accessorTestProvider{id: "test/dataset-commit"}
	accessor, err := lance.NewStorageOptionsAccessor(nil, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer accessor.Close()

	t.Run("staged commit", func(t *testing.T) {
		uri := filepath.Join(t.TempDir(), "staged.lance")
		base, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})), lance.WithWriteStorageOptionsAccessor(accessor))
		if err != nil {
			t.Fatalf("create base: %v", err)
		}
		base.Close()

		tx, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{2}, []string{"b"}, []bool{true})), lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteStorageOptionsAccessor(accessor))
		if err != nil {
			t.Fatalf("WriteUncommitted: %v", err)
		}
		defer tx.Close()

		committed, err := lance.CommitTransaction(ctx, uri, tx, lance.WithCommitStorageOptionsAccessor(accessor))
		if err != nil {
			t.Fatalf("CommitTransaction: %v", err)
		}
		if rows, err := committed.CountRows(ctx); err != nil || rows != 2 {
			t.Fatalf("staged rows = %d, %v; want 2", rows, err)
		}
		committed.Close()
	})

	t.Run("batch commit", func(t *testing.T) {
		uri := filepath.Join(t.TempDir(), "batch.lance")
		base, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})), lance.WithWriteStorageOptionsAccessor(accessor))
		if err != nil {
			t.Fatalf("create base: %v", err)
		}
		base.Close()

		txA, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{2}, []string{"b"}, []bool{true})), lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteStorageOptionsAccessor(accessor))
		if err != nil {
			t.Fatalf("WriteUncommitted A: %v", err)
		}
		defer txA.Close()
		txB, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{3}, []string{"c"}, []bool{true})), lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteStorageOptionsAccessor(accessor))
		if err != nil {
			t.Fatalf("WriteUncommitted B: %v", err)
		}
		defer txB.Close()

		committed, merged, err := lance.CommitTransactionBatch(ctx, uri, []*lance.Transaction{txA, txB}, lance.WithCommitStorageOptionsAccessor(accessor))
		if err != nil {
			t.Fatalf("CommitTransactionBatch: %v", err)
		}
		defer committed.Close()
		defer merged.Close()
		if rows, err := committed.CountRows(ctx); err != nil || rows != 3 {
			t.Fatalf("batch rows = %d, %v; want 3", rows, err)
		}
	})
}

func scanAccessorIDs(t *testing.T, ctx context.Context, dataset *lance.Dataset) []int64 {
	t.Helper()
	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()
	var ids []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	return ids
}

func TestStorageOptionsAccessorPresence(t *testing.T) {
	requireNative(t)

	t.Run("static none", func(t *testing.T) {
		accessor, err := lance.NewStorageOptionsAccessor(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer accessor.Close()
		snapshot, has, err := accessor.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if has || snapshot != nil {
			t.Fatalf("static none snapshot = %v, has=%v", snapshot, has)
		}
	})

	t.Run("static empty", func(t *testing.T) {
		accessor, err := lance.NewStorageOptionsAccessor(map[string]string{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer accessor.Close()
		snapshot, has, err := accessor.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if !has || snapshot == nil || len(snapshot) != 0 {
			t.Fatalf("static empty snapshot = %v, has=%v", snapshot, has)
		}
	})

	t.Run("provider nil and empty", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			options map[string]string
			has     bool
		}{
			// A provider returning nil is native None at the callback boundary;
			// the native accessor then falls back to defaults, which is Some(empty)
			// when no initial options are present.
			{name: "nil", options: nil, has: true},
			{name: "empty", options: map[string]string{}, has: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				provider := &accessorTestProvider{id: "test/presence/" + tc.name, options: tc.options}
				accessor, err := lance.NewStorageOptionsAccessor(nil, provider)
				if err != nil {
					t.Fatal(err)
				}
				defer accessor.Close()
				if got := accessor.ProviderID(); got != provider.id {
					t.Fatalf("provider id = %q, want %q", got, provider.id)
				}
				snapshot, has, err := accessor.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				if has != tc.has || (tc.has && snapshot == nil) || (!tc.has && snapshot != nil) {
					t.Fatalf("provider snapshot = %v, has=%v; want has=%v", snapshot, has, tc.has)
				}
			})
		}
	})
}
