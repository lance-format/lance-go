package ffi

import (
	"fmt"
	"runtime"
	"sort"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/cdata"
)

// OperationKind mirrors LanceOperationKind.
type OperationKind int32

const (
	OperationUnknown           OperationKind = 0
	OperationAppend            OperationKind = 1
	OperationDelete            OperationKind = 2
	OperationOverwrite         OperationKind = 3
	OperationCreateIndex       OperationKind = 4
	OperationRewrite           OperationKind = 5
	OperationDataReplacement   OperationKind = 6
	OperationDataOverlay       OperationKind = 7
	OperationMerge             OperationKind = 8
	OperationRestore           OperationKind = 9
	OperationReserveFragments  OperationKind = 10
	OperationUpdate            OperationKind = 11
	OperationProject           OperationKind = 12
	OperationUpdateConfig      OperationKind = 13
	OperationUpdateMemWALState OperationKind = 14
	OperationClone             OperationKind = 15
	OperationUpdateBases       OperationKind = 16
)

// Operation is an owned native transaction operation.
type Operation struct {
	b      *bindings
	handle atomic.Uintptr
}

func wrapOperation(b *bindings, handle uintptr) *Operation {
	op := &Operation{b: b}
	op.handle.Store(handle)
	return op
}

// Close releases the operation handle. It is idempotent.
func (o *Operation) Close() {
	handle := o.handle.Swap(0)
	if handle == 0 {
		return
	}
	runtime.LockOSThread()
	o.b.operationClose(handle)
	runtime.UnlockOSThread()
}

// Kind returns the operation variant.
func (o *Operation) Kind() (OperationKind, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return OperationUnknown, ErrClosed
	}
	var kind int32
	runtime.LockOSThread()
	rc := o.b.operationKind(handle, &kind)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return OperationUnknown, nativeErr
		}
		return OperationUnknown, fmt.Errorf("ffi: lance_operation_kind failed without setting an error")
	}
	runtime.UnlockOSThread()
	return OperationKind(kind), nil
}

// FragmentCount returns the fragment count for Append or Overwrite.
func (o *Operation) FragmentCount() (uint64, error) {
	return o.count("lance_operation_fragment_count", o.b.operationFragmentCount)
}

// AppendFragmentCount returns the number of fragments in an Append operation.
func (o *Operation) AppendFragmentCount() (uint64, error) {
	return o.count("lance_operation_append_fragment_count", o.b.operationAppendFragmentCount)
}

func (o *Operation) count(name string, call func(uintptr, *uint64) int32) (uint64, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}
	var count uint64
	runtime.LockOSThread()
	rc := call(handle, &count)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: %s failed without setting an error", name)
	}
	runtime.UnlockOSThread()
	return count, nil
}

// AppendFragmentAt clones one Append fragment as an independent detached handle.
func (o *Operation) AppendFragmentAt(index uint64) (*Fragment, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	var out uintptr
	runtime.LockOSThread()
	rc := o.b.operationAppendFragmentAt(handle, index, &out)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_operation_append_fragment_at failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_operation_append_fragment_at returned NULL without setting an error")
	}
	return wrapFragment(o.b, out), nil
}

// RestoreVersion returns the target version of a Restore operation.
func (o *Operation) RestoreVersion() (uint64, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}
	var version uint64
	runtime.LockOSThread()
	rc := o.b.operationRestoreVersion(handle, &version)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_operation_restore_version failed without setting an error")
	}
	runtime.UnlockOSThread()
	return version, nil
}

// ReserveFragmentsCount returns the reserved fragment count.
func (o *Operation) ReserveFragmentsCount() (uint32, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}
	var count uint32
	runtime.LockOSThread()
	rc := o.b.operationReserveFragmentsCount(handle, &count)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_operation_reserve_fragments_count failed without setting an error")
	}
	runtime.UnlockOSThread()
	return count, nil
}

// UpdateBasesCount returns the number of new base paths.
func (o *Operation) UpdateBasesCount() (uint64, error) {
	return o.count("lance_operation_update_bases_count", o.b.operationUpdateBasesCount)
}

// BasePath is one native base-path registration.
type BasePath struct {
	ID            uint32
	Path          string
	Name          *string
	IsDatasetRoot bool
}

// BasePathAt copies one borrowed base-path view from an UpdateBases operation.
func (o *Operation) BasePathAt(index uint64) (BasePath, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return BasePath{}, ErrClosed
	}
	var view basePathView
	runtime.LockOSThread()
	rc := o.b.operationUpdateBasesAt(handle, index, &view)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return BasePath{}, nativeErr
		}
		return BasePath{}, fmt.Errorf("ffi: lance_operation_update_bases_at failed without setting an error")
	}
	if view.Path == 0 {
		runtime.UnlockOSThread()
		return BasePath{}, fmt.Errorf("ffi: lance_operation_update_bases_at returned a NULL path")
	}
	result := BasePath{
		ID:            view.ID,
		Path:          goString((*byte)(unsafe.Pointer(view.Path))),
		IsDatasetRoot: view.IsDatasetRoot,
	}
	if view.Name != 0 {
		name := goString((*byte)(unsafe.Pointer(view.Name)))
		result.Name = &name
	}
	runtime.UnlockOSThread()
	return result, nil
}

// DeleteUpdatedFragmentCount returns the number of updated Delete fragments.
func (o *Operation) DeleteUpdatedFragmentCount() (uint64, error) {
	return o.count("lance_operation_delete_updated_fragment_count", o.b.operationDeleteUpdatedFragmentCount)
}

// DeleteUpdatedFragmentMetadataJSONAt serializes one updated Delete fragment.
func (o *Operation) DeleteUpdatedFragmentMetadataJSONAt(index uint64) ([]byte, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	runtime.LockOSThread()
	ptr := o.b.operationDeleteUpdatedFragmentMetadataJSONAt(handle, index)
	if ptr == nil {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_operation_delete_updated_fragment_metadata_json_at returned NULL without setting an error")
	}
	value := goString(ptr)
	o.b.freeString(ptr)
	runtime.UnlockOSThread()
	return []byte(value), nil
}

// DeleteDeletedFragmentCount returns the number of fully deleted fragment IDs.
func (o *Operation) DeleteDeletedFragmentCount() (uint64, error) {
	return o.count("lance_operation_delete_deleted_fragment_count", o.b.operationDeleteDeletedFragmentCount)
}

// DeleteDeletedFragmentIDAt returns one fully deleted fragment ID.
func (o *Operation) DeleteDeletedFragmentIDAt(index uint64) (uint64, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}
	var id uint64
	runtime.LockOSThread()
	rc := o.b.operationDeleteDeletedFragmentIDAt(handle, index, &id)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_operation_delete_deleted_fragment_id_at failed without setting an error")
	}
	runtime.UnlockOSThread()
	return id, nil
}

// DeletePredicate returns the Delete operation's SQL predicate.
func (o *Operation) DeletePredicate() (string, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return "", ErrClosed
	}
	runtime.LockOSThread()
	ptr := o.b.operationDeletePredicate(handle)
	if ptr == nil {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_operation_delete_predicate returned NULL without setting an error")
	}
	value := goString(ptr)
	o.b.freeString(ptr)
	runtime.UnlockOSThread()
	return value, nil
}

// DataReplacementCount returns the number of data replacement groups.
func (o *Operation) DataReplacementCount() (uint64, error) {
	return o.count("lance_operation_data_replacement_count", o.b.operationDataReplacementCount)
}

// DataReplacementAt clones one data replacement group.
func (o *Operation) DataReplacementAt(index uint64) (*DataReplacementGroup, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	var out uintptr
	runtime.LockOSThread()
	rc := o.b.operationDataReplacementAt(handle, index, &out)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_operation_data_replacement_at failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_operation_data_replacement_at returned NULL without setting an error")
	}
	return wrapDataReplacementGroup(o.b, out), nil
}

// Schema exports a lossy Arrow view of an Overwrite, Merge or Project schema.
func (o *Operation) Schema() (*arrow.Schema, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	var raw cdata.ArrowSchema
	runtime.LockOSThread()
	rc := o.b.operationSchema(handle, &raw)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_operation_schema failed without setting an error")
	}
	runtime.UnlockOSThread()
	defer cdata.ReleaseSchema(&raw)
	schema, err := cdata.ImportSchema(&raw)
	if err != nil {
		return nil, fmt.Errorf("ffi: import operation schema: %w", err)
	}
	return schema, nil
}

// NativeSchema clones the in-memory native operation schema.
func (o *Operation) NativeSchema() (*Schema, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	var out uintptr
	runtime.LockOSThread()
	rc := o.b.operationSchemaClone(handle, &out)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_operation_schema_clone failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_operation_schema_clone returned NULL without setting an error")
	}
	return wrapSchema(o.b, out), nil
}

// PreservesNullability reads the assertion from a Merge or Project operation.
func (o *Operation) PreservesNullability() (bool, error) {
	handle := o.handle.Load()
	if handle == 0 {
		return false, ErrClosed
	}
	var preserves bool
	runtime.LockOSThread()
	rc := o.b.operationPreservesNullability(handle, &preserves)
	if rc != 0 {
		nativeErr := o.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return false, nativeErr
		}
		return false, fmt.Errorf("ffi: lance_operation_preserves_nullability failed without setting an error")
	}
	runtime.UnlockOSThread()
	return preserves, nil
}

// DataReplacementGroup is an owned native data replacement group.
type DataReplacementGroup struct {
	b      *bindings
	handle atomic.Uintptr
}

func wrapDataReplacementGroup(b *bindings, handle uintptr) *DataReplacementGroup {
	group := &DataReplacementGroup{b: b}
	group.handle.Store(handle)
	return group
}

// NewDataReplacementGroup clones newFile into a new group.
func NewDataReplacementGroup(fragmentID uint64, newFile *DataFile) (*DataReplacementGroup, error) {
	if newFile == nil {
		return nil, fmt.Errorf("ffi: data file must not be nil")
	}
	handle := newFile.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	runtime.LockOSThread()
	out := newFile.b.dataReplacementGroupNew(fragmentID, handle)
	if out == 0 {
		nativeErr := newFile.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_data_replacement_group_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapDataReplacementGroup(newFile.b, out), nil
}

// Close releases the group handle. It is idempotent.
func (g *DataReplacementGroup) Close() {
	handle := g.handle.Swap(0)
	if handle == 0 {
		return
	}
	runtime.LockOSThread()
	g.b.dataReplacementGroupClose(handle)
	runtime.UnlockOSThread()
}

// Clone returns an independent snapshot.
func (g *DataReplacementGroup) Clone() (*DataReplacementGroup, error) {
	handle := g.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	runtime.LockOSThread()
	out := g.b.dataReplacementGroupClone(handle)
	if out == 0 {
		nativeErr := g.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_data_replacement_group_clone returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapDataReplacementGroup(g.b, out), nil
}

// FragmentID returns the native fragment ID. Zero is valid.
func (g *DataReplacementGroup) FragmentID() (uint64, error) {
	handle := g.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}
	runtime.LockOSThread()
	id := g.b.dataReplacementGroupFragmentID(handle)
	nativeErr := g.b.takeError()
	runtime.UnlockOSThread()
	if nativeErr != nil {
		return 0, nativeErr
	}
	return id, nil
}

// File clones the group's data file.
func (g *DataReplacementGroup) File() (*DataFile, error) {
	handle := g.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	var out uintptr
	runtime.LockOSThread()
	rc := g.b.dataReplacementGroupFile(handle, &out)
	if rc != 0 {
		nativeErr := g.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_data_replacement_group_file failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_data_replacement_group_file returned NULL without setting an error")
	}
	return wrapDataFile(g.b, out), nil
}

// Transaction is an owned native Lance transaction.
type Transaction struct {
	b      *bindings
	handle atomic.Uintptr
}

func wrapTransaction(b *bindings, handle uintptr) *Transaction {
	tx := &Transaction{b: b}
	tx.handle.Store(handle)
	return tx
}

// Close releases the transaction handle. It is idempotent.
func (tx *Transaction) Close() {
	handle := tx.handle.Swap(0)
	if handle == 0 {
		return
	}
	runtime.LockOSThread()
	tx.b.transactionClose(handle)
	runtime.UnlockOSThread()
}

// ReadVersion returns the native read version. Zero is valid for an
// uncommitted create.
func (tx *Transaction) ReadVersion() (uint64, error) {
	handle := tx.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}
	var version uint64
	runtime.LockOSThread()
	rc := tx.b.transactionReadVersion(handle, &version)
	if rc != 0 {
		nativeErr := tx.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_transaction_read_version failed without setting an error")
	}
	runtime.UnlockOSThread()
	return version, nil
}

// UUID returns the native transaction UUID.
func (tx *Transaction) UUID() (string, error) {
	handle := tx.handle.Load()
	if handle == 0 {
		return "", ErrClosed
	}
	runtime.LockOSThread()
	ptr := tx.b.transactionUUID(handle)
	if ptr == nil {
		nativeErr := tx.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_transaction_uuid returned NULL without setting an error")
	}
	value := goString(ptr)
	tx.b.freeString(ptr)
	runtime.UnlockOSThread()
	return value, nil
}

// Tag returns the optional transaction tag.
func (tx *Transaction) Tag() (string, bool, error) {
	handle := tx.handle.Load()
	if handle == 0 {
		return "", false, ErrClosed
	}
	var ptr *byte
	runtime.LockOSThread()
	rc := tx.b.transactionTag(handle, &ptr)
	if rc < 0 {
		nativeErr := tx.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return "", false, nativeErr
		}
		return "", false, fmt.Errorf("ffi: lance_transaction_tag failed without setting an error")
	}
	if rc == 0 {
		runtime.UnlockOSThread()
		return "", false, nil
	}
	if ptr == nil {
		runtime.UnlockOSThread()
		return "", false, fmt.Errorf("ffi: lance_transaction_tag reported a present tag but returned NULL")
	}
	value := goString(ptr)
	tx.b.freeString(ptr)
	runtime.UnlockOSThread()
	return value, true, nil
}

// HasProperties reports whether the native property map is Some, including
// Some(empty).
func (tx *Transaction) HasProperties() (bool, error) {
	handle := tx.handle.Load()
	if handle == 0 {
		return false, ErrClosed
	}
	var present bool
	runtime.LockOSThread()
	rc := tx.b.transactionHasProperties(handle, &present)
	if rc != 0 {
		nativeErr := tx.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return false, nativeErr
		}
		return false, fmt.Errorf("ffi: lance_transaction_has_properties failed without setting an error")
	}
	runtime.UnlockOSThread()
	return present, nil
}

// PropertyCount returns the native transaction property count.
func (tx *Transaction) PropertyCount() (uint64, error) {
	handle := tx.handle.Load()
	if handle == 0 {
		return 0, ErrClosed
	}
	var count uint64
	runtime.LockOSThread()
	rc := tx.b.transactionPropertyCount(handle, &count)
	if rc != 0 {
		nativeErr := tx.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_transaction_property_count failed without setting an error")
	}
	runtime.UnlockOSThread()
	return count, nil
}

// Properties returns the native transaction property map. The boolean reports
// whether the map is present, distinguishing absent from present-empty.
func (tx *Transaction) Properties() (map[string]string, bool, error) {
	present, err := tx.HasProperties()
	if err != nil || !present {
		return nil, present, err
	}
	count, err := tx.PropertyCount()
	if err != nil {
		return nil, true, err
	}
	properties := make(map[string]string, count)
	for i := uint64(0); i < count; i++ {
		key, err := tx.propertyString(tx.b.transactionPropertyKeyAt, i)
		if err != nil {
			return nil, true, err
		}
		value, err := tx.propertyString(tx.b.transactionPropertyValueAt, i)
		if err != nil {
			return nil, true, err
		}
		properties[key] = value
	}
	return properties, true, nil
}

func (tx *Transaction) propertyString(call func(uintptr, uint64) *byte, index uint64) (string, error) {
	handle := tx.handle.Load()
	if handle == 0 {
		return "", ErrClosed
	}
	runtime.LockOSThread()
	ptr := call(handle, index)
	if ptr == nil {
		nativeErr := tx.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: transaction property lookup returned NULL without setting an error")
	}
	value := goString(ptr)
	tx.b.freeString(ptr)
	runtime.UnlockOSThread()
	return value, nil
}

// Operation clones the transaction's operation.
func (tx *Transaction) Operation() (*Operation, error) {
	handle := tx.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	var out uintptr
	runtime.LockOSThread()
	rc := tx.b.transactionOperation(handle, &out)
	if rc != 0 {
		nativeErr := tx.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_transaction_operation failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_transaction_operation returned NULL without setting an error")
	}
	return wrapOperation(tx.b, out), nil
}

// MarshalBinary serializes the transaction through native protobuf conversion.
func (tx *Transaction) MarshalBinary() ([]byte, error) {
	handle := tx.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	var ptr *byte
	var length uint64
	runtime.LockOSThread()
	rc := tx.b.transactionToProto(handle, &ptr, &length)
	if rc != 0 {
		nativeErr := tx.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_transaction_to_proto failed without setting an error")
	}
	if ptr == nil && length != 0 {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("ffi: lance_transaction_to_proto returned NULL for %d bytes", length)
	}
	if length > uint64(^uint(0)>>1) {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("ffi: transaction protobuf length %d exceeds addressable memory", length)
	}
	var data []byte
	if length > 0 {
		data = make([]byte, int(length))
		copy(data, unsafe.Slice(ptr, int(length)))
	}
	if ptr != nil {
		tx.b.freeBytes(ptr)
	}
	runtime.UnlockOSThread()
	return data, nil
}

// ParseTransaction parses one complete native protobuf transaction.
func ParseTransaction(data []byte) (*Transaction, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	out := b.transactionFromProto(data, uint64(len(data)))
	if out == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_transaction_from_proto returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapTransaction(b, out), nil
}

// ReadTransaction reads the current snapshot's transaction. It returns
// (nil, nil) when the manifest has no transaction file.
func (d *Dataset) ReadTransaction() (*Transaction, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	var out uintptr
	runtime.LockOSThread()
	rc := d.b.datasetReadTransaction(handle, &out)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_read_transaction failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, nil
	}
	return wrapTransaction(d.b, out), nil
}

// ReadTransactionByVersion reads a specific version's transaction.
func (d *Dataset) ReadTransactionByVersion(version uint64) (*Transaction, error) {
	handle := d.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	var out uintptr
	runtime.LockOSThread()
	rc := d.b.datasetReadTransactionByVersion(handle, version, &out)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_read_transaction_by_version failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, nil
	}
	return wrapTransaction(d.b, out), nil
}

// TransactionOptions controls native transaction construction.
type TransactionOptions struct {
	UUID          *string
	Tag           *string
	Properties    map[string]string
	HasProperties bool
}

// NewAppendOperation clones fragments into a native Append operation.
func NewAppendOperation(fragments []*Fragment) (*Operation, error) {
	if len(fragments) == 0 {
		return newOperation(nil, func(b *bindings) uintptr {
			return b.operationAppendNew(nil, 0)
		})
	}
	handles, b, err := fragmentHandles(fragments)
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	out := b.operationAppendNew(handles, uint64(len(handles)))
	runtime.KeepAlive(handles)
	if out == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_operation_append_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapOperation(b, out), nil
}

// NewDeleteOperation clones updated fragments and deleted IDs into a native
// Delete operation.
func NewDeleteOperation(updated []*Fragment, deletedIDs []uint64, predicate string) (*Operation, error) {
	if err := validateCString(predicate); err != nil {
		return nil, err
	}
	var handles []uintptr
	var b *bindings
	if len(updated) > 0 {
		var err error
		handles, b, err = fragmentHandles(updated)
		if err != nil {
			return nil, err
		}
	}
	if b == nil {
		var err error
		b, err = load()
		if err != nil {
			return nil, err
		}
	}
	runtime.LockOSThread()
	out := b.operationDeleteNew(handles, uint64(len(handles)), deletedIDs, uint64(len(deletedIDs)), predicate)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(deletedIDs)
	runtime.KeepAlive(predicate)
	if out == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_operation_delete_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapOperation(b, out), nil
}

// NewRestoreOperation constructs a native Restore operation.
func NewRestoreOperation(version uint64) (*Operation, error) {
	return newOperation(nil, func(b *bindings) uintptr {
		return b.operationRestoreNew(version)
	})
}

// NewReserveFragmentsOperation constructs a native ReserveFragments operation.
func NewReserveFragmentsOperation(count uint32) (*Operation, error) {
	return newOperation(nil, func(b *bindings) uintptr {
		return b.operationReserveFragmentsNew(count)
	})
}

// NewUpdateBasesOperation constructs a native UpdateBases operation.
func NewUpdateBasesOperation(bases []BasePath) (*Operation, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	raw := make([]basePath, len(bases))
	bufs := make([][][]byte, len(bases))
	for i, base := range bases {
		if err := validateCString(base.Path); err != nil {
			return nil, err
		}
		raw[i].ID = base.ID
		raw[i].IsDatasetRoot = base.IsDatasetRoot
		pathBuf := append([]byte(base.Path), 0)
		bufs[i] = append(bufs[i], pathBuf)
		raw[i].Path = uintptr(unsafe.Pointer(&pathBuf[0]))
		if base.Name != nil {
			if err := validateCString(*base.Name); err != nil {
				return nil, err
			}
			nameBuf := append([]byte(*base.Name), 0)
			bufs[i] = append(bufs[i], nameBuf)
			raw[i].Name = uintptr(unsafe.Pointer(&nameBuf[0]))
		}
	}
	var ptr *basePath
	if len(raw) > 0 {
		ptr = &raw[0]
	}
	runtime.LockOSThread()
	out := b.operationUpdateBasesNew(ptr, uint64(len(raw)))
	runtime.KeepAlive(raw)
	runtime.KeepAlive(bufs)
	if out == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_operation_update_bases_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapOperation(b, out), nil
}

// NewDataReplacementOperation constructs a native DataReplacement operation.
func NewDataReplacementOperation(groups []*DataReplacementGroup) (*Operation, error) {
	if len(groups) == 0 {
		return newOperation(nil, func(b *bindings) uintptr {
			return b.operationDataReplacementNew(nil, 0)
		})
	}
	handles := make([]uintptr, len(groups))
	for i, group := range groups {
		if group == nil {
			return nil, fmt.Errorf("ffi: data replacement group %d must not be nil", i)
		}
		handle := group.handle.Load()
		if handle == 0 {
			return nil, ErrClosed
		}
		handles[i] = handle
	}
	b := groups[0].b
	runtime.LockOSThread()
	out := b.operationDataReplacementNew(handles, uint64(len(handles)))
	runtime.KeepAlive(handles)
	if out == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_operation_data_replacement_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapOperation(b, out), nil
}

// NewProjectOperation constructs a native Project operation from a native schema.
func NewProjectOperation(schema *Schema, preservesNullability bool) (*Operation, error) {
	if schema == nil {
		return nil, fmt.Errorf("ffi: schema must not be nil")
	}
	handle := schema.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	var out uintptr
	runtime.LockOSThread()
	rc := schema.b.operationProjectNew(handle, preservesNullability, &out)
	if rc != 0 {
		nativeErr := schema.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_operation_project_new failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_operation_project_new returned NULL without setting an error")
	}
	return wrapOperation(schema.b, out), nil
}

// NewTransaction constructs a native transaction from an operation. The
// operation is cloned and remains usable.
func NewTransaction(readVersion uint64, operation *Operation, opts TransactionOptions) (*Transaction, error) {
	if operation == nil {
		return nil, fmt.Errorf("ffi: operation must not be nil")
	}
	handle := operation.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	raw, keepAlives, err := transactionOptions(opts)
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	out := operation.b.transactionNew(readVersion, handle, raw)
	for _, keep := range keepAlives {
		keep()
	}
	if out == 0 {
		nativeErr := operation.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_transaction_new returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapTransaction(operation.b, out), nil
}

func transactionOptions(opts TransactionOptions) (*transactionOptions, []func(), error) {
	raw := &transactionOptions{}
	var keepAlives []func()
	if opts.UUID != nil {
		list, err := newCStringList([]string{*opts.UUID})
		if err != nil {
			return nil, nil, err
		}
		raw.UUID = list.ptrs[0]
		keepAlives = append(keepAlives, list.keepAlive)
	}
	if opts.Tag != nil {
		list, err := newCStringList([]string{*opts.Tag})
		if err != nil {
			return nil, nil, err
		}
		raw.Tag = list.ptrs[0]
		keepAlives = append(keepAlives, list.keepAlive)
	}
	if opts.HasProperties {
		raw.HasTransactionProperties = true
		keys := make([]string, 0, len(opts.Properties))
		for key := range opts.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		values := make([]string, len(keys))
		for i, key := range keys {
			values[i] = opts.Properties[key]
		}
		keyList, err := newCStringList(keys)
		if err != nil {
			return nil, nil, err
		}
		valueList, err := newCStringList(values)
		if err != nil {
			return nil, nil, err
		}
		if len(keys) > 0 {
			raw.PropertyKeys = uintptr(unsafe.Pointer(&keyList.ptrs[0]))
			raw.PropertyValues = uintptr(unsafe.Pointer(&valueList.ptrs[0]))
		}
		raw.PropertyCount = uint64(len(keys))
		keepAlives = append(keepAlives, keyList.keepAlive, valueList.keepAlive)
	}
	return raw, keepAlives, nil
}

func newOperation(_ *bindings, call func(*bindings) uintptr) (*Operation, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	out := call(b)
	if out == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: native operation constructor returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapOperation(b, out), nil
}

func fragmentHandles(fragments []*Fragment) ([]uintptr, *bindings, error) {
	if len(fragments) == 0 {
		return nil, nil, fmt.Errorf("ffi: at least one fragment is required")
	}
	handles := make([]uintptr, len(fragments))
	for i, fragment := range fragments {
		if fragment == nil {
			return nil, nil, fmt.Errorf("ffi: fragment %d must not be nil", i)
		}
		handle := fragment.handle.Load()
		if handle == 0 {
			return nil, nil, ErrClosed
		}
		handles[i] = handle
	}
	b := fragments[0].b
	return handles, b, nil
}

// CommitTimeoutMode mirrors LanceCommitTimeoutMode.
type CommitTimeoutMode int32

const (
	CommitTimeoutDefault  CommitTimeoutMode = 0
	CommitTimeoutDisabled CommitTimeoutMode = 1
	CommitTimeoutExplicit CommitTimeoutMode = 2
)

// CommitTimeout selects the native overall commit timeout.
type CommitTimeout struct {
	Mode     CommitTimeoutMode
	Duration time.Duration
}

// CommitOptions controls native transaction publication.
type CommitOptions struct {
	UseStableRowIDs       *bool
	EnableV2ManifestPaths *bool
	MaxRetries            *uint32
	RetryTimeout          *time.Duration
	Timeout               CommitTimeout
	Detached              bool
	SkipAutoCleanup       bool
	DataStorageVersion    *string
	StorageOptions        map[string]string
	Session               *Session
}

type commitOptions struct {
	HasUseStableRowIDs       bool
	UseStableRowIDs          bool
	HasEnableV2ManifestPaths bool
	EnableV2ManifestPaths    bool
	HasMaxRetries            bool
	MaxRetries               uint32
	HasRetryTimeout          bool
	RetryTimeoutSeconds      uint64
	RetryTimeoutNanos        uint32
	TimeoutMode              int32
	TimeoutSeconds           uint64
	TimeoutNanos             uint32
	Detached                 bool
	SkipAutoCleanup          bool
	DataStorageVersion       uintptr
}

// Commit publishes one transaction through CommitBuilder::execute.
func Commit(uri string, tx *Transaction, opts CommitOptions) (*Dataset, error) {
	if tx == nil {
		return nil, fmt.Errorf("ffi: transaction must not be nil")
	}
	handle := tx.handle.Load()
	if handle == 0 {
		return nil, ErrClosed
	}
	raw, storageList, versionList, err := buildCommitOptions(opts)
	if err != nil {
		return nil, err
	}
	if err := validateCString(uri); err != nil {
		return nil, err
	}
	session := uintptr(0)
	if opts.Session != nil {
		session = opts.Session.nativeHandle()
	}

	var out uintptr
	runtime.LockOSThread()
	rc := tx.b.transactionCommit(uri, handle, raw, storageList.ptrs, session, &out)
	storageList.keepAlive()
	versionList.keepAlive()
	runtime.KeepAlive(raw)
	if rc != 0 {
		nativeErr := tx.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_transaction_commit failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_transaction_commit returned NULL without setting an error")
	}
	ds := &Dataset{b: tx.b}
	ds.handle.Store(out)
	return ds, nil
}

// CommitBatch publishes append transactions through CommitBuilder::execute_batch.
func CommitBatch(uri string, txs []*Transaction, opts CommitOptions) (*Dataset, *Transaction, error) {
	b, err := load()
	if err != nil {
		return nil, nil, err
	}
	if err := validateCString(uri); err != nil {
		return nil, nil, err
	}
	var handles []uintptr
	if len(txs) > 0 {
		b = txs[0].b
		handles = make([]uintptr, len(txs))
		for i, tx := range txs {
			if tx == nil {
				return nil, nil, fmt.Errorf("ffi: transaction %d must not be nil", i)
			}
			handle := tx.handle.Load()
			if handle == 0 {
				return nil, nil, ErrClosed
			}
			handles[i] = handle
		}
	}
	raw, storageList, versionList, err := buildCommitOptions(opts)
	if err != nil {
		return nil, nil, err
	}
	session := uintptr(0)
	if opts.Session != nil {
		session = opts.Session.nativeHandle()
	}

	var outDataset, outTransaction uintptr
	runtime.LockOSThread()
	rc := b.transactionCommitBatch(uri, handles, uint64(len(handles)), raw, storageList.ptrs, session, &outDataset, &outTransaction)
	storageList.keepAlive()
	versionList.keepAlive()
	runtime.KeepAlive(raw)
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nil, nativeErr
		}
		return nil, nil, fmt.Errorf("ffi: lance_transaction_commit_batch failed without setting an error")
	}
	runtime.UnlockOSThread()
	if outDataset == 0 || outTransaction == 0 {
		return nil, nil, fmt.Errorf("ffi: lance_transaction_commit_batch returned NULL output without setting an error")
	}
	ds := &Dataset{b: b}
	ds.handle.Store(outDataset)
	return ds, wrapTransaction(b, outTransaction), nil
}

func buildCommitOptions(opts CommitOptions) (*commitOptions, *cStringList, *cStringList, error) {
	raw := &commitOptions{}
	if opts.UseStableRowIDs != nil {
		raw.HasUseStableRowIDs = true
		raw.UseStableRowIDs = *opts.UseStableRowIDs
	}
	if opts.EnableV2ManifestPaths != nil {
		raw.HasEnableV2ManifestPaths = true
		raw.EnableV2ManifestPaths = *opts.EnableV2ManifestPaths
	}
	if opts.MaxRetries != nil {
		raw.HasMaxRetries = true
		raw.MaxRetries = *opts.MaxRetries
	}
	if opts.RetryTimeout != nil {
		if *opts.RetryTimeout < 0 {
			return nil, nil, nil, fmt.Errorf("ffi: retry timeout must not be negative")
		}
		seconds, nanos := splitDuration(*opts.RetryTimeout)
		raw.HasRetryTimeout = true
		raw.RetryTimeoutSeconds = seconds
		raw.RetryTimeoutNanos = nanos
	}
	switch opts.Timeout.Mode {
	case CommitTimeoutDefault, CommitTimeoutDisabled:
		raw.TimeoutMode = int32(opts.Timeout.Mode)
	case CommitTimeoutExplicit:
		if opts.Timeout.Duration < 0 {
			return nil, nil, nil, fmt.Errorf("ffi: commit timeout must not be negative")
		}
		raw.TimeoutMode = int32(CommitTimeoutExplicit)
		raw.TimeoutSeconds, raw.TimeoutNanos = splitDuration(opts.Timeout.Duration)
	default:
		return nil, nil, nil, fmt.Errorf("ffi: invalid commit timeout mode %d", opts.Timeout.Mode)
	}
	raw.Detached = opts.Detached
	raw.SkipAutoCleanup = opts.SkipAutoCleanup

	versionList := &cStringList{}
	if opts.DataStorageVersion != nil {
		list, err := newCStringList([]string{*opts.DataStorageVersion})
		if err != nil {
			return nil, nil, nil, err
		}
		versionList = list
		raw.DataStorageVersion = list.ptrs[0]
	}
	storageList, err := storageOptionList(opts.StorageOptions)
	if err != nil {
		return nil, nil, nil, err
	}
	return raw, storageList, versionList, nil
}

func splitDuration(d time.Duration) (uint64, uint32) {
	seconds := uint64(d / time.Second)
	nanos := uint32(d % time.Second)
	return seconds, nanos
}

// WriteUncommittedOptions augments a write with uncommitted transaction
// properties.
type WriteUncommittedOptions struct {
	Write                 WriteOptions
	StorageOptions        map[string]string
	TransactionProperties map[string]string
	HasProperties         bool
	Session               *Session
}

type uncommittedWriteOptions struct {
	Write                 writeOptions
	TransactionProperties uintptr
}

// WriteUncommitted writes reader as uncommitted Lance data files.
func WriteUncommitted(uri string, reader array.RecordReader, opts WriteUncommittedOptions) (*Transaction, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, fmt.Errorf("ffi: record reader must not be nil")
	}
	schema := reader.Schema()
	if schema == nil {
		return nil, fmt.Errorf("ffi: record reader schema must not be nil")
	}
	writeOpts := opts.Write
	if writeOpts.Mode < WriteModeCreate || writeOpts.Mode > WriteModeOverwrite {
		return nil, fmt.Errorf("ffi: invalid write mode %d", writeOpts.Mode)
	}
	if err := validateCString(uri); err != nil {
		return nil, err
	}
	if writeOpts.Params.DataStorageVersion != "" {
		if err := validateCString(writeOpts.Params.DataStorageVersion); err != nil {
			return nil, err
		}
	}
	storageList, err := storageOptionList(opts.StorageOptions)
	if err != nil {
		return nil, err
	}
	versionList := &cStringList{}
	if writeOpts.Params.DataStorageVersion != "" {
		versionList, err = newCStringList([]string{writeOpts.Params.DataStorageVersion})
		if err != nil {
			return nil, err
		}
	}

	var raw uncommittedWriteOptions
	raw.Write.Params.MaxRowsPerFile = writeOpts.Params.MaxRowsPerFile
	raw.Write.Params.MaxRowsPerGroup = writeOpts.Params.MaxRowsPerGroup
	raw.Write.Params.MaxBytesPerFile = writeOpts.Params.MaxBytesPerFile
	raw.Write.Params.EnableStableRowIDs = writeOpts.Params.EnableStableRowIDs
	if len(versionList.ptrs) > 0 {
		raw.Write.Params.DataStorageVersion = &versionList.bufs[0][0]
	}
	if writeOpts.AutoCleanup != nil {
		if writeOpts.AutoCleanup.OlderThanNanos < -999999999 || writeOpts.AutoCleanup.OlderThanNanos > 999999999 {
			return nil, fmt.Errorf("ffi: auto-cleanup nanoseconds must be in [-999999999, 999999999]")
		}
		raw.Write.AutoCleanup = &autoCleanupParams{
			Interval:         writeOpts.AutoCleanup.Interval,
			OlderThanSeconds: writeOpts.AutoCleanup.OlderThanSeconds,
			OlderThanNanos:   writeOpts.AutoCleanup.OlderThanNanos,
		}
	}
	raw.Write.SkipAutoCleanup = writeOpts.SkipAutoCleanup

	propertyList := &cStringList{}
	if opts.HasProperties {
		keys := make([]string, 0, len(opts.TransactionProperties))
		for key := range opts.TransactionProperties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		values := make([]string, len(keys))
		for i, key := range keys {
			values[i] = opts.TransactionProperties[key]
		}
		if len(keys) == 0 {
			propertyList = &cStringList{ptrs: []uintptr{0}}
		} else {
			propertyList, err = newCStringPairs(keys, values)
			if err != nil {
				return nil, err
			}
		}
		raw.TransactionProperties = uintptr(unsafe.Pointer(&propertyList.ptrs[0]))
	}

	var rawSchema cdata.ArrowSchema
	if err := cdata.ExportSchema(schema, &rawSchema); err != nil {
		return nil, fmt.Errorf("ffi: export write schema: %w", err)
	}
	defer cdata.ReleaseSchema(&rawSchema)

	var rawStream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &rawStream); err != nil {
		return nil, fmt.Errorf("ffi: export write stream: %w", err)
	}
	streamConsumed := false
	defer func() {
		if !streamConsumed {
			cdata.ReleaseStream(&rawStream)
		}
	}()

	session := uintptr(0)
	if opts.Session != nil {
		session = opts.Session.nativeHandle()
	}

	runtime.LockOSThread()
	var out uintptr
	rc := b.datasetWriteUncommittedStream(uri, &rawSchema, &rawStream, int32(writeOpts.Mode), &raw, storageList.ptrs, session, &out)
	streamConsumed = true
	versionList.keepAlive()
	storageList.keepAlive()
	propertyList.keepAlive()
	runtime.KeepAlive(&raw)
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_write_uncommitted_stream failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_dataset_write_uncommitted_stream returned NULL without setting an error")
	}
	return wrapTransaction(b, out), nil
}

func newCStringPairs(keys, values []string) (*cStringList, error) {
	if len(keys) != len(values) {
		return nil, fmt.Errorf("ffi: key/value length mismatch")
	}
	list := &cStringList{
		ptrs:  make([]uintptr, len(keys)*2+1),
		bufs:  make([][]byte, 0, len(keys)*2),
		owned: true,
	}
	for i := range keys {
		keyBuf, err := cStringBuffer(keys[i])
		if err != nil {
			return nil, err
		}
		valueBuf, err := cStringBuffer(values[i])
		if err != nil {
			return nil, err
		}
		list.bufs = append(list.bufs, keyBuf, valueBuf)
		list.ptrs[i*2] = uintptr(unsafe.Pointer(&keyBuf[0]))
		list.ptrs[i*2+1] = uintptr(unsafe.Pointer(&valueBuf[0]))
	}
	return list, nil
}

func cStringBuffer(value string) ([]byte, error) {
	if err := validateCString(value); err != nil {
		return nil, err
	}
	buf := make([]byte, len(value)+1)
	copy(buf, value)
	return buf, nil
}

// Base path raw structs.

type basePath struct {
	ID            uint32
	Path          uintptr
	Name          uintptr
	IsDatasetRoot bool
}

type basePathView struct {
	ID            uint32
	Path          uintptr
	Name          uintptr
	IsDatasetRoot bool
}

type transactionOptions struct {
	UUID                     uintptr
	Tag                      uintptr
	HasTransactionProperties bool
	PropertyKeys             uintptr
	PropertyValues           uintptr
	PropertyCount            uint64
}
