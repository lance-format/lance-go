package lance

import (
	"context"
	"fmt"
	"math"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/cdata"
	"github.com/lance-format/lance-go/internal/ffi"
)

// FragmentFileVersion selects the native file format for a staged replacement
// data file. The values match the native LanceFileFormatVersion discriminants.
type FragmentFileVersion int32

const (
	FragmentFileVersionV1   FragmentFileVersion = 0
	FragmentFileVersionV2_0 FragmentFileVersion = 1
	FragmentFileVersionV2_1 FragmentFileVersion = 2
	FragmentFileVersionV2_2 FragmentFileVersion = 3
	FragmentFileVersionV2_3 FragmentFileVersion = 4
)

// FragmentOffsets is one fragment entry in an UpdatedFragmentOffsets value.
// Offsets are forwarded as a set: repeated values collapse in native.
type FragmentOffsets struct {
	FragmentID uint64
	Offsets    []uint32
}

// UpdatedFragmentOffsets is an owned native mapping from fragment ID to
// physical row offsets. Native owns duplicate handling and the underlying
// bitmap representation; entry iteration order is unspecified.
type UpdatedFragmentOffsets struct {
	native *ffi.UpdatedFragmentOffsets
	res    *resource
}

func newUpdatedFragmentOffsets(native *ffi.UpdatedFragmentOffsets) *UpdatedFragmentOffsets {
	offsets := &UpdatedFragmentOffsets{native: native}
	offsets.res = newResource(native.Close)
	return offsets
}

// NewUpdatedFragmentOffsets copies entries into an owned native value. Pass a
// nil slice to create the explicit empty value. Omit the value from
// NewUpdateOperation to represent the native absent value.
func NewUpdatedFragmentOffsets(entries []FragmentOffsets) (*UpdatedFragmentOffsets, error) {
	nativeEntries := make([]ffi.FragmentOffsets, len(entries))
	for i := range entries {
		nativeEntries[i] = ffi.FragmentOffsets{
			FragmentID: entries[i].FragmentID,
			Offsets:    entries[i].Offsets,
		}
	}
	native, err := ffi.NewUpdatedFragmentOffsets(nativeEntries)
	if err != nil {
		return nil, wrapError("new updated fragment offsets", "", err)
	}
	return newUpdatedFragmentOffsets(native), nil
}

// Close releases the offsets. It is idempotent.
func (o *UpdatedFragmentOffsets) Close() error {
	if o == nil {
		return nil
	}
	o.res.close()
	return nil
}

// Clone returns an independently owned copy.
func (o *UpdatedFragmentOffsets) Clone() (*UpdatedFragmentOffsets, error) {
	if o == nil || !o.res.acquire() {
		return nil, closedError("clone updated fragment offsets", "")
	}
	defer o.res.release()
	native, err := o.native.Clone()
	if err != nil {
		return nil, wrapError("clone updated fragment offsets", "", err)
	}
	return newUpdatedFragmentOffsets(native), nil
}

// Count returns the number of fragment entries.
func (o *UpdatedFragmentOffsets) Count() (uint64, error) {
	if o == nil || !o.res.acquire() {
		return 0, closedError("updated fragment offsets count", "")
	}
	defer o.res.release()
	count, err := o.native.Count()
	if err != nil {
		return 0, wrapError("updated fragment offsets count", "", err)
	}
	return count, nil
}

// FragmentIDAt returns the fragment ID of one entry.
func (o *UpdatedFragmentOffsets) FragmentIDAt(index uint64) (uint64, error) {
	if o == nil || !o.res.acquire() {
		return 0, closedError("updated fragment offsets fragment id", "")
	}
	defer o.res.release()
	fragmentID, err := o.native.FragmentIDAt(index)
	if err != nil {
		return 0, wrapError("updated fragment offsets fragment id", "", err)
	}
	return fragmentID, nil
}

// OffsetsAt returns every offset of one slot in native bitmap order.
func (o *UpdatedFragmentOffsets) OffsetsAt(index uint64) ([]uint32, error) {
	if o == nil || !o.res.acquire() {
		return nil, closedError("updated fragment offsets offsets", "")
	}
	defer o.res.release()
	offsets, err := o.native.OffsetsAt(index)
	if err != nil {
		return nil, wrapError("updated fragment offsets offsets", "", err)
	}
	return offsets, nil
}

// Entries returns every fragment entry in native iteration order.
func (o *UpdatedFragmentOffsets) Entries() ([]FragmentOffsets, error) {
	count, err := o.Count()
	if err != nil {
		return nil, err
	}
	entries := make([]FragmentOffsets, 0, count)
	for index := uint64(0); index < count; index++ {
		fragmentID, err := o.FragmentIDAt(index)
		if err != nil {
			return nil, err
		}
		offsets, err := o.OffsetsAt(index)
		if err != nil {
			return nil, err
		}
		entries = append(entries, FragmentOffsets{FragmentID: fragmentID, Offsets: offsets})
	}
	return entries, nil
}

// UpdateColumnsResult is the owned result of Fragment.UpdateColumns.
type UpdateColumnsResult struct {
	native *ffi.UpdateColumnsResult
	res    *resource
	uri    string
}

func newUpdateColumnsResult(native *ffi.UpdateColumnsResult, uri string) *UpdateColumnsResult {
	result := &UpdateColumnsResult{native: native, uri: uri}
	result.res = newResource(native.Close)
	return result
}

// Close releases the result. It is idempotent. The detached fragment returned
// by Fragment remains independently owned.
func (r *UpdateColumnsResult) Close() error {
	if r == nil {
		return nil
	}
	r.res.close()
	return nil
}

// Fragment returns detached updated fragment metadata.
func (r *UpdateColumnsResult) Fragment() (*Fragment, error) {
	if r == nil || !r.res.acquire() {
		return nil, closedError("update columns fragment", r.uriOrEmpty())
	}
	defer r.res.release()
	native, err := r.native.Fragment()
	if err != nil {
		return nil, wrapError("update columns fragment", r.uri, err)
	}
	return newFragment(native, r.uri), nil
}

// FieldsModified returns the native modified field IDs.
func (r *UpdateColumnsResult) FieldsModified() ([]uint32, error) {
	if r == nil || !r.res.acquire() {
		return nil, closedError("update columns fields modified", r.uriOrEmpty())
	}
	defer r.res.release()
	fields, err := r.native.FieldsModified()
	if err != nil {
		return nil, wrapError("update columns fields modified", r.uri, err)
	}
	return fields, nil
}

// MatchedOffsets returns matched physical row offsets in native order.
func (r *UpdateColumnsResult) MatchedOffsets() ([]uint32, error) {
	if r == nil || !r.res.acquire() {
		return nil, closedError("update columns matched offsets", r.uriOrEmpty())
	}
	defer r.res.release()
	offsets, err := r.native.MatchedOffsets()
	if err != nil {
		return nil, wrapError("update columns matched offsets", r.uri, err)
	}
	return offsets, nil
}

func (r *UpdateColumnsResult) uriOrEmpty() string {
	if r == nil {
		return ""
	}
	return r.uri
}

// AddColumnsSQL stages SQL-computed columns on a clone of this fragment. The
// result fragment is detached metadata; publication remains an explicit
// operation and transaction step.
func (f *Fragment) AddColumnsSQL(ctx context.Context, columns []SQLColumn, batchSize ...uint64) (*Fragment, *NativeSchema, error) {
	if f == nil || !f.res.acquire() {
		return nil, nil, closedError("fragment add columns sql", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, nil, wrapError("fragment add columns sql", f.uri, err)
	}
	size, err := fragmentBatchSize(batchSize)
	if err != nil {
		return nil, nil, wrapError("fragment add columns sql", f.uri, err)
	}
	nativeColumns := make([]ffi.SQLColumn, len(columns))
	for i := range columns {
		nativeColumns[i] = ffi.SQLColumn{Name: columns[i].Name, Expression: columns[i].Expression}
	}
	native, schema, err := f.native.AddColumnsSQL(nativeColumns, size)
	if err != nil {
		return nil, nil, wrapError("fragment add columns sql", f.uri, err)
	}
	return newFragment(native, f.uri), newNativeSchema(schema), nil
}

// AddColumnsNulls stages all-null columns described by columns. The schema
// supplies only the new logical field definitions; native allocates field IDs.
func (f *Fragment) AddColumnsNulls(ctx context.Context, columns *arrow.Schema) (*Fragment, *NativeSchema, error) {
	if f == nil || !f.res.acquire() {
		return nil, nil, closedError("fragment add columns nulls", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, nil, wrapError("fragment add columns nulls", f.uri, err)
	}
	if columns == nil {
		return nil, nil, invalidError("fragment add columns nulls", f.uri, "columns schema must not be nil")
	}
	native, schema, err := f.native.AddColumnsNulls(columns)
	if err != nil {
		return nil, nil, wrapError("fragment add columns nulls", f.uri, err)
	}
	return newFragment(native, f.uri), newNativeSchema(schema), nil
}

// AddColumnsStream stages columns read from source. Native consumes the
// transferred stream exactly once; the caller retains ownership of source.
func (f *Fragment) AddColumnsStream(ctx context.Context, source array.RecordReader, batchSize ...uint64) (*Fragment, *NativeSchema, error) {
	if f == nil || !f.res.acquire() {
		return nil, nil, closedError("fragment add columns stream", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, nil, wrapError("fragment add columns stream", f.uri, err)
	}
	if source == nil {
		return nil, nil, invalidError("fragment add columns stream", f.uri, "source must not be nil")
	}
	size, err := fragmentBatchSize(batchSize)
	if err != nil {
		return nil, nil, wrapError("fragment add columns stream", f.uri, err)
	}
	native, schema, err := f.native.AddColumnsStream(source, size)
	if err != nil {
		return nil, nil, wrapError("fragment add columns stream", f.uri, err)
	}
	return newFragment(native, f.uri), newNativeSchema(schema), nil
}

// AddColumnsBatchUDF stages columns produced by udf. The mapper, output schema
// and checkpoint store use the same callbacks as Dataset.AddColumnsBatchUDF.
func (f *Fragment) AddColumnsBatchUDF(ctx context.Context, udf BatchUDF) (*Fragment, *NativeSchema, error) {
	if f == nil || !f.res.acquire() {
		return nil, nil, closedError("fragment add columns batch udf", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, nil, wrapError("fragment add columns batch udf", f.uri, err)
	}
	if udf.Mapper == nil {
		return nil, nil, invalidError("fragment add columns batch udf", f.uri, "mapper must not be nil")
	}
	if udf.OutputSchema == nil {
		return nil, nil, invalidError("fragment add columns batch udf", f.uri, "output schema must not be nil")
	}
	var exported cdata.ArrowSchema
	if err := cdata.ExportSchema(udf.OutputSchema, &exported); err != nil {
		return nil, nil, invalidError("fragment add columns batch udf", f.uri, "output schema: %v", err)
	}
	defer cdata.ReleaseSchema(&exported)
	var store ffi.BatchUDFStore
	if udf.Store != nil {
		store = batchUDFStoreAdapter{store: udf.Store}
	}
	native, schema, err := f.native.AddColumnsBatchUDF(ffi.BatchUDF{
		Mapper:       udf.Mapper,
		OutputSchema: &exported,
		Store:        store,
	}, udf.ReadColumns, udf.ReadColumns != nil, udf.BatchSize)
	if err != nil {
		return nil, nil, wrapError("fragment add columns batch udf", f.uri, err)
	}
	return newFragment(native, f.uri), newNativeSchema(schema), nil
}

// MergeColumns joins source into a clone of this fragment.
func (f *Fragment) MergeColumns(ctx context.Context, source array.RecordReader, leftOn, rightOn string) (*Fragment, *NativeSchema, error) {
	if f == nil || !f.res.acquire() {
		return nil, nil, closedError("fragment merge columns", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, nil, wrapError("fragment merge columns", f.uri, err)
	}
	if source == nil {
		return nil, nil, invalidError("fragment merge columns", f.uri, "source must not be nil")
	}
	native, schema, err := f.native.MergeColumns(source, leftOn, rightOn)
	if err != nil {
		return nil, nil, wrapError("fragment merge columns", f.uri, err)
	}
	return newFragment(native, f.uri), newNativeSchema(schema), nil
}

// UpdateColumns stages a native update from source.
func (f *Fragment) UpdateColumns(ctx context.Context, source array.RecordReader, leftOn, rightOn string) (*UpdateColumnsResult, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("fragment update columns", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, wrapError("fragment update columns", f.uri, err)
	}
	if source == nil {
		return nil, invalidError("fragment update columns", f.uri, "source must not be nil")
	}
	native, err := f.native.UpdateColumns(source, leftOn, rightOn)
	if err != nil {
		return nil, wrapError("fragment update columns", f.uri, err)
	}
	return newUpdateColumnsResult(native, f.uri), nil
}

// WriteColumns stages a replacement data file for schema.
func (f *Fragment) WriteColumns(ctx context.Context, schema *NativeSchema, source array.RecordReader) (*DataReplacementGroup, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("fragment write columns", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, wrapError("fragment write columns", f.uri, err)
	}
	if source == nil {
		return nil, invalidError("fragment write columns", f.uri, "source must not be nil")
	}
	if schema == nil {
		return nil, invalidError("fragment write columns", f.uri, "schema must not be nil")
	}
	if !schema.res.acquire() {
		return nil, closedError("fragment write columns schema", f.uri)
	}
	defer schema.res.release()
	native, err := f.native.WriteColumns(schema.native, source)
	if err != nil {
		return nil, wrapError("fragment write columns", f.uri, err)
	}
	return newDataReplacementGroup(native), nil
}

// WriteColumnWithVersion stages a replacement data file with an explicit file
// format. Native rejects FragmentFileVersionV1 and incompatible targets.
func (f *Fragment) WriteColumnWithVersion(ctx context.Context, schema *NativeSchema, source array.RecordReader, version FragmentFileVersion) (*DataReplacementGroup, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("fragment write column with version", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, wrapError("fragment write column with version", f.uri, err)
	}
	if source == nil {
		return nil, invalidError("fragment write column with version", f.uri, "source must not be nil")
	}
	if schema == nil {
		return nil, invalidError("fragment write column with version", f.uri, "schema must not be nil")
	}
	if !schema.res.acquire() {
		return nil, closedError("fragment write column with version schema", f.uri)
	}
	defer schema.res.release()
	native, err := f.native.WriteColumnWithVersion(schema.native, source, int32(version))
	if err != nil {
		return nil, wrapError("fragment write column with version", f.uri, err)
	}
	return newDataReplacementGroup(native), nil
}

// DeleteRows stages physical row offsets on a clone of this fragment. It
// returns nil when the offsets delete every physical row.
func (f *Fragment) DeleteRows(ctx context.Context, offsets []uint32) (*Fragment, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("fragment delete rows", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, wrapError("fragment delete rows", f.uri, err)
	}
	native, err := f.native.DeleteRows(offsets)
	if err != nil {
		return nil, wrapError("fragment delete rows", f.uri, err)
	}
	if native == nil {
		return nil, nil
	}
	return newFragment(native, f.uri), nil
}

func fragmentBatchSize(values []uint64) (*uint32, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > 1 {
		return nil, fmt.Errorf("batch size accepts at most one value")
	}
	if values[0] > math.MaxUint32 {
		return nil, fmt.Errorf("batch size %d exceeds uint32", values[0])
	}
	size := uint32(values[0])
	return &size, nil
}
