package lance_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
)

func TestFragmentAddColumnsVariantsPublish(t *testing.T) {
	t.Run("sql", func(t *testing.T) {
		dataset, uri, _ := newMutationDataset(t)
		ctx := context.Background()
		baseVersion := mutationVersion(t, dataset)
		fragment := mustOpenFragment(t, dataset, 0)

		updated, schema, err := fragment.AddColumnsSQL(ctx, []lance.SQLColumn{
			{Name: "sql_value", Expression: "id * 2 + 1"},
		}, 2)
		if err != nil {
			t.Fatalf("AddColumnsSQL: %v", err)
		}
		if updated.IsBound() {
			t.Fatal("SQL output fragment is bound, want detached metadata")
		}
		requireFieldID(t, schema, "sql_value", 2)
		if got := detachedFragmentPhysicalRows(t, updated); got != 4 {
			t.Fatalf("staged sql physical rows = %d, want 4", got)
		}
		requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
		if got := mutationVersion(t, dataset); got != baseVersion {
			t.Fatalf("source version = %d, want %d", got, baseVersion)
		}

		commitMerge(t, ctx, uri, baseVersion, schema, []*lance.Fragment{updated})
		updated.Close()
		schema.Close()
		requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

		reopened := openDataset(t, ctx, uri)
		requireMutationRows(t, reopened, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
		if got := mustScanInt64(t, reopened, "sql_value"); !equalInt64(got, []int64{3, 5, 7, 9}) {
			t.Fatalf("published sql values = %v, want [3 5 7 9]", got)
		}
		reopenedSchema := mustNativeSchema(t, reopened)
		requireFieldID(t, reopenedSchema, "sql_value", 2)
		reopenedSchema.Close()
	})

	t.Run("nulls", func(t *testing.T) {
		dataset, uri, _ := newMutationDataset(t)
		ctx := context.Background()
		baseVersion := mutationVersion(t, dataset)
		fragment := mustOpenFragment(t, dataset, 0)
		nulls := arrow.NewSchema([]arrow.Field{
			{Name: "null_value", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
		}, nil)

		updated, schema, err := fragment.AddColumnsNulls(ctx, nulls)
		if err != nil {
			t.Fatalf("AddColumnsNulls: %v", err)
		}
		requireFieldID(t, schema, "null_value", 2)
		if got := detachedFragmentPhysicalRows(t, updated); got != 4 {
			t.Fatalf("staged null physical rows = %d, want 4", got)
		}

		commitMerge(t, ctx, uri, baseVersion, schema, []*lance.Fragment{updated})
		updated.Close()
		schema.Close()

		reopened := openDataset(t, ctx, uri)
		reopenedSchema := mustNativeSchema(t, reopened)
		requireFieldID(t, reopenedSchema, "null_value", 2)
		reopenedSchema.Close()
		column := findColumn(t, mustFirstBatch(t, reopened), "null_value").(*array.Int32)
		if column.Len() != 4 || column.NullN() != 4 {
			t.Fatalf("published null column len/nulls = %d/%d, want 4/4", column.Len(), column.NullN())
		}
	})

	t.Run("stream", func(t *testing.T) {
		dataset, uri, _ := newMutationDataset(t)
		ctx := context.Background()
		baseVersion := mutationVersion(t, dataset)
		fragment := mustOpenFragment(t, dataset, 0)
		reader := &releaseCountingReader{RecordReader: oneColumnInt32Reader(t, "stream_value", []int32{7, 8, 9, 10})}

		updated, schema, err := fragment.AddColumnsStream(ctx, reader)
		if err != nil {
			t.Fatalf("AddColumnsStream: %v", err)
		}
		if reader.releases != 1 {
			t.Fatalf("stream released reader %d times, want 1", reader.releases)
		}
		requireFieldID(t, schema, "stream_value", 2)

		commitMerge(t, ctx, uri, baseVersion, schema, []*lance.Fragment{updated})
		updated.Close()
		schema.Close()
		reopened := openDataset(t, ctx, uri)
		scanner, err := reopened.Scan()
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		defer scanner.Close()
		var got []int32
		for batch, err := range scanner.Records(ctx) {
			if err != nil {
				t.Fatalf("Records: %v", err)
			}
			got = append(got, collectInt32(t, batch, "stream_value")...)
		}
		if !slices.Equal(got, []int32{7, 8, 9, 10}) {
			t.Fatalf("published stream values = %v, want [7 8 9 10]", got)
		}
	})

	t.Run("batch udf", func(t *testing.T) {
		dataset, uri, _ := newMutationDataset(t)
		ctx := context.Background()
		baseVersion := mutationVersion(t, dataset)
		fragment := mustOpenFragment(t, dataset, 0)
		outputSchema := arrow.NewSchema([]arrow.Field{
			{Name: "udf_value", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		}, nil)
		mapper := func(input arrow.RecordBatch) (arrow.RecordBatch, error) {
			return array.NewRecordBatch(outputSchema, []arrow.Array{input.Column(0)}, input.NumRows()), nil
		}

		updated, schema, err := fragment.AddColumnsBatchUDF(ctx, lance.BatchUDF{
			Mapper:       mapper,
			OutputSchema: outputSchema,
			ReadColumns:  []string{"id"},
		})
		if err != nil {
			t.Fatalf("AddColumnsBatchUDF: %v", err)
		}
		requireFieldID(t, schema, "udf_value", 2)

		commitMerge(t, ctx, uri, baseVersion, schema, []*lance.Fragment{updated})
		updated.Close()
		schema.Close()
		reopened := openDataset(t, ctx, uri)
		if got := mustScanInt64(t, reopened, "udf_value"); !equalInt64(got, []int64{1, 2, 3, 4}) {
			t.Fatalf("published UDF values = %v, want [1 2 3 4]", got)
		}
	})
}

func TestFragmentMergeAndUpdatePublish(t *testing.T) {
	dataset, uri, _ := newMutationDataset(t)
	ctx := context.Background()
	baseVersion := mutationVersion(t, dataset)

	mergeFragment := mustOpenFragment(t, dataset, 0)
	merged, mergedSchema, err := mergeFragment.MergeColumns(ctx, makeMergeReader(t, []int64{1, 2, 3, 4}, []int64{10, 20, 30, 40}), "id", "id")
	if err != nil {
		t.Fatalf("MergeColumns: %v", err)
	}
	requireFieldID(t, mergedSchema, "score", 2)
	if got := detachedFragmentPhysicalRows(t, merged); got != 4 {
		t.Fatalf("staged merged physical rows = %d, want 4", got)
	}
	commitMerge(t, ctx, uri, baseVersion, mergedSchema, []*lance.Fragment{merged})
	merged.Close()
	mergedSchema.Close()

	reopened := openDataset(t, ctx, uri)
	requireMutationRows(t, reopened, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if got := mustScanInt64(t, reopened, "score"); !equalInt64(got, []int64{10, 20, 30, 40}) {
		t.Fatalf("published merged scores = %v, want [10 20 30 40]", got)
	}
	reopenedSchema := mustNativeSchema(t, reopened)
	requireFieldID(t, reopenedSchema, "score", 2)
	reopenedSchema.Close()

	updateVersion := mutationVersion(t, reopened)
	updateFragment := mustOpenFragment(t, reopened, 0)
	result, err := updateFragment.UpdateColumns(ctx, makeReader(t, testSchema(),
		makeRecord(t, testSchema(), []int64{1, 3}, []string{"x", "z"}, []bool{true, true})), "id", "id")
	if err != nil {
		t.Fatalf("UpdateColumns: %v", err)
	}
	fields, err := result.FieldsModified()
	if err != nil {
		t.Fatalf("FieldsModified: %v", err)
	}
	if !equalUint32(fields, []uint32{1}) {
		t.Fatalf("modified fields = %v, want [1]", fields)
	}
	matched, err := result.MatchedOffsets()
	if err != nil {
		t.Fatalf("MatchedOffsets: %v", err)
	}
	if !equalUint32(matched, []uint32{0, 2}) {
		t.Fatalf("matched offsets = %v, want [0 2]", matched)
	}
	updatedFragment, err := result.Fragment()
	if err != nil {
		t.Fatalf("UpdateColumnsResult.Fragment: %v", err)
	}
	if got := detachedFragmentPhysicalRows(t, updatedFragment); got != 4 {
		t.Fatalf("updated physical rows = %d, want 4", got)
	}

	offsets, err := lance.NewUpdatedFragmentOffsets([]lance.FragmentOffsets{
		{FragmentID: 0, Offsets: matched},
	})
	if err != nil {
		t.Fatalf("NewUpdatedFragmentOffsets: %v", err)
	}
	updateOperation, err := lance.NewUpdateOperation(lance.UpdateOperationOptions{
		UpdatedFragments:       []*lance.Fragment{updatedFragment},
		FieldsModified:         fields,
		UpdatedFragmentOffsets: offsets,
	})
	if err != nil {
		t.Fatalf("NewUpdateOperation: %v", err)
	}
	operationFields, err := updateOperation.UpdateFieldsModified()
	if err != nil {
		t.Fatalf("operation UpdateFieldsModified: %v", err)
	}
	if !equalUint32(operationFields, []uint32{1}) {
		t.Fatalf("operation fields = %v, want [1]", operationFields)
	}
	operationOffsets, present, err := updateOperation.UpdateUpdatedFragmentOffsets()
	if err != nil {
		t.Fatalf("operation UpdateUpdatedFragmentOffsets: %v", err)
	}
	if !present {
		t.Fatal("operation offsets present = false, want true")
	}
	entries, err := operationOffsets.Entries()
	if err != nil {
		t.Fatalf("operation offset entries: %v", err)
	}
	if len(entries) != 1 || entries[0].FragmentID != 0 || !equalUint32(entries[0].Offsets, []uint32{0, 2}) {
		t.Fatalf("operation offset entries = %+v, want fragment 0 [0 2]", entries)
	}
	operationOffsets.Close()

	nilOffsetsOperation, err := lance.NewUpdateOperation(lance.UpdateOperationOptions{
		UpdatedFragments: []*lance.Fragment{updatedFragment},
		FieldsModified:   []uint32{1},
	})
	if err != nil {
		t.Fatalf("NewUpdateOperation without offsets: %v", err)
	}
	if _, present, err := nilOffsetsOperation.UpdateUpdatedFragmentOffsets(); err != nil {
		t.Fatalf("read absent update offsets: %v", err)
	} else if present {
		t.Fatal("update offsets present without an UpdatedFragmentOffsets value")
	}
	nilOffsetsOperation.Close()

	emptyOffsets, err := lance.NewUpdatedFragmentOffsets(nil)
	if err != nil {
		t.Fatalf("NewUpdatedFragmentOffsets(nil): %v", err)
	}
	emptyOffsetsOperation, err := lance.NewUpdateOperation(lance.UpdateOperationOptions{
		UpdatedFragments:       []*lance.Fragment{updatedFragment},
		FieldsModified:         []uint32{1},
		UpdatedFragmentOffsets: emptyOffsets,
	})
	if err != nil {
		t.Fatalf("NewUpdateOperation with empty offsets: %v", err)
	}
	emptyNativeOffsets, present, err := emptyOffsetsOperation.UpdateUpdatedFragmentOffsets()
	if err != nil {
		t.Fatalf("read present empty update offsets: %v", err)
	}
	if !present {
		t.Fatal("explicit empty updated fragment offsets reported absent")
	}
	if count, err := emptyNativeOffsets.Count(); err != nil {
		t.Fatalf("empty offsets count: %v", err)
	} else if count != 0 {
		t.Fatalf("empty offsets count = %d, want 0", count)
	}
	emptyNativeOffsets.Close()
	emptyOffsetsOperation.Close()
	emptyOffsets.Close()

	tx, err := lance.NewTransaction(updateVersion, updateOperation)
	if err != nil {
		t.Fatalf("NewTransaction update: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, uri, tx)
	if err != nil {
		t.Fatalf("CommitTransaction update: %v", err)
	}
	committed.Close()
	tx.Close()
	updateOperation.Close()
	updatedFragment.Close()
	offsets.Close()
	result.Close()

	reopened = openDataset(t, ctx, uri)
	requireMutationRows(t, reopened, []int64{1, 2, 3, 4}, []string{"x", "b", "z", "d"})

	reopenedSchema = mustNativeSchema(t, reopened)
	requireFieldID(t, reopenedSchema, "name", 1)
	reopenedSchema.Close()
	if got := mustScanInt64(t, reopened, "score"); !equalInt64(got, []int64{10, 20, 30, 40}) {
		t.Fatalf("scores after update = %v, want [10 20 30 40]", got)
	}

	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if got := mutationVersion(t, dataset); got != baseVersion {
		t.Fatalf("source version after update = %d, want %d", got, baseVersion)
	}
}

func TestFragmentWriteColumnsPublish(t *testing.T) {
	dataset, uri, _ := newMutationDataset(t)
	ctx := context.Background()
	baseVersion := mutationVersion(t, dataset)
	fragment := mustOpenFragment(t, dataset, 0)
	fullSchema := mustFragmentNativeSchema(t, fragment)
	valueSchema, err := fullSchema.Project("name")
	if err != nil {
		fullSchema.Close()
		t.Fatalf("Project name: %v", err)
	}
	fullSchema.Close()

	group, err := fragment.WriteColumns(ctx, valueSchema, oneColumnStringReader(t, "name", []string{"w", "x", "y", "z"}))
	if err != nil {
		t.Fatalf("WriteColumns: %v", err)
	}
	file, err := group.File()
	if err != nil {
		t.Fatalf("group File: %v", err)
	}
	major, minor, err := file.FileVersion()
	if err != nil {
		t.Fatalf("FileVersion: %v", err)
	}
	if major != 2 || minor != 2 {
		t.Fatalf("replacement version = %d.%d, want 2.2", major, minor)
	}
	fieldIDs, err := file.FieldIDs()
	if err != nil {
		t.Fatalf("FieldIDs: %v", err)
	}
	if len(fieldIDs) != 1 || fieldIDs[0] != 1 {
		t.Fatalf("replacement field IDs = %v, want [1]", fieldIDs)
	}
	file.Close()
	commitReplacement(t, ctx, uri, baseVersion, group)
	group.Close()
	valueSchema.Close()

	reopened := openDataset(t, ctx, uri)
	requireMutationRows(t, reopened, []int64{1, 2, 3, 4}, []string{"w", "x", "y", "z"})
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	explicitFragment := mustOpenFragment(t, reopened, 0)
	explicitFull := mustFragmentNativeSchema(t, explicitFragment)
	explicitSchema, err := explicitFull.Project("name")
	if err != nil {
		explicitFull.Close()
		t.Fatalf("Project name explicit: %v", err)
	}
	explicitFull.Close()
	explicitGroup, err := explicitFragment.WriteColumnWithVersion(ctx, explicitSchema,
		oneColumnStringReader(t, "name", []string{"W", "X", "Y", "Z"}), lance.FragmentFileVersionV2_3)
	if err != nil {
		t.Fatalf("WriteColumnWithVersion V2_3: %v", err)
	}
	explicitFile, err := explicitGroup.File()
	if err != nil {
		t.Fatalf("explicit group File: %v", err)
	}
	major, minor, err = explicitFile.FileVersion()
	if err != nil {
		t.Fatalf("explicit FileVersion: %v", err)
	}
	if major != 2 || minor != 3 {
		t.Fatalf("explicit replacement version = %d.%d, want 2.3", major, minor)
	}
	explicitFile.Close()
	commitReplacement(t, ctx, uri, mutationVersion(t, reopened), explicitGroup)
	explicitGroup.Close()
	explicitSchema.Close()
	explicitFragment.Close()

	reopened = openDataset(t, ctx, uri)
	requireMutationRows(t, reopened, []int64{1, 2, 3, 4}, []string{"W", "X", "Y", "Z"})

	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if got := mutationVersion(t, dataset); got != baseVersion {
		t.Fatalf("source version after replacement = %d, want %d", got, baseVersion)
	}
}

func TestFragmentWriteColumnsRejectsV1AndPreservesDeletedSlots(t *testing.T) {
	dataset, uri, _ := newMutationDataset(t)
	ctx := context.Background()
	baseVersion := mutationVersion(t, dataset)
	fragment := mustOpenFragment(t, dataset, 0)

	deleted, err := fragment.DeleteRows(ctx, []uint32{0})
	if err != nil {
		t.Fatalf("DeleteRows: %v", err)
	}
	deleteOperation, err := lance.NewDeleteOperation([]*lance.Fragment{deleted}, nil, "")
	if err != nil {
		t.Fatalf("NewDeleteOperation: %v", err)
	}
	tx, err := lance.NewTransaction(baseVersion, deleteOperation)
	if err != nil {
		t.Fatalf("NewTransaction delete: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, uri, tx)
	if err != nil {
		t.Fatalf("CommitTransaction delete: %v", err)
	}
	committed.Close()
	tx.Close()
	deleteOperation.Close()
	deleted.Close()
	fragment.Close()

	reopened := openDataset(t, ctx, uri)
	requireMutationRows(t, reopened, []int64{2, 3, 4}, []string{"b", "c", "d"})
	replacementFragment := mustOpenFragment(t, reopened, 0)
	fullSchema := mustFragmentNativeSchema(t, replacementFragment)
	valueSchema, err := fullSchema.Project("name")
	if err != nil {
		fullSchema.Close()
		t.Fatalf("Project name: %v", err)
	}
	fullSchema.Close()

	reader := &releaseCountingReader{RecordReader: oneColumnStringReader(t, "name", []string{"a2", "b2", "c2", "d2"})}
	group, err := replacementFragment.WriteColumns(ctx, valueSchema, reader)
	if err != nil {
		t.Fatalf("WriteColumns with deleted slot: %v", err)
	}
	if reader.releases != 1 {
		t.Fatalf("replacement stream released reader %d times, want 1", reader.releases)
	}
	commitReplacement(t, ctx, uri, mutationVersion(t, reopened), group)
	group.Close()
	valueSchema.Close()
	replacementFragment.Close()

	reopened = openDataset(t, ctx, uri)
	requireMutationRows(t, reopened, []int64{2, 3, 4}, []string{"b2", "c2", "d2"})

	v1Fragment := mustOpenFragment(t, reopened, 0)
	v1Full := mustFragmentNativeSchema(t, v1Fragment)
	v1Schema, err := v1Full.Project("name")
	if err != nil {
		v1Full.Close()
		t.Fatalf("Project name V1: %v", err)
	}
	v1Full.Close()
	group, err = v1Fragment.WriteColumnWithVersion(ctx, v1Schema,
		oneColumnStringReader(t, "name", []string{"a", "b", "c", "d"}), lance.FragmentFileVersionV1)
	if group != nil {
		group.Close()
		t.Fatal("V1 write returned a group, want nil")
	}
	if !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("V1 write error = %v, want ErrInvalidArgument", err)
	}
	v1Schema.Close()
	v1Fragment.Close()
	requireMutationRows(t, reopened, []int64{2, 3, 4}, []string{"b2", "c2", "d2"})
}

func TestFragmentDeleteRowsPublishPartialAndFull(t *testing.T) {
	dataset, uri, _ := newMutationDataset(t)
	ctx := context.Background()
	baseVersion := mutationVersion(t, dataset)
	fragment := mustOpenFragment(t, dataset, 0)

	duplicate, err := fragment.DeleteRows(ctx, []uint32{0, 0})
	if err != nil {
		t.Fatalf("DeleteRows duplicate: %v", err)
	}
	if deleted := mustDeletedRows(t, duplicate); deleted != 1 {
		t.Fatalf("duplicate offset deleted rows = %d, want 1", deleted)
	}
	duplicate.Close()

	partial, err := fragment.DeleteRows(ctx, []uint32{0})
	if err != nil {
		t.Fatalf("DeleteRows partial: %v", err)
	}
	if physical := mustPhysicalRows(t, partial); physical != 4 {
		t.Fatalf("partial physical rows = %d, want 4", physical)
	}
	operation, err := lance.NewDeleteOperation([]*lance.Fragment{partial}, nil, "")
	if err != nil {
		t.Fatalf("NewDeleteOperation partial: %v", err)
	}
	tx, err := lance.NewTransaction(baseVersion, operation)
	if err != nil {
		t.Fatalf("NewTransaction partial: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, uri, tx)
	if err != nil {
		t.Fatalf("CommitTransaction partial: %v", err)
	}
	committed.Close()
	tx.Close()
	operation.Close()
	partial.Close()
	fragment.Close()

	reopened := openDataset(t, ctx, uri)
	requireMutationRows(t, reopened, []int64{2, 3, 4}, []string{"b", "c", "d"})

	fullFragment := mustOpenFragment(t, reopened, 0)
	full, err := fullFragment.DeleteRows(ctx, []uint32{0, 1, 2, 3})
	if err != nil {
		t.Fatalf("DeleteRows full: %v", err)
	}
	if full != nil {
		full.Close()
		t.Fatal("full DeleteRows returned a fragment, want nil")
	}
	fullOperation, err := lance.NewDeleteOperation(nil, []uint64{0}, "")
	if err != nil {
		t.Fatalf("NewDeleteOperation full: %v", err)
	}
	tx, err = lance.NewTransaction(mutationVersion(t, reopened), fullOperation)
	if err != nil {
		t.Fatalf("NewTransaction full: %v", err)
	}
	committed, err = lance.CommitTransaction(ctx, uri, tx)
	if err != nil {
		t.Fatalf("CommitTransaction full: %v", err)
	}
	committed.Close()
	tx.Close()
	fullOperation.Close()
	fullFragment.Close()

	reopened = openDataset(t, ctx, uri)
	rows, err := reopened.CountRows(ctx)
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("rows after full physical delete = %d, want 0", rows)
	}
}

func TestFragmentMutationOwnershipAndErrors(t *testing.T) {
	dataset, _, _ := newMutationDataset(t)
	ctx := context.Background()
	baseVersion := mutationVersion(t, dataset)
	fragment := mustOpenFragment(t, dataset, 0)

	closedFragment := mustOpenFragment(t, dataset, 0)
	if err := closedFragment.Close(); err != nil {
		t.Fatalf("Close fragment: %v", err)
	}
	_, _, err := closedFragment.AddColumnsSQL(ctx, []lance.SQLColumn{{Name: "closed", Expression: "1"}})
	if !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("closed fragment error = %v, want ErrClosed", err)
	}
	if _, err := closedFragment.DeleteRows(ctx, nil); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("closed fragment delete error = %v, want ErrClosed", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	reader := &releaseCountingReader{RecordReader: oneColumnInt32Reader(t, "canceled", []int32{1, 2, 3, 4})}
	_, _, err = fragment.AddColumnsStream(canceled, reader)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled stream error = %v, want context.Canceled", err)
	}
	if reader.releases != 0 {
		t.Fatalf("canceled stream released reader %d times, want 0", reader.releases)
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if got := mutationVersion(t, dataset); got != baseVersion {
		t.Fatalf("source version = %d, want %d", got, baseVersion)
	}

	badReader := &releaseCountingReader{RecordReader: oneColumnInt32Reader(t, "too_early", []int32{1, 2, 3, 4})}
	_, _, err = fragment.AddColumnsStream(ctx, badReader, 0)
	if !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("zero batch size stream error = %v, want ErrInvalidArgument", err)
	}
	if badReader.releases != 1 {
		t.Fatalf("early-failing stream released reader %d times, want 1", badReader.releases)
	}

	snapshotScanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("source Scan: %v", err)
	}
	successReader := &releaseCountingReader{RecordReader: oneColumnInt32Reader(t, "owned", []int32{1, 2, 3, 4})}
	updated, schema, err := fragment.AddColumnsStream(ctx, successReader)
	if err != nil {
		t.Fatalf("AddColumnsStream ownership: %v", err)
	}
	if successReader.releases != 1 {
		t.Fatalf("successful stream released reader %d times, want 1", successReader.releases)
	}
	if !fragment.IsBound() {
		t.Fatal("source fragment is detached after staging, want bound")
	}
	if updated.IsBound() {
		t.Fatal("staged fragment is bound, want detached")
	}
	updated.Close()
	schema.Close()

	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if got := mustPhysicalRows(t, fragment); got != 4 {
		t.Fatalf("source fragment physical rows after staging = %d, want 4", got)
	}
	if got := scannerInt64(t, snapshotScanner, "id"); !equalInt64(got, []int64{1, 2, 3, 4}) {
		t.Fatalf("source scanner rows after staging = %v, want [1 2 3 4]", got)
	}
	if err := snapshotScanner.Close(); err != nil {
		t.Fatalf("close source scanner: %v", err)
	}
	if got := mutationVersion(t, dataset); got != baseVersion {
		t.Fatalf("source version after staging = %d, want %d", got, baseVersion)
	}

	_, _, err = fragment.AddColumnsSQL(ctx, []lance.SQLColumn{{Name: "bad", Expression: "id >>>"}})
	if !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("invalid SQL error = %v, want ErrInvalidArgument", err)
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
}

func mustOpenFragment(t *testing.T, dataset *lance.Dataset, id uint64) *lance.Fragment {
	t.Helper()
	fragment, err := dataset.OpenFragment(id)
	if err != nil {
		t.Fatalf("OpenFragment(%d): %v", id, err)
	}
	t.Cleanup(func() { _ = fragment.Close() })
	return fragment
}

func openDataset(t *testing.T, ctx context.Context, uri string) *lance.Dataset {
	t.Helper()
	dataset, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = dataset.Close() })
	return dataset
}

func commitMerge(t *testing.T, ctx context.Context, uri string, version uint64, schema *lance.NativeSchema, fragments []*lance.Fragment) {
	t.Helper()
	operation, err := lance.NewMergeOperation(schema, fragments, false)
	if err != nil {
		t.Fatalf("NewMergeOperation: %v", err)
	}
	tx, err := lance.NewTransaction(version, operation)
	if err != nil {
		t.Fatalf("NewTransaction merge: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, uri, tx)
	if err != nil {
		t.Fatalf("CommitTransaction merge: %v", err)
	}
	committed.Close()
	tx.Close()
	operation.Close()
}

func commitReplacement(t *testing.T, ctx context.Context, uri string, version uint64, group *lance.DataReplacementGroup) {
	t.Helper()
	operation, err := lance.NewDataReplacementOperation(group)
	if err != nil {
		t.Fatalf("NewDataReplacementOperation: %v", err)
	}
	tx, err := lance.NewTransaction(version, operation)
	if err != nil {
		t.Fatalf("NewTransaction replacement: %v", err)
	}
	committed, err := lance.CommitTransaction(ctx, uri, tx)
	if err != nil {
		t.Fatalf("CommitTransaction replacement: %v", err)
	}
	committed.Close()
	tx.Close()
	operation.Close()
}

func mustNativeSchema(t *testing.T, dataset *lance.Dataset) *lance.NativeSchema {
	t.Helper()
	schema, err := dataset.NativeSchema()
	if err != nil {
		t.Fatalf("NativeSchema: %v", err)
	}
	return schema
}

func mustFragmentNativeSchema(t *testing.T, fragment *lance.Fragment) *lance.NativeSchema {
	t.Helper()
	schema, err := fragment.NativeSchema()
	if err != nil {
		t.Fatalf("fragment NativeSchema: %v", err)
	}
	return schema
}

func requireFieldID(t *testing.T, schema *lance.NativeSchema, column string, want int32) {
	t.Helper()
	got, err := schema.FieldID(column)
	if err != nil {
		t.Fatalf("FieldID(%s): %v", column, err)
	}
	if got != want {
		t.Fatalf("FieldID(%s) = %d, want %d", column, got, want)
	}
}

func mustFirstBatch(t *testing.T, source interface {
	Scan(...lance.ScanOption) (*lance.Scanner, error)
}) arrow.RecordBatch {
	t.Helper()
	scanner, err := source.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		batch.Retain()
		t.Cleanup(batch.Release)
		return batch
	}
	t.Fatal("fragment scan returned no batches")
	return nil
}

func mustScanInt64(t *testing.T, source interface {
	Scan(...lance.ScanOption) (*lance.Scanner, error)
}, column string) []int64 {
	t.Helper()
	scanner, err := source.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	defer scanner.Close()
	return scannerInt64(t, scanner, column)
}

func scannerInt64(t *testing.T, scanner *lance.Scanner, column string) []int64 {
	t.Helper()
	var values []int64
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		values = append(values, collectInt64(t, batch, column)...)
	}
	return values
}

func mustPhysicalRows(t *testing.T, fragment *lance.Fragment) uint64 {
	t.Helper()
	rows, err := fragment.PhysicalRows()
	if err != nil {
		t.Fatalf("PhysicalRows: %v", err)
	}
	return rows
}

func mustDeletedRows(t *testing.T, fragment *lance.Fragment) uint64 {
	t.Helper()
	rows, err := fragment.DeletedRows()
	if err != nil {
		t.Fatalf("DeletedRows: %v", err)
	}
	return rows
}

func oneColumnInt32Reader(t *testing.T, name string, values []int32) array.RecordReader {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: name, Type: arrow.PrimitiveTypes.Int32, Nullable: true},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)
	column := builder.Field(0).(*array.Int32Builder)
	valid := make([]bool, len(values))
	for i := range valid {
		valid[i] = true
	}
	column.AppendValues(values, valid)
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return makeReader(t, schema, record)
}

func oneColumnStringReader(t *testing.T, name string, values []string) array.RecordReader {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: name, Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	t.Cleanup(builder.Release)
	column := builder.Field(0).(*array.StringBuilder)
	valid := make([]bool, len(values))
	for i := range valid {
		valid[i] = true
	}
	column.AppendValues(values, valid)
	record := builder.NewRecordBatch()
	t.Cleanup(record.Release)
	return makeReader(t, schema, record)
}

func equalUint32(got, want []uint32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
