package lance

import (
	"context"
	"time"

	"github.com/lance-format/lance-go/internal/ffi"
)

// DatasetVersion is one committed dataset version.
//
// Metadata is a copy and remains valid after the Dataset is closed.
type DatasetVersion struct {
	ID        uint64
	Timestamp time.Time
	Metadata  map[string]string
}

// CurrentVersion returns the version checked out by this dataset.
func (d *Dataset) CurrentVersion(ctx context.Context) (DatasetVersion, error) {
	if err := checkContext(ctx); err != nil {
		return DatasetVersion{}, err
	}
	if err := d.acquire("current version"); err != nil {
		return DatasetVersion{}, err
	}
	defer d.res.release()

	info, err := d.native.CurrentVersion()
	if err != nil {
		return DatasetVersion{}, wrapError("current version", d.uri, err)
	}
	return datasetVersionFromFFI(info), nil
}

// Versions snapshots the committed version history in ascending version order.
//
// The returned values are copies and remain valid after the Dataset is closed.
func (d *Dataset) Versions(ctx context.Context) ([]DatasetVersion, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire("versions"); err != nil {
		return nil, err
	}
	defer d.res.release()

	infos, err := d.native.Versions()
	if err != nil {
		return nil, wrapError("versions", d.uri, err)
	}
	versions := make([]DatasetVersion, len(infos))
	for i, info := range infos {
		versions[i] = datasetVersionFromFFI(info)
	}
	return versions, nil
}

// VersionRefs returns the lightweight attached-version manifest reference IDs,
// ordered by version. These are storage references, not full version snapshots.
func (d *Dataset) VersionRefs(ctx context.Context) ([]uint64, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire("version refs"); err != nil {
		return nil, err
	}
	defer d.res.release()

	refs, err := d.native.VersionRefs()
	if err != nil {
		return nil, wrapError("version refs", d.uri, err)
	}
	return append([]uint64(nil), refs...), nil
}

// Restore commits a new dataset version whose content is version and returns
// an independent dataset at that new commit. It never rewinds the receiver.
//
// Restore always commits, including when version is already the latest
// version. The committed version is reported as success even if ctx is
// canceled while the synchronous native restore is running, because the new
// commit cannot be rolled back safely. version must be greater than zero.
func (d *Dataset) Restore(ctx context.Context, version uint64) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if version == 0 {
		return nil, invalidError("restore", d.uri, "version must be greater than zero")
	}
	if err := d.acquire("restore"); err != nil {
		return nil, err
	}
	defer d.res.release()

	clone, err := d.native.Clone()
	if err != nil {
		return nil, wrapError("restore", d.uri, err)
	}
	restored, err := clone.Restore(version)
	clone.Close()
	if err != nil {
		return nil, wrapError("restore", d.uri, err)
	}
	return d.adopt(restored), nil
}

func datasetVersionFromFFI(info ffi.VersionInfo) DatasetVersion {
	return DatasetVersion{
		ID:        info.ID,
		Timestamp: info.Timestamp,
		Metadata:  copyMetadata(info.Metadata),
	}
}

func copyMetadata(metadata map[string]string) map[string]string {
	result := make(map[string]string, len(metadata))
	for key, value := range metadata {
		result[key] = value
	}
	return result
}
