package ffi

import (
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sort"
	"time"
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

// UnmarshalJSON decodes native camelCase-independent wire data without
// converting integer identifiers through float64.
func (b *BranchIdentifier) UnmarshalJSON(data []byte) error {
	var wire struct {
		VersionMapping [][]json.RawMessage `json:"version_mapping"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	mapping := make([]BranchVersionMapping, 0, len(wire.VersionMapping))
	for i, pair := range wire.VersionMapping {
		if len(pair) != 2 {
			return fmt.Errorf("version_mapping[%d]: got %d elements, want 2", i, len(pair))
		}
		var version uint64
		if err := json.Unmarshal(pair[0], &version); err != nil {
			return fmt.Errorf("version_mapping[%d] version: %w", i, err)
		}
		var uuid string
		if err := json.Unmarshal(pair[1], &uuid); err != nil {
			return fmt.Errorf("version_mapping[%d] uuid: %w", i, err)
		}
		mapping = append(mapping, BranchVersionMapping{Version: version, UUID: uuid})
	}
	b.VersionMapping = mapping
	return nil
}

// BranchInfo is a copied snapshot of one named branch.
type BranchInfo struct {
	Name          string
	ParentBranch  string
	ParentVersion uint64
	CreateAt      time.Time
	ManifestSize  uint64
	Metadata      map[string]string
	Identifier    BranchIdentifier
}

// BranchOptions selects branch creation behavior.
//
// A nil *BranchOptions inherits the source dataset's store parameters. A
// non-nil value, including one with an empty StorageOptions map, passes
// explicit storage options to native branch creation.
type BranchOptions struct {
	StorageOptions map[string]string
}

type branchJSON struct {
	ParentBranch  *string           `json:"parentBranch"`
	Identifier    BranchIdentifier  `json:"identifier"`
	ParentVersion uint64            `json:"parentVersion"`
	CreateAt      uint64            `json:"createAt"`
	ManifestSize  uint64            `json:"manifestSize"`
	Metadata      map[string]string `json:"metadata"`
}

func (w branchJSON) info(name string) (BranchInfo, error) {
	if w.CreateAt > math.MaxInt64 {
		return BranchInfo{}, fmt.Errorf("branch %q createAt %d overflows int64", name, w.CreateAt)
	}
	parent := ""
	if w.ParentBranch != nil {
		parent = *w.ParentBranch
	}
	mapping := make([]BranchVersionMapping, len(w.Identifier.VersionMapping))
	copy(mapping, w.Identifier.VersionMapping)
	return BranchInfo{
		Name:          name,
		ParentBranch:  parent,
		ParentVersion: w.ParentVersion,
		CreateAt:      time.Unix(int64(w.CreateAt), 0).UTC(),
		ManifestSize:  w.ManifestSize,
		Metadata:      copyStringMap(w.Metadata),
		Identifier:    BranchIdentifier{VersionMapping: mapping},
	}, nil
}

// Branches snapshots all branches. The result is ordered by name.
func (d *Dataset) Branches() ([]BranchInfo, error) {
	text, err := d.jsonString("lance_dataset_branches_json", func(handle uintptr) *byte {
		return d.b.datasetBranchesJSON(handle)
	})
	if err != nil {
		return nil, err
	}

	var raw map[string]branchJSON
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("ffi: parse branch list: %w", err)
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]BranchInfo, 0, len(names))
	for _, name := range names {
		info, err := raw[name].info(name)
		if err != nil {
			return nil, fmt.Errorf("ffi: parse branch list: %w", err)
		}
		result = append(result, info)
	}
	return result, nil
}

// Branch fetches one branch by name.
func (d *Dataset) Branch(name string) (BranchInfo, error) {
	if err := validateRefName(name, "branch"); err != nil {
		return BranchInfo{}, err
	}
	text, err := d.jsonString("lance_dataset_branch_get_json", func(handle uintptr) *byte {
		return d.b.datasetBranchGetJSON(handle, name)
	})
	if err != nil {
		return BranchInfo{}, err
	}
	var raw branchJSON
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return BranchInfo{}, fmt.Errorf("ffi: parse branch %q: %w", name, err)
	}
	info, err := raw.info(name)
	if err != nil {
		return BranchInfo{}, err
	}
	return info, nil
}

// CurrentBranchIdentifier reports the receiver's branch lineage.
func (d *Dataset) CurrentBranchIdentifier() (BranchIdentifier, error) {
	text, err := d.jsonString("lance_dataset_branch_identifier_json", func(handle uintptr) *byte {
		return d.b.datasetBranchIdentifierJSON(handle)
	})
	if err != nil {
		return BranchIdentifier{}, err
	}
	var identifier BranchIdentifier
	if err := json.Unmarshal([]byte(text), &identifier); err != nil {
		return BranchIdentifier{}, fmt.Errorf("ffi: parse branch identifier: %w", err)
	}
	return identifier, nil
}

// CreateBranch shallow-clones ref into a new branch and returns its dataset.
func (d *Dataset) CreateBranch(name string, ref Ref, options *BranchOptions) (*Dataset, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	if err := validateRefName(name, "branch"); err != nil {
		return nil, err
	}
	if err := ref.validate(); err != nil {
		return nil, err
	}

	var storageList *cStringList
	if options != nil {
		list, err := branchStorageList(options.StorageOptions)
		if err != nil {
			return nil, err
		}
		storageList = list
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	nativeRef, err := b.newReferenceLocked(ref)
	if err != nil {
		return nil, err
	}
	defer b.refClose(nativeRef)

	var storageOptions []uintptr
	if storageList != nil {
		defer storageList.keepAlive()
		storageOptions = storageList.ptrs
	}
	created := b.datasetBranchCreate(handle, name, nativeRef, storageOptions)
	if created == 0 {
		return nil, nativeNullError(b, "lance_dataset_branch_create")
	}
	return datasetFromHandle(b, created), nil
}

// DeleteBranch removes a branch using native reference-protection rules.
func (d *Dataset) DeleteBranch(name string) error {
	return d.branchDelete("lance_dataset_branch_delete", name, false)
}

// ForceDeleteBranch removes a branch even when its BranchContents is missing.
func (d *Dataset) ForceDeleteBranch(name string) error {
	return d.branchDelete("lance_dataset_branch_force_delete", name, true)
}

func (d *Dataset) branchDelete(op, name string, force bool) error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}
	if err := validateRefName(name, "branch"); err != nil {
		return err
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	call := b.datasetBranchDelete
	if force {
		call = b.datasetBranchForceDelete
	}
	if rc := call(handle, name); rc != 0 {
		return nativeActionError(b, op, rc)
	}
	return nil
}

// ReplaceBranchMetadata replaces all metadata on an existing branch.
func (d *Dataset) ReplaceBranchMetadata(name string, metadata map[string]string) error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}
	if err := validateRefName(name, "branch"); err != nil {
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
		defer list.keepAlive()
		values = list.ptrs
	}
	if rc := b.datasetBranchReplaceMetadata(handle, name, values); rc != 0 {
		return nativeActionError(b, "lance_dataset_branch_replace_metadata", rc)
	}
	return nil
}

func branchStorageList(options map[string]string) (*cStringList, error) {
	keys := make([]string, 0, len(options))
	for key, value := range options {
		if key == "" {
			return nil, fmt.Errorf("storage option key must not be empty")
		}
		if err := validateCString(key); err != nil {
			return nil, fmt.Errorf("storage option key %q: %w", key, err)
		}
		if err := validateCString(value); err != nil {
			return nil, fmt.Errorf("storage option value for %q: %w", key, err)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		values = append(values, key, options[key])
	}
	return newPresentCStringList(values)
}

func copyStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
