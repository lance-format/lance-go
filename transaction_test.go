package lance_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	lance "github.com/lance-format/lance-go"
)

func requireErrorCode(t *testing.T, err error, code lance.Code) {
	t.Helper()
	var lanceErr *lance.Error
	if !errors.As(err, &lanceErr) {
		t.Fatalf("error = %v, want *lance.Error with code %v", err, code)
	}
	if lanceErr.Code != code {
		t.Fatalf("error code = %v, want %v (error: %v)", lanceErr.Code, code, err)
	}
}

func scanIDs(t *testing.T, scanner *lance.Scanner) []int64 {
	t.Helper()
	defer scanner.Close()
	var ids []int64
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("scan records: %v", err)
		}
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func fragmentTakeIDs(t *testing.T, fragment *lance.Fragment, indices []uint32) []int64 {
	t.Helper()
	batches, err := fragment.Take(context.Background(), indices)
	if err != nil {
		t.Fatalf("fragment take: %v", err)
	}
	defer func() {
		for _, batch := range batches {
			batch.Release()
		}
	}()
	var ids []int64
	for _, batch := range batches {
		ids = append(ids, collectInt64(t, batch, "id")...)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func TestUncommittedCreateSerializeAndCommit(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3}, []string{"a", "b", "c"}, []bool{true, true, true})

	tx, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, record), lance.WithWriteMode(lance.WriteModeCreate))
	if err != nil {
		t.Fatalf("WriteUncommitted: %v", err)
	}
	if _, err := tx.ReadVersion(); err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if uuid, err := tx.UUID(); err != nil || uuid == "" {
		t.Fatalf("UUID = %q, %v; want generated UUID", uuid, err)
	}
	if _, err := lance.Open(ctx, uri); err == nil {
		t.Fatalf("Open succeeded before commit")
	} else if !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("Open before commit error = %v, want ErrNotFound", err)
	}

	encoded, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if err := tx.Close(); err != nil {
		t.Fatalf("Close transaction: %v", err)
	}
	parsed, err := lance.ParseTransaction(encoded)
	if err != nil {
		t.Fatalf("ParseTransaction: %v", err)
	}
	defer parsed.Close()

	committed, err := lance.CommitTransaction(ctx, uri, parsed, lance.WithCommitStableRowIDs(true))
	if err != nil {
		t.Fatalf("CommitTransaction: %v", err)
	}
	if ids := scanIDs(t, mustScan(t, committed)); len(ids) != 3 {
		t.Fatalf("committed ids = %v, want 3 rows", ids)
	}
	if err := committed.Close(); err != nil {
		t.Fatalf("Close committed dataset: %v", err)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after commit: %v", err)
	}
	defer reopened.Close()
	if rows, err := reopened.CountRows(ctx); err != nil || rows != 3 {
		t.Fatalf("rows = %d, %v; want 3", rows, err)
	}
}

func mustScan(t *testing.T, dataset *lance.Dataset) *lance.Scanner {
	t.Helper()
	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return scanner
}

func TestCommitModesAndBatch(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	baseURI := filepath.Join(t.TempDir(), "base.lance")
	schema := testSchema()

	base := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})
	baseDS, err := lance.Create(ctx, baseURI, makeReader(t, schema, base))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	baseVersion, err := baseDS.Version()
	if err != nil {
		baseDS.Close()
		t.Fatalf("Version: %v", err)
	}
	if err := baseDS.Close(); err != nil {
		t.Fatalf("Close base dataset: %v", err)
	}

	appendRecord := makeRecord(t, schema, []int64{3}, []string{"c"}, []bool{true})
	appendTx, err := lance.WriteUncommitted(ctx, baseURI, makeReader(t, schema, appendRecord), lance.WithWriteMode(lance.WriteModeAppend))
	if err != nil {
		t.Fatalf("WriteUncommitted append: %v", err)
	}
	if got, err := appendTx.ReadVersion(); err != nil || got != baseVersion {
		appendTx.Close()
		t.Fatalf("append read version = %d, %v; want %d", got, err, baseVersion)
	}
	appended, err := lance.CommitTransaction(ctx, baseURI, appendTx, lance.WithCommitMaxRetries(0), lance.WithCommitRetryTimeout(0), lance.WithCommitTimeout(lance.CommitTimeout{Mode: lance.CommitTimeoutDisabled}))
	if err != nil {
		appendTx.Close()
		t.Fatalf("CommitTransaction append: %v", err)
	}
	appendTx.Close()
	if rows, err := appended.CountRows(ctx); err != nil || rows != 3 {
		t.Fatalf("rows after append = %d, %v; want 3", rows, err)
	}
	if err := appended.Close(); err != nil {
		t.Fatalf("Close append dataset: %v", err)
	}

	overwriteRecord := makeRecord(t, schema, []int64{7}, []string{"g"}, []bool{true})
	overwriteTx, err := lance.WriteUncommitted(ctx, baseURI, makeReader(t, schema, overwriteRecord), lance.WithWriteMode(lance.WriteModeOverwrite))
	if err != nil {
		t.Fatalf("WriteUncommitted overwrite: %v", err)
	}
	overwritten, err := lance.CommitTransaction(ctx, baseURI, overwriteTx, lance.WithCommitStableRowIDs(false), lance.WithCommitV2ManifestPaths(false))
	if err != nil {
		overwriteTx.Close()
		t.Fatalf("CommitTransaction overwrite: %v", err)
	}
	overwriteTx.Close()
	if rows, err := overwritten.CountRows(ctx); err != nil || rows != 1 {
		t.Fatalf("rows after overwrite = %d, %v; want 1", rows, err)
	}
	if err := overwritten.Close(); err != nil {
		t.Fatalf("Close overwrite dataset: %v", err)
	}

	missingURI := filepath.Join(t.TempDir(), "created-by-append.lance")
	createByAppend := makeRecord(t, schema, []int64{11}, []string{"k"}, []bool{true})
	tx, err := lance.WriteUncommitted(ctx, missingURI, makeReader(t, schema, createByAppend), lance.WithWriteMode(lance.WriteModeAppend))
	if err != nil {
		t.Fatalf("WriteUncommitted append to missing dataset: %v", err)
	}
	created, err := lance.CommitTransaction(ctx, missingURI, tx)
	if err != nil {
		tx.Close()
		t.Fatalf("Commit append-to-create: %v", err)
	}
	tx.Close()
	if rows, err := created.CountRows(ctx); err != nil || rows != 1 {
		t.Fatalf("rows after append-to-create = %d, %v; want 1", rows, err)
	}
	if err := created.Close(); err != nil {
		t.Fatalf("Close created dataset: %v", err)
	}

	duplicateCreate := makeRecord(t, schema, []int64{12}, []string{"l"}, []bool{true})
	duplicateTx, err := lance.WriteUncommitted(ctx, missingURI, makeReader(t, schema, duplicateCreate), lance.WithWriteMode(lance.WriteModeCreate))
	if err == nil {
		duplicateTx.Close()
		t.Fatalf("WriteUncommitted duplicate create succeeded")
	} else if !errors.Is(err, lance.ErrAlreadyExists) {
		t.Fatalf("WriteUncommitted duplicate create error = %v, want ErrAlreadyExists", err)
	}
	unchanged, err := lance.Open(ctx, missingURI)
	if err != nil {
		t.Fatalf("Open after failed create: %v", err)
	}
	if rows, err := unchanged.CountRows(ctx); err != nil || rows != 1 {
		t.Fatalf("rows after failed create = %d, %v; want 1", rows, err)
	}
	unchanged.Close()

	batchURI := filepath.Join(t.TempDir(), "batch.lance")
	first := makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})
	batchBase, err := lance.Create(ctx, batchURI, makeReader(t, schema, first))
	if err != nil {
		t.Fatalf("Create batch base: %v", err)
	}
	batchVersion, err := batchBase.Version()
	if err != nil {
		batchBase.Close()
		t.Fatalf("Version batch base: %v", err)
	}
	batchBase.Close()

	txA, err := lance.WriteUncommitted(ctx, batchURI, makeReader(t, schema, makeRecord(t, schema, []int64{2}, []string{"b"}, []bool{true})), lance.WithWriteMode(lance.WriteModeAppend))
	if err != nil {
		t.Fatalf("WriteUncommitted batch A: %v", err)
	}
	txB, err := lance.WriteUncommitted(ctx, batchURI, makeReader(t, schema, makeRecord(t, schema, []int64{3}, []string{"c"}, []bool{true})), lance.WithWriteMode(lance.WriteModeAppend))
	if err != nil {
		t.Fatalf("WriteUncommitted batch B: %v", err)
	}
	for _, tx := range []*lance.Transaction{txA, txB} {
		if got, err := tx.ReadVersion(); err != nil || got != batchVersion {
			t.Fatalf("batch transaction read version = %d, %v; want %d", got, err, batchVersion)
		}
	}
	batchResult, merged, err := lance.CommitTransactionBatch(ctx, batchURI, []*lance.Transaction{txA, txB})
	if err != nil {
		t.Fatalf("CommitTransactionBatch: %v", err)
	}
	if rows, err := batchResult.CountRows(ctx); err != nil || rows != 3 {
		t.Fatalf("rows after batch = %d, %v; want 3", rows, err)
	}
	mergedOp, err := merged.Operation()
	if err != nil {
		t.Fatalf("merged Operation: %v", err)
	}
	if count, err := mergedOp.AppendFragmentCount(); err != nil || count != 2 {
		t.Fatalf("merged append fragments = %d, %v; want 2", count, err)
	}
	mergedOp.Close()
	merged.Close()
	batchResult.Close()
	txA.Close()
	txB.Close()
}

func TestFragmentsAcrossDatasetCloseAndStagedDelete(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "fragments.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"}, []bool{true, true, true, true})
	// Each fragment has two rows so deleting one row is a partial deletion.
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record), lance.WithWriteParams(lance.WriteParams{MaxRowsPerFile: 2, MaxRowsPerGroup: 2}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer dataset.Close()
	version, err := dataset.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	fragments, err := dataset.Fragments()
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	for _, fragment := range fragments {
		defer fragment.Close()
	}
	if len(fragments) != 2 {
		t.Fatalf("fragment count = %d, want 2", len(fragments))
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("Close dataset: %v", err)
	}
	byFirstRow := make(map[int64]*lance.Fragment)
	for _, fragment := range fragments {
		ids := fragmentTakeIDs(t, fragment, []uint32{0, 1})
		if len(ids) != 2 || (ids[0] != 1 && ids[0] != 3) || ids[1] != ids[0]+1 {
			t.Fatalf("fragment %d take = %v, want [1 2] or [3 4]", fragment.ID(), ids)
		}
		byFirstRow[ids[0]] = fragment
		if got := scanIDs(t, mustFragmentScan(t, fragment)); !reflect.DeepEqual(got, ids) {
			t.Fatalf("fragment %d scan = %v, take = %v", fragment.ID(), got, ids)
		}
		filtered, err := fragment.Scan(lance.WithAdditionalSQLFilter("id >= 2"))
		if err != nil {
			t.Fatalf("additional SQL filter: %v", err)
		}
		want := []int64{3, 4}
		if ids[0] == 1 {
			want = []int64{2}
		}
		if got := scanIDs(t, filtered); !reflect.DeepEqual(got, want) {
			t.Fatalf("filtered fragment = %v, want %v", got, want)
		}
		if count, err := fragment.CountRows(nil); err != nil || count != 2 {
			t.Fatalf("fragment count = %d, %v; want 2", count, err)
		}
		if count, err := fragment.CountRows(ptr("id >= 2")); err != nil || count != uint64(len(want)) {
			t.Fatalf("filtered count = %d, %v; want %d", count, err, len(want))
		}
	}
	partial := byFirstRow[1]
	if partial == nil || byFirstRow[3] == nil {
		t.Fatal("missing expected fragment row groups")
	}
	updated, err := partial.Delete(ctx, "id = 1")
	if err != nil || updated == nil {
		t.Fatalf("partial Delete = %v, %v; want a staged fragment", updated, err)
	}
	defer updated.Close()
	if count, err := partial.CountRows(nil); err != nil || count != 2 {
		t.Fatalf("source count after staged delete = %d, %v; want 2", count, err)
	}
	if count, err := updated.CountRows(nil); err != nil || count != 1 {
		t.Fatalf("staged count = %d, %v; want 1", count, err)
	}
	if got := scanIDs(t, mustFragmentScan(t, updated)); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("staged rows = %v, want [2]", got)
	}
	op, err := lance.NewDeleteOperation([]*lance.Fragment{updated}, nil, "id = 1")
	if err != nil {
		t.Fatalf("NewDeleteOperation: %v", err)
	}
	defer op.Close()
	tx, err := lance.NewTransaction(version, op)
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	defer tx.Close()
	partialCommit, err := lance.CommitTransaction(ctx, uri, tx)
	if err != nil {
		t.Fatalf("Commit partial delete: %v", err)
	}
	defer partialCommit.Close()
	if got := scanIDs(t, mustScan(t, partialCommit)); !reflect.DeepEqual(got, []int64{2, 3, 4}) {
		t.Fatalf("rows after partial commit = %v, want [2 3 4]", got)
	}
	partialCommit.Close()
	latest, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after partial delete: %v", err)
	}
	defer latest.Close()
	persisted, err := latest.OpenFragment(partial.ID())
	if err != nil {
		t.Fatalf("Open partial fragment: %v", err)
	}
	defer persisted.Close()
	if count, err := persisted.DeletedRows(); err != nil || count != 1 {
		t.Fatalf("persisted deleted rows = %d, %v; want 1", count, err)
	}
	if got := scanIDs(t, mustFragmentScan(t, persisted)); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("persisted partial rows = %v, want [2]", got)
	}
	allRowID := byFirstRow[3].ID()
	allRows, err := latest.OpenFragment(allRowID)
	if err != nil {
		t.Fatalf("Open remaining fragment: %v", err)
	}
	defer allRows.Close()
	if deleted, err := allRows.Delete(ctx, "id >= 3"); err != nil || deleted != nil {
		if deleted != nil {
			deleted.Close()
		}
		t.Fatalf("all-row Delete = %v, %v; want nil, nil", deleted, err)
	}
	latestVersion, err := latest.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	deleteOp, err := lance.NewDeleteOperation(nil, []uint64{allRowID}, "id >= 3")
	if err != nil {
		t.Fatalf("NewDeleteOperation all rows: %v", err)
	}
	defer deleteOp.Close()
	deleteTx, err := lance.NewTransaction(latestVersion, deleteOp)
	if err != nil {
		t.Fatalf("NewTransaction all rows: %v", err)
	}
	defer deleteTx.Close()
	afterAll, err := lance.CommitTransaction(ctx, uri, deleteTx)
	if err != nil {
		t.Fatalf("Commit all-row delete: %v", err)
	}
	afterAll.Close()
	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after all-row delete: %v", err)
	}
	defer reopened.Close()
	if got := scanIDs(t, mustScan(t, reopened)); !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("final rows = %v, want [2]", got)
	}
	deletedFragment, err := reopened.OpenFragment(allRowID)
	if deletedFragment != nil {
		deletedFragment.Close()
		t.Fatalf("fully deleted fragment %d is still present", allRowID)
	}
	if !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("OpenFragment(deleted) error = %v, want ErrNotFound", err)
	}
}

func mustFragmentScan(t *testing.T, fragment *lance.Fragment) *lance.Scanner {
	t.Helper()
	scanner, err := fragment.Scan()
	if err != nil {
		t.Fatalf("fragment Scan: %v", err)
	}
	return scanner
}

func ptr[T any](value T) *T { return &value }

func TestDetachedAppendInspectionAndCommit(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "detached.lance")
	schema := testSchema()
	base := makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, base))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	version, err := dataset.Version()
	if err != nil {
		dataset.Close()
		t.Fatalf("Version: %v", err)
	}
	idField, err := dataset.NativeSchema()
	if err != nil {
		dataset.Close()
		t.Fatalf("NativeSchema: %v", err)
	}
	fieldID, err := idField.FieldID("id")
	idField.Close()
	if err != nil {
		dataset.Close()
		t.Fatalf("FieldID: %v", err)
	}
	dataset.Close()

	appendRecord := makeRecord(t, schema, []int64{2}, []string{"b"}, []bool{true})
	tx, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, appendRecord), lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteTransactionProperties(map[string]string{"source": "uncommitted"}))
	if err != nil {
		t.Fatalf("WriteUncommitted: %v", err)
	}
	op, err := tx.Operation()
	if err != nil {
		t.Fatalf("Operation: %v", err)
	}
	if kind, err := op.Kind(); err != nil || kind != lance.OperationAppend {
		t.Fatalf("kind = %v, %v; want Append", kind, err)
	}
	count, err := op.AppendFragmentCount()
	if err != nil || count != 1 {
		t.Fatalf("append fragment count = %d, %v; want 1", count, err)
	}
	fragment, err := op.AppendFragment(0)
	if err != nil {
		t.Fatalf("AppendFragment: %v", err)
	}
	if fragment.IsBound() {
		t.Fatalf("uncommitted append fragment is bound")
	}
	// Staged fragment IDs are placeholders until commit assigns final IDs.
	if fragment.ID() != 0 {
		t.Fatalf("uncommitted append placeholder ID = %d, want 0", fragment.ID())
	}
	if metadata, err := fragment.MetadataJSON(); err != nil || !json.Valid(metadata) {
		t.Fatalf("MetadataJSON = %q, %v", metadata, err)
	}
	if _, err := fragment.Schema(); err != nil {
		t.Fatalf("detached fragment Schema: %v", err)
	}
	file, found, err := fragment.DataFileForField(fieldID)
	if err != nil || !found {
		t.Fatalf("DataFileForField(%d) = %v, %v; want found", fieldID, found, err)
	}
	if path, err := file.Path(); err != nil || path == "" {
		t.Fatalf("DataFile.Path = %q, %v", path, err)
	}
	file.Close()
	rebuiltOp, err := lance.NewAppendOperation(fragment)
	if err != nil {
		t.Fatalf("NewAppendOperation from detached fragment: %v", err)
	}
	for name, call := range map[string]func() error{
		"scan": func() error { _, err := fragment.Scan(); return err },
		"take": func() error { _, err := fragment.Take(ctx, nil); return err },
		"count": func() error {
			_, err := fragment.CountRows(nil)
			return err
		},
		"delete": func() error {
			_, err := fragment.Delete(ctx, "true")
			return err
		},
	} {
		requireErrorCode(t, call(), lance.CodeNotSupported)
		_ = name
	}
	fragment.Close()
	op.Close()
	tx.Close()

	rebuilt, err := lance.NewTransaction(version, rebuiltOp,
		lance.WithTransactionUUID("detached-rebuild"),
		lance.WithTransactionTag("rebuilt"),
		lance.WithTransactionProperties(map[string]string{"source": "detached"}))
	if err != nil {
		t.Fatalf("NewTransaction rebuilt: %v", err)
	}
	rebuiltOp.Close()
	committed, err := lance.CommitTransaction(ctx, uri, rebuilt)
	if err != nil {
		t.Fatalf("Commit rebuilt append: %v", err)
	}
	if rows, err := committed.CountRows(ctx); err != nil || rows != 2 {
		t.Fatalf("rows after rebuilt append = %d, %v; want 2", rows, err)
	}
	committedTx, err := committed.ReadTransaction()
	if err != nil {
		committed.Close()
		t.Fatalf("ReadTransaction committed: %v", err)
	}
	if uuid, err := committedTx.UUID(); err != nil || uuid != "detached-rebuild" {
		t.Fatalf("committed UUID = %q, %v", uuid, err)
	}
	if tag, present, err := committedTx.Tag(); err != nil || !present || tag != "rebuilt" {
		t.Fatalf("committed tag = %q, %v, %v", tag, present, err)
	}
	if props, present, err := committedTx.Properties(); err != nil || !present || props["source"] != "detached" {
		t.Fatalf("committed properties = %v, %v, %v", props, present, err)
	}
	committedTx.Close()
	rebuilt.Close()
	committed.Close()

	baseSnapshot, err := lance.Open(ctx, uri, lance.WithVersion(version))
	if err != nil {
		t.Fatalf("Open base snapshot: %v", err)
	}
	defer baseSnapshot.Close()
	if rows, err := baseSnapshot.CountRows(ctx); err != nil || rows != 1 {
		t.Fatalf("base snapshot rows = %d, %v; want 1", rows, err)
	}
}

func TestTransactionProtoOptionalFieldsAndFailures(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "proto.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	readTx, err := dataset.ReadTransaction()
	if err != nil {
		dataset.Close()
		t.Fatalf("ReadTransaction: %v", err)
	}
	if readTx == nil {
		dataset.Close()
		t.Fatalf("ReadTransaction returned nil for committed dataset")
	}
	version, err := readTx.ReadVersion()
	if err != nil || version != 0 {
		t.Fatalf("create transaction read version = %d, %v; want 0", version, err)
	}
	encoded, err := readTx.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary committed: %v", err)
	}
	parsed, err := lance.ParseTransaction(encoded)
	if err != nil {
		t.Fatalf("ParseTransaction committed: %v", err)
	}
	if parsedVersion, err := parsed.ReadVersion(); err != nil || parsedVersion != version {
		t.Fatalf("parsed version = %d, %v; want %d", parsedVersion, err, version)
	}
	parsed.Close()
	readTx.Close()
	// ReadVersion is the input snapshot; transaction lookup uses the
	// committed dataset version (1 for the initial Create).
	committedVersion, err := dataset.Version()
	if err != nil || committedVersion != 1 {
		t.Fatalf("created dataset version = %d, %v; want 1", committedVersion, err)
	}
	byVersion, err := dataset.ReadTransactionByVersion(committedVersion)
	if err != nil || byVersion == nil {
		dataset.Close()
		t.Fatalf("ReadTransactionByVersion(%d) = %v, %v", committedVersion, byVersion, err)
	}
	byVersion.Close()
	missing, err := dataset.ReadTransactionByVersion(version + 1000)
	if missing != nil || !errors.Is(err, lance.ErrNotFound) {
		dataset.Close()
		t.Fatalf("ReadTransactionByVersion(missing) = %v, %v; want ErrNotFound", missing, err)
	}
	dataset.Close()

	largeVersion := ^uint64(0) - 1
	reserveOp, err := lance.NewReserveFragmentsOperation(123)
	if err != nil {
		t.Fatalf("NewReserveFragmentsOperation: %v", err)
	}
	largeTx, err := lance.NewTransaction(largeVersion, reserveOp,
		lance.WithTransactionUUID(""),
		lance.WithTransactionTag(""),
		lance.WithTransactionProperties(nil))
	if err != nil {
		t.Fatalf("NewTransaction large: %v", err)
	}
	reserveOp.Close()
	if uuid, err := largeTx.UUID(); err != nil || uuid != "" {
		t.Fatalf("explicit empty UUID = %q, %v", uuid, err)
	}
	if tag, present, err := largeTx.Tag(); err != nil || !present || tag != "" {
		t.Fatalf("explicit empty tag = %q, %v, %v", tag, present, err)
	}
	if props, present, err := largeTx.Properties(); err != nil || !present || len(props) != 0 {
		t.Fatalf("explicit empty properties = %v, %v, %v", props, present, err)
	}
	largeEncoded, err := largeTx.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary large: %v", err)
	}
	largeParsed, err := lance.ParseTransaction(largeEncoded)
	if err != nil {
		t.Fatalf("ParseTransaction large: %v", err)
	}
	if got, err := largeParsed.ReadVersion(); err != nil || got != largeVersion {
		t.Fatalf("large parsed version = %d, %v; want %d", got, err, largeVersion)
	}
	largeOp, err := largeParsed.Operation()
	if err != nil {
		t.Fatalf("large Parsed Operation: %v", err)
	}
	if kind, err := largeOp.Kind(); err != nil || kind != lance.OperationReserveFragments {
		t.Fatalf("large operation kind = %v, %v", kind, err)
	}
	if count, err := largeOp.ReserveFragmentsCount(); err != nil || count != 123 {
		t.Fatalf("reserve fragments count = %d, %v; want 123", count, err)
	}
	if _, err := largeOp.AppendFragmentCount(); err == nil {
		t.Fatalf("AppendFragmentCount on reserve operation succeeded")
	} else {
		requireErrorCode(t, err, lance.CodeNotSupported)
	}
	largeOp.Close()
	largeParsed.Close()
	largeTx.Close()

	if _, err := lance.ParseTransaction([]byte{0xff, 0x00}); err == nil {
		t.Fatalf("ParseTransaction accepted invalid protobuf")
	} else {
		requireErrorCode(t, err, lance.CodeInvalidArgument)
	}
	unknown, err := lance.ParseTransaction(nil)
	if err != nil {
		t.Fatalf("ParseTransaction empty: %v", err)
	}
	unknownOp, err := unknown.Operation()
	if err != nil {
		unknown.Close()
		t.Fatalf("unknown Operation: %v", err)
	}
	unknownOp.Close()
	if _, err := unknown.MarshalBinary(); err == nil {
		unknown.Close()
		t.Fatalf("MarshalBinary unknown transaction succeeded")
	} else {
		requireErrorCode(t, err, lance.CodeNotSupported)
	}
	unknown.Close()

	detachedCreateURI := filepath.Join(t.TempDir(), "detached-create.lance")
	createRecord := makeRecord(t, schema, []int64{5}, []string{"e"}, []bool{true})
	createTx, err := lance.WriteUncommitted(ctx, detachedCreateURI, makeReader(t, schema, createRecord), lance.WithWriteMode(lance.WriteModeCreate))
	if err != nil {
		t.Fatalf("WriteUncommitted detached create: %v", err)
	}
	if _, err := lance.CommitTransaction(ctx, detachedCreateURI, createTx, lance.WithCommitV2ManifestPaths(true), lance.WithCommitDetached(true)); err == nil {
		createTx.Close()
		t.Fatalf("detached create commit succeeded")
	} else {
		requireErrorCode(t, err, lance.CodeNotSupported)
	}
	createTx.Close()
	if _, err := lance.Open(ctx, detachedCreateURI); !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("detached create left a dataset: %v", err)
	}
}

func TestDetachedCommitReadback(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "detached-commit.lance")
	schema := testSchema()
	base := makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, base))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	baseVersion, err := dataset.Version()
	if err != nil {
		dataset.Close()
		t.Fatalf("Version: %v", err)
	}
	dataset.Close()

	appendRecord := makeRecord(t, schema, []int64{2}, []string{"b"}, []bool{true})
	tx, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, appendRecord), lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteTransactionProperties(map[string]string{"detached": "yes"}))
	if err != nil {
		t.Fatalf("WriteUncommitted: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, uri, tx, lance.WithCommitDetached(true), lance.WithCommitV2ManifestPaths(true))
	if err != nil {
		t.Fatalf("Commit detached: %v", err)
	}
	detachedVersion, err := committed.Version()
	if err != nil {
		committed.Close()
		t.Fatalf("detached Version: %v", err)
	}
	if detachedVersion&0x8000000000000000 == 0 {
		committed.Close()
		t.Fatalf("detached version = %d, want high bit set", detachedVersion)
	}
	if rows, err := committed.CountRows(ctx); err != nil || rows != 2 {
		t.Fatalf("detached rows = %d, %v; want 2", rows, err)
	}
	detachedTx, err := committed.ReadTransaction()
	if err != nil {
		committed.Close()
		t.Fatalf("detached ReadTransaction: %v", err)
	}
	if props, present, err := detachedTx.Properties(); err != nil || !present || props["detached"] != "yes" {
		t.Fatalf("detached properties = %v, %v, %v", props, present, err)
	}
	detachedTx.Close()
	committed.Close()
	tx.Close()

	latest, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open latest: %v", err)
	}
	defer latest.Close()
	if version, err := latest.Version(); err != nil || version != baseVersion {
		t.Fatalf("latest version = %d, %v; want %d", version, err, baseVersion)
	}
	if rows, err := latest.CountRows(ctx); err != nil || rows != 1 {
		t.Fatalf("latest rows = %d, %v; want 1 after detached commit", rows, err)
	}
}
