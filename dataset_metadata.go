package lance

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
)

// MetadataUpdateMode controls whether metadata updates merge with or replace
// the existing metadata.
type MetadataUpdateMode int32

const (
	// MetadataMerge updates only the keys present in the update.
	MetadataMerge MetadataUpdateMode = 0
	// MetadataReplace replaces the complete metadata map with the update.
	MetadataReplace MetadataUpdateMode = 1
)

// MetadataUpdate sets or removes one metadata key. A nil Value removes the
// key. An empty non-nil Value is a real empty string.
type MetadataUpdate struct {
	Key   string
	Value *string
}

// FieldReferenceKind selects how a FieldReference identifies a field.
type FieldReferenceKind int32

const (
	FieldReferenceByID   FieldReferenceKind = 0
	FieldReferenceByPath FieldReferenceKind = 1
)

// FieldReference identifies a dataset field by stable field id or by nested
// path. FieldID is used for FieldReferenceByID; Path is used for
// FieldReferenceByPath.
type FieldReference struct {
	Kind    FieldReferenceKind
	FieldID int32
	Path    string
}

// FieldMetadataUpdate applies one metadata update set to a field.
type FieldMetadataUpdate struct {
	Field   FieldReference
	Updates []MetadataUpdate
	Mode    MetadataUpdateMode
}

// TableMetadata returns the dataset's table metadata.
func (d *Dataset) TableMetadata() (map[string]string, error) {
	if err := d.acquire("table metadata"); err != nil {
		return nil, err
	}
	defer d.res.release()
	raw, err := d.native.MetadataJSON()
	if err != nil {
		return nil, wrapError("table metadata", d.uri, err)
	}
	metadata, err := ffi.DecodeStringMap(raw)
	if err != nil {
		return nil, wrapError("table metadata", d.uri, err)
	}
	return metadata, nil
}

// UpdateTableMetadata returns a new Dataset with table metadata updated.
// MetadataMerge updates only the supplied keys; MetadataReplace replaces the
// complete table metadata map. A nil value removes a key.
func (d *Dataset) UpdateTableMetadata(ctx context.Context, mode MetadataUpdateMode, updates ...MetadataUpdate) (*Dataset, map[string]string, error) {
	if err := checkContext(ctx); err != nil {
		return nil, nil, err
	}
	if !validMetadataUpdateMode(mode) {
		return nil, nil, invalidError("update table metadata", d.uri, "invalid metadata update mode %d", mode)
	}
	keys, values, err := metadataUpdateArgs(updates)
	if err != nil {
		return nil, nil, invalidError("update table metadata", d.uri, "%v", err)
	}
	var metadata map[string]string
	next, err := d.mutate("update table metadata", func(native *ffi.Dataset) error {
		raw, err := native.UpdateMetadataJSON(keys, values, int32(mode))
		if err != nil {
			return err
		}
		metadata, err = ffi.DecodeStringMap(raw)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return next, metadata, nil
}

// SchemaMetadata returns the dataset schema's metadata.
func (d *Dataset) SchemaMetadata() (arrow.Metadata, error) {
	if err := d.acquire("schema metadata"); err != nil {
		return arrow.Metadata{}, err
	}
	defer d.res.release()
	schema, err := d.native.Schema()
	if err != nil {
		return arrow.Metadata{}, wrapError("schema metadata", d.uri, err)
	}
	return schema.Metadata(), nil
}

// UpdateSchemaMetadata returns a new Dataset with schema metadata updated.
// Field metadata is preserved; only the schema-level metadata map changes.
func (d *Dataset) UpdateSchemaMetadata(ctx context.Context, mode MetadataUpdateMode, updates ...MetadataUpdate) (*Dataset, map[string]string, error) {
	if err := checkContext(ctx); err != nil {
		return nil, nil, err
	}
	if !validMetadataUpdateMode(mode) {
		return nil, nil, invalidError("update schema metadata", d.uri, "invalid metadata update mode %d", mode)
	}
	keys, values, err := metadataUpdateArgs(updates)
	if err != nil {
		return nil, nil, invalidError("update schema metadata", d.uri, "%v", err)
	}
	var metadata map[string]string
	next, err := d.mutate("update schema metadata", func(native *ffi.Dataset) error {
		raw, err := native.UpdateSchemaMetadataJSON(keys, values, int32(mode))
		if err != nil {
			return err
		}
		metadata, err = ffi.DecodeStringMap(raw)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return next, metadata, nil
}

// FieldMetadata returns the metadata of a field addressed by id or nested
// path.
func (d *Dataset) FieldMetadata(ref FieldReference) (arrow.Metadata, error) {
	if err := validateFieldReference(ref); err != nil {
		return arrow.Metadata{}, invalidError("field metadata", d.uri, "%v", err)
	}
	if err := d.acquire("field metadata"); err != nil {
		return arrow.Metadata{}, err
	}
	defer d.res.release()

	nativeRef := ffi.FieldReference{Kind: ffi.FieldReferenceKind(ref.Kind), FieldID: ref.FieldID, Path: ref.Path}
	fieldID, err := d.native.ResolveFieldID(nativeRef)
	if err != nil {
		return arrow.Metadata{}, wrapError("field metadata", d.uri, err)
	}
	schema, err := d.native.Schema()
	if err != nil {
		return arrow.Metadata{}, wrapError("field metadata", d.uri, err)
	}
	count, err := d.native.FieldIDCount()
	if err != nil {
		return arrow.Metadata{}, wrapError("field metadata", d.uri, err)
	}
	for index := uint64(0); index < count; index++ {
		id, err := d.native.FieldIDAt(index)
		if err != nil {
			return arrow.Metadata{}, wrapError("field metadata", d.uri, err)
		}
		if id == fieldID {
			metadata, ok := fieldMetadataAt(schema, index)
			if !ok {
				break
			}
			return metadata, nil
		}
	}
	return arrow.Metadata{}, &Error{Op: "field metadata", URI: d.uri, Code: CodeNotFound, Message: "field not found"}
}

// UpdateFieldMetadata returns a new Dataset with metadata updated on one or
// more fields. Each update may merge or replace that field's metadata.
func (d *Dataset) UpdateFieldMetadata(ctx context.Context, updates ...FieldMetadataUpdate) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return nil, invalidError("update field metadata", d.uri, "at least one field update is required")
	}
	nativeUpdates := make([]ffi.FieldMetadataUpdate, len(updates))
	for i, update := range updates {
		if err := validateFieldReference(update.Field); err != nil {
			return nil, invalidError("update field metadata", d.uri, "update %d: %v", i, err)
		}
		if !validMetadataUpdateMode(update.Mode) {
			return nil, invalidError("update field metadata", d.uri, "update %d: invalid metadata update mode %d", i, update.Mode)
		}
		keys, values, err := metadataUpdateArgs(update.Updates)
		if err != nil {
			return nil, invalidError("update field metadata", d.uri, "update %d: %v", i, err)
		}
		nativeUpdates[i] = ffi.FieldMetadataUpdate{
			Field:  ffi.FieldReference{Kind: ffi.FieldReferenceKind(update.Field.Kind), FieldID: update.Field.FieldID, Path: update.Field.Path},
			Keys:   keys,
			Values: values,
			Mode:   int32(update.Mode),
		}
	}
	next, err := d.mutate("update field metadata", func(native *ffi.Dataset) error {
		return native.UpdateFieldMetadata(nativeUpdates)
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

// Config returns a copy of the dataset configuration snapshot.
func (d *Dataset) Config() (map[string]string, error) {
	if err := d.acquire("config"); err != nil {
		return nil, err
	}
	defer d.res.release()
	config, err := d.native.Config()
	if err != nil {
		return nil, wrapError("config", d.uri, err)
	}
	return config, nil
}

// UpdateConfig returns a new Dataset with configuration updated according to
// mode. MetadataMerge changes only supplied keys; MetadataReplace replaces the
// complete configuration. A nil value removes a key under either mode. The
// returned map is the complete resulting configuration.
func (d *Dataset) UpdateConfig(ctx context.Context, mode MetadataUpdateMode, updates ...MetadataUpdate) (*Dataset, map[string]string, error) {
	if err := checkContext(ctx); err != nil {
		return nil, nil, err
	}
	if !validMetadataUpdateMode(mode) {
		return nil, nil, invalidError("update config", d.uri, "invalid metadata update mode %d", mode)
	}
	keys, values, err := metadataUpdateArgs(updates)
	if err != nil {
		return nil, nil, invalidError("update config", d.uri, "%v", err)
	}
	var config map[string]string
	next, err := d.mutate("update config", func(native *ffi.Dataset) error {
		result, err := native.UpdateConfigJSON(keys, values, int32(mode))
		if err != nil {
			return err
		}
		config = result
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return next, config, nil
}

func validMetadataUpdateMode(mode MetadataUpdateMode) bool {
	return mode == MetadataMerge || mode == MetadataReplace
}

func metadataUpdateArgs(updates []MetadataUpdate) ([]string, []*string, error) {
	keys := make([]string, len(updates))
	values := make([]*string, len(updates))
	for i, update := range updates {
		if err := validateCString(update.Key); err != nil {
			return nil, nil, err
		}
		keys[i] = update.Key
		values[i] = update.Value
	}
	return keys, values, nil
}

func validateFieldReference(ref FieldReference) error {
	switch ref.Kind {
	case FieldReferenceByID:
		if ref.FieldID < 0 {
			return &Error{Code: CodeInvalidArgument, Message: "field id must not be negative"}
		}
	case FieldReferenceByPath:
		if ref.Path == "" {
			return &Error{Code: CodeInvalidArgument, Message: "field path must not be empty"}
		}
		if err := validateCString(ref.Path); err != nil {
			return err
		}
	default:
		return &Error{Code: CodeInvalidArgument, Message: "field reference kind is invalid"}
	}
	return nil
}

func fieldMetadataAt(schema *arrow.Schema, target uint64) (arrow.Metadata, bool) {
	if schema == nil {
		return arrow.Metadata{}, false
	}
	index := target
	return findFieldMetadata(schema.Fields(), &index)
}

// lanceChildFields mirrors Lance's field tree. Dictionary values are not
// fields, and a fixed-size list only has a child when its element is a struct;
// other fixed-size lists are stored as one logical leaf.
func lanceChildFields(dt arrow.DataType) []arrow.Field {
	switch typ := dt.(type) {
	case *arrow.StructType:
		return typ.Fields()
	case *arrow.ListType:
		return []arrow.Field{typ.ElemField()}
	case *arrow.LargeListType:
		return []arrow.Field{typ.ElemField()}
	case *arrow.FixedSizeListType:
		element := typ.ElemField()
		if _, ok := element.Type.(*arrow.StructType); ok {
			return []arrow.Field{element}
		}
		return nil
	case *arrow.MapType:
		return []arrow.Field{typ.ElemField()}
	default:
		return nil
	}
}

func findFieldMetadata(fields []arrow.Field, target *uint64) (arrow.Metadata, bool) {
	for _, field := range fields {
		if *target == 0 {
			return field.Metadata, true
		}
		(*target)--
		if metadata, ok := findFieldMetadata(lanceChildFields(field.Type), target); ok {
			return metadata, true
		}
	}
	return arrow.Metadata{}, false
}
