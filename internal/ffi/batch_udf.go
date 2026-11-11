package ffi

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/cdata"
	"github.com/lance-format/lance-go/internal/handle"
)

// BatchInfo identifies one cached batch of one fragment during a batch UDF.
type BatchInfo struct {
	FragmentID uint32
	BatchIndex uint64
}

// BatchUDFStore is the checkpoint store used by a batch UDF. Every method is
// required. Batches passed to InsertBatch are borrowed for the call; retain a
// batch before keeping it beyond the return. Batches returned by GetBatch are
// owned by the adapter and released after the callback exports them.
type BatchUDFStore interface {
	GetBatch(info BatchInfo) (arrow.RecordBatch, bool, error)
	InsertBatch(info BatchInfo, batch arrow.RecordBatch) error
	GetFragment(fragmentID uint32) (string, bool, error)
	InsertFragment(fragmentID uint64, fragmentJSON string) error
}

// BatchUDF is the native-facing batch UDF configuration.
type BatchUDF struct {
	Mapper       func(arrow.RecordBatch) (arrow.RecordBatch, error)
	OutputSchema *cdata.ArrowSchema
	Store        BatchUDFStore
}

type batchInfo struct {
	FragmentID uint32
	BatchIndex uint64
}

type ownedString struct {
	Data    *byte
	Release uintptr
}

type callbackError struct {
	Code    int32
	Message ownedString
}

type batchUDFStore struct {
	GetBatch       uintptr
	InsertBatch    uintptr
	GetFragment    uintptr
	InsertFragment uintptr
	CallbackCtx    uintptr
}

type batchUDF struct {
	Mapper       uintptr
	MapperCtx    uintptr
	OutputSchema *cdata.ArrowSchema
	Store        *batchUDFStore
}

type batchUDFAdapter struct {
	mapper func(arrow.RecordBatch) (arrow.RecordBatch, error)
	store  BatchUDFStore
}

var (
	batchUDFCallbacksOnce  sync.Once
	ownedReleaseOnce       sync.Once
	callbackBatchMapper    uintptr
	callbackBatchGet       uintptr
	callbackBatchInsert    uintptr
	callbackFragmentGet    uintptr
	callbackFragmentInsert uintptr
	callbackOwnedRelease   uintptr
)

func initBatchUDFCallbacks() {
	batchUDFCallbacksOnce.Do(func() {
		callbackBatchMapper = purego.NewCallback(batchUDFMapperCallback)
		callbackBatchGet = purego.NewCallback(batchUDFGetBatchCallback)
		callbackBatchInsert = purego.NewCallback(batchUDFInsertBatchCallback)
		callbackFragmentGet = purego.NewCallback(batchUDFGetFragmentCallback)
		callbackFragmentInsert = purego.NewCallback(batchUDFInsertFragmentCallback)
	})
	initOwnedReleaseCallback()
}

// initOwnedReleaseCallback installs the shared release hook used by every
// native-owned string produced by a Go callback. Namespace context providers
// reuse it, so the process-wide callback budget grows by exactly one for the
// provider entry point itself, not one per string field.
func initOwnedReleaseCallback() {
	ownedReleaseOnce.Do(func() {
		callbackOwnedRelease = purego.NewCallback(ownedStringReleaseCallback)
	})
}

// AddColumnsBatchUDF computes one or more new columns through a Go mapper.
// readColumnsSet distinguishes the C NULL read-columns option from an empty
// list. batchSize nil likewise means the option is absent; a non-nil value is
// forwarded verbatim, including zero.
func (d *Dataset) AddColumnsBatchUDF(udf BatchUDF, readColumns []string, readColumnsSet bool, batchSize *uint32) error {
	initBatchUDFCallbacks()
	if udf.Mapper == nil {
		return fmt.Errorf("ffi: batch UDF mapper is nil")
	}
	if udf.OutputSchema == nil {
		return fmt.Errorf("ffi: batch UDF output schema is nil")
	}
	if err := validateCStrings(readColumns); err != nil {
		return err
	}

	readPtrs, readList, err := batchUDFReadColumns(readColumns, readColumnsSet)
	if err != nil {
		return err
	}

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

	var readPtr *uintptr
	if readPtrs != nil {
		readPtr = &readPtrs[0]
	}

	handleID := d.handle.Load()
	if handleID == 0 {
		return ErrClosed
	}
	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if rc := b.datasetAddColumnsBatchUDF(handleID, native, readPtr, batchSize); rc != 0 {
		if nativeErr := b.takeError(); nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_add_columns_batch_udf failed without setting an error")
	}
	runtime.KeepAlive(adapter)
	runtime.KeepAlive(readPtrs)
	runtime.KeepAlive(readList)
	runtime.KeepAlive(native)
	return nil
}

func batchUDFReadColumns(columns []string, present bool) ([]uintptr, *cStringList, error) {
	if !present {
		return nil, nil, nil
	}
	if len(columns) == 0 {
		// A non-NULL pointer to a NULL entry represents Some(empty).
		return []uintptr{0}, &cStringList{}, nil
	}
	list, err := newCStringList(columns)
	if err != nil {
		return nil, nil, err
	}
	return list.ptrs, list, nil
}

func batchUDFMapperCallback(ctx uintptr, input *cdata.ArrowArray, inputSchema *cdata.ArrowSchema, output *cdata.ArrowArray, cbErr *callbackError) (ret uintptr) {
	if output == nil {
		setCallbackError(cbErr, errors.New("batch UDF mapper received a nil output slot"))
		return 1
	}
	*output = cdata.ArrowArray{}
	clearCallbackError(cbErr)
	defer func() {
		if r := recover(); r != nil {
			if output.Release != 0 {
				cdata.ReleaseArray(output)
			}
			*output = cdata.ArrowArray{}
			setCallbackError(cbErr, fmt.Errorf("batch UDF mapper panic: %v", r))
			ret = 1
		}
	}()
	adapter, ok := lookupBatchUDFAdapter(ctx)
	if !ok {
		setCallbackError(cbErr, errors.New("batch UDF adapter is no longer available"))
		return 1
	}

	// The native input is borrowed for this callback only. Copying it before
	// invoking the mapper keeps retained batches and output columns valid
	// after the producer input is released.
	inputBatch, err := cdata.CopyBorrowedRecordBatch(input, inputSchema)
	if err != nil {
		setCallbackError(cbErr, err)
		return 1
	}
	defer inputBatch.Release()

	result, err := adapter.mapper(inputBatch)
	if err != nil {
		setCallbackError(cbErr, err)
		return 1
	}
	if result == nil {
		setCallbackError(cbErr, errors.New("batch UDF mapper returned a nil record batch"))
		return 1
	}
	defer result.Release()
	if err := cdata.ExportRecordBatch(result, output); err != nil {
		setCallbackError(cbErr, err)
		return 1
	}
	return 0
}

func batchUDFGetBatchCallback(ctx uintptr, info *batchInfo, output *cdata.ArrowArray, cbErr *callbackError) (ret uintptr) {
	if output == nil {
		setCallbackError(cbErr, errors.New("batch UDF get_batch received a nil output slot"))
		return ^uintptr(0)
	}
	*output = cdata.ArrowArray{}
	clearCallbackError(cbErr)
	defer func() {
		if r := recover(); r != nil {
			if output.Release != 0 {
				cdata.ReleaseArray(output)
			}
			*output = cdata.ArrowArray{}
			setCallbackError(cbErr, fmt.Errorf("batch UDF get_batch panic: %v", r))
			ret = ^uintptr(0)
		}
	}()
	adapter, ok := lookupBatchUDFAdapter(ctx)
	if !ok {
		setCallbackError(cbErr, errors.New("batch UDF adapter is no longer available"))
		return ^uintptr(0)
	}
	if info == nil {
		setCallbackError(cbErr, errors.New("batch UDF get_batch received nil info"))
		return ^uintptr(0)
	}
	batch, found, err := adapter.store.GetBatch(BatchInfo{FragmentID: info.FragmentID, BatchIndex: info.BatchIndex})
	if err != nil {
		setCallbackError(cbErr, err)
		return ^uintptr(0)
	}
	if !found {
		return 0
	}
	if batch == nil {
		setCallbackError(cbErr, errors.New("batch UDF store returned a nil batch with found=true"))
		return ^uintptr(0)
	}
	defer batch.Release()
	if err := cdata.ExportRecordBatch(batch, output); err != nil {
		setCallbackError(cbErr, err)
		return ^uintptr(0)
	}
	return 1
}

func batchUDFInsertBatchCallback(ctx uintptr, info *batchInfo, batch *cdata.ArrowArray, batchSchema *cdata.ArrowSchema, cbErr *callbackError) (ret uintptr) {
	clearCallbackError(cbErr)
	defer func() {
		if r := recover(); r != nil {
			setCallbackError(cbErr, fmt.Errorf("batch UDF insert_batch panic: %v", r))
			ret = 1
		}
	}()
	adapter, ok := lookupBatchUDFAdapter(ctx)
	if !ok {
		setCallbackError(cbErr, errors.New("batch UDF adapter is no longer available"))
		return 1
	}
	if info == nil || batch == nil || batchSchema == nil {
		setCallbackError(cbErr, errors.New("batch UDF insert_batch received nil data"))
		return 1
	}
	schema, err := cdata.ImportSchema(batchSchema)
	if err != nil {
		setCallbackError(cbErr, fmt.Errorf("import checkpoint batch schema: %w", err))
		return 1
	}
	owned, err := cdata.ImportRecordBatch(batch, schema)
	if err != nil {
		setCallbackError(cbErr, fmt.Errorf("import checkpoint batch: %w", err))
		return 1
	}
	defer owned.Release()
	if err := adapter.store.InsertBatch(BatchInfo{FragmentID: info.FragmentID, BatchIndex: info.BatchIndex}, owned); err != nil {
		setCallbackError(cbErr, err)
		return 1
	}
	return 0
}

func batchUDFGetFragmentCallback(ctx uintptr, fragmentID uint32, out *ownedString, cbErr *callbackError) (ret uintptr) {
	if out == nil {
		setCallbackError(cbErr, errors.New("batch UDF get_fragment received a nil output slot"))
		return ^uintptr(0)
	}
	*out = ownedString{}
	clearCallbackError(cbErr)
	defer func() {
		if r := recover(); r != nil {
			*out = ownedString{}
			setCallbackError(cbErr, fmt.Errorf("batch UDF get_fragment panic: %v", r))
			ret = ^uintptr(0)
		}
	}()
	adapter, ok := lookupBatchUDFAdapter(ctx)
	if !ok {
		setCallbackError(cbErr, errors.New("batch UDF adapter is no longer available"))
		return ^uintptr(0)
	}
	fragmentJSON, found, err := adapter.store.GetFragment(fragmentID)
	if err != nil {
		setCallbackError(cbErr, err)
		return ^uintptr(0)
	}
	if !found {
		return 0
	}
	*out = newOwnedString(fragmentJSON)
	return 1
}

func batchUDFInsertFragmentCallback(ctx uintptr, fragmentID uint64, fragmentJSON *byte, cbErr *callbackError) (ret uintptr) {
	if adapter, ok := lookupCommitLockContext(ctx); ok {
		return adapter.acquire(fragmentID, (*unsafe.Pointer)(unsafe.Pointer(fragmentJSON)), cbErr)
	}
	clearCallbackError(cbErr)
	defer func() {
		if r := recover(); r != nil {
			setCallbackError(cbErr, fmt.Errorf("batch UDF insert_fragment panic: %v", r))
			ret = 1
		}
	}()
	adapter, ok := lookupBatchUDFAdapter(ctx)
	if !ok {
		setCallbackError(cbErr, errors.New("batch UDF adapter is no longer available"))
		return 1
	}
	// The native string is borrowed for this callback; copy it before the Go
	// store can retain it.
	if err := adapter.store.InsertFragment(fragmentID, goString(fragmentJSON)); err != nil {
		setCallbackError(cbErr, err)
		return 1
	}
	return 0
}

func lookupBatchUDFAdapter(ctx uintptr) (*batchUDFAdapter, bool) {
	if ctx == 0 {
		return nil, false
	}
	value, ok := handle.Get(uint64(ctx))
	if !ok {
		return nil, false
	}
	adapter, ok := value.(*batchUDFAdapter)
	return adapter, ok && adapter != nil
}

func clearCallbackError(dst *callbackError) {
	if dst != nil {
		*dst = callbackError{}
	}
}

func setCallbackError(dst *callbackError, err error) {
	if dst == nil || err == nil {
		return
	}
	message := err.Error()
	if message == "" {
		message = "Go callback failed"
	}
	*dst = callbackError{Code: callbackErrorCode(err), Message: newOwnedString(message)}
}

func callbackErrorCode(err error) int32 {
	if err == nil {
		return int32(CodeOK)
	}
	var native *Error
	if errors.As(err, &native) && native != nil && native.Code != CodeOK {
		return int32(native.Code)
	}
	var coded interface{ ErrorCode() int32 }
	if errors.As(err, &coded) {
		if code := Code(coded.ErrorCode()); code != CodeOK {
			return int32(code)
		}
	}
	// A generic Go error has no native category. Reporting internal keeps it
	// from being misclassified as a caller argument error.
	return int32(CodeInternal)
}

var ownedStrings sync.Map // map[uintptr]*ownedStringBox

type ownedStringBox struct {
	value  ownedString
	data   []byte
	pinner runtime.Pinner
}

func newOwnedString(value string) ownedString {
	initOwnedReleaseCallback()
	data := append([]byte(value), 0)
	box := &ownedStringBox{data: data}
	box.pinner.Pin(&data[0])
	box.value.Data = &data[0]
	box.value.Release = callbackOwnedRelease
	ownedStrings.Store(uintptr(unsafe.Pointer(&data[0])), box)
	return box.value
}

func ownedStringReleaseCallback(s *ownedString) (ret uintptr) {
	if s == nil || s.Data == nil {
		return
	}
	key := uintptr(unsafe.Pointer(s.Data))
	s.Release = 0
	if value, ok := ownedStrings.LoadAndDelete(key); ok {
		box, _ := value.(*ownedStringBox)
		box.pinner.Unpin()
		runtime.KeepAlive(box)
	}
	return
}
