package lance

import (
	"context"
	"strings"

	"github.com/lance-format/lance-go/internal/ffi"
)

// Ref identifies a dataset state for CheckoutRef and branch or tag creation.
//
// Construct references with VersionRef, BranchRef, BranchVersionRef, or TagRef.
// The zero Ref is invalid and is rejected by native calls.
type Ref struct {
	kind    ffi.RefKind
	version uint64
	branch  string
	tag     string
}

// VersionRef selects a version number resolved on the branch the receiver is
// checked out on. On a main-branch dataset this is the absolute dataset
// version; on another branch it is that branch's version number.
func VersionRef(version uint64) Ref {
	return Ref{kind: ffi.RefVersionNumber, version: version}
}

// BranchRef selects the latest version of a branch. The special name "main"
// is normalized by native code.
func BranchRef(name string) Ref {
	return Ref{kind: ffi.RefBranchLatest, branch: name}
}

// BranchVersionRef selects a specific version on a branch. version is relative
// to the branch and is not an absolute dataset version.
func BranchVersionRef(name string, version uint64) Ref {
	return Ref{kind: ffi.RefBranchVersion, branch: name, version: version}
}

// TagRef selects the version recorded by a tag.
func TagRef(name string) Ref {
	return Ref{kind: ffi.RefTag, tag: name}
}

func (r Ref) toFFI() ffi.Ref {
	return ffi.Ref{
		Kind:    r.kind,
		Version: r.version,
		Branch:  r.branch,
		Tag:     r.tag,
	}
}

// CheckoutVersion returns an independent dataset at version, resolved on the
// branch the receiver is checked out on. On a main-branch dataset version is
// the absolute dataset version.
//
// The returned dataset owns its own native handle and must be closed by the
// caller. The receiver remains open and unchanged.
func (d *Dataset) CheckoutVersion(ctx context.Context, version uint64) (*Dataset, error) {
	return d.checkout(ctx, "checkout version", VersionRef(version))
}

// CheckoutTag returns an independent dataset at the version recorded by tag.
func (d *Dataset) CheckoutTag(ctx context.Context, tag string) (*Dataset, error) {
	return d.checkout(ctx, "checkout tag", TagRef(tag))
}

// CheckoutBranch returns an independent dataset at the latest version of
// branch. branch must not be empty; "main" is normalized by native code.
func (d *Dataset) CheckoutBranch(ctx context.Context, branch string) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateResourceName("checkout branch", d.uri, "branch", branch); err != nil {
		return nil, err
	}
	if err := d.acquire("checkout branch"); err != nil {
		return nil, err
	}
	defer d.res.release()

	native, err := d.native.CheckoutBranch(branch)
	if err != nil {
		return nil, wrapError("checkout branch", d.uri, err)
	}
	return d.adopt(native), nil
}

// CheckoutRef returns an independent dataset at ref.
func (d *Dataset) CheckoutRef(ctx context.Context, ref Ref) (*Dataset, error) {
	return d.checkout(ctx, "checkout ref", ref)
}

func (d *Dataset) checkout(ctx context.Context, op string, ref Ref) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := ref.validate(op, d.uri); err != nil {
		return nil, err
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()

	native, err := d.native.CheckoutRef(ref.toFFI())
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	return d.adopt(native), nil
}

func validateResourceName(op, uri, kind, name string) error {
	if name == "" {
		return invalidError(op, uri, "%s name must not be empty", kind)
	}
	if strings.IndexByte(name, 0) >= 0 {
		return invalidError(op, uri, "%s name must not contain NUL", kind)
	}
	return nil
}

func (r Ref) validate(op, uri string) error {
	switch r.kind {
	case ffi.RefVersionNumber:
		return nil
	case ffi.RefBranchVersion, ffi.RefBranchLatest:
		return validateResourceName(op, uri, "branch", r.branch)
	case ffi.RefTag:
		return validateResourceName(op, uri, "tag", r.tag)
	default:
		return invalidError(op, uri, "invalid reference")
	}
}

func validateMetadata(op, uri string, metadata map[string]string) error {
	for key, value := range metadata {
		if key == "" {
			return invalidError(op, uri, "metadata key must not be empty")
		}
		if strings.IndexByte(key, 0) >= 0 {
			return invalidError(op, uri, "metadata key must not contain NUL")
		}
		if strings.IndexByte(value, 0) >= 0 {
			return invalidError(op, uri, "metadata value for %q must not contain NUL", key)
		}
	}
	return nil
}
