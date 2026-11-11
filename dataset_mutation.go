package lance

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
)

// Delete returns a new Dataset at a committed version with the rows matching
// predicate removed, along with the number of deleted rows. d is not changed:
// its version, rows and open scanners keep the pre-delete snapshot.
//
// The returned Dataset owns an independent native handle and must be closed by
// the caller. Closing either dataset does not affect the other.
//
// The native delete is synchronous. ctx is checked before the operation
// starts; it is not observed once the native commit is in progress. If the
// commit succeeds after ctx is canceled, the committed Dataset is returned
// instead of an error that could be mistaken for an uncommitted mutation.
func (d *Dataset) Delete(ctx context.Context, predicate string) (*Dataset, uint64, error) {
	if err := checkContext(ctx); err != nil {
		return nil, 0, err
	}
	if predicate == "" {
		return nil, 0, invalidError("delete", d.uri, "predicate must not be empty")
	}
	if err := validateCString(predicate); err != nil {
		return nil, 0, invalidError("delete", d.uri, "predicate: %v", err)
	}

	var deleted uint64
	next, err := d.mutate("delete", func(native *ffi.Dataset) error {
		var err error
		deleted, err = native.Delete(predicate)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return next, deleted, nil
}

// Update returns a new Dataset at a committed version with updates applied to
// rows matching predicate, along with the number of updated rows. An empty
// predicate updates every row. d is not changed.
//
// Each ColumnUpdate.Value is a SQL scalar expression evaluated by Lance per
// row. At least one update is required. The returned Dataset owns an
// independent native handle and must be closed by the caller.
//
// The native update is synchronous. ctx is checked before the operation
// starts; it is not observed once the native commit is in progress. If the
// commit succeeds after ctx is canceled, the committed Dataset is returned.
func (d *Dataset) Update(ctx context.Context, predicate string, updates ...ColumnUpdate) (*Dataset, uint64, error) {
	if err := checkContext(ctx); err != nil {
		return nil, 0, err
	}
	if err := validateCString(predicate); err != nil {
		return nil, 0, invalidError("update", d.uri, "predicate: %v", err)
	}
	if err := validateMutationUpdates(updates); err != nil {
		return nil, 0, invalidError("update", d.uri, "%v", err)
	}

	nativeUpdates := make([]ffi.ColumnUpdate, len(updates))
	for i, update := range updates {
		nativeUpdates[i] = ffi.ColumnUpdate{Column: update.Column, Value: update.Value}
	}

	var updated uint64
	next, err := d.mutate("update", func(native *ffi.Dataset) error {
		var err error
		updated, err = native.Update(predicate, nativeUpdates)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return next, updated, nil
}

// MergeInsert returns a new Dataset at a committed version after merging
// source into d on onColumns. d is not changed.
//
// params controls matched and unmatched row behavior. Its zero value performs
// find-or-create: existing keys are kept and missing source keys are inserted.
// See MergeInsertParams for expression requirements.
//
// source is borrowed and may be partially or fully advanced, including on
// failure. Its schema must be compatible with d. The caller retains ownership
// and must release source after this method returns. Do not use or release it
// concurrently with the call. Argument validation failures before stream export
// leave source untouched.
//
// The returned Dataset owns an independent native handle and must be closed by
// the caller. The native merge is synchronous; ctx is checked before the
// operation starts and is not observed once the native commit is in progress.
func (d *Dataset) MergeInsert(ctx context.Context, onColumns []string, source array.RecordReader, params MergeInsertParams) (*Dataset, MergeInsertResult, error) {
	if err := checkContext(ctx); err != nil {
		return nil, MergeInsertResult{}, err
	}
	if source == nil {
		return nil, MergeInsertResult{}, invalidError("merge insert", d.uri, "source record reader must not be nil")
	}
	if err := validateMutationKeys(onColumns, "merge insert"); err != nil {
		return nil, MergeInsertResult{}, invalidError("merge insert", d.uri, "%v", err)
	}
	if err := params.validate(); err != nil {
		return nil, MergeInsertResult{}, invalidError("merge insert", d.uri, "%v", err)
	}

	nativeParams := ffi.MergeInsertParams{
		WhenMatched:                      int32(params.WhenMatched),
		WhenMatchedExpression:            params.WhenMatchedExpression,
		WhenNotMatched:                   int32(params.WhenNotMatched),
		WhenNotMatchedBySource:           int32(params.WhenNotMatchedBySource),
		WhenNotMatchedBySourceExpression: params.WhenNotMatchedBySourceExpression,
	}

	var result ffi.MergeInsertResult
	next, err := d.mutate("merge insert", func(native *ffi.Dataset) error {
		var err error
		result, err = native.MergeInsert(onColumns, source, nativeParams)
		return err
	})
	if err != nil {
		return nil, MergeInsertResult{}, err
	}
	return next, MergeInsertResult{
		InsertedRows: result.InsertedRows,
		UpdatedRows:  result.UpdatedRows,
		DeletedRows:  result.DeletedRows,
	}, nil
}

// Merge returns a new Dataset at a committed version after left-joining the
// non-key columns of source into d. d is not changed.
//
// leftOn may name a dataset column, _rowid or _rowaddr. rightOn names the join
// field in source. Field lookup is exact and case-sensitive. Empty key strings
// are forwarded to native validation. The source field named by rightOn is the
// join key and is not appended; every other source field must be new to d.
//
// source is borrowed and may be partially or fully advanced, including on
// failure. The caller retains ownership and must release source after this
// method returns. Do not use or release it concurrently with the call. Argument
// validation failures before stream export leave source untouched.
//
// The returned Dataset owns an independent native handle and must be closed by
// the caller. The native merge is synchronous; ctx is checked before the
// operation starts and is not observed once the native commit is in progress.
func (d *Dataset) Merge(ctx context.Context, leftOn, rightOn string, source array.RecordReader) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, invalidError("merge", d.uri, "source record reader must not be nil")
	}
	if err := validateCString(leftOn); err != nil {
		return nil, invalidError("merge", d.uri, "left key: %v", err)
	}
	if err := validateCString(rightOn); err != nil {
		return nil, invalidError("merge", d.uri, "right key: %v", err)
	}

	next, err := d.mutate("merge", func(native *ffi.Dataset) error {
		return native.Merge(leftOn, rightOn, source)
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

// Truncate returns a new Dataset at a committed version with no rows. d is not
// changed, and its historical versions remain readable.
//
// The returned Dataset owns an independent native handle and must be closed by
// the caller. The native truncate is synchronous; ctx is checked before the
// operation starts and is not observed once the native commit is in progress.
func (d *Dataset) Truncate(ctx context.Context) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	next, err := d.mutate("truncate", func(native *ffi.Dataset) error {
		return native.Truncate()
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

// mutate clones d's current native snapshot, applies one native mutation to
// the clone, and returns a root Dataset that owns the clone. The receiver's
// handle is never mutated, so its version and open scanners stay on the
// original snapshot. Callers validate ctx and arguments before calling mutate.
func (d *Dataset) mutate(op string, apply func(*ffi.Dataset) error) (*Dataset, error) {
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()

	clone, err := d.native.Clone()
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	if err := apply(clone); err != nil {
		clone.Close()
		return nil, wrapError(op, d.uri, err)
	}

	next := &Dataset{native: clone, uri: d.uri}
	next.res = newResource(clone.Close)
	return next, nil
}
