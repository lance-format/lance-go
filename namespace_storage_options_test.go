//go:build linux || darwin || windows

package lance_test

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/namespace"
)

func TestNamespaceStorageOptionsAccessorSurvivesNamespaceClose(t *testing.T) {
	ctx := context.Background()
	ns, _ := newManagedDirectory(t)
	tableID := []string{"accessor_table"}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)

	created, err := lance.WriteToNamespace(ctx, ns, tableID, makeReader(t, schema,
		makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true}),
	))
	if err != nil {
		ns.Close()
		t.Fatalf("WriteToNamespace: %v", err)
	}
	if err := created.Close(); err != nil {
		ns.Close()
		t.Fatalf("close created dataset: %v", err)
	}

	description, err := ns.DescribeTable(ctx, &namespace.DescribeTableRequest{ID: &tableID})
	if err != nil {
		ns.Close()
		t.Fatalf("DescribeTable: %v", err)
	}
	if description.Location == nil || *description.Location == "" {
		ns.Close()
		t.Fatal("DescribeTable returned no location")
	}
	location := *description.Location

	accessor, err := lance.NewNamespaceStorageOptionsAccessor(ns, tableID)
	if err != nil {
		ns.Close()
		t.Fatalf("NewNamespaceStorageOptionsAccessor: %v", err)
	}
	defer accessor.Close()
	if err := ns.Close(); err != nil {
		t.Fatalf("close namespace: %v", err)
	}

	if _, _, err := accessor.Snapshot(); err != nil {
		t.Fatalf("snapshot after namespace close: %v", err)
	}
	dataset, err := lance.Open(ctx, location, lance.WithStorageOptionsAccessor(accessor))
	if err != nil {
		t.Fatalf("Open after namespace close: %v", err)
	}
	defer dataset.Close()
	if ids := scanManagedIDs(t, ctx, dataset); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("scan ids after namespace close = %v, want [1 2]", ids)
	}
}
