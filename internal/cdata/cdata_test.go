package cdata

import (
	"encoding/binary"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/handle"
)

var (
	fixtureReleaseCalls atomic.Int64
	fixtureReleasePtr   atomic.Uintptr
	fixtureRelease      = purego.NewCallback(func(ptr uintptr) {
		fixtureReleaseCalls.Add(1)
		fixtureReleasePtr.Store(ptr)
	})
)

func resetFixtureRelease() {
	fixtureReleaseCalls.Store(0)
	fixtureReleasePtr.Store(0)
}

func callArrayRelease(t *testing.T, a *ArrowArray) {
	t.Helper()
	if a.Release == 0 {
		t.Fatal("ArrowArray has no release callback")
	}
	_, _, _ = purego.SyscallN(a.Release, uintptr(unsafe.Pointer(a)))
}

func callSchemaRelease(t *testing.T, s *ArrowSchema) {
	t.Helper()
	if s.Release == 0 {
		t.Fatal("ArrowSchema has no release callback")
	}
	_, _, _ = purego.SyscallN(s.Release, uintptr(unsafe.Pointer(s)))
}

func cBuffer(a *ArrowArray, i int) unsafe.Pointer {
	if a.Buffers == nil {
		return nil
	}
	return (*[1 << 20]unsafe.Pointer)(unsafe.Pointer(a.Buffers))[i]
}

func cChild(a *ArrowArray, i int) *ArrowArray {
	return (*[1 << 20]*ArrowArray)(unsafe.Pointer(a.Children))[i]
}

func cSchemaChild(s *ArrowSchema, i int) *ArrowSchema {
	return (*[1 << 20]*ArrowSchema)(unsafe.Pointer(s.Children))[i]
}

type fixture struct {
	pinner  runtime.Pinner
	keep    []any
	schemas []*ArrowSchema
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	t.Cleanup(f.pinner.Unpin)
	return f
}

func (f *fixture) bytes(b []byte) *byte {
	if len(b) == 0 {
		b = []byte{0}
	}
	f.keep = append(f.keep, b)
	f.pinner.Pin(&b[0])
	return &b[0]
}

func (f *fixture) cstr(s string) *byte {
	return f.bytes(append([]byte(s), 0))
}

func pinSlice[T any](f *fixture, s []T) unsafe.Pointer {
	if len(s) == 0 {
		return nil
	}
	f.keep = append(f.keep, s)
	f.pinner.Pin(&s[0])
	return unsafe.Pointer(&s[0])
}

func (f *fixture) pointers(ps ...unsafe.Pointer) *unsafe.Pointer {
	s := append([]unsafe.Pointer(nil), ps...)
	f.keep = append(f.keep, s)
	f.pinner.Pin(&s[0])
	return (*unsafe.Pointer)(unsafe.Pointer(&s[0]))
}

func (f *fixture) array() *ArrowArray {
	a := &ArrowArray{}
	f.keep = append(f.keep, a)
	f.pinner.Pin(a)
	return a
}

func (f *fixture) schema() *ArrowSchema {
	s := &ArrowSchema{}
	f.keep = append(f.keep, s)
	f.pinner.Pin(s)
	return s
}

func (f *fixture) arrayPtrs(as ...*ArrowArray) **ArrowArray {
	s := append([]*ArrowArray(nil), as...)
	f.keep = append(f.keep, s)
	f.pinner.Pin(&s[0])
	return (**ArrowArray)(unsafe.Pointer(&s[0]))
}

func (f *fixture) schemaPtrs(ss ...*ArrowSchema) **ArrowSchema {
	s := append([]*ArrowSchema(nil), ss...)
	f.keep = append(f.keep, s)
	f.pinner.Pin(&s[0])
	return (**ArrowSchema)(unsafe.Pointer(&s[0]))
}

func (f *fixture) int32Array(values []int32, validity []byte, nullCount int64) *ArrowArray {
	a := f.array()
	a.Length = int64(len(values))
	a.NullCount = nullCount
	a.NBuffers = 2
	var validityPtr unsafe.Pointer
	if validity != nil {
		validityPtr = pinSlice(f, validity)
	}
	a.Buffers = f.pointers(validityPtr, pinSlice(f, values))
	a.Release = fixtureRelease
	return a
}

func (f *fixture) stringArray(values []string) *ArrowArray {
	offsets := make([]int32, len(values)+1)
	var data []byte
	for i, v := range values {
		offsets[i] = int32(len(data))
		data = append(data, v...)
	}
	offsets[len(values)] = int32(len(data))
	a := f.array()
	a.Length = int64(len(values))
	a.NBuffers = 3
	a.Buffers = f.pointers(nil, pinSlice(f, offsets), pinSlice(f, data))
	a.Release = fixtureRelease
	return a
}

func TestABILayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("C ABI layout assertions are for 64-bit platforms")
	}
	var schemaValue ArrowSchema
	var arrayValue ArrowArray
	var streamValue ArrowArrayStream
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"ArrowSchema size", unsafe.Sizeof(schemaValue), 72},
		{"ArrowArray size", unsafe.Sizeof(arrayValue), 80},
		{"ArrowArrayStream size", unsafe.Sizeof(streamValue), 40},
		{"ArrowSchema.NChildren", unsafe.Offsetof(schemaValue.NChildren), 32},
		{"ArrowSchema.Children", unsafe.Offsetof(schemaValue.Children), 40},
		{"ArrowSchema.Dictionary", unsafe.Offsetof(schemaValue.Dictionary), 48},
		{"ArrowSchema.Release", unsafe.Offsetof(schemaValue.Release), 56},
		{"ArrowSchema.PrivateData", unsafe.Offsetof(schemaValue.PrivateData), 64},
		{"ArrowArray.Buffers", unsafe.Offsetof(arrayValue.Buffers), 40},
		{"ArrowArray.Children", unsafe.Offsetof(arrayValue.Children), 48},
		{"ArrowArray.Dictionary", unsafe.Offsetof(arrayValue.Dictionary), 56},
		{"ArrowArray.Release", unsafe.Offsetof(arrayValue.Release), 64},
		{"ArrowArray.PrivateData", unsafe.Offsetof(arrayValue.PrivateData), 72},
		{"ArrowArrayStream.GetSchema", unsafe.Offsetof(streamValue.GetSchema), 0},
		{"ArrowArrayStream.GetNext", unsafe.Offsetof(streamValue.GetNext), 8},
		{"ArrowArrayStream.GetLastError", unsafe.Offsetof(streamValue.GetLastError), 16},
		{"ArrowArrayStream.Release", unsafe.Offsetof(streamValue.Release), 24},
		{"ArrowArrayStream.PrivateData", unsafe.Offsetof(streamValue.PrivateData), 32},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestFormatOfAndParse(t *testing.T) {
	decimal := &arrow.DictionaryType{
		IndexType: arrow.PrimitiveTypes.Int16,
		ValueType: arrow.BinaryTypes.String,
		Ordered:   true,
	}
	tests := []struct {
		dt   arrow.DataType
		want string
	}{
		{arrow.Null, "n"},
		{arrow.FixedWidthTypes.Boolean, "b"},
		{arrow.PrimitiveTypes.Int32, "i"},
		{arrow.PrimitiveTypes.Uint64, "L"},
		{arrow.FixedWidthTypes.Float16, "e"},
		{&arrow.FixedSizeBinaryType{ByteWidth: 16}, "w:16"},
		{&arrow.Decimal32Type{Precision: 5, Scale: 2}, "d:5,2,32"},
		{&arrow.Decimal64Type{Precision: 10, Scale: 3}, "d:10,3,64"},
		{&arrow.Decimal128Type{Precision: 38, Scale: 9}, "d:38,9"},
		{&arrow.Decimal256Type{Precision: 76, Scale: 18}, "d:76,18,256"},
		{arrow.FixedWidthTypes.Date32, "tdD"},
		{arrow.FixedWidthTypes.Date64, "tdm"},
		{arrow.FixedWidthTypes.Time32s, "tts"},
		{arrow.FixedWidthTypes.Time64ns, "ttn"},
		{&arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, "tsu:UTC"},
		{arrow.FixedWidthTypes.Duration_ms, "tDm"},
		{arrow.FixedWidthTypes.MonthDayNanoInterval, "tin"},
		{arrow.BinaryTypes.Binary, "z"},
		{arrow.BinaryTypes.String, "u"},
		{arrow.ListOf(arrow.PrimitiveTypes.Int32), "+l"},
		{arrow.LargeListOf(arrow.PrimitiveTypes.Int32), "+L"},
		{arrow.FixedSizeListOf(3, arrow.PrimitiveTypes.Int32), "+w:3"},
		{arrow.StructOf(arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Int32}), "+s"},
		{arrow.MapOf(arrow.BinaryTypes.String, arrow.PrimitiveTypes.Int32), "+m"},
		{decimal, "s"},
	}
	for _, tc := range tests {
		got, err := FormatOf(tc.dt)
		if err != nil {
			t.Fatalf("FormatOf(%s): %v", tc.dt, err)
		}
		if got != tc.want {
			t.Errorf("FormatOf(%s) = %q, want %q", tc.dt, got, tc.want)
		}
		if tc.want[0] == '+' {
			continue
		}
		parsed, err := ParseFormat(got)
		if err != nil {
			t.Fatalf("ParseFormat(%q): %v", got, err)
		}
		if !arrow.TypeEqual(parsed, tc.dt) && tc.dt.ID() != arrow.DICTIONARY {
			t.Errorf("ParseFormat(%q) = %s, want %s", got, parsed, tc.dt)
		}
	}
}

func TestExportSchemaStructureAndRelease(t *testing.T) {
	md := arrow.NewMetadata([]string{"owner"}, []string{"lance"})
	fields := []arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{
			Name:     "tags",
			Type:     arrow.ListOfField(arrow.Field{Name: "item", Type: arrow.BinaryTypes.String, Nullable: true}),
			Nullable: true,
			Metadata: arrow.NewMetadata([]string{"field"}, []string{"tags"}),
		},
		{Name: "point", Type: arrow.StructOf(
			arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Float64},
			arrow.Field{Name: "y", Type: arrow.PrimitiveTypes.Float64},
		)},
		{Name: "attrs", Type: arrow.MapOf(arrow.BinaryTypes.String, arrow.PrimitiveTypes.Int32)},
		{Name: "dict", Type: &arrow.DictionaryType{
			IndexType: arrow.PrimitiveTypes.Int16,
			ValueType: arrow.BinaryTypes.String,
			Ordered:   true,
		}, Nullable: true},
	}
	schema := arrow.NewSchema(fields, &md)
	var root ArrowSchema
	if err := ExportSchema(schema, &root); err != nil {
		t.Fatalf("ExportSchema: %v", err)
	}
	if root.Format == nil || goString(root.Format) != "+s" {
		t.Fatalf("root format = %v", root.Format)
	}
	if root.NChildren != int64(len(fields)) {
		t.Fatalf("root children = %d, want %d", root.NChildren, len(fields))
	}
	if root.Name == nil || goString(root.Name) != "" {
		t.Errorf("root name = %v, want empty", root.Name)
	}
	if got := cSchemaChild(&root, 0); goString(got.Format) != "l" || got.Flags&FlagNullable != 0 {
		t.Errorf("id schema format/flags = %q/%d", goString(got.Format), got.Flags)
	}
	if got := cSchemaChild(&root, 1); goString(got.Format) != "+l" || got.Flags&FlagNullable == 0 {
		t.Errorf("tags schema format/flags = %q/%d", goString(got.Format), got.Flags)
	}
	if got := cSchemaChild(&root, 2); goString(got.Format) != "+s" || got.NChildren != 2 {
		t.Errorf("point schema format/children = %q/%d", goString(got.Format), got.NChildren)
	}
	if got := cSchemaChild(&root, 3); goString(got.Format) != "+m" || got.NChildren != 1 {
		t.Errorf("attrs schema format/children = %q/%d", goString(got.Format), got.NChildren)
	}
	dict := cSchemaChild(&root, 4)
	if goString(dict.Format) != "s" || dict.Dictionary == nil || goString(dict.Dictionary.Format) != "u" {
		t.Errorf("dict schema format/dictionary = %q/%v", goString(dict.Format), dict.Dictionary)
	}
	if dict.Flags&FlagDictionaryOrdered == 0 {
		t.Error("dictionary-ordered flag is not set")
	}
	callSchemaRelease(t, &root)
	if root.Release != 0 {
		t.Error("root release was not cleared")
	}
	if cSchemaChild(&root, 0).Release != 0 {
		t.Error("child release was not cleared")
	}
}

func TestImportSchemaForeignFixture(t *testing.T) {
	f := newFixture(t)
	item := f.schema()
	item.Format = f.cstr("i")
	item.Name = f.cstr("item")
	item.Flags = FlagNullable

	list := f.schema()
	list.Format = f.cstr("+w:3")
	list.Name = f.cstr("points")
	list.NChildren = 1
	list.Children = f.schemaPtrs(item)

	dictValues := f.schema()
	dictValues.Format = f.cstr("u")
	dictValues.Name = f.cstr("")
	dict := f.schema()
	dict.Format = f.cstr("i")
	dict.Name = f.cstr("labels")
	dict.Flags = FlagNullable | FlagDictionaryOrdered
	dict.Dictionary = dictValues

	md := make([]byte, 4+8+1+1)
	binary.NativeEndian.PutUint32(md[0:], 1)
	binary.NativeEndian.PutUint32(md[4:], 1)
	md[8] = 'k'
	binary.NativeEndian.PutUint32(md[9:], 1)
	md[13] = 'v'

	root := f.schema()
	root.Format = f.cstr("+s")
	root.Name = f.cstr("")
	root.Metadata = f.bytes(md)
	root.NChildren = 2
	root.Children = f.schemaPtrs(list, dict)

	schema, err := ImportSchema(root)
	if err != nil {
		t.Fatalf("ImportSchema: %v", err)
	}
	if got, ok := schema.Metadata().GetValue("k"); !ok || got != "v" {
		t.Errorf("root metadata k = %q, %v", got, ok)
	}
	points := schema.Field(0)
	fl, ok := points.Type.(*arrow.FixedSizeListType)
	if !ok || fl.Len() != 3 {
		t.Fatalf("points type = %s, want fixed-size list of 3", points.Type)
	}
	if fl.ElemField().Name != "item" || !fl.ElemField().Nullable {
		t.Errorf("fixed-size list element = %+v", fl.ElemField())
	}
	labels := schema.Field(1)
	dt, ok := labels.Type.(*arrow.DictionaryType)
	if !ok {
		t.Fatalf("labels type = %s, want dictionary", labels.Type)
	}
	if dt.IndexType.ID() != arrow.INT32 || dt.ValueType.ID() != arrow.STRING || !dt.Ordered {
		t.Errorf("dictionary type = %+v", dt)
	}
	if !labels.Nullable {
		t.Error("labels field should be nullable")
	}
}

func TestImportForeignArrayReleaseOnce(t *testing.T) {
	resetFixtureRelease()
	f := newFixture(t)
	src := f.int32Array([]int32{10, 20, 30}, []byte{0b101}, 1)
	arr, err := ImportArray(src, arrow.PrimitiveTypes.Int32)
	if err != nil {
		t.Fatalf("ImportArray: %v", err)
	}
	if src.Release != 0 || src.PrivateData != nil {
		t.Fatal("source was not moved")
	}
	if fixtureReleaseCalls.Load() != 0 {
		t.Fatal("foreign release ran before the array was released")
	}
	if arr.Len() != 3 || arr.NullN() != 1 {
		t.Fatalf("imported length/nulls = %d/%d", arr.Len(), arr.NullN())
	}
	if got := arr.(*array.Int32).Value(1); got != 20 {
		t.Errorf("value = %d, want 20", got)
	}
	if arr.IsValid(1) {
		t.Error("null element is valid")
	}

	slice := array.NewSlice(arr, 1, 3)
	arr.Release()
	if fixtureReleaseCalls.Load() != 0 {
		t.Fatal("release ran while a slice still referenced the buffers")
	}
	slice.Release()
	if got := fixtureReleaseCalls.Load(); got != 1 {
		t.Fatalf("foreign release calls = %d, want 1", got)
	}
	if fixtureReleasePtr.Load() == uintptr(unsafe.Pointer(src)) {
		t.Fatal("release received the original struct address; want the moved copy")
	}
}

func TestImportForeignNestedList(t *testing.T) {
	resetFixtureRelease()
	f := newFixture(t)
	values := f.int32Array([]int32{10, 20, 30, 40}, nil, 0)
	root := f.array()
	root.Length = 2
	root.NBuffers = 2
	root.NChildren = 1
	root.Buffers = f.pointers(nil, pinSlice(f, []int32{0, 2, 4}))
	root.Children = f.arrayPtrs(values)
	root.Release = fixtureRelease

	dt := arrow.ListOf(arrow.PrimitiveTypes.Int32)
	arr, err := ImportArray(root, dt)
	if err != nil {
		t.Fatalf("ImportArray: %v", err)
	}
	list, ok := arr.(*array.List)
	if !ok {
		t.Fatalf("array type = %T", arr)
	}
	if list.Len() != 2 || list.ListValues().Len() != 4 {
		t.Fatalf("list length/values = %d/%d", list.Len(), list.ListValues().Len())
	}
	if start, end := list.ValueOffsets(0); start != 0 || end != 2 {
		t.Errorf("first list offsets = %d,%d", start, end)
	}
	if got := list.ListValues().(*array.Int32).Value(2); got != 30 {
		t.Errorf("nested value = %d, want 30", got)
	}
	arr.Release()
	if got := fixtureReleaseCalls.Load(); got != 1 {
		t.Fatalf("foreign release calls = %d, want 1 (child release must not run separately)", got)
	}
}

func TestImportChildWithoutReleaseIsOwnedByRoot(t *testing.T) {
	resetFixtureRelease()
	f := newFixture(t)
	values := f.int32Array([]int32{10, 20}, nil, 0)
	values.Release = 0
	root := f.array()
	root.Length = 1
	root.NBuffers = 2
	root.NChildren = 1
	root.Buffers = f.pointers(nil, pinSlice(f, []int32{0, 2}))
	root.Children = f.arrayPtrs(values)
	root.Release = fixtureRelease

	arr, err := ImportArray(root, arrow.ListOf(arrow.PrimitiveTypes.Int32))
	if err != nil {
		t.Fatalf("ImportArray: %v", err)
	}
	arr.Release()
	if got := fixtureReleaseCalls.Load(); got != 1 {
		t.Fatalf("foreign release calls = %d, want 1", got)
	}
}

func TestMovedChildStructuresAreReleasedByParent(t *testing.T) {
	before := handle.Len()
	schema := arrow.NewSchema([]arrow.Field{{
		Name: "values",
		Type: arrow.ListOf(arrow.PrimitiveTypes.Int32),
	}}, nil)
	var root ArrowSchema
	if err := ExportSchema(schema, &root); err != nil {
		t.Fatalf("ExportSchema: %v", err)
	}
	child := cSchemaChild(&root, 0)
	movedSchema := *child
	child.Release = 0
	ReleaseSchema(&root)
	// The moved list schema still owns its element schema, so two handles
	// remain: the child and its grandchild.
	if got := handle.Len(); got != before+2 {
		t.Fatalf("moved child schema handle was released by the parent: got %d, want %d", got, before+2)
	}
	ReleaseSchema(&movedSchema)
	if movedSchema.Release != 0 {
		t.Fatal("moved child schema release was not cleared")
	}
	if got := handle.Len(); got != before {
		t.Fatalf("schema handles leaked: %d, want %d", got, before)
	}

	mem := memory.NewGoAllocator()
	lb := array.NewListBuilder(mem, arrow.PrimitiveTypes.Int32)
	defer lb.Release()
	lb.Append(true)
	lb.ValueBuilder().(*array.Int32Builder).AppendValues([]int32{1, 2}, nil)
	list := lb.NewArray()
	defer list.Release()
	var dst ArrowArray
	if err := ExportArray(list, &dst); err != nil {
		t.Fatalf("ExportArray: %v", err)
	}
	childArray := cChild(&dst, 0)
	movedArray := *childArray
	childArray.Release = 0
	ReleaseArray(&dst)
	// The moved child is the list values array; one handle remains.
	if got := handle.Len(); got != before+1 {
		t.Fatalf("moved child array handle was released by the parent: got %d, want %d", got, before+1)
	}
	if movedArray.Length != 2 || (*[2]int32)(cBuffer(&movedArray, 1))[0] != 1 {
		t.Fatal("moved child array data was freed with the parent")
	}
	ReleaseArray(&movedArray)
	if movedArray.Release != 0 {
		t.Fatal("moved child array release was not cleared")
	}
	if got := handle.Len(); got != before {
		t.Fatalf("array handles leaked: %d, want %d", got, before)
	}
}

func TestImportForeignRecordBatch(t *testing.T) {
	resetFixtureRelease()
	f := newFixture(t)
	ids := f.int32Array([]int32{7, 8}, nil, 0)
	names := f.stringArray([]string{"ab", "cde"})
	root := f.array()
	root.Length = 2
	root.NBuffers = 1
	root.NChildren = 2
	root.Buffers = f.pointers(nil)
	root.Children = f.arrayPtrs(ids, names)
	root.Release = fixtureRelease

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	batch, err := ImportRecordBatch(root, schema)
	if err != nil {
		t.Fatalf("ImportRecordBatch: %v", err)
	}
	if batch.NumRows() != 2 || batch.NumCols() != 2 {
		t.Fatalf("batch shape = %dx%d", batch.NumRows(), batch.NumCols())
	}
	if got := batch.Column(0).(*array.Int32).Value(1); got != 8 {
		t.Errorf("id = %d", got)
	}
	if got := batch.Column(1).(*array.String).Value(1); got != "cde" {
		t.Errorf("name = %q", got)
	}
	batch.Release()
	if got := fixtureReleaseCalls.Load(); got != 1 {
		t.Fatalf("foreign release calls = %d, want 1", got)
	}
}

func TestImportInvalidArrayConsumesSource(t *testing.T) {
	resetFixtureRelease()
	f := newFixture(t)
	src := f.array()
	src.Length = 1
	src.NBuffers = 1 // Int32 requires two buffers.
	src.Buffers = f.pointers(nil)
	src.Release = fixtureRelease
	if _, err := ImportArray(src, arrow.PrimitiveTypes.Int32); err == nil {
		t.Fatal("ImportArray accepted an array with too few buffers")
	}
	if got := fixtureReleaseCalls.Load(); got != 1 {
		t.Fatalf("foreign release calls = %d, want 1", got)
	}
	if src.Release != 0 {
		t.Fatal("source release was not consumed")
	}
}

func TestImportDictionaryFixture(t *testing.T) {
	resetFixtureRelease()
	f := newFixture(t)
	dict := f.stringArray([]string{"zero", "one"})
	root := f.array()
	root.Length = 3
	root.NBuffers = 2
	root.Buffers = f.pointers(nil, pinSlice(f, []int32{0, 1, 0}))
	root.Dictionary = dict
	root.Release = fixtureRelease

	dt := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}
	arr, err := ImportArray(root, dt)
	if err != nil {
		t.Fatalf("ImportArray: %v", err)
	}
	d, ok := arr.(*array.Dictionary)
	if !ok {
		t.Fatalf("array type = %T", arr)
	}
	if d.Dictionary().Len() != 2 || d.GetValueIndex(1) != 1 {
		t.Errorf("dictionary length/index = %d/%d", d.Dictionary().Len(), d.GetValueIndex(1))
	}
	if got := d.Dictionary().(*array.String).Value(d.GetValueIndex(1)); got != "one" {
		t.Errorf("dictionary value = %q", got)
	}
	arr.Release()
	if got := fixtureReleaseCalls.Load(); got != 1 {
		t.Fatalf("foreign release calls = %d, want 1", got)
	}
}

func TestExportArrayLifetimeAndSlice(t *testing.T) {
	mem := memory.NewGoAllocator()
	b := array.NewInt32Builder(mem)
	b.AppendValues([]int32{10, 20, 30}, []bool{true, false, true})
	arr := b.NewArray()
	b.Release()
	slice := array.NewSlice(arr, 1, 3)
	arr.Release()
	arr = nil

	var dst ArrowArray
	if err := ExportArray(slice, &dst); err != nil {
		t.Fatalf("ExportArray: %v", err)
	}
	slice.Release()
	slice = nil
	runtime.GC()

	if dst.Length != 2 || dst.Offset != 1 || (dst.NullCount != 1 && dst.NullCount != -1) {
		t.Fatalf("exported shape = length %d offset %d nulls %d", dst.Length, dst.Offset, dst.NullCount)
	}
	if dst.NBuffers != 2 {
		t.Fatalf("exported buffers = %d, want 2", dst.NBuffers)
	}
	allValues := (*[3]int32)(cBuffer(&dst, 1))
	if allValues[0] != 10 || allValues[1] != 20 || allValues[2] != 30 {
		t.Errorf("exported values = %v", allValues[:3])
	}
	if dst.Offset != 1 || allValues[dst.Offset] != 20 {
		t.Errorf("offset %d did not select value 20", dst.Offset)
	}
	callArrayRelease(t, &dst)
	if dst.Release != 0 {
		t.Error("release was not cleared")
	}
}

func TestExportNestedRoundTrip(t *testing.T) {
	mem := memory.NewGoAllocator()

	lb := array.NewListBuilder(mem, arrow.PrimitiveTypes.Int32)
	defer lb.Release()
	lb.Append(true)
	lb.ValueBuilder().(*array.Int32Builder).AppendValues([]int32{1, 2}, nil)
	lb.Append(true)
	lb.ValueBuilder().(*array.Int32Builder).AppendValues([]int32{3}, nil)
	listArr := lb.NewArray()
	defer listArr.Release()

	mb := array.NewMapBuilder(mem, arrow.BinaryTypes.String, arrow.PrimitiveTypes.Int32, true)
	defer mb.Release()
	mb.Append(true)
	mb.KeyBuilder().(*array.StringBuilder).Append("a")
	mb.ItemBuilder().(*array.Int32Builder).Append(9)
	mapArr := mb.NewArray()
	defer mapArr.Release()

	dictValues := fStringArray(t, mem, []string{"zero", "one"})
	defer dictValues.Release()
	indices := fInt32Array(t, mem, []int32{0, 1, 0})
	defer indices.Release()
	dictArr := array.NewDictionaryArray(
		&arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String},
		indices, dictValues,
	)
	defer dictArr.Release()

	for _, src := range []arrow.Array{listArr, mapArr, dictArr} {
		var dst ArrowArray
		if err := ExportArray(src, &dst); err != nil {
			t.Fatalf("export %s: %v", src.DataType(), err)
		}
		imported, err := ImportArray(&dst, src.DataType())
		if err != nil {
			t.Fatalf("import %s: %v", src.DataType(), err)
		}
		if !array.Equal(src, imported) {
			t.Errorf("%s round trip differs", src.DataType())
		}
		imported.Release()
	}
}

func fInt32Array(t *testing.T, mem memory.Allocator, values []int32) arrow.Array {
	t.Helper()
	b := array.NewInt32Builder(mem)
	defer b.Release()
	b.AppendValues(values, nil)
	return b.NewArray()
}

func fStringArray(t *testing.T, mem memory.Allocator, values []string) arrow.Array {
	t.Helper()
	b := array.NewStringBuilder(mem)
	defer b.Release()
	b.AppendValues(values, nil)
	return b.NewArray()
}

func TestExportRecordBatchAndStream(t *testing.T) {
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	batch := array.NewRecordBatch(schema, []arrow.Array{
		fInt32Array(t, mem, []int32{1, 2}),
		fStringArray(t, mem, []string{"a", "bb"}),
	}, 2)
	defer batch.Release()

	var exported ArrowArray
	if err := ExportRecordBatch(batch, &exported); err != nil {
		t.Fatalf("ExportRecordBatch: %v", err)
	}
	if exported.Length != 2 || exported.NChildren != 2 {
		t.Fatalf("record batch root shape = %d/%d", exported.Length, exported.NChildren)
	}
	if got := cChild(&exported, 0); got.Length != 2 || cBuffer(got, 1) == nil {
		t.Error("first column was not exported")
	}
	if got := cChild(&exported, 1); got.Length != 2 || cBuffer(got, 2) == nil {
		t.Error("second column was not exported")
	}
	imported, err := ImportRecordBatch(&exported, schema)
	if err != nil {
		t.Fatalf("ImportRecordBatch exported: %v", err)
	}
	if got := imported.Column(1).(*array.String).Value(1); got != "bb" {
		t.Errorf("imported value = %q", got)
	}
	imported.Release()

	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{batch})
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	var stream ArrowArrayStream
	if err := ExportRecordReader(reader, &stream); err != nil {
		t.Fatalf("ExportRecordReader: %v", err)
	}
	reader.Release()

	var gotSchema ArrowSchema
	if ret, _, _ := purego.SyscallN(stream.GetSchema, uintptr(unsafe.Pointer(&stream)), uintptr(unsafe.Pointer(&gotSchema))); ret != 0 {
		t.Fatalf("get_schema returned %d", ret)
	}
	importedSchema, err := ImportSchema(&gotSchema)
	if err != nil {
		t.Fatalf("import stream schema: %v", err)
	}
	if importedSchema.NumFields() != 2 {
		t.Errorf("stream schema fields = %d", importedSchema.NumFields())
	}
	callSchemaRelease(t, &gotSchema)

	var gotBatch ArrowArray
	if ret, _, _ := purego.SyscallN(stream.GetNext, uintptr(unsafe.Pointer(&stream)), uintptr(unsafe.Pointer(&gotBatch))); ret != 0 {
		t.Fatalf("get_next returned %d", ret)
	}
	first, err := ImportRecordBatch(&gotBatch, importedSchema)
	if err != nil {
		t.Fatalf("import stream batch: %v", err)
	}
	if first.NumRows() != 2 {
		t.Errorf("stream batch rows = %d", first.NumRows())
	}
	first.Release()

	var eof ArrowArray
	if ret, _, _ := purego.SyscallN(stream.GetNext, uintptr(unsafe.Pointer(&stream)), uintptr(unsafe.Pointer(&eof))); ret != 0 {
		t.Fatalf("second get_next returned %d", ret)
	}
	if eof.Release != 0 {
		t.Fatal("end-of-stream array still has a release callback")
	}

	_, _, _ = purego.SyscallN(stream.Release, uintptr(unsafe.Pointer(&stream)))
	if stream.Release != 0 {
		t.Error("stream release was not cleared")
	}
}

type failingReader struct {
	schema *arrow.Schema
}

func (r *failingReader) Retain()                        {}
func (r *failingReader) Release()                       {}
func (r *failingReader) Schema() *arrow.Schema          { return r.schema }
func (r *failingReader) Next() bool                     { return false }
func (r *failingReader) RecordBatch() arrow.RecordBatch { return nil }
func (r *failingReader) Record() arrow.RecordBatch      { return nil }
func (r *failingReader) Err() error                     { return errors.New("reader failed") }

func TestStreamErrorReporting(t *testing.T) {
	reader := &failingReader{schema: arrow.NewSchema([]arrow.Field{{Name: "a", Type: arrow.PrimitiveTypes.Int32}}, nil)}
	var stream ArrowArrayStream
	if err := ExportRecordReader(reader, &stream); err != nil {
		t.Fatalf("ExportRecordReader: %v", err)
	}
	var out ArrowArray
	ret, _, _ := purego.SyscallN(stream.GetNext, uintptr(unsafe.Pointer(&stream)), uintptr(unsafe.Pointer(&out)))
	if ret != uintptr(syscall.EIO) {
		t.Fatalf("get_next returned %d, want EIO", ret)
	}
	if out.Release != 0 {
		t.Fatal("failed get_next returned a release callback")
	}
	errPtr := stream.GetLastError
	if errPtr == 0 {
		t.Fatal("get_last_error callback is nil")
	}
	p, _, _ := purego.SyscallN(stream.GetLastError, uintptr(unsafe.Pointer(&stream)))
	if p == 0 {
		t.Fatal("get_last_error returned nil")
	}
	if got := goString((*byte)(unsafe.Pointer(p))); !strings.Contains(got, "reader failed") {
		t.Errorf("last error = %q", got)
	}
	_, _, _ = purego.SyscallN(stream.Release, uintptr(unsafe.Pointer(&stream)))
}

func goString(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

func TestImportInvalidDataTypeConsumesSource(t *testing.T) {
	resetFixtureRelease()
	f := newFixture(t)
	src := f.int32Array([]int32{1}, nil, 0)
	if _, err := ImportArray(src, &arrow.BinaryViewType{}); err == nil {
		t.Fatal("ImportArray accepted a view type")
	}
	if got := fixtureReleaseCalls.Load(); got != 1 {
		t.Fatalf("foreign release calls = %d, want 1", got)
	}
	if src.Release != 0 {
		t.Fatal("source release was not consumed before datatype validation")
	}
}

func TestImportRecordBatchInvalidSchemaConsumesSource(t *testing.T) {
	resetFixtureRelease()
	f := newFixture(t)
	src := f.int32Array([]int32{1}, nil, 0)
	if _, err := ImportRecordBatch(src, nil); err == nil {
		t.Fatal("ImportRecordBatch accepted a nil schema")
	}
	if got := fixtureReleaseCalls.Load(); got != 1 {
		t.Fatalf("foreign release calls = %d, want 1", got)
	}
	if src.Release != 0 {
		t.Fatal("record batch source release was not consumed")
	}
}

func TestPublicReleaseHelpers(t *testing.T) {
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int32}}, nil)

	var schemaDst ArrowSchema
	if err := ExportSchema(schema, &schemaDst); err != nil {
		t.Fatalf("ExportSchema: %v", err)
	}
	movedSchema := schemaDst
	schemaDst = ArrowSchema{}
	ReleaseSchema(&movedSchema)
	if movedSchema.Release != 0 {
		t.Fatal("moved schema release was not cleared")
	}
	ReleaseSchema(&movedSchema)
	ReleaseSchema(nil)

	arr := fInt32Array(t, mem, []int32{1, 2})
	var arrayDst ArrowArray
	if err := ExportArray(arr, &arrayDst); err != nil {
		t.Fatalf("ExportArray: %v", err)
	}
	arr.Release()
	movedArray := arrayDst
	arrayDst = ArrowArray{}
	ReleaseArray(&movedArray)
	if movedArray.Release != 0 {
		t.Fatal("moved array release was not cleared")
	}
	ReleaseArray(&movedArray)
	ReleaseArray(nil)

	resetFixtureRelease()
	f := newFixture(t)
	foreign := f.int32Array([]int32{7}, nil, 0)
	ReleaseArray(foreign)
	ReleaseArray(foreign)
	if got := fixtureReleaseCalls.Load(); got != 1 {
		t.Fatalf("foreign helper release calls = %d, want 1", got)
	}

	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{
		array.NewRecordBatch(schema, []arrow.Array{fInt32Array(t, mem, []int32{5})}, 1),
	})
	if err != nil {
		t.Fatalf("NewRecordReader: %v", err)
	}
	var stream ArrowArrayStream
	if err := ExportRecordReader(reader, &stream); err != nil {
		t.Fatalf("ExportRecordReader: %v", err)
	}
	defer reader.Release()
	movedStream := stream
	stream = ArrowArrayStream{}
	ReleaseStream(&movedStream)
	if movedStream.Release != 0 {
		t.Fatal("moved stream release was not cleared")
	}
	ReleaseStream(&movedStream)
	ReleaseStream(nil)
}
