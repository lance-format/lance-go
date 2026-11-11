package lance

import (
	"context"
	"time"

	"github.com/lance-format/lance-go/internal/ffi"
)

// BranchVersionMapping records one version-to-manifest mapping in a branch
// identifier. UUID is empty when the native manifest reference has no UUID.
type BranchVersionMapping struct {
	Version uint64
	UUID    string
}

// BranchIdentifier maps a branch's versions to the manifest UUIDs they were
// cloned from.
type BranchIdentifier struct {
	VersionMapping []BranchVersionMapping
}

// Branch is a named, writable line of dataset history.
//
// ParentBranch is empty for a root branch. CreateAt has second precision
// because the native branch representation stores Unix seconds. Metadata and
// Identifier are copies and remain valid after the Dataset is closed.
type Branch struct {
	Name          string
	ParentBranch  string
	ParentVersion uint64
	CreateAt      time.Time
	ManifestSize  uint64
	Metadata      map[string]string
	Identifier    BranchIdentifier
}

// BranchOption configures CreateBranch.
type BranchOption func(*branchOptions) error

type branchOptions struct {
	storage *ffi.BranchOptions
}

// WithBranchStorageOptions passes storage options to native branch creation.
//
// A nil options map inherits the source dataset's store parameters, matching
// CreateBranch without this option. A non-nil empty map explicitly passes an
// empty parameter set, which is distinct from inheriting.
func WithBranchStorageOptions(options map[string]string) BranchOption {
	return func(target *branchOptions) error {
		if options == nil {
			return nil
		}
		copied, err := copyStorageOptions(options)
		if err != nil {
			return err
		}
		target.storage = &ffi.BranchOptions{StorageOptions: copied}
		return nil
	}
}

// Branches lists the explicit branches recorded in the shared catalog,
// ordered by name. The implicit main branch is not included; main is always
// addressable through BranchRef("main") and CheckoutBranch.
//
// Branches are shared catalog state, not a dataset-local snapshot.
func (d *Dataset) Branches(ctx context.Context) ([]Branch, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire("branches"); err != nil {
		return nil, err
	}
	defer d.res.release()

	infos, err := d.native.Branches()
	if err != nil {
		return nil, wrapError("branches", d.uri, err)
	}
	branches := make([]Branch, len(infos))
	for i, info := range infos {
		branches[i] = branchFromFFI(info)
	}
	return branches, nil
}

// Branch returns one explicit branch by name. The implicit main branch has no
// catalog entry; use CheckoutBranch("main") to read it.
func (d *Dataset) Branch(ctx context.Context, name string) (Branch, error) {
	if err := checkContext(ctx); err != nil {
		return Branch{}, err
	}
	if err := validateResourceName("get branch", d.uri, "branch", name); err != nil {
		return Branch{}, err
	}
	if err := d.acquire("get branch"); err != nil {
		return Branch{}, err
	}
	defer d.res.release()

	info, err := d.native.Branch(name)
	if err != nil {
		return Branch{}, wrapError("get branch", d.uri, err)
	}
	return branchFromFFI(info), nil
}

// CurrentBranchIdentifier reports the lineage of the receiver's branch.
func (d *Dataset) CurrentBranchIdentifier(ctx context.Context) (BranchIdentifier, error) {
	if err := checkContext(ctx); err != nil {
		return BranchIdentifier{}, err
	}
	if err := d.acquire("branch identifier"); err != nil {
		return BranchIdentifier{}, err
	}
	defer d.res.release()

	identifier, err := d.native.CurrentBranchIdentifier()
	if err != nil {
		return BranchIdentifier{}, wrapError("branch identifier", d.uri, err)
	}
	return branchIdentifierFromFFI(identifier), nil
}

// CreateBranch creates branch name from ref and returns an independent dataset
// checked out on the new branch.
//
// The returned dataset must be closed by the caller. The receiver remains open
// and unchanged. Branches are shared catalog state; other open datasets observe
// the new branch through their next catalog read.
func (d *Dataset) CreateBranch(ctx context.Context, name string, ref Ref, opts ...BranchOption) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateResourceName("create branch", d.uri, "branch", name); err != nil {
		return nil, err
	}
	if err := ref.validate("create branch", d.uri); err != nil {
		return nil, err
	}
	var config branchOptions
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&config); err != nil {
			return nil, invalidError("create branch", d.uri, "%v", err)
		}
	}
	if err := d.acquire("create branch"); err != nil {
		return nil, err
	}
	defer d.res.release()

	clone, err := d.native.Clone()
	if err != nil {
		return nil, wrapError("create branch", d.uri, err)
	}
	created, err := clone.CreateBranch(name, ref.toFFI(), config.storage)
	clone.Close()
	if err != nil {
		return nil, wrapError("create branch", d.uri, err)
	}
	return d.adopt(created), nil
}

// DeleteBranch removes a branch using native reference-protection rules.
//
// Deletion fails when child branches or tags still reference the branch.
func (d *Dataset) DeleteBranch(ctx context.Context, name string) error {
	return d.branchMutation(ctx, "delete branch", name, func(clone *ffi.Dataset) error {
		return clone.DeleteBranch(name)
	})
}

// ForceDeleteBranch removes a branch even when its native branch contents are
// missing, for recovery from an interrupted branch creation. Child branches
// and tags still protect the branch from deletion.
func (d *Dataset) ForceDeleteBranch(ctx context.Context, name string) error {
	return d.branchMutation(ctx, "force delete branch", name, func(clone *ffi.Dataset) error {
		return clone.ForceDeleteBranch(name)
	})
}

// ReplaceBranchMetadata replaces all metadata on an existing branch. An empty
// map removes every metadata entry.
func (d *Dataset) ReplaceBranchMetadata(ctx context.Context, name string, metadata map[string]string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := validateMetadata("replace branch metadata", d.uri, metadata); err != nil {
		return err
	}
	return d.branchMutation(ctx, "replace branch metadata", name, func(clone *ffi.Dataset) error {
		return clone.ReplaceBranchMetadata(name, metadata)
	})
}

func (d *Dataset) branchMutation(ctx context.Context, op, name string, apply func(*ffi.Dataset) error) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := validateResourceName(op, d.uri, "branch", name); err != nil {
		return err
	}
	if err := d.acquire(op); err != nil {
		return err
	}
	defer d.res.release()

	clone, err := d.native.Clone()
	if err != nil {
		return wrapError(op, d.uri, err)
	}
	defer clone.Close()
	if err := apply(clone); err != nil {
		return wrapError(op, d.uri, err)
	}
	return nil
}

func branchFromFFI(info ffi.BranchInfo) Branch {
	return Branch{
		Name:          info.Name,
		ParentBranch:  info.ParentBranch,
		ParentVersion: info.ParentVersion,
		CreateAt:      info.CreateAt,
		ManifestSize:  info.ManifestSize,
		Metadata:      copyMetadata(info.Metadata),
		Identifier:    branchIdentifierFromFFI(info.Identifier),
	}
}

func branchIdentifierFromFFI(identifier ffi.BranchIdentifier) BranchIdentifier {
	mapping := make([]BranchVersionMapping, len(identifier.VersionMapping))
	for i, entry := range identifier.VersionMapping {
		mapping[i] = BranchVersionMapping{Version: entry.Version, UUID: entry.UUID}
	}
	return BranchIdentifier{VersionMapping: mapping}
}
