package lance_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"

	lance "github.com/lance-format/lance-go"
)

// newVersionedDataset creates a two-version dataset and returns a handle
// checked out at the latest version. Version 1 has ids 1,2; version 2 adds 3,4.
func newVersionedDataset(t *testing.T) (*lance.Dataset, string, *arrow.Schema) {
	t.Helper()
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()

	first := makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})
	created, err := lance.Create(ctx, uri, makeReader(t, schema, first))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := created.Close(); err != nil {
		t.Fatalf("Close create dataset: %v", err)
	}

	second := makeRecord(t, schema, []int64{3, 4}, []string{"c", "d"}, []bool{true, true})
	dataset, err := lance.Append(ctx, uri, makeReader(t, schema, second))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	t.Cleanup(func() { _ = dataset.Close() })
	return dataset, uri, schema
}

func TestCurrentVersionAndVersions(t *testing.T) {
	dataset, _, _ := newVersionedDataset(t)
	ctx := context.Background()

	current, err := dataset.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if current.ID != 2 {
		t.Fatalf("CurrentVersion.ID = %d, want 2", current.ID)
	}
	if current.Timestamp.IsZero() {
		t.Fatal("CurrentVersion.Timestamp is zero")
	}
	if current.Metadata == nil {
		t.Fatal("CurrentVersion.Metadata is nil")
	}

	versions, err := dataset.Versions(ctx)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("len(Versions) = %d, want 2", len(versions))
	}
	if versions[0].ID != 1 || versions[1].ID != 2 {
		t.Fatalf("version ids = [%d %d], want [1 2]", versions[0].ID, versions[1].ID)
	}
	if versions[0].Timestamp.IsZero() || versions[1].Timestamp.IsZero() {
		t.Fatal("version timestamp is zero")
	}
	if versions[1].Timestamp.Before(versions[0].Timestamp) {
		t.Fatalf("version timestamps are not ordered: %v then %v", versions[0].Timestamp, versions[1].Timestamp)
	}
	if versions[0].Metadata == nil || versions[1].Metadata == nil {
		t.Fatal("version metadata is nil")
	}

	// Returned metadata is a copy, not a view over the native snapshot.
	versions[0].Metadata["injected"] = "value"
	again, err := dataset.Versions(ctx)
	if err != nil {
		t.Fatalf("Versions second call: %v", err)
	}
	if _, ok := again[0].Metadata["injected"]; ok {
		t.Fatal("mutating returned version metadata changed a later snapshot")
	}

	refs, err := dataset.VersionRefs(ctx)
	if err != nil {
		t.Fatalf("VersionRefs: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("VersionRefs returned no lightweight references")
	}
}

func TestVersionSnapshotsSurviveClose(t *testing.T) {
	dataset, _, _ := newVersionedDataset(t)
	ctx := context.Background()

	versions, err := dataset.Versions(ctx)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	current, err := dataset.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(versions) != 2 || versions[1].ID != 2 {
		t.Fatalf("version snapshot changed after close: %+v", versions)
	}
	if current.ID != 2 || current.Metadata == nil {
		t.Fatalf("current version snapshot changed after close: %+v", current)
	}
	versions[1].Metadata["after-close"] = "value"
	if _, ok := versions[1].Metadata["after-close"]; !ok {
		t.Fatal("version snapshot is not independent after close")
	}
}

func TestCheckoutVersionAndRefs(t *testing.T) {
	dataset, _, _ := newVersionedDataset(t)
	ctx := context.Background()

	first, err := dataset.CheckoutVersion(ctx, 1)
	if err != nil {
		t.Fatalf("CheckoutVersion(1): %v", err)
	}
	defer first.Close()
	requireMutationRows(t, first, []int64{1, 2}, []string{"a", "b"})

	latest, err := dataset.CheckoutVersion(ctx, 2)
	if err != nil {
		t.Fatalf("CheckoutVersion(2): %v", err)
	}
	defer latest.Close()
	requireMutationRows(t, latest, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	if _, err := dataset.CheckoutVersion(ctx, 0); !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("CheckoutVersion(0) error = %v, want ErrNotFound", err)
	}
	if _, err := dataset.CheckoutVersion(ctx, 99); !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("CheckoutVersion(99) error = %v, want ErrNotFound", err)
	}

	byRef, err := dataset.CheckoutRef(ctx, lance.VersionRef(1))
	if err != nil {
		t.Fatalf("CheckoutRef(VersionRef(1)): %v", err)
	}
	defer byRef.Close()
	requireMutationRows(t, byRef, []int64{1, 2}, []string{"a", "b"})

	main, err := dataset.CheckoutBranch(ctx, "main")
	if err != nil {
		t.Fatalf("CheckoutBranch(main): %v", err)
	}
	defer main.Close()
	requireMutationRows(t, main, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	byBranchRef, err := dataset.CheckoutRef(ctx, lance.BranchRef("main"))
	if err != nil {
		t.Fatalf("CheckoutRef(BranchRef(main)): %v", err)
	}
	defer byBranchRef.Close()
	requireMutationRows(t, byBranchRef, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	byBranchVersion, err := dataset.CheckoutRef(ctx, lance.BranchVersionRef("main", 1))
	if err != nil {
		t.Fatalf("CheckoutRef(BranchVersionRef(main, 1)): %v", err)
	}
	defer byBranchVersion.Close()
	requireMutationRows(t, byBranchVersion, []int64{1, 2}, []string{"a", "b"})

	if _, err := dataset.CheckoutBranch(ctx, ""); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CheckoutBranch(\"\") error = %v, want ErrInvalidArgument", err)
	}
	if _, err := dataset.CheckoutRef(ctx, lance.Ref{}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CheckoutRef(zero) error = %v, want ErrInvalidArgument", err)
	}
}

func TestRestoreCommitsNewVersion(t *testing.T) {
	dataset, uri, _ := newVersionedDataset(t)
	ctx := context.Background()

	restored, err := dataset.Restore(ctx, 1)
	if err != nil {
		t.Fatalf("Restore(1): %v", err)
	}
	defer restored.Close()
	requireMutationRows(t, restored, []int64{1, 2}, []string{"a", "b"})
	restoredVersion, err := restored.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("restored CurrentVersion: %v", err)
	}
	if restoredVersion.ID != 3 {
		t.Fatalf("restored version = %d, want 3", restoredVersion.ID)
	}

	// The source dataset is unchanged by Restore.
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if got := mutationVersion(t, dataset); got != 2 {
		t.Fatalf("source version after restore = %d, want 2", got)
	}

	// Restoring the current version still commits a new version.
	again, err := restored.Restore(ctx, 3)
	if err != nil {
		t.Fatalf("Restore(3): %v", err)
	}
	defer again.Close()
	requireMutationRows(t, again, []int64{1, 2}, []string{"a", "b"})
	if got := mutationVersion(t, again); got != 4 {
		t.Fatalf("unconditional restore version = %d, want 4", got)
	}
	if got := mutationVersion(t, restored); got != 3 {
		t.Fatalf("restored source version after second restore = %d, want 3", got)
	}

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()
	if got := mutationVersion(t, reopened); got != 4 {
		t.Fatalf("latest version after restores = %d, want 4", got)
	}
	requireMutationRows(t, reopened, []int64{1, 2}, []string{"a", "b"})

	if _, err := dataset.Restore(ctx, 0); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("Restore(0) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := dataset.Restore(ctx, 99); !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("Restore(99) error = %v, want ErrNotFound", err)
	}
}

func TestVersioningValidationCanceledAndClosed(t *testing.T) {
	dataset, _, _ := newVersionedDataset(t)
	ctx := context.Background()

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := dataset.Versions(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Versions(canceled) error = %v, want context.Canceled", err)
	}
	if _, err := dataset.CheckoutVersion(canceled, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckoutVersion(canceled) error = %v, want context.Canceled", err)
	}
	if _, err := dataset.Restore(canceled, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("Restore(canceled) error = %v, want context.Canceled", err)
	}
	if err := dataset.CreateTag(canceled, "tag", lance.VersionRef(1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateTag(canceled) error = %v, want context.Canceled", err)
	}
	if _, err := dataset.CreateBranch(canceled, "branch", lance.VersionRef(1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateBranch(canceled) error = %v, want context.Canceled", err)
	}

	if _, err := dataset.CheckoutBranch(ctx, "main\x00suffix"); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CheckoutBranch(NUL) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := dataset.CheckoutRef(ctx, lance.TagRef("tag\x00suffix")); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CheckoutRef(NUL tag) error = %v, want ErrInvalidArgument", err)
	}
	if err := dataset.CreateTag(ctx, "tag\x00suffix", lance.VersionRef(1)); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CreateTag(NUL) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := dataset.CreateBranch(ctx, "branch\x00suffix", lance.VersionRef(1)); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CreateBranch(NUL) error = %v, want ErrInvalidArgument", err)
	}
	if err := dataset.ReplaceBranchMetadata(ctx, "branch", map[string]string{"key": "value\x00suffix"}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("ReplaceBranchMetadata(NUL value) error = %v, want ErrInvalidArgument", err)
	}

	if err := dataset.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := dataset.CurrentVersion(ctx); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("CurrentVersion(closed) error = %v, want ErrClosed", err)
	}
	if _, err := dataset.Versions(ctx); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("Versions(closed) error = %v, want ErrClosed", err)
	}
	if _, err := dataset.CheckoutVersion(ctx, 1); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("CheckoutVersion(closed) error = %v, want ErrClosed", err)
	}
	if _, err := dataset.CheckoutBranch(ctx, "main"); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("CheckoutBranch(closed) error = %v, want ErrClosed", err)
	}
	if _, err := dataset.Restore(ctx, 1); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("Restore(closed) error = %v, want ErrClosed", err)
	}
	if _, err := dataset.Tags(ctx); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("Tags(closed) error = %v, want ErrClosed", err)
	}
	if _, err := dataset.Branches(ctx); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("Branches(closed) error = %v, want ErrClosed", err)
	}
	if _, err := dataset.CreateBranch(ctx, "branch", lance.VersionRef(1)); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("CreateBranch(closed) error = %v, want ErrClosed", err)
	}
}

func TestCheckoutsSurviveSourceClose(t *testing.T) {
	dataset, _, _ := newVersionedDataset(t)
	ctx := context.Background()

	first, err := dataset.CheckoutVersion(ctx, 1)
	if err != nil {
		t.Fatalf("CheckoutVersion(1): %v", err)
	}
	defer first.Close()
	main, err := dataset.CheckoutBranch(ctx, "main")
	if err != nil {
		t.Fatalf("CheckoutBranch(main): %v", err)
	}
	defer main.Close()

	if err := dataset.Close(); err != nil {
		t.Fatalf("Close source: %v", err)
	}
	requireMutationRows(t, first, []int64{1, 2}, []string{"a", "b"})
	requireMutationRows(t, main, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
}
