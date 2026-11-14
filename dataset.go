package lance

import (
	"context"
	"errors"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
)

// Dataset is a checked-out snapshot of a Lance dataset. Mutations return a new
// Dataset and leave this snapshot unchanged. It is safe for concurrent use.
// Close waits for in-flight operations before releasing the native handle.
type Dataset struct {
	native *ffi.Dataset
	res    *resource
	uri    string
}

// Open opens a dataset. A zero version selects the latest version.
func Open(ctx context.Context, uri string, opts ...OpenOption) (*Dataset, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uri == "" {
		return nil, invalidError("open", uri, "dataset URI must not be empty")
	}
	if err := validateCString(uri); err != nil {
		return nil, invalidError("open", uri, "%v", err)
	}

	config := openConfig{}
	for _, option := range opts {
		if option == nil {
			return nil, invalidError("open", uri, "nil OpenOption")
		}
		if err := option(&config); err != nil {
			return nil, invalidError("open", uri, "%v", err)
		}
	}
	if config.baseStoreParams != nil && config.commitLock != nil {
		return nil, invalidError("open", uri, "WithBaseStoreParams is not supported with WithOpenCommitLock; the per-base open entry has no commit-lock parameter")
	}

	var storageAccessor *ffi.StorageOptionsAccessor
	var releaseStorage func()
	var err error
	if config.commitLock != nil {
		storageAccessor, releaseStorage, err = resolveLockStorageOptions("open", uri, config.storageOptions, config.storageOptionsSet, config.storageOptionsAccessor)
	} else {
		storageAccessor, releaseStorage, err = resolveStorageOptions("open", uri, config.storageOptions, config.storageOptionsSet, config.storageOptionsAccessor)
	}
	if err != nil {
		return nil, err
	}
	defer releaseStorage()
	if config.baseStoreParams != nil && storageAccessor == nil && (config.storageOptionsSet || config.storageOptions != nil) {
		// The per-base open entry accepts only an accessor; bridge static
		// options so nil versus present-empty semantics are preserved.
		storageAccessor, err = ffi.NewStorageOptionsAccessorWithPresence(config.storageOptions, true, nil)
		if err != nil {
			return nil, wrapError("open", uri, err)
		}
		defer storageAccessor.Close()
	}

	releaseLock, err := config.commitLock.pin("open", uri)
	if err != nil {
		return nil, err
	}
	defer releaseLock()

	session, releaseSession, err := acquireSession("open", uri, config.session)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	baseStoreParams, baseReleases, err := nativeBaseStoreParams("open", uri, config.baseStoreParams)
	defer releaseAll(baseReleases)
	if err != nil {
		return nil, err
	}

	nativeOptions := ffi.OpenOptions{
		StorageOptionsAccessor: storageAccessor,
		BaseStoreParams:        baseStoreParams,
		Version:                config.version,
		Session:                session,
	}
	if config.baseStoreParams == nil {
		if config.commitLock != nil {
			nativeOptions.CommitLock = config.commitLock.native
		} else {
			nativeOptions.StorageOptions = config.storageOptions
		}
	}
	native, err := ffi.OpenDatasetWithOptions(uri, nativeOptions)
	if err != nil {
		return nil, wrapError("open", uri, err)
	}
	if err := ctx.Err(); err != nil {
		native.Close()
		return nil, err
	}

	dataset := &Dataset{native: native, uri: uri}
	dataset.res = newResource(native.Close)
	return dataset, nil
}

// Close releases the dataset. It is idempotent and waits for active
// operations before calling native close.
func (d *Dataset) Close() error {
	d.res.close()
	return nil
}

func (d *Dataset) acquire(op string) error {
	if !d.res.acquire() {
		return closedError(op, d.uri)
	}
	return nil
}

// adopt wraps an independently owned native dataset returned by a checkout or
// restore operation. The receiver supplies only the URI and is not otherwise
// shared with the result.
func (d *Dataset) adopt(native *ffi.Dataset) *Dataset {
	derived := &Dataset{native: native, uri: d.uri}
	derived.res = newResource(native.Close)
	return derived
}

// Schema returns the dataset schema.
func (d *Dataset) Schema() (*arrow.Schema, error) {
	if err := d.acquire("schema"); err != nil {
		return nil, err
	}
	defer d.res.release()

	schema, err := d.native.Schema()
	if err != nil {
		return nil, wrapError("schema", d.uri, err)
	}
	return schema, nil
}

// Version returns the checked-out dataset version.
func (d *Dataset) Version() (uint64, error) {
	if err := d.acquire("version"); err != nil {
		return 0, err
	}
	defer d.res.release()

	version, err := d.native.Version()
	if err != nil {
		return 0, wrapError("version", d.uri, err)
	}
	return version, nil
}

// LatestVersion returns the latest committed dataset version.
func (d *Dataset) LatestVersion(ctx context.Context) (uint64, error) {
	if err := checkContext(ctx); err != nil {
		return 0, err
	}
	if err := d.acquire("latest version"); err != nil {
		return 0, err
	}
	defer d.res.release()

	version, err := d.native.LatestVersion()
	if err != nil {
		return 0, wrapError("latest version", d.uri, err)
	}
	return version, nil
}

// CountRows returns the manifest row count.
func (d *Dataset) CountRows(ctx context.Context) (uint64, error) {
	if err := checkContext(ctx); err != nil {
		return 0, err
	}
	if err := d.acquire("count rows"); err != nil {
		return 0, err
	}
	defer d.res.release()

	rows, err := d.native.CountRows()
	if err != nil {
		return 0, wrapError("count rows", d.uri, err)
	}
	return rows, nil
}

// CountRowsFiltered returns the row count matching a SQL filter.
func (d *Dataset) CountRowsFiltered(ctx context.Context, filter string) (uint64, error) {
	if err := checkContext(ctx); err != nil {
		return 0, err
	}
	if err := validateCString(filter); err != nil {
		return 0, invalidError("count rows filtered", d.uri, "%v", err)
	}
	if err := d.acquire("count rows filtered"); err != nil {
		return 0, err
	}
	defer d.res.release()

	rows, err := d.native.CountRowsFiltered(filter)
	if err != nil {
		return 0, wrapError("count rows filtered", d.uri, err)
	}
	return rows, nil
}

// Validate checks dataset consistency.
func (d *Dataset) Validate(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := d.acquire("validate"); err != nil {
		return err
	}
	defer d.res.release()

	if err := d.native.Validate(); err != nil {
		return wrapError("validate", d.uri, err)
	}
	return nil
}

// FieldStatistics reports compressed on-disk bytes for one field.
type FieldStatistics struct {
	FieldID     uint32
	BytesOnDisk uint64
}

// DataStatistics computes per-field on-disk statistics.
func (d *Dataset) DataStatistics(ctx context.Context) ([]FieldStatistics, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire("data statistics"); err != nil {
		return nil, err
	}
	defer d.res.release()

	stats, err := d.native.DataStatistics()
	if err != nil {
		return nil, wrapError("data statistics", d.uri, err)
	}
	result := make([]FieldStatistics, len(stats))
	for i, stat := range stats {
		result[i] = FieldStatistics{FieldID: stat.FieldID, BytesOnDisk: stat.BytesOnDisk}
	}
	return result, nil
}

// Take reads rows by zero-based offsets. The caller owns the returned batches.
func (d *Dataset) Take(ctx context.Context, indices []uint64, columns ...string) ([]arrow.RecordBatch, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateColumnNames(columns); err != nil {
		return nil, invalidError("take", d.uri, "%v", err)
	}
	if err := d.acquire("take"); err != nil {
		return nil, err
	}
	defer d.res.release()

	batches, err := d.native.Take(indices, columns)
	if err != nil {
		return nil, wrapError("take", d.uri, err)
	}
	if err := ctx.Err(); err != nil {
		for _, batch := range batches {
			batch.Release()
		}
		return nil, err
	}
	return batches, nil
}

// TakeRows reads rows by Lance row ID. Missing or deleted IDs may be omitted.
// The caller owns the returned batches.
func (d *Dataset) TakeRows(ctx context.Context, rowIDs []uint64, columns ...string) ([]arrow.RecordBatch, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateColumnNames(columns); err != nil {
		return nil, invalidError("take rows", d.uri, "%v", err)
	}
	if err := d.acquire("take rows"); err != nil {
		return nil, err
	}
	defer d.res.release()

	batches, err := d.native.TakeRows(rowIDs, columns)
	if err != nil {
		return nil, wrapError("take rows", d.uri, err)
	}
	if err := ctx.Err(); err != nil {
		for _, batch := range batches {
			batch.Release()
		}
		return nil, err
	}
	return batches, nil
}

func validateColumnNames(columns []string) error {
	for _, column := range columns {
		if column == "" {
			return errors.New("column name must not be empty")
		}
		if err := validateCString(column); err != nil {
			return err
		}
	}
	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
