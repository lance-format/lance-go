package ffi

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"
)

// VersionInfo is one snapshot of a committed dataset version.
type VersionInfo struct {
	ID        uint64
	Timestamp time.Time
	Metadata  map[string]string
}

// CurrentVersion reports the version checked out by this handle.
func (d *Dataset) CurrentVersion() (VersionInfo, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return VersionInfo{}, ErrClosed
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	version := b.datasetCurrentVersion(handle)
	if version == 0 {
		return VersionInfo{}, nativeNullError(b, "lance_dataset_current_version")
	}
	defer b.versionClose(version)

	id := b.versionID(version)
	if err := b.takeError(); err != nil {
		return VersionInfo{}, err
	}
	timestampNS := b.versionTimestampNS(version)
	if err := b.takeError(); err != nil {
		return VersionInfo{}, err
	}
	metadata, err := versionMetadataLocked(b, version)
	if err != nil {
		return VersionInfo{}, err
	}
	return VersionInfo{
		ID:        id,
		Timestamp: time.Unix(0, timestampNS).UTC(),
		Metadata:  metadata,
	}, nil
}

// Versions snapshots the committed version history in ascending version order.
func (d *Dataset) Versions() ([]VersionInfo, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	versions := b.datasetVersions(handle)
	if versions == 0 {
		return nil, nativeNullError(b, "lance_dataset_versions")
	}
	defer b.versionsClose(versions)

	count := b.versionsCount(versions)
	if err := b.takeError(); err != nil {
		return nil, err
	}
	result := make([]VersionInfo, 0, count)
	for i := uint64(0); i < count; i++ {
		id := b.versionsIDAt(versions, i)
		if err := b.takeError(); err != nil {
			return nil, err
		}
		timestampMS := b.versionsTimestampMSAt(versions, i)
		if err := b.takeError(); err != nil {
			return nil, err
		}
		version := b.versionsAt(versions, i)
		if version == 0 {
			return nil, nativeNullError(b, "lance_versions_at")
		}
		metadata, err := versionMetadataLocked(b, version)
		if err != nil {
			return nil, err
		}
		result = append(result, VersionInfo{
			ID:        id,
			Timestamp: time.UnixMilli(int64(timestampMS)).UTC(),
			Metadata:  metadata,
		})
	}
	return result, nil
}

// VersionRefs snapshots the lightweight manifest reference ids.
func (d *Dataset) VersionRefs() ([]uint64, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	refs := b.datasetVersionRefs(handle)
	if refs == 0 {
		return nil, nativeNullError(b, "lance_dataset_version_refs")
	}
	defer b.versionRefsClose(refs)

	count := b.versionRefsCount(refs)
	if err := b.takeError(); err != nil {
		return nil, err
	}
	result := make([]uint64, 0, count)
	for i := uint64(0); i < count; i++ {
		id := b.versionRefsIDAt(refs, i)
		if err := b.takeError(); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, nil
}

// RefKind identifies the native reference variant carried by Ref.
type RefKind uint8

const (
	// RefVersionNumber addresses a version number on the current branch.
	RefVersionNumber RefKind = iota + 1
	// RefBranchVersion addresses a version number on a named branch.
	RefBranchVersion
	// RefBranchLatest addresses the latest version of a named branch.
	RefBranchLatest
	// RefTag addresses the version named by a tag.
	RefTag
)

// Ref is a lightweight native version reference. Build values with its
// constructors; the zero value is invalid.
type Ref struct {
	Kind    RefKind
	Version uint64
	Branch  string
	Tag     string
}

func (r Ref) validate() error {
	switch r.Kind {
	case RefVersionNumber:
		return nil
	case RefBranchVersion, RefBranchLatest:
		if r.Branch == "" {
			return fmt.Errorf("branch name must not be empty")
		}
		return validateCString(r.Branch)
	case RefTag:
		if r.Tag == "" {
			return fmt.Errorf("tag name must not be empty")
		}
		return validateCString(r.Tag)
	default:
		return fmt.Errorf("invalid reference kind %d", r.Kind)
	}
}

// newReferenceLocked builds a native reference. The caller must hold the OS
// thread for the lifetime of the returned handle.
func (b *bindings) newReferenceLocked(ref Ref) (uintptr, error) {
	var native uintptr
	switch ref.Kind {
	case RefVersionNumber:
		native = b.refVersionNumber(ref.Version)
	case RefBranchVersion:
		native = b.refVersion(ref.Branch, true, ref.Version)
	case RefBranchLatest:
		native = b.refVersion(ref.Branch, false, 0)
	case RefTag:
		native = b.refTag(ref.Tag)
	default:
		return 0, fmt.Errorf("ffi: invalid reference kind %d", ref.Kind)
	}
	if native == 0 {
		return 0, nativeNullError(b, "native reference construction")
	}
	return native, nil
}

// CheckoutRef returns an independent dataset checked out at ref.
func (d *Dataset) CheckoutRef(ref Ref) (*Dataset, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	if err := ref.validate(); err != nil {
		return nil, err
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	nativeRef, err := b.newReferenceLocked(ref)
	if err != nil {
		return nil, err
	}
	defer b.refClose(nativeRef)

	next := b.datasetCheckoutRef(handle, nativeRef)
	if next == 0 {
		return nil, nativeNullError(b, "lance_dataset_checkout_ref")
	}
	return datasetFromHandle(b, next), nil
}

// CheckoutBranch returns an independent dataset at the latest branch version.
func (d *Dataset) CheckoutBranch(branch string) (*Dataset, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	if branch == "" {
		return nil, fmt.Errorf("ffi: branch name must not be empty")
	}
	if err := validateCString(branch); err != nil {
		return nil, err
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	next := b.datasetCheckoutBranch(handle, branch)
	if next == 0 {
		return nil, nativeNullError(b, "lance_dataset_checkout_branch")
	}
	return datasetFromHandle(b, next), nil
}

// Restore commits a new version whose content is the requested version and
// returns an independent dataset at that new commit.
func (d *Dataset) Restore(version uint64) (*Dataset, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	if version == 0 {
		return nil, fmt.Errorf("ffi: restore version must be greater than zero")
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	restored := b.datasetRestore(handle, version)
	if restored == 0 {
		return nil, nativeNullError(b, "lance_dataset_restore")
	}
	return datasetFromHandle(b, restored), nil
}

func versionMetadataLocked(b *bindings, version uintptr) (map[string]string, error) {
	count := b.versionMetadataCount(version)
	if err := b.takeError(); err != nil {
		return nil, err
	}
	metadata := make(map[string]string, count)
	for i := uint64(0); i < count; i++ {
		keyPtr := b.versionMetadataKeyAt(version, i)
		if keyPtr == 0 {
			return nil, nativeNullError(b, "lance_version_metadata_key_at")
		}
		key := goString((*byte)(unsafe.Pointer(keyPtr)))
		valuePtr := b.versionMetadataValueAt(version, i)
		if valuePtr == 0 {
			return nil, nativeNullError(b, "lance_version_metadata_value_at")
		}
		metadata[key] = goString((*byte)(unsafe.Pointer(valuePtr)))
	}
	return metadata, nil
}

func datasetFromHandle(b *bindings, handle uintptr) *Dataset {
	out := &Dataset{b: b}
	out.handle.Store(handle)
	return out
}

func nativeNullError(b *bindings, op string) error {
	if err := b.takeError(); err != nil {
		return err
	}
	return fmt.Errorf("ffi: %s returned NULL without setting an error", op)
}

func nativeActionError(b *bindings, op string, rc int32) error {
	if err := b.takeError(); err != nil {
		return err
	}
	return fmt.Errorf("ffi: %s returned %d without setting an error", op, rc)
}
