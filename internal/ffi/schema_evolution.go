package ffi

import (
	"fmt"
	"runtime"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/cdata"
)

// FieldReferenceKind selects how a field reference identifies a field.
type FieldReferenceKind int32

const (
	FieldReferenceID   FieldReferenceKind = 0
	FieldReferencePath FieldReferenceKind = 1
)

// FieldReference identifies a field by id or by nested path.
type FieldReference struct {
	Kind    FieldReferenceKind
	FieldID int32
	Path    string
}

// FieldMetadataUpdate replaces or merges metadata for one field.
type FieldMetadataUpdate struct {
	Field  FieldReference
	Keys   []string
	Values []*string
	Mode   int32
}

// ColumnAlteration is one native alter_columns entry. Rename nil keeps the
// name; NullableMode uses the native sentinel values; DataType nil keeps the
// type. At least one of them must change.
type ColumnAlteration struct {
	Path         string
	Rename       *string
	NullableMode int32
	DataType     arrow.DataType
}

// SQLColumn is one add_columns_sql entry.
type SQLColumn struct {
	Name       string
	Expression string
}

type fieldReference struct {
	Kind    int32
	FieldID int32
	Path    *byte
}

type fieldMetadataUpdate struct {
	Field  fieldReference
	Keys   *uintptr
	Values *uintptr
	Count  uint64
	Mode   int32
}

type sqlColumn struct {
	Name       uintptr
	Expression uintptr
}

type columnAlteration struct {
	Path         *byte
	Rename       *byte
	NullableMode int32
	DataType     *cdata.ArrowSchema
}

func (d *Dataset) MetadataJSON() (string, error) {
	return d.readMetadataJSON(d.b.datasetMetadataJSON)
}

func (d *Dataset) UpdateMetadataJSON(keys []string, values []*string, mode int32) (string, error) {
	return d.updateMetadataJSON(d.b.datasetUpdateMetadataJSON, keys, values, mode)
}

func (d *Dataset) UpdateSchemaMetadataJSON(keys []string, values []*string, mode int32) (string, error) {
	return d.updateMetadataJSON(d.b.datasetUpdateSchemaMetadataJSON, keys, values, mode)
}

func (d *Dataset) UpdateFieldMetadata(updates []FieldMetadataUpdate) error {
	if len(updates) == 0 {
		return invalidf("field metadata update list is empty")
	}
	native := make([]fieldMetadataUpdate, len(updates))
	lists := make([]*cStringList, 0, len(updates)*2)
	paths := make([][]byte, 0, len(updates))
	defer func() {
		for _, list := range lists {
			list.keepAlive()
		}
	}()
	for i, update := range updates {
		if len(update.Keys) != len(update.Values) {
			return invalidf("field metadata update keys and values have different lengths")
		}
		if update.Mode != 0 && update.Mode != 1 {
			return invalidf("field metadata update mode %d is invalid", update.Mode)
		}
		ref, path, err := nativeFieldReference(update.Field)
		if err != nil {
			return err
		}
		if path != nil {
			paths = append(paths, path)
		}
		keys, err := newCStringList(update.Keys)
		if err != nil {
			return err
		}
		lists = append(lists, keys)
		values, err := newNullableCStringList(update.Values)
		if err != nil {
			return err
		}
		lists = append(lists, values)
		native[i] = fieldMetadataUpdate{
			Field:  ref,
			Keys:   listPtr(keys),
			Values: listPtr(values),
			Count:  uint64(len(update.Keys)),
			Mode:   update.Mode,
		}
	}

	handleID := d.handle.Load()
	if handleID == 0 {
		return ErrClosed
	}
	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.datasetUpdateFieldMetadata(handleID, &native[0], uint64(len(native))); rc != 0 {
		if err := b.takeError(); err != nil {
			return err
		}
		return fmt.Errorf("ffi: lance_dataset_update_field_metadata failed without setting an error")
	}
	runtime.KeepAlive(native)
	runtime.KeepAlive(lists)
	runtime.KeepAlive(paths)
	return nil
}

func (d *Dataset) FieldIDCount() (uint64, error) {
	handleID := d.handle.Load()
	if handleID == 0 {
		return 0, ErrClosed
	}
	b := d.b
	var count uint64
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.datasetFieldIDCount(handleID, &count); rc != 0 {
		if err := b.takeError(); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("ffi: lance_dataset_field_id_count failed without setting an error")
	}
	return count, nil
}

func (d *Dataset) FieldIDAt(index uint64) (int32, error) {
	handleID := d.handle.Load()
	if handleID == 0 {
		return 0, ErrClosed
	}
	b := d.b
	var fieldID int32
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.datasetFieldIDAt(handleID, index, &fieldID); rc != 0 {
		if err := b.takeError(); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("ffi: lance_dataset_field_id_at failed without setting an error")
	}
	return fieldID, nil
}

func (d *Dataset) ResolveFieldID(ref FieldReference) (int32, error) {
	native, path, err := nativeFieldReference(ref)
	if err != nil {
		return 0, err
	}
	handleID := d.handle.Load()
	if handleID == 0 {
		return 0, ErrClosed
	}
	b := d.b
	var fieldID int32
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.datasetResolveFieldID(handleID, &native, &fieldID); rc != 0 {
		if err := b.takeError(); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("ffi: lance_dataset_resolve_field_id failed without setting an error")
	}
	runtime.KeepAlive(path)
	return fieldID, nil
}

func (d *Dataset) DropColumns(columns []string) error {
	if len(columns) == 0 {
		return invalidf("drop columns list is empty")
	}
	list, err := newCStringList(columns)
	if err != nil {
		return err
	}
	handleID := d.handle.Load()
	if handleID == 0 {
		return ErrClosed
	}
	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.datasetDropColumns(handleID, listPtr(list), uint64(len(columns))); rc != 0 {
		if err := b.takeError(); err != nil {
			return err
		}
		return fmt.Errorf("ffi: lance_dataset_drop_columns failed without setting an error")
	}
	list.keepAlive()
	return nil
}

func (d *Dataset) AlterColumns(alterations []ColumnAlteration) error {
	if len(alterations) == 0 {
		return invalidf("alter columns list is empty")
	}
	native := make([]columnAlteration, len(alterations))
	schemas := make([]cdata.ArrowSchema, 0, len(alterations))
	paths := make([][]byte, 0, len(alterations)*2)
	defer func() {
		for i := range schemas {
			cdata.ReleaseSchema(&schemas[i])
		}
	}()
	for i, alteration := range alterations {
		path, pathPtr, err := newCStringPtr(alteration.Path)
		if err != nil {
			return err
		}
		paths = append(paths, path)
		native[i].Path = pathPtr
		if alteration.Rename != nil {
			buf, ptr, err := newCStringPtr(*alteration.Rename)
			if err != nil {
				return err
			}
			paths = append(paths, buf)
			native[i].Rename = ptr
		}
		native[i].NullableMode = alteration.NullableMode
		if alteration.DataType != nil {
			schema := arrow.NewSchema([]arrow.Field{{Name: "", Type: alteration.DataType, Nullable: true}}, nil)
			var exported cdata.ArrowSchema
			if err := cdata.ExportSchema(schema, &exported); err != nil {
				return fmt.Errorf("ffi: export alter column data type: %w", err)
			}
			schemas = append(schemas, exported)
			if exported.Children == nil || *exported.Children == nil {
				return invalidf("exported alter column data type has no child schema")
			}
			native[i].DataType = *exported.Children
		}
	}

	handleID := d.handle.Load()
	if handleID == 0 {
		return ErrClosed
	}
	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.datasetAlterColumns(handleID, &native[0], uint64(len(native))); rc != 0 {
		if err := b.takeError(); err != nil {
			return err
		}
		return fmt.Errorf("ffi: lance_dataset_alter_columns failed without setting an error")
	}
	runtime.KeepAlive(native)
	runtime.KeepAlive(paths)
	runtime.KeepAlive(schemas)
	return nil
}

func (d *Dataset) AddColumnsSQL(columns []SQLColumn, batchSize uint64) error {
	if len(columns) == 0 {
		return invalidf("add columns SQL list is empty")
	}
	names := make([]string, len(columns))
	expressions := make([]string, len(columns))
	for i, column := range columns {
		names[i] = column.Name
		expressions[i] = column.Expression
	}
	nameList, err := newCStringList(names)
	if err != nil {
		return err
	}
	exprList, err := newCStringList(expressions)
	if err != nil {
		return err
	}
	native := make([]sqlColumn, len(columns))
	for i := range native {
		native[i] = sqlColumn{
			Name:       nameList.ptrs[i],
			Expression: exprList.ptrs[i],
		}
	}
	handleID := d.handle.Load()
	if handleID == 0 {
		return ErrClosed
	}
	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.datasetAddColumnsSQL(handleID, &native[0], uint64(len(native)), batchSize); rc != 0 {
		if err := b.takeError(); err != nil {
			return err
		}
		return fmt.Errorf("ffi: lance_dataset_add_columns_sql failed without setting an error")
	}
	nameList.keepAlive()
	exprList.keepAlive()
	runtime.KeepAlive(native)
	return nil
}

func (d *Dataset) AddColumnsNulls(schema *arrow.Schema) error {
	if schema == nil {
		return invalidf("add columns schema is nil")
	}
	var exported cdata.ArrowSchema
	if err := cdata.ExportSchema(schema, &exported); err != nil {
		return fmt.Errorf("ffi: export add columns schema: %w", err)
	}
	defer cdata.ReleaseSchema(&exported)
	handleID := d.handle.Load()
	if handleID == 0 {
		return ErrClosed
	}
	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if rc := b.datasetAddColumnsNulls(handleID, &exported); rc != 0 {
		if err := b.takeError(); err != nil {
			return err
		}
		return fmt.Errorf("ffi: lance_dataset_add_columns_nulls failed without setting an error")
	}
	runtime.KeepAlive(&exported)
	return nil
}

func (d *Dataset) AddColumnsStream(reader array.RecordReader, batchSize uint64) error {
	if reader == nil {
		return invalidf("add columns stream reader is nil")
	}
	var stream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &stream); err != nil {
		return fmt.Errorf("ffi: export add columns stream: %w", err)
	}
	consumed := false
	defer func() {
		if !consumed {
			cdata.ReleaseStream(&stream)
		}
	}()

	handleID := d.handle.Load()
	if handleID == 0 {
		return ErrClosed
	}
	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	rc := b.datasetAddColumnsStream(handleID, &stream, batchSize)
	// Native consumes the stream on every return path after a non-NULL value.
	consumed = true
	if rc != 0 {
		if err := b.takeError(); err != nil {
			return err
		}
		return fmt.Errorf("ffi: lance_dataset_add_columns_stream failed without setting an error")
	}
	runtime.KeepAlive(&stream)
	return nil
}

func (d *Dataset) readMetadataJSON(call func(uintptr) *byte) (string, error) {
	handleID := d.handle.Load()
	if handleID == 0 {
		return "", ErrClosed
	}
	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	out := call(handleID)
	if out == nil {
		if err := b.takeError(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("ffi: metadata JSON call returned a nil pointer without setting an error")
	}
	defer b.freeString(out)
	return goString(out), nil
}

func (d *Dataset) updateMetadataJSON(call func(uintptr, *uintptr, *uintptr, uint64, int32) *byte, keys []string, values []*string, mode int32) (string, error) {
	if len(keys) != len(values) {
		return "", invalidf("metadata update keys and values have different lengths")
	}
	if mode != 0 && mode != 1 {
		return "", invalidf("metadata update mode %d is invalid", mode)
	}
	keyList, err := newCStringList(keys)
	if err != nil {
		return "", err
	}
	valueList, err := newNullableCStringList(values)
	if err != nil {
		return "", err
	}
	handleID := d.handle.Load()
	if handleID == 0 {
		return "", ErrClosed
	}
	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	out := call(handleID, listPtr(keyList), listPtr(valueList), uint64(len(keys)), mode)
	keyList.keepAlive()
	valueList.keepAlive()
	if out == nil {
		if err := b.takeError(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("ffi: metadata update returned a nil pointer without setting an error")
	}
	defer b.freeString(out)
	return goString(out), nil
}

func nativeFieldReference(ref FieldReference) (fieldReference, []byte, error) {
	switch ref.Kind {
	case FieldReferenceID:
		if ref.FieldID < 0 {
			return fieldReference{}, nil, invalidf("field id %d is negative", ref.FieldID)
		}
		return fieldReference{Kind: int32(FieldReferenceID), FieldID: ref.FieldID}, nil, nil
	case FieldReferencePath:
		if ref.Path == "" {
			return fieldReference{}, nil, invalidf("field path is empty")
		}
		path, ptr, err := newCStringPtr(ref.Path)
		if err != nil {
			return fieldReference{}, nil, err
		}
		return fieldReference{Kind: int32(FieldReferencePath), Path: ptr}, path, nil
	default:
		return fieldReference{}, nil, invalidf("field reference kind %d is invalid", ref.Kind)
	}
}

func listPtr(list *cStringList) *uintptr {
	if list == nil || len(list.ptrs) == 0 {
		return nil
	}
	return &list.ptrs[0]
}
