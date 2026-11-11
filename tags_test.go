package lance_test

import (
	"context"
	"errors"
	"maps"
	"testing"

	lance "github.com/lance-format/lance-go"
)

func TestTagsLifecycleAndMetadata(t *testing.T) {
	dataset, _, _ := newVersionedDataset(t)
	ctx := context.Background()

	versionsBefore, err := dataset.Versions(ctx)
	if err != nil {
		t.Fatalf("Versions before tags: %v", err)
	}

	if err := dataset.CreateTag(ctx, "beta", lance.VersionRef(1)); err != nil {
		t.Fatalf("CreateTag(beta): %v", err)
	}
	if err := dataset.CreateTag(ctx, "alpha", lance.VersionRef(2)); err != nil {
		t.Fatalf("CreateTag(alpha): %v", err)
	}

	tags, err := dataset.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 2 || tags[0].Name != "alpha" || tags[1].Name != "beta" {
		t.Fatalf("tags = %+v, want alpha then beta", tags)
	}
	alpha, beta := tags[0], tags[1]
	if alpha.Version != 2 || beta.Version != 1 {
		t.Fatalf("tag versions = alpha %d beta %d, want 2 and 1", alpha.Version, beta.Version)
	}
	if alpha.CreatedAt == nil || alpha.UpdatedAt == nil || beta.CreatedAt == nil || beta.UpdatedAt == nil {
		t.Fatalf("tag timestamps are nil: alpha=%+v beta=%+v", alpha, beta)
	}
	if !beta.CreatedAt.Equal(*beta.UpdatedAt) {
		t.Fatalf("create timestamps differ: created %v updated %v", beta.CreatedAt, beta.UpdatedAt)
	}
	if alpha.Metadata == nil || beta.Metadata == nil {
		t.Fatal("tag metadata is nil")
	}

	byTag, err := dataset.Tag(ctx, "beta")
	if err != nil {
		t.Fatalf("Tag(beta): %v", err)
	}
	byTag.Metadata["injected"] = "value"
	byTagAgain, err := dataset.Tag(ctx, "beta")
	if err != nil {
		t.Fatalf("Tag(beta) second call: %v", err)
	}
	if _, ok := byTagAgain.Metadata["injected"]; ok {
		t.Fatal("mutating returned tag metadata changed a later snapshot")
	}

	checkedOut, err := dataset.CheckoutTag(ctx, "beta")
	if err != nil {
		t.Fatalf("CheckoutTag(beta): %v", err)
	}
	defer checkedOut.Close()
	requireMutationRows(t, checkedOut, []int64{1, 2}, []string{"a", "b"})

	byRef, err := dataset.CheckoutRef(ctx, lance.TagRef("alpha"))
	if err != nil {
		t.Fatalf("CheckoutRef(TagRef(alpha)): %v", err)
	}
	defer byRef.Close()
	requireMutationRows(t, byRef, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	if err := dataset.CreateTag(ctx, "beta", lance.VersionRef(2)); !errors.Is(err, lance.ErrCommitConflict) {
		t.Fatalf("duplicate CreateTag error = %v, want ErrCommitConflict", err)
	}
	if err := dataset.CreateTag(ctx, "missing", lance.VersionRef(99)); !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("CreateTag(unknown version) error = %v, want ErrNotFound", err)
	}
	if err := dataset.CreateTag(ctx, "", lance.VersionRef(1)); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("CreateTag(empty name) error = %v, want ErrInvalidArgument", err)
	}

	beforeUpdate, err := dataset.Tag(ctx, "beta")
	if err != nil {
		t.Fatalf("Tag(beta) before update: %v", err)
	}
	if err := dataset.UpdateTag(ctx, "beta", lance.VersionRef(2)); err != nil {
		t.Fatalf("UpdateTag(beta): %v", err)
	}
	afterUpdate, err := dataset.Tag(ctx, "beta")
	if err != nil {
		t.Fatalf("Tag(beta) after update: %v", err)
	}
	if afterUpdate.Version != 2 {
		t.Fatalf("updated tag version = %d, want 2", afterUpdate.Version)
	}
	if afterUpdate.CreatedAt == nil || !afterUpdate.CreatedAt.Equal(*beforeUpdate.CreatedAt) {
		t.Fatalf("update changed created_at: before %v after %v", beforeUpdate.CreatedAt, afterUpdate.CreatedAt)
	}
	if afterUpdate.UpdatedAt == nil || afterUpdate.UpdatedAt.Before(*beforeUpdate.UpdatedAt) {
		t.Fatalf("update moved updated_at backwards: before %v after %v", beforeUpdate.UpdatedAt, afterUpdate.UpdatedAt)
	}
	if err := dataset.UpdateTag(ctx, "missing", lance.VersionRef(1)); !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("UpdateTag(missing) error = %v, want ErrNotFound", err)
	}

	wantMetadata := map[string]string{"env": "prod", "team": "data"}
	if err := dataset.ReplaceTagMetadata(ctx, "beta", wantMetadata); err != nil {
		t.Fatalf("ReplaceTagMetadata(beta): %v", err)
	}
	withMetadata, err := dataset.Tag(ctx, "beta")
	if err != nil {
		t.Fatalf("Tag(beta) with metadata: %v", err)
	}
	if !maps.Equal(withMetadata.Metadata, wantMetadata) {
		t.Fatalf("tag metadata = %v, want %v", withMetadata.Metadata, wantMetadata)
	}
	if err := dataset.ReplaceTagMetadata(ctx, "beta", map[string]string{}); err != nil {
		t.Fatalf("ReplaceTagMetadata(empty): %v", err)
	}
	cleared, err := dataset.Tag(ctx, "beta")
	if err != nil {
		t.Fatalf("Tag(beta) after clear: %v", err)
	}
	if len(cleared.Metadata) != 0 {
		t.Fatalf("tag metadata after clear = %v, want empty", cleared.Metadata)
	}
	if err := dataset.ReplaceTagMetadata(ctx, "beta", map[string]string{"": "value"}); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("ReplaceTagMetadata(empty key) error = %v, want ErrInvalidArgument", err)
	}

	if err := dataset.DeleteTag(ctx, "alpha"); err != nil {
		t.Fatalf("DeleteTag(alpha): %v", err)
	}
	remaining, err := dataset.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags after delete: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Name != "beta" {
		t.Fatalf("tags after delete = %+v, want beta", remaining)
	}
	if err := dataset.DeleteTag(ctx, "alpha"); !errors.Is(err, lance.ErrNotFound) {
		t.Fatalf("DeleteTag(missing) error = %v, want ErrNotFound", err)
	}
	if err := dataset.DeleteTag(ctx, ""); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("DeleteTag(empty) error = %v, want ErrInvalidArgument", err)
	}

	// Tag operations are catalog-only and do not change the source snapshot.
	requireMutationRows(t, dataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})

	versionsAfter, err := dataset.Versions(ctx)
	if err != nil {
		t.Fatalf("Versions after tags: %v", err)
	}
	if len(versionsAfter) != len(versionsBefore) {
		t.Fatalf("tag operations changed version count: before %d after %d", len(versionsBefore), len(versionsAfter))
	}
	for i := range versionsAfter {
		if versionsAfter[i].ID != versionsBefore[i].ID {
			t.Fatalf("tag operations changed version %d id: before %d after %d", i, versionsBefore[i].ID, versionsAfter[i].ID)
		}
	}
}
