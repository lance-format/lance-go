package lance

import (
	"context"
	"time"

	"github.com/lance-format/lance-go/internal/ffi"
)

// Tag is a named pointer to a dataset version.
//
// Tags are stored in a catalog shared by every dataset that uses the same
// storage location. A Tag is a copied snapshot and remains valid after the
// Dataset is closed.
type Tag struct {
	Name         string
	Branch       string
	Version      uint64
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
	ManifestSize uint64
	Metadata     map[string]string
}

// Tags lists the tags visible to this dataset, ordered by name.
func (d *Dataset) Tags(ctx context.Context) ([]Tag, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire("tags"); err != nil {
		return nil, err
	}
	defer d.res.release()

	infos, err := d.native.Tags()
	if err != nil {
		return nil, wrapError("tags", d.uri, err)
	}
	tags := make([]Tag, len(infos))
	for i, info := range infos {
		tags[i] = tagFromFFI(info)
	}
	return tags, nil
}

// Tag returns one tag by name.
func (d *Dataset) Tag(ctx context.Context, name string) (Tag, error) {
	if err := checkContext(ctx); err != nil {
		return Tag{}, err
	}
	if err := validateResourceName("get tag", d.uri, "tag", name); err != nil {
		return Tag{}, err
	}
	if err := d.acquire("get tag"); err != nil {
		return Tag{}, err
	}
	defer d.res.release()

	info, err := d.native.Tag(name)
	if err != nil {
		return Tag{}, wrapError("get tag", d.uri, err)
	}
	return tagFromFFI(info), nil
}

// CreateTag creates a tag pointing at ref.
//
// Tags are shared catalog state: other open datasets observe the tag through
// their next catalog read, not through a dataset-local snapshot.
func (d *Dataset) CreateTag(ctx context.Context, name string, ref Ref) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := validateResourceName("create tag", d.uri, "tag", name); err != nil {
		return err
	}
	if err := ref.validate("create tag", d.uri); err != nil {
		return err
	}
	if err := d.acquire("create tag"); err != nil {
		return err
	}
	defer d.res.release()

	if err := d.native.CreateTag(name, ref.toFFI()); err != nil {
		return wrapError("create tag", d.uri, err)
	}
	return nil
}

// UpdateTag points an existing tag at ref and preserves its creation time.
func (d *Dataset) UpdateTag(ctx context.Context, name string, ref Ref) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := validateResourceName("update tag", d.uri, "tag", name); err != nil {
		return err
	}
	if err := ref.validate("update tag", d.uri); err != nil {
		return err
	}
	if err := d.acquire("update tag"); err != nil {
		return err
	}
	defer d.res.release()

	if err := d.native.UpdateTag(name, ref.toFFI()); err != nil {
		return wrapError("update tag", d.uri, err)
	}
	return nil
}

// DeleteTag removes an existing tag.
func (d *Dataset) DeleteTag(ctx context.Context, name string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := validateResourceName("delete tag", d.uri, "tag", name); err != nil {
		return err
	}
	if err := d.acquire("delete tag"); err != nil {
		return err
	}
	defer d.res.release()

	if err := d.native.DeleteTag(name); err != nil {
		return wrapError("delete tag", d.uri, err)
	}
	return nil
}

// ReplaceTagMetadata replaces all metadata on an existing tag. An empty map
// removes every metadata entry.
func (d *Dataset) ReplaceTagMetadata(ctx context.Context, name string, metadata map[string]string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := validateResourceName("replace tag metadata", d.uri, "tag", name); err != nil {
		return err
	}
	if err := validateMetadata("replace tag metadata", d.uri, metadata); err != nil {
		return err
	}
	if err := d.acquire("replace tag metadata"); err != nil {
		return err
	}
	defer d.res.release()

	if err := d.native.ReplaceTagMetadata(name, metadata); err != nil {
		return wrapError("replace tag metadata", d.uri, err)
	}
	return nil
}

func tagFromFFI(info ffi.TagInfo) Tag {
	return Tag{
		Name:         info.Name,
		Branch:       info.Branch,
		Version:      info.Version,
		CreatedAt:    info.CreatedAt,
		UpdatedAt:    info.UpdatedAt,
		ManifestSize: info.ManifestSize,
		Metadata:     copyMetadata(info.Metadata),
	}
}
