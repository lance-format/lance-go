package ffi

import (
	"fmt"
	"runtime"
	"sort"
	"time"
	"unsafe"
)

// TagInfo is a copied snapshot of one named tag.
type TagInfo struct {
	Name         string
	Branch       string
	Version      uint64
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
	ManifestSize uint64
	Metadata     map[string]string
}

// Tags snapshots all tags. The result is ordered by name.
func (d *Dataset) Tags() ([]TagInfo, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	tags := b.datasetTags(handle)
	if tags == 0 {
		return nil, nativeNullError(b, "lance_dataset_tags")
	}
	defer b.tagsClose(tags)

	count := b.tagsCount(tags)
	if err := b.takeError(); err != nil {
		return nil, err
	}
	result := make([]TagInfo, 0, count)
	for i := uint64(0); i < count; i++ {
		tag := b.tagsAt(tags, i)
		if tag == 0 {
			return nil, nativeNullError(b, "lance_tags_at")
		}
		info, err := tagInfoLocked(b, tag)
		if err != nil {
			return nil, err
		}
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// Tag fetches one tag by name.
func (d *Dataset) Tag(name string) (TagInfo, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return TagInfo{}, ErrClosed
	}
	if err := validateRefName(name, "tag"); err != nil {
		return TagInfo{}, err
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	tag := b.datasetTagGet(handle, name)
	if tag == 0 {
		return TagInfo{}, nativeNullError(b, "lance_dataset_tag_get")
	}
	defer b.tagClose(tag)
	return tagInfoLocked(b, tag)
}

// CreateTag creates a tag targeting ref.
func (d *Dataset) CreateTag(name string, ref Ref) error {
	return d.tagRefAction("lance_dataset_tag_create", name, ref, func(handle, nativeRef uintptr) int32 {
		return d.b.datasetTagCreate(handle, name, nativeRef)
	})
}

// UpdateTag retargets an existing tag to ref.
func (d *Dataset) UpdateTag(name string, ref Ref) error {
	return d.tagRefAction("lance_dataset_tag_update", name, ref, func(handle, nativeRef uintptr) int32 {
		return d.b.datasetTagUpdate(handle, name, nativeRef)
	})
}

// DeleteTag removes one tag.
func (d *Dataset) DeleteTag(name string) error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}
	if err := validateRefName(name, "tag"); err != nil {
		return err
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if rc := b.datasetTagDelete(handle, name); rc != 0 {
		return nativeActionError(b, "lance_dataset_tag_delete", rc)
	}
	return nil
}

// ReplaceTagMetadata replaces all metadata on an existing tag.
func (d *Dataset) ReplaceTagMetadata(name string, metadata map[string]string) error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}
	if err := validateRefName(name, "tag"); err != nil {
		return err
	}
	list, err := metadataList(metadata)
	if err != nil {
		return err
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var values []uintptr
	if list != nil {
		list.keepAlive()
		values = list.ptrs
	}
	if rc := b.datasetTagReplaceMetadata(handle, name, values); rc != 0 {
		return nativeActionError(b, "lance_dataset_tag_replace_metadata", rc)
	}
	return nil
}

func (d *Dataset) tagRefAction(op, name string, ref Ref, call func(handle, nativeRef uintptr) int32) error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}
	if err := validateRefName(name, "tag"); err != nil {
		return err
	}
	if err := ref.validate(); err != nil {
		return err
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	nativeRef, err := b.newReferenceLocked(ref)
	if err != nil {
		return err
	}
	rc := call(handle, nativeRef)
	b.refClose(nativeRef)
	if rc != 0 {
		return nativeActionError(b, op, rc)
	}
	return nil
}

func tagInfoLocked(b *bindings, tag uintptr) (TagInfo, error) {
	namePtr := b.tagName(tag)
	if namePtr == 0 {
		return TagInfo{}, nativeNullError(b, "lance_tag_name")
	}
	info := TagInfo{Name: goString((*byte)(unsafe.Pointer(namePtr)))}

	info.Version = b.tagVersion(tag)
	if err := b.takeError(); err != nil {
		return TagInfo{}, err
	}

	var branchPtr uintptr
	rc := b.tagBranch(tag, &branchPtr)
	if err := b.takeError(); err != nil {
		return TagInfo{}, err
	}
	switch rc {
	case 0:
	case 1:
		if branchPtr == 0 {
			return TagInfo{}, fmt.Errorf("ffi: lance_tag_branch returned a NULL branch without setting an error")
		}
		info.Branch = goString((*byte)(unsafe.Pointer(branchPtr)))
	default:
		return TagInfo{}, fmt.Errorf("ffi: lance_tag_branch returned %d without setting an error", rc)
	}

	createdAt, err := tagTimestampLocked(b, "lance_tag_created_at_ns", tag, b.tagCreatedAtNS)
	if err != nil {
		return TagInfo{}, err
	}
	info.CreatedAt = createdAt

	updatedAt, err := tagTimestampLocked(b, "lance_tag_updated_at_ns", tag, b.tagUpdatedAtNS)
	if err != nil {
		return TagInfo{}, err
	}
	info.UpdatedAt = updatedAt

	info.ManifestSize = b.tagManifestSize(tag)
	if err := b.takeError(); err != nil {
		return TagInfo{}, err
	}

	metadata, err := tagMetadataLocked(b, tag)
	if err != nil {
		return TagInfo{}, err
	}
	info.Metadata = metadata
	return info, nil
}

func tagTimestampLocked(b *bindings, op string, tag uintptr, accessor func(uintptr, *int64) int32) (*time.Time, error) {
	var timestampNS int64
	rc := accessor(tag, &timestampNS)
	if err := b.takeError(); err != nil {
		return nil, err
	}
	switch rc {
	case 0:
		return nil, nil
	case 1:
		timestamp := time.Unix(0, timestampNS).UTC()
		return &timestamp, nil
	default:
		return nil, fmt.Errorf("ffi: %s returned %d without setting an error", op, rc)
	}
}

func tagMetadataLocked(b *bindings, tag uintptr) (map[string]string, error) {
	count := b.tagMetadataCount(tag)
	if err := b.takeError(); err != nil {
		return nil, err
	}
	metadata := make(map[string]string, count)
	for i := uint64(0); i < count; i++ {
		keyPtr := b.tagMetadataKeyAt(tag, i)
		if keyPtr == 0 {
			return nil, nativeNullError(b, "lance_tag_metadata_key_at")
		}
		key := goString((*byte)(unsafe.Pointer(keyPtr)))
		valuePtr := b.tagMetadataValueAt(tag, i)
		if valuePtr == 0 {
			return nil, nativeNullError(b, "lance_tag_metadata_value_at")
		}
		metadata[key] = goString((*byte)(unsafe.Pointer(valuePtr)))
	}
	return metadata, nil
}

func validateRefName(name, kind string) error {
	if name == "" {
		return fmt.Errorf("%s name must not be empty", kind)
	}
	return validateCString(name)
}

func metadataList(metadata map[string]string) (*cStringList, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(metadata))
	for key, value := range metadata {
		if key == "" {
			return nil, fmt.Errorf("metadata key must not be empty")
		}
		if err := validateCString(key); err != nil {
			return nil, fmt.Errorf("metadata key %q: %w", key, err)
		}
		if err := validateCString(value); err != nil {
			return nil, fmt.Errorf("metadata value for %q: %w", key, err)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		values = append(values, key, metadata[key])
	}
	return newCStringList(values)
}
