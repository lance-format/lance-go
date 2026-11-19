//go:build linux || darwin || windows

package lance_test

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/namespace"
)

func requireDatasetURI(t *testing.T, dataset *lance.Dataset, want, label string) {
	t.Helper()
	if got := dataset.URI(); got != want {
		t.Fatalf("%s URI = %q, want %q", label, got, want)
	}
}

func closeDatasetURI(t *testing.T, dataset *lance.Dataset, want, label string) {
	t.Helper()
	requireDatasetURI(t, dataset, want, label)
	if err := dataset.Close(); err != nil {
		t.Fatalf("close %s: %v", label, err)
	}
}

func requireDatasetURIMatchesLocation(t *testing.T, dataset *lance.Dataset, location, label string) {
	t.Helper()
	got := dataset.URI()
	normalized := got
	if !strings.Contains(location, "://") {
		normalized = strings.TrimPrefix(got, "file://")
	}
	if normalized != location {
		t.Fatalf("%s URI = %q, want the native URI for describe_table location %q", label, got, location)
	}
}

func TestDatasetURIDirectLifecycle(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "uri.lance")
	schema := testSchema()
	record := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})

	created, err := lance.Create(ctx, uri, makeReader(t, schema, record))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	requireDatasetURI(t, created, uri, "created dataset")

	opened, err := lance.Open(ctx, uri)
	if err != nil {
		created.Close()
		t.Fatalf("Open: %v", err)
	}
	requireDatasetURI(t, opened, uri, "opened dataset")
	closeDatasetURI(t, opened, uri, "opened dataset")

	var wg sync.WaitGroup
	mismatches := make(chan string, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 32; j++ {
				if got := created.URI(); got != uri {
					mismatches <- got
					return
				}
			}
		}()
	}
	if err := created.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wg.Wait()
	close(mismatches)
	for got := range mismatches {
		t.Fatalf("concurrent URI = %q, want %q", got, uri)
	}
	requireDatasetURI(t, created, uri, "closed dataset")
}

func TestDatasetURIMutationSnapshots(t *testing.T) {
	current, uri, schema := newMutationDataset(t)
	ctx := context.Background()

	deleted, _, err := current.Delete(ctx, "id = 1")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	closeDatasetURI(t, current, uri, "delete source")
	current = deleted
	requireDatasetURI(t, current, uri, "delete result")

	updated, _, err := current.Update(ctx, "id = 2", lance.ColumnUpdate{Column: "name", Value: "'b2'"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	closeDatasetURI(t, current, uri, "update source")
	current = updated
	requireDatasetURI(t, current, uri, "update result")

	inserted, _, err := current.MergeInsert(ctx, []string{"id"}, makeReader(t, schema,
		makeRecord(t, schema, []int64{5}, []string{"e"}, []bool{true}),
	), lance.MergeInsertParams{})
	if err != nil {
		t.Fatalf("MergeInsert: %v", err)
	}
	closeDatasetURI(t, current, uri, "merge insert source")
	current = inserted
	requireDatasetURI(t, current, uri, "merge insert result")

	merged, err := current.Merge(ctx, "id", "id", makeMergeReader(t, []int64{1, 3}, []int64{10, 30}))
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	closeDatasetURI(t, current, uri, "merge source")
	current = merged
	requireDatasetURI(t, current, uri, "merge result")

	truncated, err := current.Truncate(ctx)
	if err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	closeDatasetURI(t, current, uri, "truncate source")
	closeDatasetURI(t, truncated, uri, "truncate result")
}

func TestDatasetURIVersionSnapshots(t *testing.T) {
	dataset, uri, _ := newVersionedDataset(t)
	ctx := context.Background()

	first, err := dataset.CheckoutVersion(ctx, 1)
	if err != nil {
		t.Fatalf("CheckoutVersion: %v", err)
	}
	closeDatasetURI(t, first, uri, "checkout version result")

	main, err := dataset.CheckoutBranch(ctx, "main")
	if err != nil {
		t.Fatalf("CheckoutBranch: %v", err)
	}
	closeDatasetURI(t, main, uri, "checkout branch result")

	byRef, err := dataset.CheckoutRef(ctx, lance.VersionRef(1))
	if err != nil {
		t.Fatalf("CheckoutRef: %v", err)
	}
	closeDatasetURI(t, byRef, uri, "checkout ref result")

	if err := dataset.CreateTag(ctx, "uri-snapshot", lance.VersionRef(1)); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	tagged, err := dataset.CheckoutTag(ctx, "uri-snapshot")
	if err != nil {
		t.Fatalf("CheckoutTag: %v", err)
	}
	closeDatasetURI(t, tagged, uri, "checkout tag result")

	branch, err := dataset.CreateBranch(ctx, "uri-branch", lance.VersionRef(1))
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if got := branch.URI(); got == uri || !strings.Contains(got, "uri-branch") {
		t.Fatalf("created branch URI = %q, want a branch-qualified native URI containing %q", got, "uri-branch")
	}
	if err := branch.Close(); err != nil {
		t.Fatalf("close create branch result: %v", err)
	}

	restored, err := dataset.Restore(ctx, 1)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	closeDatasetURI(t, restored, uri, "restore result")

	requireDatasetURI(t, dataset, uri, "version receiver")
}

func TestDatasetURINamespaceLocation(t *testing.T) {
	ctx := context.Background()
	ns, _ := newManagedDirectory(t)
	defer ns.Close()
	tableID := []string{"uri_namespace_location"}
	tableName := strings.Join(tableID, "$")
	schema := testSchema()

	created, err := lance.WriteToNamespace(ctx, ns, tableID, makeReader(t, schema,
		makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true}),
	))
	if err != nil {
		t.Fatalf("WriteToNamespace: %v", err)
	}
	description, err := ns.DescribeTable(ctx, &namespace.DescribeTableRequest{ID: &tableID})
	if err != nil {
		created.Close()
		t.Fatalf("DescribeTable: %v", err)
	}
	if description.Location == nil || *description.Location == "" {
		created.Close()
		t.Fatalf("DescribeTable location = %v", description.Location)
	}
	location := *description.Location
	requireDatasetURIMatchesLocation(t, created, location, "namespace created dataset")
	if got := created.URI(); got == tableName {
		t.Fatalf("namespace created URI = table ID %q, want native location", got)
	}

	// The request identifier still supplies error diagnostics after close.
	if err := created.Close(); err != nil {
		t.Fatalf("close created namespace dataset: %v", err)
	}
	if _, err := created.CountRows(ctx); err == nil || !strings.Contains(err.Error(), "count rows "+tableName+":") {
		t.Fatalf("closed namespace diagnostic = %v, want the %q request identifier", err, tableName)
	}

	opened, err := lance.OpenFromNamespace(ctx, ns, tableID)
	if err != nil {
		ns.Close()
		t.Fatalf("OpenFromNamespace: %v", err)
	}
	requireDatasetURIMatchesLocation(t, opened, location, "namespace opened dataset")

	mutated, _, err := opened.Delete(ctx, "id = 1")
	if err != nil {
		opened.Close()
		ns.Close()
		t.Fatalf("namespace Delete: %v", err)
	}
	requireDatasetURIMatchesLocation(t, mutated, location, "namespace mutation result")
	if err := mutated.Close(); err != nil {
		t.Fatalf("close namespace mutation result: %v", err)
	}
	requireDatasetURIMatchesLocation(t, opened, location, "namespace opened dataset")
	if err := opened.Close(); err != nil {
		t.Fatalf("close namespace opened dataset: %v", err)
	}

	appended, err := lance.WriteToNamespace(ctx, ns, tableID, makeReader(t, schema,
		makeRecord(t, schema, []int64{3}, []string{"c"}, []bool{true}),
	), lance.WithWriteMode(lance.WriteModeAppend))
	if err != nil {
		ns.Close()
		t.Fatalf("WriteToNamespace append: %v", err)
	}
	requireDatasetURIMatchesLocation(t, appended, location, "namespace append result")
	if err := appended.Close(); err != nil {
		t.Fatalf("close namespace append result: %v", err)
	}

	tx, err := lance.WriteUncommitted(ctx, location, makeReader(t, schema,
		makeRecord(t, schema, []int64{4}, []string{"d"}, []bool{true}),
	), lance.WithWriteMode(lance.WriteModeAppend))
	if err != nil {
		ns.Close()
		t.Fatalf("WriteUncommitted: %v", err)
	}
	defer tx.Close()
	committed, err := lance.CommitTransactionToNamespace(ctx, ns, tableID, location, tx)
	if err != nil {
		ns.Close()
		t.Fatalf("CommitTransactionToNamespace: %v", err)
	}
	requireDatasetURIMatchesLocation(t, committed, location, "namespace commit result")

	if err := ns.Close(); err != nil {
		t.Fatalf("close namespace: %v", err)
	}
	requireDatasetURIMatchesLocation(t, committed, location, "namespace commit after namespace close")
	if err := committed.Close(); err != nil {
		t.Fatalf("close committed dataset: %v", err)
	}
}
