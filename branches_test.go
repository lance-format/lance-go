package lance_test

import (
	"context"
	"errors"
	"testing"

	lance "github.com/lance-format/lance-go"
)

func branchNames(branches []lance.Branch) []string {
	names := make([]string, len(branches))
	for i, branch := range branches {
		names[i] = branch.Name
	}
	return names
}

func TestBranchesLifecycleAndCheckout(t *testing.T) {
	dataset, _, _ := newVersionedDataset(t)
	ctx := context.Background()

	versionsBefore, err := dataset.Versions(ctx)
	if err != nil {
		t.Fatalf("Versions before branches: %v", err)
	}

	// main is an implicit branch: it is valid for checkout and refs, but it has
	// no BranchContents entry and does not appear in the native branch list.
	branches, err := dataset.Branches(ctx)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if len(branches) != 0 {
		t.Fatalf("initial branches = %v, want none", branchNames(branches))
	}
	if _, err := dataset.Branch(ctx, "main"); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("Branch(main) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := dataset.CreateBranch(ctx, "main", lance.VersionRef(1)); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CreateBranch(main) error = %v, want ErrInvalidArgument", err)
	}

	// A branch from a main version has no parent branch, but records the source
	// version and a concrete identifier.
	dev, err := dataset.CreateBranch(ctx, "dev", lance.VersionRef(1))
	if err != nil {
		t.Fatalf("CreateBranch(dev): %v", err)
	}
	defer dev.Close()
	requireMutationRows(t, dev, []int64{1, 2}, []string{"a", "b"})

	devInfo, err := dataset.Branch(ctx, "dev")
	if err != nil {
		t.Fatalf("Branch(dev): %v", err)
	}
	if devInfo.ParentBranch != "" {
		t.Fatalf("dev.ParentBranch = %q, want empty for a main-rooted branch", devInfo.ParentBranch)
	}
	if devInfo.ParentVersion != 1 {
		t.Fatalf("dev.ParentVersion = %d, want 1", devInfo.ParentVersion)
	}
	if devInfo.CreateAt.IsZero() {
		t.Fatal("dev.CreateAt is zero")
	}
	if devInfo.Metadata == nil {
		t.Fatal("dev.Metadata is nil")
	}
	if len(devInfo.Identifier.VersionMapping) == 0 {
		t.Fatal("dev.Identifier.VersionMapping is empty")
	}
	devIdentifier, err := dev.CurrentBranchIdentifier(ctx)
	if err != nil {
		t.Fatalf("dev.CurrentBranchIdentifier: %v", err)
	}
	if len(devIdentifier.VersionMapping) == 0 {
		t.Fatal("dev current branch identifier has no version mappings")
	}

	release, err := dataset.CreateBranch(ctx, "release", lance.BranchRef("main"))
	if err != nil {
		t.Fatalf("CreateBranch(release): %v", err)
	}
	defer release.Close()
	requireMutationRows(t, release, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	// Creating from a branch records that branch as the parent.
	child, err := dev.CreateBranch(ctx, "child", lance.BranchRef("dev"))
	if err != nil {
		t.Fatalf("CreateBranch(child): %v", err)
	}
	defer child.Close()
	requireMutationRows(t, child, []int64{1, 2}, []string{"a", "b"})
	childInfo, err := dataset.Branch(ctx, "child")
	if err != nil {
		t.Fatalf("Branch(child): %v", err)
	}
	if childInfo.ParentBranch != "dev" {
		t.Fatalf("child.ParentBranch = %q, want dev", childInfo.ParentBranch)
	}

	// Version-number checkouts are resolved on the receiver's branch.
	devFirst, err := dev.CheckoutVersion(ctx, 1)
	if err != nil {
		t.Fatalf("dev.CheckoutVersion(1): %v", err)
	}
	defer devFirst.Close()
	requireMutationRows(t, devFirst, []int64{1, 2}, []string{"a", "b"})

	checkout, err := dataset.CheckoutBranch(ctx, "release")
	if err != nil {
		t.Fatalf("CheckoutBranch(release): %v", err)
	}
	defer checkout.Close()
	requireMutationRows(t, checkout, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	branchVersion, err := dataset.CheckoutRef(ctx, lance.BranchVersionRef("dev", 1))
	if err != nil {
		t.Fatalf("CheckoutRef(BranchVersionRef(dev, 1)): %v", err)
	}
	defer branchVersion.Close()
	requireMutationRows(t, branchVersion, []int64{1, 2}, []string{"a", "b"})

	wantMetadata := map[string]string{"owner": "team", "purpose": "testing"}
	if err := dataset.ReplaceBranchMetadata(ctx, "dev", wantMetadata); err != nil {
		t.Fatalf("ReplaceBranchMetadata(dev): %v", err)
	}
	withMetadata, err := dataset.Branch(ctx, "dev")
	if err != nil {
		t.Fatalf("Branch(dev) with metadata: %v", err)
	}
	for key, value := range wantMetadata {
		if withMetadata.Metadata[key] != value {
			t.Fatalf("dev metadata[%q] = %q, want %q", key, withMetadata.Metadata[key], value)
		}
	}
	if err := dataset.ReplaceBranchMetadata(ctx, "dev", map[string]string{}); err != nil {
		t.Fatalf("ReplaceBranchMetadata(empty): %v", err)
	}
	cleared, err := dataset.Branch(ctx, "dev")
	if err != nil {
		t.Fatalf("Branch(dev) after clear: %v", err)
	}
	if len(cleared.Metadata) != 0 {
		t.Fatalf("dev metadata after clear = %v, want empty", cleared.Metadata)
	}
	if err := dataset.ReplaceBranchMetadata(ctx, "dev", map[string]string{"": "value"}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("ReplaceBranchMetadata(empty key) error = %v, want ErrInvalidArgument", err)
	}

	// main is reserved and cannot be deleted as an explicit branch.
	if err := dataset.DeleteBranch(ctx, "main"); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("DeleteBranch(main) error = %v, want ErrInvalidArgument", err)
	}

	// A child branch protects its parent from deletion, including force delete.
	if err := dataset.DeleteBranch(ctx, "dev"); !errors.Is(err, lance.ErrCommitConflict) {
		t.Fatalf("DeleteBranch(dev with child) error = %v, want ErrCommitConflict", err)
	}
	if err := dataset.ForceDeleteBranch(ctx, "dev"); !errors.Is(err, lance.ErrCommitConflict) {
		t.Fatalf("ForceDeleteBranch(dev with child) error = %v, want ErrCommitConflict", err)
	}

	// A tag pointing at the branch also protects it.
	if err := dataset.CreateTag(ctx, "dev-tag", lance.BranchVersionRef("dev", 1)); err != nil {
		t.Fatalf("CreateTag(dev-tag): %v", err)
	}

	if err := child.Close(); err != nil {
		t.Fatalf("Close child: %v", err)
	}
	if err := dataset.DeleteBranch(ctx, "child"); err != nil {
		t.Fatalf("DeleteBranch(child): %v", err)
	}
	if err := dataset.DeleteBranch(ctx, "dev"); !errors.Is(err, lance.ErrCommitConflict) {
		t.Fatalf("DeleteBranch(dev with tag) error = %v, want ErrCommitConflict", err)
	}
	if err := dataset.DeleteTag(ctx, "dev-tag"); err != nil {
		t.Fatalf("DeleteTag(dev-tag): %v", err)
	}
	if err := dev.Close(); err != nil {
		t.Fatalf("Close dev: %v", err)
	}
	if err := dataset.DeleteBranch(ctx, "dev"); err != nil {
		t.Fatalf("DeleteBranch(dev): %v", err)
	}
	if err := release.Close(); err != nil {
		t.Fatalf("Close release: %v", err)
	}
	if err := dataset.ForceDeleteBranch(ctx, "release"); err != nil {
		t.Fatalf("ForceDeleteBranch(release): %v", err)
	}
	if err := dataset.DeleteBranch(ctx, "missing"); !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("DeleteBranch(missing) error = %v, want ErrNotFound", err)
	}
	// Force delete also recovers a branch whose BranchContents is already gone.
	if err := dataset.ForceDeleteBranch(ctx, "missing"); err != nil {
		t.Fatalf("ForceDeleteBranch(missing): %v", err)
	}

	remaining, err := dataset.Branches(ctx)
	if err != nil {
		t.Fatalf("Branches after delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("branches after delete = %v, want none", branchNames(remaining))
	}

	// Branch operations are catalog-only and do not change the source snapshot.
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	versionsAfter, err := dataset.Versions(ctx)
	if err != nil {
		t.Fatalf("Versions after branches: %v", err)
	}
	if len(versionsAfter) != len(versionsBefore) {
		t.Fatalf("branch operations changed main version count: before %d after %d", len(versionsBefore), len(versionsAfter))
	}
	for i := range versionsAfter {
		if versionsAfter[i].ID != versionsBefore[i].ID {
			t.Fatalf("branch operations changed main version %d id: before %d after %d", i, versionsBefore[i].ID, versionsAfter[i].ID)
		}
	}
}

func TestBranchesOptionsAndValidation(t *testing.T) {
	dataset, _, _ := newVersionedDataset(t)
	ctx := context.Background()

	inherited, err := dataset.CreateBranch(ctx, "inherit", lance.BranchRef("main"), lance.WithBranchStorageOptions(nil))
	if err != nil {
		t.Fatalf("CreateBranch(inherit, nil storage options): %v", err)
	}
	requireMutationRows(t, inherited, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if err := inherited.Close(); err != nil {
		t.Fatalf("Close inherit: %v", err)
	}
	if err := dataset.DeleteBranch(ctx, "inherit"); err != nil {
		t.Fatalf("DeleteBranch(inherit): %v", err)
	}

	explicit, err := dataset.CreateBranch(ctx, "explicit", lance.BranchRef("main"), lance.WithBranchStorageOptions(map[string]string{}))
	if err != nil {
		t.Fatalf("CreateBranch(explicit empty storage options): %v", err)
	}
	requireMutationRows(t, explicit, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	if err := explicit.Close(); err != nil {
		t.Fatalf("Close explicit: %v", err)
	}
	if err := dataset.DeleteBranch(ctx, "explicit"); err != nil {
		t.Fatalf("DeleteBranch(explicit): %v", err)
	}

	if _, err := dataset.CreateBranch(ctx, "bad", lance.BranchRef("main"), lance.WithBranchStorageOptions(map[string]string{"": "value"})); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CreateBranch(empty storage key) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := dataset.CreateBranch(ctx, "", lance.BranchRef("main")); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CreateBranch(empty name) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := dataset.CreateBranch(ctx, "bad", lance.BranchRef("")); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CreateBranch(empty ref) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := dataset.Branch(ctx, ""); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("Branch(empty) error = %v, want ErrInvalidArgument", err)
	}
	if err := dataset.DeleteBranch(ctx, ""); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("DeleteBranch(empty) error = %v, want ErrInvalidArgument", err)
	}
}
