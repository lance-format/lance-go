package ffi

import (
	"fmt"
	"runtime"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/cdata"
	"github.com/lance-format/lance-go/internal/handle"
)

// UpdatedFragmentOffsets is the owned native representation of physical row
// offsets grouped by fragment ID. Native owns ordering, duplicate handling and
// the underlying bitmap representation.
type UpdatedFragmentOffsets struct {
	b      *bindings
	handle atomic.Uintptr
}

// FragmentOffsets is one fragment entry for NewUpdatedFragmentOffsets.
type FragmentOffsets struct {
	FragmentID uint64
	Offsets    []uint32
}

type updatedFragmentOffsetsView struct {
	FragmentID  uint64
	Offsets     *uint32
	OffsetCount uint64
}

// NewUpdatedFragmentOffsets copies entries into an owned native offsets value.
// Pass nil to produce the explicit empty value; omit the value entirely from
// NewUpdateOperation to represent native None.
func NewUpdatedFragmentOffsets(entries []FragmentOffsets) (*UpdatedFragmentOffsets, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	views := make([]updatedFragmentOffsetsView, len(entries))
	for i := range entries {
		views[i].FragmentID = entries[i].FragmentID
		views[i].OffsetCount = uint64(len(entries[i].Offsets))
		if len(entries[i].Offsets) > 0 {
			views[i].Offsets = &entries[i].Offsets[0]
		}
	}
	var ptr *updatedFragmentOffsetsView
	if len(views) > 0 {
		ptr = &views[0]
	}
	runtime.LockOSThread()
	native := b.updatedFragmentOffsetsNew(ptr, uint64(len(views)))
	runtime.KeepAlive(views)
	runtime.KeepAlive(entries)
	runtime.UnlockOSThread()
	if native == 0 {
		return nil, mutationError(b, "construct updated fragment offsets")
	}
	offsets := &UpdatedFragmentOffsets{b: b}
	offsets.handle.Store(native)
	return offsets, nil
}

// Clone returns an independently owned copy.
func (o *UpdatedFragmentOffsets) Clone() (*UpdatedFragmentOffsets, error) {
	if o == nil {
		return nil, fmt.Errorf("ffi: clone nil updated fragment offsets")
	}
	handleValue := o.handle.Load()
	if handleValue == 0 {
		return nil, fmt.Errorf("ffi: clone closed updated fragment offsets")
	}
	runtime.LockOSThread()
	native := o.b.updatedFragmentOffsetsClone(handleValue)
	runtime.UnlockOSThread()
	if native == 0 {
		return nil, mutationError(o.b, "clone updated fragment offsets")
	}
	clone := &UpdatedFragmentOffsets{b: o.b}
	clone.handle.Store(native)
	return clone, nil
}

// Close releases the native value. It is idempotent.
func (o *UpdatedFragmentOffsets) Close() {
	if o == nil {
		return
	}
	handleValue := o.handle.Swap(0)
	if handleValue == 0 {
		return
	}
	runtime.LockOSThread()
	o.b.updatedFragmentOffsetsClose(handleValue)
	runtime.UnlockOSThread()
}

// Count returns the number of fragment entries.
func (o *UpdatedFragmentOffsets) Count() (uint64, error) {
	if o == nil {
		return 0, fmt.Errorf("ffi: read nil updated fragment offsets")
	}
	handleValue := o.handle.Load()
	if handleValue == 0 {
		return 0, fmt.Errorf("ffi: read closed updated fragment offsets")
	}
	var count uint64
	runtime.LockOSThread()
	rc := o.b.updatedFragmentOffsetsCount(handleValue, &count)
	runtime.UnlockOSThread()
	if rc != 0 {
		return 0, mutationError(o.b, "read updated fragment offsets count")
	}
	return count, nil
}

// FragmentIDAt returns the fragment ID of one entry.
func (o *UpdatedFragmentOffsets) FragmentIDAt(index uint64) (uint64, error) {
	if o == nil {
		return 0, fmt.Errorf("ffi: read nil updated fragment offsets")
	}
	handleValue := o.handle.Load()
	if handleValue == 0 {
		return 0, fmt.Errorf("ffi: read closed updated fragment offsets")
	}
	var fragmentID uint64
	runtime.LockOSThread()
	rc := o.b.updatedFragmentOffsetsFragmentIDAt(handleValue, index, &fragmentID)
	runtime.UnlockOSThread()
	if rc != 0 {
		return 0, mutationError(o.b, "read updated fragment offsets fragment id")
	}
	return fragmentID, nil
}

// OffsetAt returns one offset by entry index and ascending offset position.
func (o *UpdatedFragmentOffsets) OffsetAt(index, position uint64) (uint32, error) {
	if o == nil {
		return 0, fmt.Errorf("ffi: read nil updated fragment offsets")
	}
	handleValue := o.handle.Load()
	if handleValue == 0 {
		return 0, fmt.Errorf("ffi: read closed updated fragment offsets")
	}
	var offset uint32
	runtime.LockOSThread()
	rc := o.b.updatedFragmentOffsetsOffsetAt(handleValue, index, position, &offset)
	runtime.UnlockOSThread()
	if rc != 0 {
		return 0, mutationError(o.b, "read updated fragment offsets offset")
	}
	return offset, nil
}

// OffsetsAt returns every offset of one fragment entry in native order.
func (o *UpdatedFragmentOffsets) OffsetsAt(index uint64) ([]uint32, error) {
	if o == nil {
		return nil, fmt.Errorf("ffi: read nil updated fragment offsets")
	}
	handleValue := o.handle.Load()
	if handleValue == 0 {
		return nil, fmt.Errorf("ffi: read closed updated fragment offsets")
	}
	var count uint64
	runtime.LockOSThread()
	rc := o.b.updatedFragmentOffsetsOffsetCountAt(handleValue, index, &count)
	runtime.UnlockOSThread()
	if rc != 0 {
		return nil, mutationError(o.b, "read updated fragment offsets offset count")
	}
	offsets := make([]uint32, 0, count)
	for position := uint64(0); position < count; position++ {
		offset, err := o.OffsetAt(index, position)
		if err != nil {
			return nil, err
		}
		offsets = append(offsets, offset)
	}
	return offsets, nil
}

// UpdateColumnsResult is the owned result of a native fragment update.
type UpdateColumnsResult struct {
	b      *bindings
	handle atomic.Uintptr
}

// Close releases the native result. It is idempotent.
func (r *UpdateColumnsResult) Close() {
	if r == nil {
		return
	}
	handleValue := r.handle.Swap(0)
	if handleValue == 0 {
		return
	}
	runtime.LockOSThread()
	r.b.fragmentUpdateColumnsResultClose(handleValue)
	runtime.UnlockOSThread()
}

// Fragment clones the detached updated fragment metadata.
func (r *UpdateColumnsResult) Fragment() (*Fragment, error) {
	if r == nil {
		return nil, fmt.Errorf("ffi: read nil update columns result")
	}
	handleValue := r.handle.Load()
	if handleValue == 0 {
		return nil, fmt.Errorf("ffi: read closed update columns result")
	}
	var result uintptr
	runtime.LockOSThread()
	rc := r.b.fragmentUpdateColumnsResultFragment(handleValue, &result)
	runtime.UnlockOSThread()
	if rc != 0 {
		return nil, mutationError(r.b, "read update columns fragment")
	}
	if result == 0 {
		return nil, fmt.Errorf("ffi: native update columns result returned NULL fragment")
	}
	return wrapFragment(r.b, result), nil
}

// FieldsModified returns the native modified field IDs in native order.
func (r *UpdateColumnsResult) FieldsModified() ([]uint32, error) {
	if r == nil {
		return nil, fmt.Errorf("ffi: read nil update columns result")
	}
	handleValue := r.handle.Load()
	if handleValue == 0 {
		return nil, fmt.Errorf("ffi: read closed update columns result")
	}
	var count uint64
	runtime.LockOSThread()
	rc := r.b.fragmentUpdateColumnsResultFieldsModifiedCount(handleValue, &count)
	runtime.UnlockOSThread()
	if rc != 0 {
		return nil, mutationError(r.b, "read update columns fields modified count")
	}
	fields := make([]uint32, 0, count)
	for index := uint64(0); index < count; index++ {
		var fieldID uint32
		runtime.LockOSThread()
		rc = r.b.fragmentUpdateColumnsResultFieldsModifiedAt(handleValue, index, &fieldID)
		runtime.UnlockOSThread()
		if rc != 0 {
			return nil, mutationError(r.b, "read update columns modified field id")
		}
		fields = append(fields, fieldID)
	}
	return fields, nil
}

// MatchedOffsets returns matched physical row offsets in native bitmap order.
func (r *UpdateColumnsResult) MatchedOffsets() ([]uint32, error) {
	if r == nil {
		return nil, fmt.Errorf("ffi: read nil update columns result")
	}
	handleValue := r.handle.Load()
	if handleValue == 0 {
		return nil, fmt.Errorf("ffi: read closed update columns result")
	}
	var count uint64
	runtime.LockOSThread()
	rc := r.b.fragmentUpdateColumnsResultMatchedOffsetCount(handleValue, &count)
	runtime.UnlockOSThread()
	if rc != 0 {
		return nil, mutationError(r.b, "read update columns matched offset count")
	}
	offsets := make([]uint32, 0, count)
	for index := uint64(0); index < count; index++ {
		var offset uint32
		runtime.LockOSThread()
		rc = r.b.fragmentUpdateColumnsResultMatchedOffsetAt(handleValue, index, &offset)
		runtime.UnlockOSThread()
		if rc != 0 {
			return nil, mutationError(r.b, "read update columns matched offset")
		}
		offsets = append(offsets, offset)
	}
	return offsets, nil
}

// AddColumnsSQL stages SQL-computed columns on a clone of the fragment.
func (f *Fragment) AddColumnsSQL(columns []SQLColumn, batchSize *uint32) (*Fragment, *Schema, error) {
	if f == nil {
		return nil, nil, fmt.Errorf("ffi: add columns sql on nil fragment")
	}
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, nil, fmt.Errorf("ffi: add columns sql on closed fragment")
	}
	names := make([]string, len(columns))
	expressions := make([]string, len(columns))
	for i := range columns {
		names[i] = columns[i].Name
		expressions[i] = columns[i].Expression
	}
	nameList, err := newCStringList(names)
	if err != nil {
		return nil, nil, err
	}
	expressionList, err := newCStringList(expressions)
	if err != nil {
		return nil, nil, err
	}
	native := make([]sqlColumn, len(columns))
	for i := range columns {
		native[i] = sqlColumn{
			Name:       nameList.ptrs[i],
			Expression: expressionList.ptrs[i],
		}
	}
	var columnsPtr *sqlColumn
	if len(native) > 0 {
		columnsPtr = &native[0]
	}
	var outFragment, outSchema uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentAddColumnsSQL(handleValue, columnsPtr, uint64(len(native)), batchSize, &outFragment, &outSchema)
	runtime.KeepAlive(nameList)
	runtime.KeepAlive(expressionList)
	runtime.KeepAlive(native)
	runtime.UnlockOSThread()
	if rc != 0 {
		closeMutationOutputs(f.b, outFragment, outSchema)
		return nil, nil, mutationError(f.b, "fragment add columns sql")
	}
	return adoptFragmentSchema(f.b, outFragment, outSchema)
}

// AddColumnsNulls stages all-null columns described by columns.
func (f *Fragment) AddColumnsNulls(columns *arrow.Schema) (*Fragment, *Schema, error) {
	if f == nil {
		return nil, nil, fmt.Errorf("ffi: add columns nulls on nil fragment")
	}
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, nil, fmt.Errorf("ffi: add columns nulls on closed fragment")
	}
	var exported cdata.ArrowSchema
	if err := cdata.ExportSchema(columns, &exported); err != nil {
		return nil, nil, err
	}
	defer cdata.ReleaseSchema(&exported)
	var outFragment, outSchema uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentAddColumnsNulls(handleValue, &exported, &outFragment, &outSchema)
	runtime.UnlockOSThread()
	if rc != 0 {
		closeMutationOutputs(f.b, outFragment, outSchema)
		return nil, nil, mutationError(f.b, "fragment add columns nulls")
	}
	return adoptFragmentSchema(f.b, outFragment, outSchema)
}

// AddColumnsStream stages columns read from reader. The stream retains its own
// reference; reader remains owned by the caller.
func (f *Fragment) AddColumnsStream(reader array.RecordReader, batchSize *uint32) (*Fragment, *Schema, error) {
	if f == nil {
		return nil, nil, fmt.Errorf("ffi: add columns stream on nil fragment")
	}
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, nil, fmt.Errorf("ffi: add columns stream on closed fragment")
	}
	var stream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &stream); err != nil {
		return nil, nil, err
	}
	consumed := false
	defer func() {
		if !consumed {
			cdata.ReleaseStream(&stream)
		}
	}()
	var outFragment, outSchema uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentAddColumnsStream(handleValue, &stream, batchSize, &outFragment, &outSchema)
	consumed = true
	runtime.UnlockOSThread()
	if rc != 0 {
		closeMutationOutputs(f.b, outFragment, outSchema)
		return nil, nil, mutationError(f.b, "fragment add columns stream")
	}
	return adoptFragmentSchema(f.b, outFragment, outSchema)
}

// AddColumnsBatchUDF stages columns produced by udf.
func (f *Fragment) AddColumnsBatchUDF(udf BatchUDF, readColumns []string, readColumnsSet bool, batchSize *uint32) (*Fragment, *Schema, error) {
	if f == nil {
		return nil, nil, fmt.Errorf("ffi: add columns batch udf on nil fragment")
	}
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, nil, fmt.Errorf("ffi: add columns batch udf on closed fragment")
	}
	initBatchUDFCallbacks()
	adapter := &batchUDFAdapter{mapper: udf.Mapper, store: udf.Store}
	ctx := handle.New(adapter)
	defer handle.Remove(ctx)
	native := &batchUDF{
		Mapper:       callbackBatchMapper,
		MapperCtx:    uintptr(ctx),
		OutputSchema: udf.OutputSchema,
	}
	if udf.Store != nil {
		native.Store = &batchUDFStore{
			GetBatch:       callbackBatchGet,
			InsertBatch:    callbackBatchInsert,
			GetFragment:    callbackFragmentGet,
			InsertFragment: callbackFragmentInsert,
			CallbackCtx:    uintptr(ctx),
		}
	}
	readPtrs, readList, err := batchUDFReadColumns(readColumns, readColumnsSet)
	if err != nil {
		return nil, nil, err
	}
	var readPtr *uintptr
	if len(readPtrs) > 0 {
		readPtr = &readPtrs[0]
	}
	var outFragment, outSchema uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentAddColumnsBatchUDF(handleValue, native, readPtr, batchSize, &outFragment, &outSchema)
	runtime.KeepAlive(adapter)
	runtime.KeepAlive(native)
	runtime.KeepAlive(readPtrs)
	runtime.KeepAlive(readList)
	runtime.UnlockOSThread()
	if rc != 0 {
		closeMutationOutputs(f.b, outFragment, outSchema)
		return nil, nil, mutationError(f.b, "fragment add columns batch udf")
	}
	return adoptFragmentSchema(f.b, outFragment, outSchema)
}

// MergeColumns merges reader into the fragment using leftOn/rightOn.
func (f *Fragment) MergeColumns(reader array.RecordReader, leftOn, rightOn string) (*Fragment, *Schema, error) {
	if f == nil {
		return nil, nil, fmt.Errorf("ffi: merge columns on nil fragment")
	}
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, nil, fmt.Errorf("ffi: merge columns on closed fragment")
	}
	if err := validateCString(leftOn); err != nil {
		return nil, nil, err
	}
	if err := validateCString(rightOn); err != nil {
		return nil, nil, err
	}
	var stream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &stream); err != nil {
		return nil, nil, err
	}
	consumed := false
	defer func() {
		if !consumed {
			cdata.ReleaseStream(&stream)
		}
	}()
	var outFragment, outSchema uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentMergeColumns(handleValue, &stream, leftOn, rightOn, &outFragment, &outSchema)
	consumed = true
	runtime.UnlockOSThread()
	if rc != 0 {
		closeMutationOutputs(f.b, outFragment, outSchema)
		return nil, nil, mutationError(f.b, "fragment merge columns")
	}
	return adoptFragmentSchema(f.b, outFragment, outSchema)
}

// UpdateColumns stages an update from reader using leftOn/rightOn.
func (f *Fragment) UpdateColumns(reader array.RecordReader, leftOn, rightOn string) (*UpdateColumnsResult, error) {
	if f == nil {
		return nil, fmt.Errorf("ffi: update columns on nil fragment")
	}
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, fmt.Errorf("ffi: update columns on closed fragment")
	}
	if err := validateCString(leftOn); err != nil {
		return nil, err
	}
	if err := validateCString(rightOn); err != nil {
		return nil, err
	}
	var stream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &stream); err != nil {
		return nil, err
	}
	consumed := false
	defer func() {
		if !consumed {
			cdata.ReleaseStream(&stream)
		}
	}()
	var outResult uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentUpdateColumns(handleValue, &stream, leftOn, rightOn, &outResult)
	consumed = true
	runtime.UnlockOSThread()
	if rc != 0 {
		if outResult != 0 {
			f.b.fragmentUpdateColumnsResultClose(outResult)
		}
		return nil, mutationError(f.b, "fragment update columns")
	}
	if outResult == 0 {
		return nil, fmt.Errorf("ffi: native update columns returned NULL result")
	}
	result := &UpdateColumnsResult{b: f.b}
	result.handle.Store(outResult)
	return result, nil
}

// WriteColumns stages a replacement data file for schema.
func (f *Fragment) WriteColumns(schema *Schema, reader array.RecordReader) (*DataReplacementGroup, error) {
	return f.writeColumns(schema, reader, nil)
}

// WriteColumnWithVersion stages a replacement data file with an explicit
// native file format version.
func (f *Fragment) WriteColumnWithVersion(schema *Schema, reader array.RecordReader, fileVersion int32) (*DataReplacementGroup, error) {
	return f.writeColumns(schema, reader, &fileVersion)
}

func (f *Fragment) writeColumns(schema *Schema, reader array.RecordReader, fileVersion *int32) (*DataReplacementGroup, error) {
	if f == nil {
		return nil, fmt.Errorf("ffi: write columns on nil fragment")
	}
	if schema == nil {
		return nil, fmt.Errorf("ffi: write columns with nil schema")
	}
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, fmt.Errorf("ffi: write columns on closed fragment")
	}
	schemaHandle := schema.handle.Load()
	if schemaHandle == 0 {
		return nil, fmt.Errorf("ffi: write columns with closed schema")
	}
	var stream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &stream); err != nil {
		return nil, err
	}
	consumed := false
	defer func() {
		if !consumed {
			cdata.ReleaseStream(&stream)
		}
	}()
	var outGroup uintptr
	runtime.LockOSThread()
	var rc int32
	if fileVersion == nil {
		rc = f.b.fragmentWriteColumns(handleValue, schemaHandle, &stream, &outGroup)
	} else {
		rc = f.b.fragmentWriteColumnWithVersion(handleValue, schemaHandle, &stream, *fileVersion, &outGroup)
	}
	consumed = true
	runtime.UnlockOSThread()
	if rc != 0 {
		if outGroup != 0 {
			f.b.dataReplacementGroupClose(outGroup)
		}
		return nil, mutationError(f.b, "fragment write columns")
	}
	if outGroup == 0 {
		return nil, fmt.Errorf("ffi: native write columns returned NULL group")
	}
	return wrapDataReplacementGroup(f.b, outGroup), nil
}

// DeleteRows stages physical offsets on a clone of the fragment. A NULL native
// output means the offsets deleted every physical row.
func (f *Fragment) DeleteRows(offsets []uint32) (*Fragment, error) {
	if f == nil {
		return nil, fmt.Errorf("ffi: delete rows on nil fragment")
	}
	handleValue := f.handle.Load()
	if handleValue == 0 {
		return nil, fmt.Errorf("ffi: delete rows on closed fragment")
	}
	var ptr *uint32
	if len(offsets) > 0 {
		ptr = &offsets[0]
	}
	var outFragment uintptr
	runtime.LockOSThread()
	rc := f.b.fragmentDeleteRows(handleValue, ptr, uint64(len(offsets)), &outFragment)
	runtime.KeepAlive(offsets)
	runtime.UnlockOSThread()
	if rc != 0 {
		if outFragment != 0 {
			f.b.fragmentClose(outFragment)
		}
		return nil, mutationError(f.b, "fragment delete rows")
	}
	if outFragment == 0 {
		return nil, nil
	}
	return wrapFragment(f.b, outFragment), nil
}

func closeMutationOutputs(b *bindings, fragment, schema uintptr) {
	if fragment != 0 {
		b.fragmentClose(fragment)
	}
	if schema != 0 {
		b.schemaClose(schema)
	}
}

func mutationError(b *bindings, op string) error {
	if nativeErr := b.takeError(); nativeErr != nil {
		return nativeErr
	}
	return fmt.Errorf("ffi: %s failed without setting an error", op)
}

func adoptFragmentSchema(b *bindings, fragment, schema uintptr) (*Fragment, *Schema, error) {
	if fragment != 0 && schema != 0 {
		return wrapFragment(b, fragment), wrapSchema(b, schema), nil
	}
	if fragment != 0 {
		wrapFragment(b, fragment).Close()
	}
	if schema != 0 {
		wrapSchema(b, schema).Close()
	}
	return nil, nil, fmt.Errorf("ffi: native fragment mutation returned incomplete fragment/schema outputs")
}

// UpdateOperationOptions carries the subset of native Update options needed to
// publish fragment update results.
type UpdateOperationOptions struct {
	UpdatedFragments       []*Fragment
	FieldsModified         []uint32
	UpdatedFragmentOffsets *UpdatedFragmentOffsets
}

type compactedSsTable struct {
	DataFile     *byte
	DeletionFile *byte
}

// updateModeRewriteColumns is LANCE_UPDATE_MODE_REWRITE_COLUMNS. Fragment
// UpdateColumns stages a horizontal column rewrite, so publishing its result
// must carry that native mode.
const updateModeRewriteColumns int32 = 1

type updateOperationOptions struct {
	RemovedFragmentIDs                 *uint64
	RemovedFragmentCount               uint64
	UpdatedFragments                   *uintptr
	UpdatedFragmentCount               uint64
	NewFragments                       *uintptr
	NewFragmentCount                   uint64
	FieldsModified                     *uint32
	FieldsModifiedCount                uint64
	CompactedSsTables                  *compactedSsTable
	CompactedSsTableCount              uint64
	FieldsForPreservingFragBitmap      *uint32
	FieldsForPreservingFragBitmapCount uint64
	HasUpdateMode                      bool
	UpdateMode                         int32
	InsertedRowsFilter                 uintptr
	UpdatedFragmentOffsets             uintptr
}

// NewMergeOperation clones a complete final fragment list and complete schema.
func NewMergeOperation(schema *Schema, fragments []*Fragment, preservesNullability bool) (*Operation, error) {
	if schema == nil {
		return nil, fmt.Errorf("ffi: merge operation with nil schema")
	}
	schemaHandle := schema.handle.Load()
	if schemaHandle == 0 {
		return nil, fmt.Errorf("ffi: merge operation with closed schema")
	}
	handles := make([]uintptr, len(fragments))
	for i, fragment := range fragments {
		if fragment == nil {
			return nil, fmt.Errorf("ffi: merge operation fragment %d is nil", i)
		}
		handleValue := fragment.handle.Load()
		if handleValue == 0 {
			return nil, fmt.Errorf("ffi: merge operation fragment %d is closed", i)
		}
		handles[i] = handleValue
	}
	var fragmentsPtr *uintptr
	if len(handles) > 0 {
		fragmentsPtr = &handles[0]
	}
	operation, err := newOperation(schema.b, func(b *bindings) uintptr {
		native := b.operationMergeNew(schemaHandle, fragmentsPtr, uint64(len(handles)), preservesNullability)
		runtime.KeepAlive(handles)
		return native
	})
	if err != nil {
		return nil, err
	}
	return operation, nil
}

// NewUpdateOperation clones updated fragments and options into a native Update.
func NewUpdateOperation(options UpdateOperationOptions) (*Operation, error) {
	var handles []uintptr
	var b *bindings
	if len(options.UpdatedFragments) > 0 {
		var err error
		handles, b, err = fragmentHandles(options.UpdatedFragments)
		if err != nil {
			return nil, err
		}
	}
	var offsetsHandle uintptr
	if options.UpdatedFragmentOffsets != nil {
		offsetsHandle = options.UpdatedFragmentOffsets.handle.Load()
		if offsetsHandle == 0 {
			return nil, fmt.Errorf("ffi: update operation with closed updated fragment offsets")
		}
		if b == nil {
			b = options.UpdatedFragmentOffsets.b
		}
	}
	if b == nil {
		var err error
		b, err = load()
		if err != nil {
			return nil, err
		}
	}
	var fragmentsPtr *uintptr
	if len(handles) > 0 {
		fragmentsPtr = &handles[0]
	}
	var fieldsPtr *uint32
	if len(options.FieldsModified) > 0 {
		fieldsPtr = &options.FieldsModified[0]
	}
	native := &updateOperationOptions{
		UpdatedFragments:       fragmentsPtr,
		UpdatedFragmentCount:   uint64(len(handles)),
		FieldsModified:         fieldsPtr,
		FieldsModifiedCount:    uint64(len(options.FieldsModified)),
		HasUpdateMode:          true,
		UpdateMode:             updateModeRewriteColumns,
		UpdatedFragmentOffsets: offsetsHandle,
	}
	operation, err := newOperation(b, func(b *bindings) uintptr {
		out := b.operationUpdateNew(native)
		runtime.KeepAlive(handles)
		runtime.KeepAlive(options.FieldsModified)
		return out
	})
	if err != nil {
		return nil, err
	}
	return operation, nil
}

// MergeFragmentCount returns the number of fragments in a Merge operation.
func (o *Operation) MergeFragmentCount() (uint64, error) {
	return o.count("operation merge fragment count", o.b.operationMergeFragmentCount)
}

// MergeFragmentAt clones one fragment from a Merge operation.
func (o *Operation) MergeFragmentAt(index uint64) (*Fragment, error) {
	handleValue := o.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	var fragment uintptr
	runtime.LockOSThread()
	rc := o.b.operationMergeFragmentAt(handleValue, index, &fragment)
	runtime.UnlockOSThread()
	if rc != 0 {
		return nil, mutationError(o.b, "operation merge fragment at")
	}
	if fragment == 0 {
		return nil, fmt.Errorf("ffi: native merge fragment returned NULL")
	}
	return wrapFragment(o.b, fragment), nil
}

// UpdateUpdatedFragmentCount returns the number of updated fragments.
func (o *Operation) UpdateUpdatedFragmentCount() (uint64, error) {
	return o.count("operation update updated fragment count", o.b.operationUpdateUpdatedFragmentCount)
}

// UpdateUpdatedFragmentAt clones one updated fragment.
func (o *Operation) UpdateUpdatedFragmentAt(index uint64) (*Fragment, error) {
	handleValue := o.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	var fragment uintptr
	runtime.LockOSThread()
	rc := o.b.operationUpdateUpdatedFragmentAt(handleValue, index, &fragment)
	runtime.UnlockOSThread()
	if rc != 0 {
		return nil, mutationError(o.b, "operation update updated fragment at")
	}
	if fragment == 0 {
		return nil, fmt.Errorf("ffi: native update fragment returned NULL")
	}
	return wrapFragment(o.b, fragment), nil
}

// UpdateFieldsModified returns the operation's modified field IDs.
func (o *Operation) UpdateFieldsModified() ([]uint32, error) {
	handleValue := o.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	var count uint64
	runtime.LockOSThread()
	rc := o.b.operationUpdateFieldsModifiedCount(handleValue, &count)
	runtime.UnlockOSThread()
	if rc != 0 {
		return nil, mutationError(o.b, "operation update fields modified count")
	}
	fields := make([]uint32, 0, count)
	for index := uint64(0); index < count; index++ {
		var fieldID uint32
		runtime.LockOSThread()
		rc = o.b.operationUpdateFieldsModifiedAt(handleValue, index, &fieldID)
		runtime.UnlockOSThread()
		if rc != 0 {
			return nil, mutationError(o.b, "operation update fields modified at")
		}
		fields = append(fields, fieldID)
	}
	return fields, nil
}

// UpdateUpdatedFragmentOffsets returns the native offset value when present.
func (o *Operation) UpdateUpdatedFragmentOffsets() (*UpdatedFragmentOffsets, bool, error) {
	handleValue := o.handle.Load()
	if handleValue == 0 {
		return nil, false, ErrClosed
	}
	var present bool
	var offsets uintptr
	runtime.LockOSThread()
	rc := o.b.operationUpdateUpdatedFragmentOffsets(handleValue, &present, &offsets)
	runtime.UnlockOSThread()
	if rc != 0 {
		return nil, false, mutationError(o.b, "operation update updated fragment offsets")
	}
	if !present {
		return nil, false, nil
	}
	if offsets == 0 {
		return nil, false, fmt.Errorf("ffi: native update offsets reported present with NULL value")
	}
	result := &UpdatedFragmentOffsets{b: o.b}
	result.handle.Store(offsets)
	return result, true, nil
}
