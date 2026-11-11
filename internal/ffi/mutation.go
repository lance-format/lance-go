package ffi

import (
	"fmt"
	"runtime"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/cdata"
)

// ColumnUpdate assigns Value, a SQL scalar expression, to Column.
type ColumnUpdate struct {
	Column string
	Value  string
}

// MergeInsertParams mirrors LanceMergeInsertParams. Field values are native
// discriminants; the root package maps its typed enums onto them.
type MergeInsertParams struct {
	WhenMatched                      int32
	WhenMatchedExpression            string
	WhenNotMatched                   int32
	WhenNotMatchedBySource           int32
	WhenNotMatchedBySourceExpression string
}

// MergeInsertResult reports the rows affected by MergeInsert.
type MergeInsertResult struct {
	InsertedRows uint64
	UpdatedRows  uint64
	DeletedRows  uint64
}

// mergeInsertParams is the C layout of LanceMergeInsertParams.
type mergeInsertParams struct {
	WhenMatched                int32
	WhenMatchedExpr            *byte
	WhenNotMatched             int32
	WhenNotMatchedBySource     int32
	WhenNotMatchedBySourceExpr *byte
}

// mergeInsertResult is the C layout of LanceMergeInsertResult.
type mergeInsertResult struct {
	NumInsertedRows uint64
	NumUpdatedRows  uint64
	NumDeletedRows  uint64
}

// Delete removes rows matching predicate from this handle.
func (d *Dataset) Delete(predicate string) (uint64, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}
	if predicate == "" {
		return 0, fmt.Errorf("ffi: delete predicate must not be empty")
	}
	if err := validateCString(predicate); err != nil {
		return 0, fmt.Errorf("ffi: delete predicate: %w", err)
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var deleted uint64
	if rc := b.datasetDelete(handle, predicate, &deleted); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_dataset_delete failed without setting an error")
	}
	return deleted, nil
}

// Update applies SQL expressions to rows matching predicate. An empty
// predicate updates every row and is passed to C as NULL.
func (d *Dataset) Update(predicate string, updates []ColumnUpdate) (uint64, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}
	if err := validateCString(predicate); err != nil {
		return 0, fmt.Errorf("ffi: update predicate: %w", err)
	}
	if len(updates) == 0 {
		return 0, fmt.Errorf("ffi: update requires at least one column")
	}

	columns := make([]string, len(updates))
	values := make([]string, len(updates))
	for i, update := range updates {
		if update.Column == "" {
			return 0, fmt.Errorf("ffi: update column %d must not be empty", i)
		}
		if update.Value == "" {
			return 0, fmt.Errorf("ffi: update value for column %q must not be empty", update.Column)
		}
		if err := validateCString(update.Column); err != nil {
			return 0, fmt.Errorf("ffi: update column %d: %w", i, err)
		}
		if err := validateCString(update.Value); err != nil {
			return 0, fmt.Errorf("ffi: update value for column %q: %w", update.Column, err)
		}
		columns[i] = update.Column
		values[i] = update.Value
	}

	columnList, err := newCStringList(columns)
	if err != nil {
		return 0, err
	}
	valueList, err := newCStringList(values)
	if err != nil {
		return 0, err
	}

	var predicateList *cStringList
	var predicatePtr uintptr
	if predicate != "" {
		predicateList, err = newCStringList([]string{predicate})
		if err != nil {
			return 0, err
		}
		predicatePtr = predicateList.ptrs[0]
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var updated uint64
	rc := b.datasetUpdate(handle, predicatePtr, columnList.ptrs, valueList.ptrs, uint64(len(updates)), &updated)
	columnList.keepAlive()
	valueList.keepAlive()
	if predicateList != nil {
		predicateList.keepAlive()
	}
	if rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_dataset_update failed without setting an error")
	}
	return updated, nil
}

// MergeInsert merges source into this handle on the given keys. The source
// stream is consumed by the native call on every return path after export.
func (d *Dataset) MergeInsert(onColumns []string, source array.RecordReader, params MergeInsertParams) (MergeInsertResult, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return MergeInsertResult{}, ErrClosed
	}
	if source == nil {
		return MergeInsertResult{}, fmt.Errorf("ffi: merge insert source reader must not be nil")
	}
	if len(onColumns) == 0 {
		return MergeInsertResult{}, fmt.Errorf("ffi: merge insert requires at least one join column")
	}
	for i, column := range onColumns {
		if column == "" {
			return MergeInsertResult{}, fmt.Errorf("ffi: merge insert join column %d must not be empty", i)
		}
		if err := validateCString(column); err != nil {
			return MergeInsertResult{}, fmt.Errorf("ffi: merge insert join column %d: %w", i, err)
		}
	}

	columnList, err := newCStringList(onColumns)
	if err != nil {
		return MergeInsertResult{}, err
	}

	rawParams := mergeInsertParams{
		WhenMatched:            params.WhenMatched,
		WhenNotMatched:         params.WhenNotMatched,
		WhenNotMatchedBySource: params.WhenNotMatchedBySource,
	}
	var matchedExprList, sourceExprList *cStringList
	if params.WhenMatchedExpression != "" {
		if err := validateCString(params.WhenMatchedExpression); err != nil {
			return MergeInsertResult{}, fmt.Errorf("ffi: merge insert when-matched expression: %w", err)
		}
		matchedExprList, err = newCStringList([]string{params.WhenMatchedExpression})
		if err != nil {
			return MergeInsertResult{}, err
		}
		rawParams.WhenMatchedExpr = &matchedExprList.bufs[0][0]
	}
	if params.WhenNotMatchedBySourceExpression != "" {
		if err := validateCString(params.WhenNotMatchedBySourceExpression); err != nil {
			return MergeInsertResult{}, fmt.Errorf("ffi: merge insert when-not-matched-by-source expression: %w", err)
		}
		sourceExprList, err = newCStringList([]string{params.WhenNotMatchedBySourceExpression})
		if err != nil {
			return MergeInsertResult{}, err
		}
		rawParams.WhenNotMatchedBySourceExpr = &sourceExprList.bufs[0][0]
	}

	var rawStream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(source, &rawStream); err != nil {
		return MergeInsertResult{}, fmt.Errorf("ffi: export merge insert source stream: %w", err)
	}
	streamConsumed := false
	defer func() {
		if !streamConsumed {
			cdata.ReleaseStream(&rawStream)
		}
	}()

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var result mergeInsertResult
	rc := b.datasetMergeInsert(handle, columnList.ptrs, uint64(len(onColumns)), &rawStream, &rawParams, &result)
	streamConsumed = true
	columnList.keepAlive()
	if matchedExprList != nil {
		matchedExprList.keepAlive()
	}
	if sourceExprList != nil {
		sourceExprList.keepAlive()
	}
	runtime.KeepAlive(&rawParams)
	if rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return MergeInsertResult{}, nativeErr
		}
		return MergeInsertResult{}, fmt.Errorf("ffi: lance_dataset_merge_insert failed without setting an error")
	}
	return MergeInsertResult{
		InsertedRows: result.NumInsertedRows,
		UpdatedRows:  result.NumUpdatedRows,
		DeletedRows:  result.NumDeletedRows,
	}, nil
}

// Merge left-joins columns from source into this handle. The source stream is
// consumed by the native call on every return path after export.
func (d *Dataset) Merge(leftOn, rightOn string, source array.RecordReader) error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}
	if source == nil {
		return fmt.Errorf("ffi: merge source reader must not be nil")
	}
	if err := validateCString(leftOn); err != nil {
		return fmt.Errorf("ffi: merge left key: %w", err)
	}
	if err := validateCString(rightOn); err != nil {
		return fmt.Errorf("ffi: merge right key: %w", err)
	}

	var rawStream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(source, &rawStream); err != nil {
		return fmt.Errorf("ffi: export merge source stream: %w", err)
	}
	streamConsumed := false
	defer func() {
		if !streamConsumed {
			cdata.ReleaseStream(&rawStream)
		}
	}()

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	rc := b.datasetMerge(handle, leftOn, rightOn, &rawStream)
	streamConsumed = true
	if rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_merge failed without setting an error")
	}
	return nil
}

// Truncate removes every row from this handle.
func (d *Dataset) Truncate() error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}

	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if rc := b.datasetTruncateTable(handle); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_truncate_table failed without setting an error")
	}
	return nil
}
