package lance_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
)

func TestCreateFragmentPublishesThroughAppend(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "dataset.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3}, []string{"a", "b", "c"}, []bool{true, true, true})
	baseRecord := makeRecord(t, schema, []int64{0}, []string{"base"}, []bool{true})
	base, err := lance.Create(ctx, uri, makeReader(t, schema, baseRecord))
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	baseVersion, err := base.Version()
	if err != nil {
		base.Close()
		t.Fatalf("base Version: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("Close base: %v", err)
	}

	session, err := lance.NewSession(0, 0)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	fragment, err := lance.CreateFragment(ctx, uri, 7, makeReader(t, schema, record), lance.WithWriteSession(session))
	if err != nil {
		t.Fatalf("CreateFragment: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close session: %v", err)
	}

	if got := fragment.ID(); got != 7 {
		t.Fatalf("fragment ID = %d, want 7", got)
	}
	if fragment.IsBound() {
		t.Fatal("new fragment is bound to a dataset, want detached metadata")
	}
	physicalRows, err := fragment.PhysicalRows()
	if err != nil {
		t.Fatalf("PhysicalRows: %v", err)
	}
	if physicalRows != 3 {
		t.Fatalf("physical rows = %d, want 3", physicalRows)
	}

	before, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open before commit: %v", err)
	}
	beforeRows, err := before.CountRows(ctx)
	if err != nil {
		before.Close()
		t.Fatalf("CountRows before commit: %v", err)
	}
	if beforeRows != 1 {
		before.Close()
		t.Fatalf("rows before commit = %d, want 1", beforeRows)
	}
	if err := before.Close(); err != nil {
		t.Fatalf("Close before dataset: %v", err)
	}

	operation, err := lance.NewAppendOperation(fragment)
	if err != nil {
		t.Fatalf("NewAppendOperation: %v", err)
	}
	if err := fragment.Close(); err != nil {
		t.Fatalf("Close fragment: %v", err)
	}
	transaction, err := lance.NewTransaction(baseVersion, operation)
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, uri, transaction)
	if err != nil {
		t.Fatalf("CommitTransaction: %v", err)
	}
	version, err := committed.Version()
	if err != nil {
		committed.Close()
		t.Fatalf("Version: %v", err)
	}
	if version != baseVersion+1 {
		committed.Close()
		t.Fatalf("version = %d, want %d", version, baseVersion+1)
	}
	if err := committed.Close(); err != nil {
		t.Fatalf("Close committed dataset: %v", err)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after commit: %v", err)
	}
	defer reopened.Close()
	rows, err := reopened.CountRows(ctx)
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if rows != 4 {
		t.Fatalf("rows = %d, want 4", rows)
	}
	if got := scanSortedInt64(t, reopened, "id"); !equalInt64(got, []int64{0, 1, 2, 3}) {
		t.Fatalf("scanned ids = %v, want [0 1 2 3]", got)
	}
	ids, err := reopened.FragmentIDs()
	if err != nil {
		t.Fatalf("FragmentIDs: %v", err)
	}
	found := false
	for _, id := range ids {
		if id == 7 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fragment IDs = %v, want the explicit ID 7", ids)
	}
}

func TestWriteFragmentsUsesNativeSplitting(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "dataset.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3, 4, 5}, []string{"a", "b", "c", "d", "e"}, []bool{true, true, true, true, true})
	baseRecord := makeRecord(t, schema, []int64{0}, []string{"base"}, []bool{true})
	base, err := lance.Create(ctx, uri, makeReader(t, schema, baseRecord))
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	baseVersion, err := base.Version()
	if err != nil {
		base.Close()
		t.Fatalf("base Version: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("Close base: %v", err)
	}

	fragments, err := lance.WriteFragments(ctx, uri, makeReader(t, schema, record), lance.WithWriteParams(lance.WriteParams{
		MaxRowsPerFile:  2,
		MaxRowsPerGroup: 1,
	}))
	if err != nil {
		t.Fatalf("WriteFragments: %v", err)
	}
	if len(fragments) != 3 {
		t.Fatalf("fragments = %d, want 3", len(fragments))
	}
	wantRows := []uint64{2, 2, 1}
	for i, fragment := range fragments {
		if fragment.IsBound() {
			t.Fatalf("fragment %d is bound, want detached metadata", i)
		}
		rows, err := fragment.PhysicalRows()
		if err != nil {
			t.Fatalf("fragment %d PhysicalRows: %v", i, err)
		}
		if rows != wantRows[i] {
			t.Fatalf("fragment %d physical rows = %d, want %d", i, rows, wantRows[i])
		}
	}

	operation, err := lance.NewAppendOperation(fragments...)
	if err != nil {
		t.Fatalf("NewAppendOperation: %v", err)
	}
	for i, fragment := range fragments {
		if err := fragment.Close(); err != nil {
			t.Fatalf("Close fragment %d: %v", i, err)
		}
	}
	transaction, err := lance.NewTransaction(baseVersion, operation)
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, uri, transaction)
	if err != nil {
		t.Fatalf("CommitTransaction: %v", err)
	}
	committed.Close()

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after commit: %v", err)
	}
	defer reopened.Close()
	rows, err := reopened.CountRows(ctx)
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if rows != 6 {
		t.Fatalf("rows = %d, want 6", rows)
	}
	if got := scanSortedInt64(t, reopened, "id"); !equalInt64(got, []int64{0, 1, 2, 3, 4, 5}) {
		t.Fatalf("scanned ids = %v, want [0 1 2 3 4 5]", got)
	}
}

func TestWriteFragmentsAggregatesIndependentCalls(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "dataset.lance")
	schema := testSchema()
	baseRecord := makeRecord(t, schema, []int64{0}, []string{"base"}, []bool{true})
	base, err := lance.Create(ctx, uri, makeReader(t, schema, baseRecord))
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	baseVersion, err := base.Version()
	if err != nil {
		base.Close()
		t.Fatalf("base Version: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("Close base: %v", err)
	}

	firstRecord := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})
	secondRecord := makeRecord(t, schema, []int64{3, 4}, []string{"c", "d"}, []bool{true, true})
	first, err := lance.WriteFragments(ctx, uri, makeReader(t, schema, firstRecord), lance.WithWriteParams(lance.WriteParams{MaxRowsPerFile: 1}))
	if err != nil {
		t.Fatalf("first WriteFragments: %v", err)
	}
	second, err := lance.WriteFragments(ctx, uri, makeReader(t, schema, secondRecord), lance.WithWriteParams(lance.WriteParams{MaxRowsPerFile: 1}))
	if err != nil {
		t.Fatalf("second WriteFragments: %v", err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("fragment counts = %d and %d, want 2 and 2", len(first), len(second))
	}

	all := append(first, second...)
	operation, err := lance.NewAppendOperation(all...)
	if err != nil {
		t.Fatalf("NewAppendOperation: %v", err)
	}
	for i, fragment := range all {
		if err := fragment.Close(); err != nil {
			t.Fatalf("Close fragment %d: %v", i, err)
		}
	}
	transaction, err := lance.NewTransaction(baseVersion, operation)
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, uri, transaction)
	if err != nil {
		t.Fatalf("CommitTransaction: %v", err)
	}
	committed.Close()

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after commit: %v", err)
	}
	defer reopened.Close()
	rows, err := reopened.CountRows(ctx)
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if rows != 5 {
		t.Fatalf("rows = %d, want 5", rows)
	}
	if got := scanSortedInt64(t, reopened, "id"); !equalInt64(got, []int64{0, 1, 2, 3, 4}) {
		t.Fatalf("scanned ids = %v, want [0 1 2 3 4]", got)
	}
}

func TestCreateFragmentPreservesNativeFieldIDs(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	sourceURI := filepath.Join(t.TempDir(), "source.lance")

	sourceSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "name", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "extra", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
	sourceRecord := buildRecord(t, sourceSchema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
		builder.Field(1).(*array.Int64Builder).AppendValues([]int64{10, 20}, nil)
		builder.Field(2).(*array.Int64Builder).AppendValues([]int64{100, 200}, nil)
	})
	source, err := lance.Create(ctx, sourceURI, makeReader(t, sourceSchema, sourceRecord))
	if err != nil {
		t.Fatalf("Create source: %v", err)
	}
	sourceVersion, err := source.Version()
	if err != nil {
		source.Close()
		t.Fatalf("source Version: %v", err)
	}

	sourceNativeSchema, err := source.NativeSchema()
	if err != nil {
		source.Close()
		t.Fatalf("NativeSchema: %v", err)
	}
	projected, err := sourceNativeSchema.Project("id", "extra")
	if err != nil {
		sourceNativeSchema.Close()
		source.Close()
		t.Fatalf("Project: %v", err)
	}
	if err := sourceNativeSchema.Close(); err != nil {
		projected.Close()
		source.Close()
		t.Fatalf("Close source schema: %v", err)
	}
	projectedArrow, err := projected.Arrow()
	if err != nil {
		projected.Close()
		source.Close()
		t.Fatalf("Arrow: %v", err)
	}
	if projectedArrow.NumFields() != 2 || projectedArrow.Field(0).Name != "id" || projectedArrow.Field(1).Name != "extra" {
		projected.Close()
		source.Close()
		t.Fatalf("projected schema = %v, want [id extra]", projectedArrow)
	}

	projectOperation, err := lance.NewProjectOperation(projected, true)
	if err != nil {
		projected.Close()
		source.Close()
		t.Fatalf("NewProjectOperation: %v", err)
	}
	projectTransaction, err := lance.NewTransaction(sourceVersion, projectOperation)
	if err != nil {
		projectOperation.Close()
		projected.Close()
		source.Close()
		t.Fatalf("NewTransaction project: %v", err)
	}
	projectOperation.Close()
	projectedDataset, err := lance.CommitTransaction(ctx, sourceURI, projectTransaction)
	if err != nil {
		projected.Close()
		source.Close()
		t.Fatalf("Commit project: %v", err)
	}
	projectedVersion, err := projectedDataset.Version()
	if err != nil {
		projectedDataset.Close()
		projected.Close()
		source.Close()
		t.Fatalf("projected Version: %v", err)
	}
	if err := projectedDataset.Close(); err != nil {
		projected.Close()
		source.Close()
		t.Fatalf("Close projected dataset: %v", err)
	}
	if err := source.Close(); err != nil {
		projected.Close()
		t.Fatalf("Close source dataset: %v", err)
	}

	projectedRecord := buildRecord(t, projectedArrow, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{10, 20}, nil)
		builder.Field(1).(*array.Int64Builder).AppendValues([]int64{300, 400}, nil)
	})
	fragment, err := lance.CreateFragment(ctx, sourceURI, 11, makeReader(t, projectedArrow, projectedRecord), lance.WithFragmentSchema(projected))
	if err != nil {
		projected.Close()
		t.Fatalf("CreateFragment: %v", err)
	}
	if err := projected.Close(); err != nil {
		t.Fatalf("Close projected schema: %v", err)
	}

	fragmentSchema, err := fragment.NativeSchema()
	if err != nil {
		t.Fatalf("fragment NativeSchema: %v", err)
	}
	id, err := fragmentSchema.FieldID("id")
	if err != nil {
		fragmentSchema.Close()
		t.Fatalf("FieldID(id): %v", err)
	}
	extra, err := fragmentSchema.FieldID("extra")
	if err != nil {
		fragmentSchema.Close()
		t.Fatalf("FieldID(extra): %v", err)
	}
	if err := fragmentSchema.Close(); err != nil {
		t.Fatalf("Close fragment schema: %v", err)
	}
	if id != 0 || extra != 2 {
		t.Fatalf("fragment field IDs = (%d, %d), want (0, 2)", id, extra)
	}

	operation, err := lance.NewAppendOperation(fragment)
	if err != nil {
		t.Fatalf("NewAppendOperation: %v", err)
	}
	if err := fragment.Close(); err != nil {
		t.Fatalf("Close fragment: %v", err)
	}
	transaction, err := lance.NewTransaction(projectedVersion, operation)
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, sourceURI, transaction)
	if err != nil {
		t.Fatalf("CommitTransaction: %v", err)
	}
	committed.Close()

	reopened, err := lance.Open(ctx, sourceURI)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}
	defer reopened.Close()
	reopenedSchema, err := reopened.NativeSchema()
	if err != nil {
		t.Fatalf("target NativeSchema: %v", err)
	}
	defer reopenedSchema.Close()
	id, err = reopenedSchema.FieldID("id")
	if err != nil {
		t.Fatalf("target FieldID(id): %v", err)
	}
	extra, err = reopenedSchema.FieldID("extra")
	if err != nil {
		t.Fatalf("target FieldID(extra): %v", err)
	}
	if id != 0 || extra != 2 {
		t.Fatalf("target field IDs = (%d, %d), want (0, 2)", id, extra)
	}
	if got := scanSortedInt64(t, reopened, "id"); !equalInt64(got, []int64{1, 2, 10, 20}) {
		t.Fatalf("scanned ids = %v, want [1 2 10 20]", got)
	}
}

func TestFragmentCreateStreamOwnership(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})

	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{record})
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	fragment, err := lance.CreateFragment(ctx, filepath.Join(t.TempDir(), "success.lance"), 1, reader)
	if err != nil {
		reader.Release()
		t.Fatalf("CreateFragment: %v", err)
	}
	if reader.Next() {
		reader.Release()
		fragment.Close()
		t.Fatal("reader returned a batch after native consumed its stream")
	}
	reader.Release()
	if err := fragment.Close(); err != nil {
		t.Fatalf("Close fragment: %v", err)
	}

	blockedRoot := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(blockedRoot, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	errorReader, err := array.NewRecordReader(schema, []arrow.RecordBatch{record})
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	_, err = lance.CreateFragment(ctx, filepath.Join(blockedRoot, "failure.lance"), 2, errorReader)
	if err == nil {
		errorReader.Release()
		t.Fatal("CreateFragment under a regular file succeeded, want native error")
	}
	if errorReader.Next() {
		errorReader.Release()
		t.Fatal("reader returned a batch after a native error consumed its stream")
	}
	errorReader.Release()
}

func TestFragmentCreateEmptyAndNativeErrors(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dir := t.TempDir()
	schema := testSchema()

	emptyReader, err := array.NewRecordReader(schema, nil)
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	defer emptyReader.Release()
	_, err = lance.CreateFragment(ctx, filepath.Join(dir, "empty.lance"), 1, emptyReader)
	if err == nil {
		t.Fatal("CreateFragment with empty input succeeded")
	}
	if !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CreateFragment empty error = %v, want ErrInvalidArgument", err)
	}
	requireLanceErrorOp(t, err, "create fragment")
	if !strings.Contains(err.Error(), "Input data was empty.") {
		t.Fatalf("CreateFragment empty error = %v, want native empty-input message", err)
	}

	propertyReader, err := array.NewRecordReader(schema, []arrow.RecordBatch{makeRecord(t, schema, []int64{9}, []string{"props"}, []bool{true})})
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	_, err = lance.CreateFragment(ctx, filepath.Join(dir, "properties.lance"), 1, propertyReader,
		lance.WithWriteTransactionProperties(map[string]string{"source": "test"}))
	if !errors.Is(err, lance.ErrInvalidArgument) {
		propertyReader.Release()
		t.Fatalf("CreateFragment with transaction properties error = %v, want ErrInvalidArgument", err)
	}
	propertyReader.Release()

	manyEmptyReader, err := array.NewRecordReader(schema, nil)
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	defer manyEmptyReader.Release()
	fragments, err := lance.WriteFragments(ctx, filepath.Join(dir, "many-empty.lance"), manyEmptyReader)
	if err != nil {
		t.Fatalf("WriteFragments with empty input: %v", err)
	}
	if len(fragments) != 0 {
		t.Fatalf("WriteFragments empty result = %d fragments, want 0", len(fragments))
	}

	targetURI := filepath.Join(dir, "target.lance")
	baseRecord := makeRecord(t, schema, []int64{0}, []string{"base"}, []bool{true})
	base, err := lance.Create(ctx, targetURI, makeReader(t, schema, baseRecord))
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	baseVersion, err := base.Version()
	if err != nil {
		base.Close()
		t.Fatalf("base Version: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("Close base: %v", err)
	}

	firstFragment, err := lance.CreateFragment(ctx, targetURI, 3, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"first"}, []bool{true})))
	if err != nil {
		t.Fatalf("CreateFragment first explicit ID: %v", err)
	}
	firstOperation, err := lance.NewAppendOperation(firstFragment)
	if err != nil {
		firstFragment.Close()
		t.Fatalf("NewAppendOperation first: %v", err)
	}
	if err := firstFragment.Close(); err != nil {
		t.Fatalf("Close first fragment: %v", err)
	}
	firstTransaction, err := lance.NewTransaction(baseVersion, firstOperation)
	if err != nil {
		t.Fatalf("NewTransaction first: %v", err)
	}
	firstCommitted, err := lance.CommitTransaction(ctx, targetURI, firstTransaction)
	if err != nil {
		t.Fatalf("CommitTransaction first: %v", err)
	}
	if err := firstCommitted.Close(); err != nil {
		t.Fatalf("Close first committed dataset: %v", err)
	}

	target, err := lance.Open(ctx, targetURI)
	if err != nil {
		t.Fatalf("Open target: %v", err)
	}
	version, err := target.Version()
	if err != nil {
		target.Close()
		t.Fatalf("Version: %v", err)
	}
	duplicate, err := lance.CreateFragment(ctx, targetURI, 3, makeReader(t, schema, makeRecord(t, schema, []int64{2}, []string{"duplicate"}, []bool{true})))
	if err != nil {
		target.Close()
		t.Fatalf("CreateFragment duplicate ID: %v", err)
	}
	operation, err := lance.NewAppendOperation(duplicate)
	if err != nil {
		target.Close()
		t.Fatalf("NewAppendOperation: %v", err)
	}
	if err := duplicate.Close(); err != nil {
		target.Close()
		t.Fatalf("Close fragment: %v", err)
	}
	transaction, err := lance.NewTransaction(version, operation)
	if err != nil {
		target.Close()
		t.Fatalf("NewTransaction: %v", err)
	}
	if _, err := lance.CommitTransaction(ctx, targetURI, transaction); err == nil {
		transaction.Close()
		target.Close()
		t.Fatal("CommitTransaction accepted a duplicate fragment ID")
	} else {
		requireLanceErrorOp(t, err, "commit")
	}
	transaction.Close()

	_, err = target.CreateFragmentFromFile(ctx, "missing.lance", 9, nil)
	if err == nil {
		target.Close()
		t.Fatal("CreateFragmentFromFile with a missing file succeeded")
	}
	requireLanceErrorOp(t, err, "create fragment from file")
	if err := target.Close(); err != nil {
		t.Fatalf("Close target: %v", err)
	}
}

func TestCreateFragmentRejectsSchemaMismatch(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	sourceURI := filepath.Join(t.TempDir(), "schema-source.lance")
	source, err := lance.Create(ctx, sourceURI, makeReader(t, testSchema(), makeRecord(t, testSchema(), []int64{1}, []string{"one"}, []bool{true})))
	if err != nil {
		t.Fatalf("Create source: %v", err)
	}
	nativeSchema, err := source.NativeSchema()
	if err != nil {
		source.Close()
		t.Fatalf("NativeSchema: %v", err)
	}
	if err := source.Close(); err != nil {
		nativeSchema.Close()
		t.Fatalf("Close source: %v", err)
	}
	defer nativeSchema.Close()

	oneFieldSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
	oneFieldRecord := buildRecord(t, oneFieldSchema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1}, []bool{true})
	})
	reader, err := array.NewRecordReader(oneFieldSchema, []arrow.RecordBatch{oneFieldRecord})
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	defer reader.Release()

	_, err = lance.CreateFragment(ctx, filepath.Join(t.TempDir(), "mismatch.lance"), 1, reader, lance.WithFragmentSchema(nativeSchema))
	if err == nil {
		t.Fatal("CreateFragment accepted a reader schema that does not match the native schema")
	}
	requireLanceErrorOp(t, err, "create fragment")
}

func TestCreateFragmentFromFile(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	sourceURI := filepath.Join(t.TempDir(), "source.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2, 3}, []string{"a", "b", "c"}, []bool{true, true, true})

	source, err := lance.Create(ctx, sourceURI, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create source: %v", err)
	}
	version, err := source.Version()
	if err != nil {
		source.Close()
		t.Fatalf("Version: %v", err)
	}
	fragments, err := source.Fragments()
	if err != nil {
		source.Close()
		t.Fatalf("Fragments: %v", err)
	}
	if len(fragments) != 1 {
		for _, fragment := range fragments {
			fragment.Close()
		}
		source.Close()
		t.Fatalf("source fragments = %d, want 1", len(fragments))
	}
	dataFile, err := fragments[0].DataFileAt(0)
	if err != nil {
		for _, fragment := range fragments {
			fragment.Close()
		}
		source.Close()
		t.Fatalf("DataFileAt: %v", err)
	}
	sourcePath, err := dataFile.Path()
	if err != nil {
		dataFile.Close()
		for _, fragment := range fragments {
			fragment.Close()
		}
		source.Close()
		t.Fatalf("DataFile.Path: %v", err)
	}
	copiedName := "copied" + filepath.Ext(sourcePath)
	copyFile(t, filepath.Join(sourceURI, "data", sourcePath), filepath.Join(sourceURI, "data", copiedName))
	if err := dataFile.Close(); err != nil {
		t.Fatalf("Close data file: %v", err)
	}
	for _, fragment := range fragments {
		if err := fragment.Close(); err != nil {
			t.Fatalf("Close source fragment: %v", err)
		}
	}

	fragment, err := source.CreateFragmentFromFile(ctx, copiedName, 42, nil)
	if err != nil {
		source.Close()
		t.Fatalf("CreateFragmentFromFile: %v", err)
	}
	explicitRows := uint64(3)
	explicitFragment, err := source.CreateFragmentFromFile(ctx, copiedName, 43, &explicitRows)
	if err != nil {
		source.Close()
		t.Fatalf("CreateFragmentFromFile with explicit physical rows: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("Close source dataset: %v", err)
	}
	if got := fragment.ID(); got != 42 {
		t.Fatalf("fragment ID = %d, want 42", got)
	}
	if fragment.IsBound() {
		t.Fatal("file fragment is bound, want detached metadata")
	}
	physicalRows, err := fragment.PhysicalRows()
	if err != nil {
		t.Fatalf("PhysicalRows: %v", err)
	}
	if physicalRows != 3 {
		t.Fatalf("physical rows = %d, want 3", physicalRows)
	}
	explicitPhysicalRows, err := explicitFragment.PhysicalRows()
	if err != nil {
		explicitFragment.Close()
		t.Fatalf("explicit PhysicalRows: %v", err)
	}
	if explicitPhysicalRows != explicitRows {
		explicitFragment.Close()
		t.Fatalf("explicit physical rows = %d, want %d", explicitPhysicalRows, explicitRows)
	}
	if err := explicitFragment.Close(); err != nil {
		t.Fatalf("Close explicit fragment: %v", err)
	}

	operation, err := lance.NewAppendOperation(fragment)
	if err != nil {
		t.Fatalf("NewAppendOperation: %v", err)
	}
	if err := fragment.Close(); err != nil {
		t.Fatalf("Close fragment: %v", err)
	}
	transaction, err := lance.NewTransaction(version, operation)
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, sourceURI, transaction)
	if err != nil {
		t.Fatalf("CommitTransaction: %v", err)
	}
	committed.Close()

	reopened, err := lance.Open(ctx, sourceURI)
	if err != nil {
		t.Fatalf("Open after commit: %v", err)
	}
	defer reopened.Close()
	rows, err := reopened.CountRows(ctx)
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if rows != 6 {
		t.Fatalf("rows = %d, want 6", rows)
	}
	if got := scanSortedInt64(t, reopened, "id"); !equalInt64(got, []int64{1, 1, 2, 2, 3, 3}) {
		t.Fatalf("scanned ids = %v, want [1 1 2 2 3 3]", got)
	}
}

func TestCreateFragmentFromFileRejectsSchemaMismatch(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	sourceURI := filepath.Join(t.TempDir(), "source.lance")
	targetURI := filepath.Join(t.TempDir(), "target.lance")
	sourceSchema := testSchema()
	sourceRecord := makeRecord(t, sourceSchema, []int64{1}, []string{"a"}, []bool{true})
	source, err := lance.Create(ctx, sourceURI, makeReader(t, sourceSchema, sourceRecord))
	if err != nil {
		t.Fatalf("Create source: %v", err)
	}
	sourceFragments, err := source.Fragments()
	if err != nil {
		source.Close()
		t.Fatalf("Fragments: %v", err)
	}
	dataFile, err := sourceFragments[0].DataFileAt(0)
	if err != nil {
		source.Close()
		t.Fatalf("DataFileAt: %v", err)
	}
	sourcePath, err := dataFile.Path()
	if err != nil {
		dataFile.Close()
		for _, fragment := range sourceFragments {
			fragment.Close()
		}
		source.Close()
		t.Fatalf("DataFile.Path: %v", err)
	}
	dataFile.Close()
	for _, fragment := range sourceFragments {
		fragment.Close()
	}
	source.Close()

	targetSchema := arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	targetRecord := buildRecord(t, targetSchema, func(builder *array.RecordBuilder) {
		builder.Field(0).(*array.StringBuilder).Append("not-an-integer")
	})
	target, err := lance.Create(ctx, targetURI, makeReader(t, targetSchema, targetRecord))
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}
	defer target.Close()
	copiedName := "mismatched" + filepath.Ext(sourcePath)
	copyFile(t, filepath.Join(sourceURI, "data", sourcePath), filepath.Join(targetURI, "data", copiedName))

	_, err = target.CreateFragmentFromFile(ctx, copiedName, 5, nil)
	if err == nil {
		t.Fatal("CreateFragmentFromFile accepted a file with an incompatible schema")
	}
	requireLanceErrorOp(t, err, "create fragment from file")
}

func buildRecord(t *testing.T, schema *arrow.Schema, fill func(*array.RecordBuilder)) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	fill(builder)
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return record
}

func scanSortedInt64(t *testing.T, dataset *lance.Dataset, field string) []int64 {
	t.Helper()
	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var values []int64
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		values = append(values, collectInt64(t, batch, field)...)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}

func requireLanceErrorOp(t *testing.T, err error, op string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want lance error with op %q", op)
	}
	var lanceErr *lance.Error
	if !errors.As(err, &lanceErr) {
		t.Fatalf("error = %T %v, want *lance.Error", err, err)
	}
	if lanceErr.Op != op {
		t.Fatalf("error op = %q, want %q (error: %v)", lanceErr.Op, op, err)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", dst, err)
	}
}
