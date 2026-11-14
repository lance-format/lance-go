package lance_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/loader"
)

const baseStoreParamsOpenSymbol = "lance_dataset_open_with_base_store_params"

func hasBaseStoreParamsOpen(t *testing.T) bool {
	t.Helper()
	library, err := loader.Load()
	if err != nil {
		t.Fatalf("load lance-c: %v", err)
	}
	_, err = library.Lookup(baseStoreParamsOpenSymbol)
	return err == nil
}

// verifyBaseStoreParamsOpenCapability exercises the unsupported public boundary
// on older libraries. A true result requires the real reopen path below.
func verifyBaseStoreParamsOpenCapability(t *testing.T) bool {
	t.Helper()
	if hasBaseStoreParamsOpen(t) {
		return true
	}
	dataset, err := lance.Open(context.Background(), filepath.Join(t.TempDir(), "missing.lance"),
		lance.WithBaseStoreParams(map[string]lance.BaseStoreParams{}))
	if dataset != nil {
		dataset.Close()
		t.Fatal("unsupported per-base open returned a dataset")
	}
	if !errors.Is(err, lance.ErrNotSupported) || !strings.Contains(err.Error(), baseStoreParamsOpenSymbol) {
		t.Fatalf("per-base open without its optional symbol = %v, want ErrNotSupported naming the symbol", err)
	}
	t.Log("verified ErrNotSupported; this library cannot exercise native per-base reopen")
	return false
}

func scanIDsErr(dataset *lance.Dataset) error {
	scanner, err := dataset.Scan(lance.WithColumns("id"))
	if err != nil {
		return err
	}
	defer scanner.Close()
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			return err
		}
		_ = batch
	}
	return nil
}

func TestOpenWithBaseStoreParamsPreservesNilAndEmpty(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	created, err := lance.Create(ctx, uri, makeReader(t, schema,
		makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	created.Close()

	ordinary, err := lance.Open(ctx, uri, lance.WithBaseStoreParams(nil))
	if err != nil {
		t.Fatalf("Open with nil base store params: %v", err)
	}
	if ids := scanIDs(t, ordinary); !equalIDs(ids, 1) {
		t.Fatalf("nil base store params ids = %v, want [1]", ids)
	}
	ordinary.Close()

	empty, err := lance.Open(ctx, uri, lance.WithBaseStoreParams(map[string]lance.BaseStoreParams{}))
	if hasBaseStoreParamsOpen(t) {
		if err != nil {
			t.Fatalf("Open with present empty base store params: %v", err)
		}
		if ids := scanIDs(t, empty); !equalIDs(ids, 1) {
			t.Fatalf("empty base store params ids = %v, want [1]", ids)
		}
		empty.Close()
		return
	}
	if !errors.Is(err, lance.ErrNotSupported) {
		t.Fatalf("Open with present empty base store params on a library without the symbol = %v, want ErrNotSupported", err)
	}
	if !strings.Contains(err.Error(), baseStoreParamsOpenSymbol) {
		t.Fatalf("unsupported error = %v, want symbol name", err)
	}
}

func TestOpenWithoutBaseStoreParamsDoesNotRequireOptionalSymbol(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	created, err := lance.Create(ctx, uri, makeReader(t, schema,
		makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	created.Close()

	opened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("ordinary Open: %v", err)
	}
	defer opened.Close()
	if ids := scanIDs(t, opened); !equalIDs(ids, 1) {
		t.Fatalf("ordinary Open ids = %v, want [1]", ids)
	}
}

func TestOpenWithBaseStoreParamsRequiresOptionalSymbol(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	_, err := lance.Open(ctx, filepath.Join(t.TempDir(), "missing.lance"),
		lance.WithBaseStoreParams(map[string]lance.BaseStoreParams{}))
	if hasBaseStoreParamsOpen(t) {
		if err == nil {
			t.Fatal("Open with the optional symbol unexpectedly succeeded")
		}
		if strings.Contains(err.Error(), "is not available in the loaded lance-c library") {
			t.Fatalf("exported optional symbol was not bound: %v", err)
		}
		return
	}
	if !errors.Is(err, lance.ErrNotSupported) {
		t.Fatalf("Open with base store params without the optional symbol = %v, want ErrNotSupported", err)
	}
	if !strings.Contains(err.Error(), baseStoreParamsOpenSymbol) {
		t.Fatalf("unsupported error = %v, want symbol name", err)
	}
}

func TestOpenWithBaseStoreParamsBridgesStaticOptionsPresence(t *testing.T) {
	requireNative(t)
	if !verifyBaseStoreParamsOpenCapability(t) {
		return
	}
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	created, err := lance.Create(ctx, uri, makeReader(t, schema,
		makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	created.Close()

	session, err := lance.NewSession(0, 0)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	opened, err := lance.Open(ctx, uri,
		lance.WithStorageOptions(map[string]string{}),
		lance.WithSession(session),
		lance.WithBaseStoreParams(map[string]lance.BaseStoreParams{}),
	)
	if err != nil {
		session.Close()
		t.Fatalf("Open with present empty static options and session: %v", err)
	}
	session.Close()
	if ids := scanIDs(t, opened); !equalIDs(ids, 1) {
		t.Fatalf("static+session ids = %v, want [1]", ids)
	}
	opened.Close()

	invalid, err := lance.Open(ctx, uri,
		lance.WithStorageOptions(map[string]string{"local_fsync": "invalid"}),
		lance.WithBaseStoreParams(map[string]lance.BaseStoreParams{}),
	)
	if err == nil {
		err = scanIDsErr(invalid)
		invalid.Close()
	}
	if err == nil {
		t.Fatal("static storage options were dropped on the per-base open route")
	}
	if !strings.Contains(err.Error(), "local_fsync") {
		t.Fatalf("invalid static option error = %v, want local_fsync diagnostic", err)
	}
}

func TestOpenWithBaseStoreParamsReopenExactPaths(t *testing.T) {
	requireNative(t)
	if !verifyBaseStoreParamsOpenCapability(t) {
		return
	}
	ctx := context.Background()
	root := t.TempDir()
	uri := "shared-memory://base-store-" + filepath.Base(root) + "/dataset.lance"
	base := filepath.Join(root, "base")
	schema := testSchema()
	session, err := lance.NewSession(0, 0)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Close()

	writeAccessor, err := lance.NewStorageOptionsAccessor(map[string]string{"local_fsync": "true"}, nil)
	if err != nil {
		t.Fatalf("NewStorageOptionsAccessor(write): %v", err)
	}
	created, err := lance.Create(ctx, uri, makeReader(t, schema,
		makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})),
		lance.WithWriteSession(session),
		lance.WithWriteParams(lance.WriteParams{
			InitialBases: []lance.BasePath{{ID: 1, Path: base, IsDatasetRoot: false}},
			TargetBases:  []uint32{1},
			BaseStoreParams: map[string]lance.BaseStoreParams{
				base: {StorageOptionsAccessor: writeAccessor},
			},
		}),
	)
	if err != nil {
		writeAccessor.Close()
		t.Fatalf("Create with initial base: %v", err)
	}
	bases, err := created.BasePaths()
	if err != nil {
		created.Close()
		writeAccessor.Close()
		t.Fatalf("BasePaths: %v", err)
	}
	registered, ok := bases[1]
	if !ok || registered.Path == "" {
		created.Close()
		writeAccessor.Close()
		t.Fatalf("registered bases = %+v, want base ID 1 with a path", bases)
	}
	// The manifest path is authoritative; do not substitute the write-time key.
	exactPath := registered.Path
	created.Close()
	writeAccessor.Close()

	invalid, err := lance.NewStorageOptionsAccessor(map[string]string{"local_fsync": "invalid"}, nil)
	if err != nil {
		t.Fatalf("NewStorageOptionsAccessor(invalid): %v", err)
	}
	defer invalid.Close()
	exact, err := lance.NewStorageOptionsAccessor(map[string]string{"local_fsync": "true"}, nil)
	if err != nil {
		t.Fatalf("NewStorageOptionsAccessor(exact): %v", err)
	}
	defer exact.Close()

	// The shared-memory root ignores local_fsync, while the local base rejects
	// an invalid dataset-level value unless an exact per-base binding wins.
	missing, err := lance.Open(ctx, uri,
		lance.WithStorageOptionsAccessor(invalid),
		lance.WithSession(session),
	)
	if err != nil {
		t.Fatalf("Open without base binding must succeed before scanning: %v", err)
	}
	missingErr := scanIDsErr(missing)
	missing.Close()
	if missingErr == nil || !strings.Contains(missingErr.Error(), "local_fsync") {
		t.Fatalf("missing base binding scan = %v, want local_fsync diagnostic", missingErr)
	}

	wrong, err := lance.Open(ctx, uri,
		lance.WithStorageOptionsAccessor(invalid),
		lance.WithSession(session),
		lance.WithBaseStoreParams(map[string]lance.BaseStoreParams{
			"shared-memory://unrelated/base": {StorageOptionsAccessor: exact},
		}),
	)
	if err != nil {
		t.Fatalf("Open with non-matching base binding: %v", err)
	}
	wrongErr := scanIDsErr(wrong)
	wrong.Close()
	if wrongErr == nil || !strings.Contains(wrongErr.Error(), "local_fsync") {
		t.Fatalf("non-matching base binding scan = %v, want local_fsync diagnostic", wrongErr)
	}

	reopened, err := lance.Open(ctx, uri,
		lance.WithStorageOptionsAccessor(invalid),
		lance.WithSession(session),
		lance.WithBaseStoreParams(map[string]lance.BaseStoreParams{
			exactPath: {StorageOptionsAccessor: exact},
		}),
	)
	if err != nil {
		t.Fatalf("Open with exact base binding: %v", err)
	}
	invalid.Close()
	exact.Close()
	if ids := scanIDs(t, reopened); !equalIDs(ids, 1, 2) {
		t.Fatalf("exact base binding ids after accessor close = %v, want [1 2]", ids)
	}
	reopened.Close()
}
