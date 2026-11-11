package cdata

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/handle"
)

const maxMetadataBytes = 1 << 26

// FormatOf returns the Arrow C Data Interface format string for dt.
//
// Dictionary types use the format string of their index type; the dictionary
// values are carried by a separate ArrowSchema.
func FormatOf(dt arrow.DataType) (string, error) {
	if err := validateDataType(dt); err != nil {
		return "", err
	}
	return formatOf(dt)
}

func formatOf(dt arrow.DataType) (string, error) {
	switch t := dt.(type) {
	case *arrow.NullType:
		return "n", nil
	case *arrow.BooleanType:
		return "b", nil
	case *arrow.Int8Type:
		return "c", nil
	case *arrow.Uint8Type:
		return "C", nil
	case *arrow.Int16Type:
		return "s", nil
	case *arrow.Uint16Type:
		return "S", nil
	case *arrow.Int32Type:
		return "i", nil
	case *arrow.Uint32Type:
		return "I", nil
	case *arrow.Int64Type:
		return "l", nil
	case *arrow.Uint64Type:
		return "L", nil
	case *arrow.Float16Type:
		return "e", nil
	case *arrow.Float32Type:
		return "f", nil
	case *arrow.Float64Type:
		return "g", nil
	case *arrow.FixedSizeBinaryType:
		return "w:" + strconv.Itoa(t.ByteWidth), nil
	case *arrow.Decimal32Type:
		return decimalFormat(t.Precision, t.Scale, 32), nil
	case *arrow.Decimal64Type:
		return decimalFormat(t.Precision, t.Scale, 64), nil
	case *arrow.Decimal128Type:
		// 128 bits is the default and, matching Arrow Go, is omitted.
		return fmt.Sprintf("d:%d,%d", t.Precision, t.Scale), nil
	case *arrow.Decimal256Type:
		return decimalFormat(t.Precision, t.Scale, 256), nil
	case *arrow.Date32Type:
		return "tdD", nil
	case *arrow.Date64Type:
		return "tdm", nil
	case *arrow.Time32Type:
		switch t.Unit {
		case arrow.Second:
			return "tts", nil
		case arrow.Millisecond:
			return "ttm", nil
		default:
			return "", unsupportedf("time32 unit %s", t.Unit)
		}
	case *arrow.Time64Type:
		switch t.Unit {
		case arrow.Microsecond:
			return "ttu", nil
		case arrow.Nanosecond:
			return "ttn", nil
		default:
			return "", unsupportedf("time64 unit %s", t.Unit)
		}
	case *arrow.TimestampType:
		prefix := ""
		switch t.Unit {
		case arrow.Second:
			prefix = "tss:"
		case arrow.Millisecond:
			prefix = "tsm:"
		case arrow.Microsecond:
			prefix = "tsu:"
		case arrow.Nanosecond:
			prefix = "tsn:"
		default:
			return "", unsupportedf("timestamp unit %s", t.Unit)
		}
		return prefix + t.TimeZone, nil
	case *arrow.DurationType:
		switch t.Unit {
		case arrow.Second:
			return "tDs", nil
		case arrow.Millisecond:
			return "tDm", nil
		case arrow.Microsecond:
			return "tDu", nil
		case arrow.Nanosecond:
			return "tDn", nil
		default:
			return "", unsupportedf("duration unit %s", t.Unit)
		}
	case *arrow.MonthIntervalType:
		return "tiM", nil
	case *arrow.DayTimeIntervalType:
		return "tiD", nil
	case *arrow.MonthDayNanoIntervalType:
		return "tin", nil
	case *arrow.BinaryType:
		return "z", nil
	case *arrow.StringType:
		return "u", nil
	case *arrow.LargeBinaryType:
		return "Z", nil
	case *arrow.LargeStringType:
		return "U", nil
	case *arrow.ListType:
		return "+l", nil
	case *arrow.LargeListType:
		return "+L", nil
	case *arrow.FixedSizeListType:
		return "+w:" + strconv.FormatInt(int64(t.Len()), 10), nil
	case *arrow.StructType:
		return "+s", nil
	case *arrow.MapType:
		return "+m", nil
	case *arrow.DictionaryType:
		return formatOf(t.IndexType)
	default:
		return "", unsupportedf("%s (%s)", dt, dt.ID())
	}
}

func decimalFormat(precision, scale int32, bits int) string {
	return fmt.Sprintf("d:%d,%d,%d", precision, scale, bits)
}

// ParseFormat parses an Arrow C Data Interface format string into a data type.
// Nested formats (list, struct, map, fixed-size list) return their container
// type without children; callers that need children must use ImportSchema.
func ParseFormat(format string) (arrow.DataType, error) {
	switch format {
	case "n":
		return arrow.Null, nil
	case "b":
		return arrow.FixedWidthTypes.Boolean, nil
	case "c":
		return arrow.PrimitiveTypes.Int8, nil
	case "C":
		return arrow.PrimitiveTypes.Uint8, nil
	case "s":
		return arrow.PrimitiveTypes.Int16, nil
	case "S":
		return arrow.PrimitiveTypes.Uint16, nil
	case "i":
		return arrow.PrimitiveTypes.Int32, nil
	case "I":
		return arrow.PrimitiveTypes.Uint32, nil
	case "l":
		return arrow.PrimitiveTypes.Int64, nil
	case "L":
		return arrow.PrimitiveTypes.Uint64, nil
	case "e":
		return arrow.FixedWidthTypes.Float16, nil
	case "f":
		return arrow.PrimitiveTypes.Float32, nil
	case "g":
		return arrow.PrimitiveTypes.Float64, nil
	case "tdD":
		return arrow.FixedWidthTypes.Date32, nil
	case "tdm":
		return arrow.FixedWidthTypes.Date64, nil
	case "tts":
		return arrow.FixedWidthTypes.Time32s, nil
	case "ttm":
		return arrow.FixedWidthTypes.Time32ms, nil
	case "ttu":
		return arrow.FixedWidthTypes.Time64us, nil
	case "ttn":
		return arrow.FixedWidthTypes.Time64ns, nil
	case "tDs":
		return arrow.FixedWidthTypes.Duration_s, nil
	case "tDm":
		return arrow.FixedWidthTypes.Duration_ms, nil
	case "tDu":
		return arrow.FixedWidthTypes.Duration_us, nil
	case "tDn":
		return arrow.FixedWidthTypes.Duration_ns, nil
	case "tiM":
		return arrow.FixedWidthTypes.MonthInterval, nil
	case "tiD":
		return arrow.FixedWidthTypes.DayTimeInterval, nil
	case "tin":
		return arrow.FixedWidthTypes.MonthDayNanoInterval, nil
	case "z":
		return arrow.BinaryTypes.Binary, nil
	case "u":
		return arrow.BinaryTypes.String, nil
	case "Z":
		return arrow.BinaryTypes.LargeBinary, nil
	case "U":
		return arrow.BinaryTypes.LargeString, nil
	case "+l":
		return &arrow.ListType{}, nil
	case "+L":
		return &arrow.LargeListType{}, nil
	case "+s":
		return &arrow.StructType{}, nil
	case "+m":
		return &arrow.MapType{}, nil
	}

	if strings.HasPrefix(format, "w:") {
		n, err := strconv.ParseInt(strings.TrimPrefix(format, "w:"), 10, 32)
		if err != nil || n <= 0 {
			return nil, invalidf("invalid fixed-size binary format %q", format)
		}
		return &arrow.FixedSizeBinaryType{ByteWidth: int(n)}, nil
	}
	if strings.HasPrefix(format, "+w:") {
		n, err := strconv.ParseInt(strings.TrimPrefix(format, "+w:"), 10, 32)
		if err != nil || n <= 0 {
			return nil, invalidf("invalid fixed-size list format %q", format)
		}
		return &arrow.FixedSizeListType{}, fmt.Errorf("%w: fixed-size list length %d requires a child", ErrInvalid, n)
	}
	if strings.HasPrefix(format, "d:") {
		parts := strings.Split(strings.TrimPrefix(format, "d:"), ",")
		if len(parts) != 2 && len(parts) != 3 {
			return nil, invalidf("invalid decimal format %q", format)
		}
		precision, err := strconv.ParseInt(parts[0], 10, 32)
		if err != nil {
			return nil, invalidf("invalid decimal precision in %q", format)
		}
		scale, err := strconv.ParseInt(parts[1], 10, 32)
		if err != nil {
			return nil, invalidf("invalid decimal scale in %q", format)
		}
		bits := int64(128)
		if len(parts) == 3 {
			bits, err = strconv.ParseInt(parts[2], 10, 32)
			if err != nil {
				return nil, invalidf("invalid decimal bit width in %q", format)
			}
		}
		switch bits {
		case 32:
			return &arrow.Decimal32Type{Precision: int32(precision), Scale: int32(scale)}, nil
		case 64:
			return &arrow.Decimal64Type{Precision: int32(precision), Scale: int32(scale)}, nil
		case 128:
			return &arrow.Decimal128Type{Precision: int32(precision), Scale: int32(scale)}, nil
		case 256:
			return &arrow.Decimal256Type{Precision: int32(precision), Scale: int32(scale)}, nil
		default:
			return nil, unsupportedf("decimal bit width %d", bits)
		}
	}
	for _, prefix := range []string{"tss:", "tsm:", "tsu:", "tsn:"} {
		if strings.HasPrefix(format, prefix) {
			var unit arrow.TimeUnit
			switch prefix {
			case "tss:":
				unit = arrow.Second
			case "tsm:":
				unit = arrow.Millisecond
			case "tsu:":
				unit = arrow.Microsecond
			case "tsn:":
				unit = arrow.Nanosecond
			}
			return &arrow.TimestampType{Unit: unit, TimeZone: strings.TrimPrefix(format, prefix)}, nil
		}
	}
	return "", unsupportedf("format %q", format)
}

// ImportSchema imports an ArrowSchema as a record batch schema. The source
// structure is borrowed: ImportSchema does not move it and does not invoke or
// clear its release callback. The returned schema owns copies of all strings
// and metadata.
func ImportSchema(src *ArrowSchema) (*arrow.Schema, error) {
	if src == nil {
		return nil, invalidf("nil ArrowSchema")
	}
	format, err := goCString(src.Format)
	if err != nil {
		return nil, fmt.Errorf("cdata: schema format: %w", err)
	}
	if format != "+s" {
		return nil, invalidf("record batch schema format is %q, want +s", format)
	}
	if src.Dictionary != nil {
		return nil, invalidf("record batch schema cannot have a top-level dictionary")
	}
	if src.Flags&FlagDictionaryOrdered != 0 {
		return nil, invalidf("record batch schema has dictionary-ordered flag")
	}
	md, err := decodeMetadata(src.Metadata)
	if err != nil {
		return nil, err
	}
	n, err := childCount(src.NChildren)
	if err != nil {
		return nil, err
	}
	if n > 0 && src.Children == nil {
		return nil, invalidf("schema has %d children but a nil children pointer", n)
	}
	fields := make([]arrow.Field, n)
	children := schemaChildren(src)
	for i := 0; i < n; i++ {
		field, err := importField(children[i])
		if err != nil {
			return nil, fmt.Errorf("cdata: schema child %d: %w", i, err)
		}
		fields[i] = field
	}
	schema := arrow.NewSchema(fields, metadataPointer(md))
	if err := schema.Validate(); err != nil {
		return nil, invalidf("invalid imported schema: %v", err)
	}
	return schema, nil
}

func metadataPointer(md arrow.Metadata) *arrow.Metadata {
	if md.Len() == 0 {
		return nil
	}
	return &md
}

func importField(src *ArrowSchema) (arrow.Field, error) {
	if src == nil {
		return arrow.Field{}, invalidf("nil child ArrowSchema")
	}
	name, err := goCString(src.Name)
	if err != nil {
		return arrow.Field{}, err
	}
	md, err := decodeMetadata(src.Metadata)
	if err != nil {
		return arrow.Field{}, err
	}
	dt, err := importType(src)
	if err != nil {
		return arrow.Field{}, err
	}
	return arrow.Field{
		Name:     name,
		Type:     dt,
		Nullable: src.Flags&FlagNullable != 0,
		Metadata: md,
	}, nil
}

type schemaExporter struct {
	value     ArrowSchema
	box       *handleBox
	pinner    runtime.Pinner
	format    []byte
	name      []byte
	meta      []byte
	children  []*schemaExporter
	childPtrs []*ArrowSchema
	dict      *schemaExporter
	pinned    bool
}

func newSchemaExporter() *schemaExporter {
	e := &schemaExporter{}
	e.pinner.Pin(&e.value)
	e.pinned = true
	return e
}

// ExportSchema exports schema into dst. On success dst owns the release
// callback and the caller must release it exactly once, possibly after
// moving the structure to another address. On error dst is zeroed and no
// resources are leaked.
func ExportSchema(schema *arrow.Schema, dst *ArrowSchema) error {
	if schema == nil {
		return invalidf("nil schema")
	}
	if dst == nil {
		return invalidf("nil destination ArrowSchema")
	}
	*dst = ArrowSchema{}
	if err := schema.Validate(); err != nil {
		return invalidf("invalid schema: %v", err)
	}

	root := newSchemaExporter()
	root.value.Flags = 0
	root.format = cString("+s")
	root.pinner.Pin(&root.format[0])
	root.value.Format = &root.format[0]
	root.name = cString("")
	root.pinner.Pin(&root.name[0])
	root.value.Name = &root.name[0]
	if md := schema.Metadata(); md.Len() > 0 {
		root.meta = encodeMetadata(md)
		root.pinner.Pin(&root.meta[0])
		root.value.Metadata = &root.meta[0]
	}
	root.value.NChildren = int64(len(schema.Fields()))
	if len(schema.Fields()) > 0 {
		children := make([]*schemaExporter, len(schema.Fields()))
		for i, field := range schema.Fields() {
			child, err := exportField(field)
			if err != nil {
				for _, done := range children[:i] {
					done.freeTree()
				}
				root.freeTree()
				return fmt.Errorf("cdata: export schema field %d: %w", i, err)
			}
			children[i] = child
		}
		root.setChildren(children)
	}
	root.register()
	*dst = root.value
	return nil
}

func exportField(field arrow.Field) (*schemaExporter, error) {
	e := newSchemaExporter()
	if err := e.fill(field.Name, field.Type, field.Nullable, field.Metadata, 0); err != nil {
		e.freeTree()
		return nil, err
	}
	return e, nil
}

func (e *schemaExporter) fill(name string, dt arrow.DataType, nullable bool, md arrow.Metadata, extraFlags int64) error {
	if err := validateDataType(dt); err != nil {
		return err
	}
	format, err := formatOf(dt)
	if err != nil {
		return err
	}
	if dt.ID() == arrow.DICTIONARY {
		dict := dt.(*arrow.DictionaryType)
		if dict.Ordered {
			extraFlags |= FlagDictionaryOrdered
		}
	}

	e.format = cString(format)
	e.pinner.Pin(&e.format[0])
	e.value.Format = &e.format[0]
	e.name = cString(name)
	e.pinner.Pin(&e.name[0])
	e.value.Name = &e.name[0]
	if md.Len() > 0 {
		e.meta = encodeMetadata(md)
		e.pinner.Pin(&e.meta[0])
		e.value.Metadata = &e.meta[0]
	}
	e.value.Flags = extraFlags
	if nullable {
		e.value.Flags |= FlagNullable
	}

	switch t := dt.(type) {
	case *arrow.DictionaryType:
		e.dict, err = exportField(arrow.Field{Name: "", Type: t.ValueType})
		if err != nil {
			return err
		}
		e.value.Dictionary = &e.dict.value
		e.value.NChildren = 0
	case *arrow.ListType:
		if err := e.addSingleChild(t.ElemField()); err != nil {
			return err
		}
	case *arrow.LargeListType:
		if err := e.addSingleChild(t.ElemField()); err != nil {
			return err
		}
	case *arrow.FixedSizeListType:
		if err := e.addSingleChild(t.ElemField()); err != nil {
			return err
		}
	case *arrow.MapType:
		e.value.Flags |= extraFlags
		if t.KeysSorted {
			e.value.Flags |= FlagMapKeysSorted
		}
		entries := t.ElemField()
		if entries.Type.ID() != arrow.STRUCT {
			return invalidf("map entries type is %s, want struct", entries.Type)
		}
		child, err := exportField(entries)
		if err != nil {
			return err
		}
		e.setChildren([]*schemaExporter{child})
	case *arrow.StructType:
		n := t.NumFields()
		if n == 0 {
			break
		}
		children := make([]*schemaExporter, n)
		for i := 0; i < n; i++ {
			child, err := exportField(t.Field(i))
			if err != nil {
				for _, done := range children[:i] {
					done.freeTree()
				}
				return err
			}
			children[i] = child
		}
		e.setChildren(children)
	}
	e.register()
	return nil
}

func (e *schemaExporter) register() {
	if e.box == nil {
		e.box = newHandleBox(e)
		e.pinner.Pin(e.box)
	}
	e.value.Release = callbackSchemaRelease
	e.value.PrivateData = unsafe.Pointer(e.box)
}

func (e *schemaExporter) setChildren(children []*schemaExporter) {
	e.children = children
	e.value.NChildren = int64(len(children))
	if len(children) == 0 {
		e.value.Children = nil
		return
	}
	e.childPtrs = make([]*ArrowSchema, len(children))
	e.pinner.Pin(&e.childPtrs[0])
	for i, child := range children {
		e.childPtrs[i] = &child.value
	}
	e.value.Children = &e.childPtrs[0]
}

func (e *schemaExporter) addSingleChild(field arrow.Field) error {
	child, err := exportField(field)
	if err != nil {
		return err
	}
	e.setChildren([]*schemaExporter{child})
	return nil
}

func (e *schemaExporter) freeSelf() {
	if !e.pinned {
		return
	}
	e.pinned = false
	e.pinner.Unpin()
	if e.box != nil {
		handle.Remove(e.box.id)
	}
	e.format = nil
	e.name = nil
	e.meta = nil
	e.children = nil
	e.childPtrs = nil
	e.dict = nil
	e.value = ArrowSchema{}
}

func (e *schemaExporter) freeTree() {
	if e == nil || !e.pinned {
		return
	}
	for _, child := range e.children {
		child.freeTree()
	}
	e.dict.freeTree()
	e.freeSelf()
}

func (e *schemaExporter) releaseSchema(_ *ArrowSchema) {
	for _, child := range e.children {
		if child == nil {
			continue
		}
		c := &child.value
		if c.Release != 0 {
			_, _, _ = purego.SyscallN(c.Release, uintptr(unsafe.Pointer(c)))
		}
		// A moved child keeps its own callback and resources.
	}
	if e.dict != nil && e.dict.value.Release != 0 {
		c := &e.dict.value
		_, _, _ = purego.SyscallN(c.Release, uintptr(unsafe.Pointer(c)))
	}
	e.freeSelf()
}

func importType(src *ArrowSchema) (arrow.DataType, error) {
	format, err := goCString(src.Format)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(format, "+w:") {
		n, parseErr := strconv.ParseInt(strings.TrimPrefix(format, "+w:"), 10, 32)
		if parseErr != nil || n <= 0 {
			return nil, invalidf("invalid fixed-size list format %q", format)
		}
		child, err := importSingleChild(src, "fixed-size list")
		if err != nil {
			return nil, err
		}
		return arrow.FixedSizeListOfField(int32(n), child), nil
	}
	dt, err := ParseFormat(format)
	if err != nil {
		return nil, err
	}

	if src.Flags&FlagDictionaryOrdered != 0 || (src.Dictionary != nil && src.Flags&FlagDictionaryOrdered != 0) {
		// The flag is meaningful only together with a dictionary, but do not
		// silently drop it when the dictionary is missing.
		if src.Dictionary == nil {
			return nil, invalidf("dictionary-ordered flag without a dictionary schema")
		}
	}
	if src.Dictionary != nil {
		if dt.ID() == arrow.DICTIONARY {
			return nil, invalidf("nested dictionary in dictionary schema")
		}
		if !isIntegerType(dt) {
			return nil, invalidf("dictionary index type %s is not an integer", dt)
		}
		if src.NChildren != 0 {
			return nil, invalidf("dictionary index schema has children")
		}
		value, err := importType(src.Dictionary)
		if err != nil {
			return nil, fmt.Errorf("dictionary values: %w", err)
		}
		return &arrow.DictionaryType{
			IndexType: dt,
			ValueType: value,
			Ordered:   src.Flags&FlagDictionaryOrdered != 0,
		}, nil
	}

	switch t := dt.(type) {
	case *arrow.StructType:
		n, err := childCount(src.NChildren)
		if err != nil {
			return nil, err
		}
		if n > 0 && src.Children == nil {
			return nil, invalidf("struct has %d children but a nil children pointer", n)
		}
		children := schemaChildren(src)
		fields := make([]arrow.Field, n)
		for i := 0; i < n; i++ {
			fields[i], err = importField(children[i])
			if err != nil {
				return nil, fmt.Errorf("struct child %d: %w", i, err)
			}
		}
		return arrow.StructOf(fields...), nil
	case *arrow.ListType:
		child, err := importSingleChild(src, "list")
		if err != nil {
			return nil, err
		}
		return arrow.ListOfField(child), nil
	case *arrow.LargeListType:
		child, err := importSingleChild(src, "large list")
		if err != nil {
			return nil, err
		}
		return arrow.LargeListOfField(child), nil
	case *arrow.MapType:
		child, err := importSingleChild(src, "map")
		if err != nil {
			return nil, err
		}
		if child.Type.ID() != arrow.STRUCT {
			return nil, invalidf("map entries type is %s, want struct", child.Type)
		}
		entries := child.Type.(*arrow.StructType)
		if entries.NumFields() != 2 {
			return nil, invalidf("map entries struct has %d fields, want 2", entries.NumFields())
		}
		if entries.Field(0).Nullable {
			return nil, invalidf("map key field is nullable")
		}
		m := arrow.MapOfFields(entries.Field(0), entries.Field(1))
		m.KeysSorted = src.Flags&FlagMapKeysSorted != 0
		return m, nil
	default:
		n, err := childCount(src.NChildren)
		if err != nil {
			return nil, err
		}
		if n != 0 {
			return nil, invalidf("type %s has children", dt)
		}
		return dt, nil
	}
}

func importSingleChild(src *ArrowSchema, what string) (arrow.Field, error) {
	if src.NChildren != 1 {
		return arrow.Field{}, invalidf("%s has %d children, want 1", what, src.NChildren)
	}
	children := schemaChildren(src)
	if len(children) == 0 || children[0] == nil {
		return arrow.Field{}, invalidf("%s has a nil child pointer", what)
	}
	return importField(children[0])
}

func childCount(n int64) (int, error) {
	return toInt(n, "child count")
}

func goCString(p *byte) (string, error) {
	if p == nil {
		return "", nil
	}
	const maxCStringBytes = 1 << 20
	for i := 0; i < maxCStringBytes; i++ {
		if *(*byte)(unsafe.Add(unsafe.Pointer(p), i)) == 0 {
			if i == 0 {
				return "", nil
			}
			return string(unsafe.Slice(p, i)), nil
		}
	}
	return "", invalidf("C string is not NUL-terminated within %d bytes", maxCStringBytes)
}

func cString(s string) []byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b
}

func encodeMetadata(md arrow.Metadata) []byte {
	if md.Len() == 0 {
		return nil
	}
	size := 4
	for i := 0; i < md.Len(); i++ {
		size += 8 + len(md.Keys()[i]) + len(md.Values()[i])
	}
	b := make([]byte, size)
	binary.NativeEndian.PutUint32(b[0:4], uint32(md.Len()))
	off := 4
	for i := 0; i < md.Len(); i++ {
		k := md.Keys()[i]
		v := md.Values()[i]
		binary.NativeEndian.PutUint32(b[off:off+4], uint32(len(k)))
		off += 4
		copy(b[off:], k)
		off += len(k)
		binary.NativeEndian.PutUint32(b[off:off+4], uint32(len(v)))
		off += 4
		copy(b[off:], v)
		off += len(v)
	}
	return b
}

func decodeMetadata(p *byte) (arrow.Metadata, error) {
	if p == nil {
		return arrow.Metadata{}, nil
	}
	base := unsafe.Pointer(p)
	read32 := func(off int) (int32, error) {
		if off < 0 || off+4 > maxMetadataBytes {
			return 0, invalidf("metadata offset %d is out of range", off)
		}
		return int32(binary.NativeEndian.Uint32(unsafe.Slice((*byte)(unsafe.Add(base, off)), 4))), nil
	}
	n, err := read32(0)
	if err != nil {
		return arrow.Metadata{}, err
	}
	if n < 0 || n > maxMetadataBytes/8 {
		return arrow.Metadata{}, invalidf("metadata pair count %d is out of range", n)
	}
	off := 4
	keys := make([]string, 0, n)
	values := make([]string, 0, n)
	for i := int32(0); i < n; i++ {
		klen, err := read32(off)
		if err != nil {
			return arrow.Metadata{}, err
		}
		off += 4
		if klen < 0 || int(klen) > maxMetadataBytes-off {
			return arrow.Metadata{}, invalidf("metadata key %d length %d is out of range", i, klen)
		}
		keys = append(keys, string(unsafe.Slice((*byte)(unsafe.Add(base, off)), int(klen))))
		off += int(klen)
		vlen, err := read32(off)
		if err != nil {
			return arrow.Metadata{}, err
		}
		off += 4
		if vlen < 0 || int(vlen) > maxMetadataBytes-off {
			return arrow.Metadata{}, invalidf("metadata value %d length %d is out of range", i, vlen)
		}
		values = append(values, string(unsafe.Slice((*byte)(unsafe.Add(base, off)), int(vlen))))
		off += int(vlen)
	}
	return arrow.NewMetadata(keys, values), nil
}
