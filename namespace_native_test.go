//go:build linux || darwin

package lance_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/loader"
	"github.com/lance-format/lance-go/namespace"
)

func newManagedDirectory(t *testing.T) (namespace.Namespace, string) {
	t.Helper()
	root := t.TempDir()
	ns, err := namespace.NewDirectory(map[string]string{"root": root})
	if errors.Is(err, loader.ErrNotFound) {
		t.Skip("lance-c is not available; set LANCE_LIB_PATH to a built liblance_c")
	}
	if err != nil {
		t.Fatalf("new directory namespace: %v", err)
	}
	return ns, root
}

func scanManagedIDs(t *testing.T, ctx context.Context, dataset *lance.Dataset) []int64 {
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
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func TestNamespaceManagedWriteOpenAppendCommit(t *testing.T) {
	ctx := context.Background()
	ns, root := newManagedDirectory(t)
	defer ns.Close()
	session, err := lance.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tableID := []string{"managed_events"}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)

	created, err := lance.WriteToNamespace(ctx, ns, tableID, makeReader(t, schema,
		makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true}),
	), lance.WithWriteSession(session))
	if err != nil {
		t.Fatalf("WriteToNamespace create: %v", err)
	}
	if rows, err := created.CountRows(ctx); err != nil || rows != 2 {
		t.Fatalf("created CountRows = %d, %v; want 2, nil", rows, err)
	}
	if err := created.Close(); err != nil {
		t.Fatalf("close created dataset: %v", err)
	}

	opened, err := lance.OpenFromNamespace(ctx, ns, tableID, lance.WithSession(session))
	if err != nil {
		t.Fatalf("OpenFromNamespace: %v", err)
	}
	if ids := scanManagedIDs(t, ctx, opened); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("initial scan ids = %v, want [1 2]", ids)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close opened dataset: %v", err)
	}

	appended, err := lance.WriteToNamespace(ctx, ns, tableID, makeReader(t, schema,
		makeRecord(t, schema, []int64{3, 4}, []string{"c", "d"}, []bool{true, true}),
	), lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteSession(session))
	if err != nil {
		t.Fatalf("WriteToNamespace append: %v", err)
	}
	if rows, err := appended.CountRows(ctx); err != nil || rows != 4 {
		t.Fatalf("appended CountRows = %d, %v; want 4, nil", rows, err)
	}
	if err := appended.Close(); err != nil {
		t.Fatalf("close appended dataset: %v", err)
	}

	description, err := ns.DescribeTable(ctx, &namespace.DescribeTableRequest{ID: &tableID})
	if err != nil {
		t.Fatalf("DescribeTable: %v", err)
	}
	if description.Location == nil || *description.Location == "" {
		t.Fatalf("DescribeTable location = %v", description.Location)
	}
	location := *description.Location

	tx, err := lance.WriteUncommitted(ctx, location, makeReader(t, schema,
		makeRecord(t, schema, []int64{5, 6}, []string{"e", "f"}, []bool{true, true}),
	), lance.WithWriteMode(lance.WriteModeAppend), lance.WithWriteSession(session))
	if err != nil {
		t.Fatalf("WriteUncommitted: %v", err)
	}
	defer tx.Close()

	committed, err := lance.CommitTransactionToNamespace(ctx, ns, tableID, location, tx, lance.WithCommitSession(session))
	if err != nil {
		t.Fatalf("CommitTransactionToNamespace: %v", err)
	}
	if rows, err := committed.CountRows(ctx); err != nil || rows != 6 {
		t.Fatalf("committed CountRows = %d, %v; want 6, nil", rows, err)
	}
	if err := committed.Close(); err != nil {
		t.Fatalf("close committed dataset: %v", err)
	}

	survivor, err := lance.OpenFromNamespace(ctx, ns, tableID, lance.WithSession(session))
	if err != nil {
		t.Fatalf("OpenFromNamespace before namespace close: %v", err)
	}
	defer survivor.Close()
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ns.Close(); err != nil {
		t.Fatalf("close namespace: %v", err)
	}
	if rows, err := survivor.CountRows(ctx); err != nil || rows != 6 {
		t.Fatalf("dataset CountRows after namespace close = %d, %v; want 6, nil", rows, err)
	}
	if ids := scanManagedIDs(t, ctx, survivor); len(ids) != 6 || ids[0] != 1 || ids[5] != 6 {
		t.Fatalf("scan after namespace close ids = %v, want [1..6]", ids)
	}
	if _, err := lance.OpenFromNamespace(ctx, ns, tableID); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("OpenFromNamespace after Close = %v, want ErrClosed", err)
	}

	reopenedNamespace, err := namespace.NewDirectory(map[string]string{"root": root})
	if err != nil {
		t.Fatalf("reopen directory namespace: %v", err)
	}
	defer reopenedNamespace.Close()
	reopened, err := lance.OpenFromNamespace(ctx, reopenedNamespace, tableID)
	if err != nil {
		t.Fatalf("OpenFromNamespace after reopening namespace: %v", err)
	}
	defer reopened.Close()
	if ids := scanManagedIDs(t, ctx, reopened); len(ids) != 6 || ids[0] != 1 || ids[5] != 6 {
		t.Fatalf("reopened scan ids = %v, want [1..6]", ids)
	}
}
