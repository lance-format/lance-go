package lance_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lance-format/lance-go"
)

func TestAddBasesCommitsIndependentVersion(t *testing.T) {
	dataset, uri, _ := newMutationDataset(t)
	ctx := context.Background()
	version := mutationVersion(t, dataset)

	before, err := dataset.BasePaths()
	if err != nil {
		t.Fatalf("BasePaths before add: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("initial base paths = %v, want empty", before)
	}
	scanner, err := dataset.Scan()
	if err != nil {
		t.Fatalf("Scan before add: %v", err)
	}

	autoPath := filepath.Join(t.TempDir(), "auto-base")
	namedPath := filepath.Join(t.TempDir(), "named-base")
	next, err := dataset.AddBases(ctx, []lance.BasePath{
		{Path: autoPath, Name: ptr("auto")},
		{ID: 42, Path: namedPath, Name: ptr(""), IsDatasetRoot: true},
	}, map[string]string{"fixture.tx": "add-bases"})
	if err != nil {
		t.Fatalf("AddBases: %v", err)
	}
	defer next.Close()

	if got := mutationVersion(t, next); got != version+1 {
		t.Fatalf("committed version = %d, want %d", got, version+1)
	}
	if got := mutationVersion(t, dataset); got != version {
		t.Fatalf("source version after add = %d, want %d", got, version)
	}

	bases, err := next.BasePaths()
	if err != nil {
		t.Fatalf("BasePaths after add: %v", err)
	}
	if len(bases) != 2 {
		t.Fatalf("base paths = %v, want two entries", bases)
	}
	auto := bases[1]
	if auto.ID != 1 || auto.Path != autoPath || auto.Name == nil || *auto.Name != "auto" || auto.IsDatasetRoot {
		t.Fatalf("allocated base = %+v, want id 1 path %q name %q data-only", auto, autoPath, "auto")
	}
	named := bases[42]
	if named.ID != 42 || named.Path != namedPath || named.Name == nil || *named.Name != "" || !named.IsDatasetRoot {
		t.Fatalf("explicit base = %+v, want id 42 path %q empty present name and root", named, namedPath)
	}

	requireMutationRows(t, next, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if ids := scannerIDs(t, scanner); !slices.Equal(ids, []int64{1, 2, 3, 4}) {
		t.Fatalf("scanner from before add ids = %v, want [1 2 3 4]", ids)
	}
	sourceBases, err := dataset.BasePaths()
	if err != nil {
		t.Fatalf("BasePaths on source after add: %v", err)
	}
	if len(sourceBases) != 0 {
		t.Fatalf("source base paths after add = %v, want empty", sourceBases)
	}

	tx, err := next.ReadTransactionByVersion(version + 1)
	if err != nil {
		t.Fatalf("ReadTransactionByVersion: %v", err)
	}
	defer tx.Close()
	properties, present, err := tx.Properties()
	if err != nil {
		t.Fatalf("Transaction.Properties: %v", err)
	}
	if !present || len(properties) != 1 || properties["fixture.tx"] != "add-bases" {
		t.Fatalf("transaction properties = %v, present %v; want only fixture.tx=add-bases", properties, present)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after add: %v", err)
	}
	defer reopened.Close()
	if got := mutationVersion(t, reopened); got != version+1 {
		t.Fatalf("reopened version = %d, want %d", got, version+1)
	}
	reopenedBases, err := reopened.BasePaths()
	if err != nil {
		t.Fatalf("reopened BasePaths: %v", err)
	}
	if !reflect.DeepEqual(reopenedBases, bases) {
		t.Fatalf("reopened base paths = %v, want %v", reopenedBases, bases)
	}

	if err := dataset.Close(); err != nil {
		t.Fatalf("Close source: %v", err)
	}
	requireMutationRows(t, next, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if _, err := next.BasePaths(); err != nil {
		t.Fatalf("BasePaths after closing source: %v", err)
	}
}

func TestAddBasesPreservesNilAndEmptyInputs(t *testing.T) {
	dataset, _, _ := newMutationDataset(t)
	ctx := context.Background()
	version := mutationVersion(t, dataset)

	namedPath := filepath.Join(t.TempDir(), "named-base")
	next, err := dataset.AddBases(ctx, []lance.BasePath{
		{ID: 7, Path: namedPath, Name: ptr("named")},
	}, nil)
	if err != nil {
		t.Fatalf("AddBases named base: %v", err)
	}
	defer next.Close()
	if got := mutationVersion(t, next); got != version+1 {
		t.Fatalf("version after named base = %d, want %d", got, version+1)
	}
	tx, err := next.ReadTransactionByVersion(version + 1)
	if err != nil {
		t.Fatalf("ReadTransactionByVersion named: %v", err)
	}
	_, present, err := tx.Properties()
	if err != nil || present {
		tx.Close()
		t.Fatalf("nil properties present = %v, %v; want native None", present, err)
	}
	tx.Close()

	unnamedPath := filepath.Join(t.TempDir(), "unnamed-base")
	withUnnamed, err := next.AddBases(ctx, []lance.BasePath{{Path: unnamedPath}}, nil)
	if err != nil {
		t.Fatalf("AddBases unnamed base: %v", err)
	}
	defer withUnnamed.Close()
	if got := mutationVersion(t, withUnnamed); got != version+2 {
		t.Fatalf("version after unnamed base = %d, want %d", got, version+2)
	}
	bases, err := withUnnamed.BasePaths()
	if err != nil {
		t.Fatalf("BasePaths after unnamed base: %v", err)
	}
	if len(bases) != 2 {
		t.Fatalf("base paths after unnamed base = %v, want two entries", bases)
	}
	var unnamed *lance.BasePath
	for id := range bases {
		if bases[id].Path == unnamedPath {
			base := bases[id]
			unnamed = &base
		}
	}
	if unnamed == nil {
		t.Fatalf("unnamed base path %q not found in %v", unnamedPath, bases)
	}
	if unnamed.Name != nil {
		t.Fatalf("unnamed base name = %q, want native None", *unnamed.Name)
	}
	if unnamed.ID == 0 {
		t.Fatalf("unnamed base kept id 0, want an allocated id")
	}
	if unnamed.IsDatasetRoot {
		t.Fatalf("unnamed base is dataset root, want data-only")
	}

	emptyVersion := mutationVersion(t, withUnnamed) + 1
	emptied, err := withUnnamed.AddBases(ctx, nil, nil)
	if err != nil {
		t.Fatalf("AddBases empty list: %v", err)
	}
	defer emptied.Close()
	if got := mutationVersion(t, emptied); got != emptyVersion {
		t.Fatalf("version after empty list = %d, want %d", got, emptyVersion)
	}
	emptiedBases, err := emptied.BasePaths()
	if err != nil {
		t.Fatalf("BasePaths after empty list: %v", err)
	}
	if !reflect.DeepEqual(emptiedBases, bases) {
		t.Fatalf("base paths after empty list = %v, want %v", emptiedBases, bases)
	}

	withEmptyProperties, err := emptied.AddBases(ctx, nil, map[string]string{})
	if err != nil {
		t.Fatalf("AddBases explicit empty properties: %v", err)
	}
	defer withEmptyProperties.Close()
	emptyPropertiesVersion := mutationVersion(t, withEmptyProperties)
	if emptyPropertiesVersion != emptyVersion+1 {
		t.Fatalf("version after empty properties = %d, want %d", emptyPropertiesVersion, emptyVersion+1)
	}
	emptyTx, err := withEmptyProperties.ReadTransactionByVersion(emptyPropertiesVersion)
	if err != nil {
		t.Fatalf("ReadTransactionByVersion empty properties: %v", err)
	}
	defer emptyTx.Close()
	properties, present, err := emptyTx.Properties()
	if err != nil {
		t.Fatalf("Transaction.Properties empty map: %v", err)
	}
	if present || len(properties) != 0 {
		t.Fatalf("persisted empty properties = %v, present %v; want absent after native protobuf normalization", properties, present)
	}
}

func TestAddBasesRejectsConflictsAndKeepsSourceUsable(t *testing.T) {
	dataset, _, _ := newMutationDataset(t)
	ctx := context.Background()
	version := mutationVersion(t, dataset)

	altPath := filepath.Join(t.TempDir(), "alt-base")
	next, err := dataset.AddBases(ctx, []lance.BasePath{
		{ID: 7, Path: altPath, Name: ptr("alt-root")},
	}, nil)
	if err != nil {
		t.Fatalf("AddBases seeded base: %v", err)
	}
	defer next.Close()
	committedVersion := mutationVersion(t, next)
	if committedVersion != version+1 {
		t.Fatalf("seeded version = %d, want %d", committedVersion, version+1)
	}

	duplicates := []struct {
		name string
		base lance.BasePath
	}{
		{"duplicate path", lance.BasePath{Path: altPath, Name: ptr("other")}},
		{"duplicate name", lance.BasePath{Path: filepath.Join(t.TempDir(), "other-base"), Name: ptr("alt-root")}},
	}
	for _, duplicate := range duplicates {
		t.Run(duplicate.name, func(t *testing.T) {
			if _, err := next.AddBases(ctx, []lance.BasePath{duplicate.base}, nil); err == nil {
				t.Fatalf("AddBases accepted %s", duplicate.name)
			} else {
				requireErrorCode(t, err, lance.CodeInvalidArgument)
				if !strings.Contains(err.Error(), "Conflict detected") {
					t.Fatalf("error = %v, want a native Conflict detected message", err)
				}
			}
			if got := mutationVersion(t, next); got != committedVersion {
				t.Fatalf("version after conflict = %d, want %d", got, committedVersion)
			}
			bases, err := next.BasePaths()
			if err != nil {
				t.Fatalf("BasePaths after conflict: %v", err)
			}
			if len(bases) != 1 || bases[7].Path != altPath || bases[7].Name == nil || *bases[7].Name != "alt-root" {
				t.Fatalf("base paths after conflict = %v, want only the seeded base", bases)
			}
			requireMutationRows(t, next, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
		})
	}
}

func TestAddBasesValidation(t *testing.T) {
	dataset, _, _ := newMutationDataset(t)
	ctx := context.Background()
	version := mutationVersion(t, dataset)

	cases := []struct {
		name  string
		bases []lance.BasePath
		props map[string]string
	}{
		{"path NUL", []lance.BasePath{{Path: "s3://bucket/base\x00"}}, nil},
		{"name NUL", []lance.BasePath{{Path: "s3://bucket/base", Name: ptr("bad\x00name")}}, nil},
		{"property key NUL", nil, map[string]string{"key\x00": "value"}},
		{"property value NUL", nil, map[string]string{"key": "value\x00"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := dataset.AddBases(ctx, tc.bases, tc.props)
			if err == nil {
				t.Fatalf("AddBases accepted %s", tc.name)
			}
			requireErrorCode(t, err, lance.CodeInvalidArgument)
		})
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := dataset.AddBases(canceled, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("AddBases canceled context error = %v, want context.Canceled", err)
	}
	if got := mutationVersion(t, dataset); got != version {
		t.Fatalf("version after rejected inputs = %d, want %d", got, version)
	}
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	closed, _, _ := newMutationDataset(t)
	if err := closed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := closed.AddBases(ctx, nil, nil); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("AddBases closed error = %v, want ErrClosed", err)
	}
	if _, err := closed.BasePaths(); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("BasePaths closed error = %v, want ErrClosed", err)
	}
}
