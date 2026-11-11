package cdata

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/handle"
)

// ImportArray imports src and transfers ownership of the C structure to Go.
//
// On success src.Release is set to zero and the producer's release callback
// will be invoked exactly once, when the last Arrow buffer reachable from the
// returned array is released. This includes buffers reached through slices,
// children, and dictionaries. On error src is consumed as well: its release
// callback is invoked exactly once if it had one.
//
// ImportArray does not copy array buffers. The caller must keep the foreign
// buffers valid until the returned array is released.
//
// Ownership is unambiguous on every path: if src is non-nil and has a non-zero
// release callback, ImportArray either moves that callback into the returned
// array's buffer owner or invokes it before returning an error. A nil src or a
// src without a release callback has no ownership to transfer.
func ImportArray(src *ArrowArray, dt arrow.DataType) (arrow.Array, error) {
	if src == nil {
		return nil, invalidf("nil ArrowArray")
	}
	moved := false
	defer func() {
		if !moved && src.Release != 0 {
			consumeArray(src)
		}
	}()
	if err := validateDataType(dt); err != nil {
		return nil, err
	}
	if src.Release == 0 {
		return nil, invalidf("ArrowArray has no release callback")
	}
	if _, err := validateArrayHeader(src, dt); err != nil {
		return nil, err
	}

	alloc := newImportAllocator(src)
	moved = true
	arr, err := importNode(src, dt, alloc)
	if err != nil {
		alloc.forceRelease()
		return nil, err
	}
	alloc.releaseIfIdle()
	return arr, nil
}

// ImportRecordBatch imports a C ArrowArray that contains a struct of columns.
//
// The source array is moved and its release callback is invoked exactly once,
// after the last imported column buffer is released. On every error path the
// source is consumed in the same way, so a failed import does not leak a
// producer-owned array. A nil source or a source without a release callback has
// no ownership to transfer. The root struct's row validity bitmap is not
// representable in arrow.RecordBatch; ImportRecordBatch therefore rejects a
// root with a non-zero null count.
func ImportRecordBatch(src *ArrowArray, schema *arrow.Schema) (arrow.RecordBatch, error) {
	if src == nil {
		return nil, invalidf("nil ArrowArray")
	}
	moved := false
	defer func() {
		if !moved && src.Release != 0 {
			consumeArray(src)
		}
	}()
	if schema == nil {
		return nil, invalidf("nil schema")
	}
	if err := validateSchema(schema); err != nil {
		return nil, invalidf("invalid schema: %v", err)
	}
	if src.Release == 0 {
		return nil, invalidf("record batch ArrowArray has no release callback")
	}
	if src.Dictionary != nil {
		return nil, invalidf("record batch array cannot have a dictionary")
	}
	if src.Offset != 0 {
		return nil, invalidf("record batch root offset is %d, want 0", src.Offset)
	}
	if src.NullCount != 0 {
		return nil, invalidf("record batch root has null count %d; RecordBatch has no row validity", src.NullCount)
	}
	rootType := arrow.StructOf(schema.Fields()...)
	length, err := validateArrayHeader(src, rootType)
	if err != nil {
		return nil, err
	}

	alloc := newImportAllocator(src)
	moved = true
	columns := make([]arrow.Array, 0, len(schema.Fields()))
	ok := false
	defer func() {
		if !ok {
			for _, col := range columns {
				col.Release()
			}
			alloc.forceRelease()
		}
	}()

	children := arrayChildren(src)
	for i, field := range schema.Fields() {
		if i >= len(children) {
			return nil, invalidf("record batch has fewer children than schema fields")
		}
		child, err := importNode(children[i], field.Type, alloc)
		if err != nil {
			return nil, fmt.Errorf("cdata: record batch column %d: %w", i, err)
		}
		if int64(child.Len()) < length {
			child.Release()
			return nil, invalidf("record batch column %d length %d is shorter than root length %d", i, child.Len(), length)
		}
		columns = append(columns, child)
	}

	batch, err := newRecordBatch(schema, columns, length)
	if err != nil {
		return nil, err
	}
	for _, col := range columns {
		col.Release()
	}
	columns = nil
	alloc.releaseIfIdle()
	ok = true
	return batch, nil
}

func consumeArray(src *ArrowArray) {
	if src == nil {
		return
	}
	fn := src.Release
	src.Release = 0
	if fn == 0 {
		return
	}
	defer func() {
		_ = recover()
	}()
	_, _, _ = purego.SyscallN(fn, uintptr(unsafe.Pointer(src)))
}

func newRecordBatch(schema *arrow.Schema, columns []arrow.Array, length int64) (batch arrow.RecordBatch, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = invalidf("constructing Arrow record batch: %v", r)
		}
	}()
	return array.NewRecordBatch(schema, columns, length), nil
}

type importAllocator struct {
	pending  atomic.Int64
	released atomic.Bool
	root     ArrowArray
	pinner   runtime.Pinner
}

func newImportAllocator(src *ArrowArray) *importAllocator {
	a := &importAllocator{}
	a.root = *src
	src.Release = 0
	src.PrivateData = nil
	a.pinner.Pin(&a.root)
	return a
}

func (a *importAllocator) Allocate(size int) []byte {
	return make([]byte, size)
}

func (a *importAllocator) Reallocate(size int, _ []byte) []byte {
	return make([]byte, size)
}

func (a *importAllocator) Free(_ []byte) {
	n := a.pending.Add(-1)
	if n <= 0 {
		a.release()
	}
}

func (a *importAllocator) releaseIfIdle() {
	if a.pending.Load() == 0 {
		a.release()
	}
}

func (a *importAllocator) forceRelease() {
	a.release()
}

func (a *importAllocator) release() {
	if !a.released.CompareAndSwap(false, true) {
		return
	}
	fn := a.root.Release
	a.root.Release = 0
	if fn != 0 {
		_, _, _ = purego.SyscallN(fn, uintptr(unsafe.Pointer(&a.root)))
	}
	a.pinner.Unpin()
}

func importNode(src *ArrowArray, dt arrow.DataType, alloc *importAllocator) (arrow.Array, error) {
	if src == nil {
		return nil, invalidf("nil child ArrowArray")
	}
	if _, isDict := dt.(*arrow.DictionaryType); !isDict && src.Dictionary != nil {
		return nil, invalidf("%s array has a dictionary pointer but is not a dictionary", dt)
	}
	length, err := validateArrayHeader(src, dt)
	if err != nil {
		return nil, err
	}
	offset64 := src.Offset
	offset, err := toInt(offset64, "array offset")
	if err != nil {
		return nil, err
	}

	buffers, err := importBuffers(src, dt, alloc)
	if err != nil {
		return nil, err
	}
	children := make([]arrow.Array, 0)
	dict := arrow.Array(nil)
	ok := false
	defer func() {
		if ok {
			return
		}
		for _, child := range children {
			child.Release()
		}
		if dict != nil {
			dict.Release()
		}
		memory.ReleaseBuffers(buffers)
	}()

	childrenSrc := arrayChildren(src)
	switch t := dt.(type) {
	case *arrow.ListType:
		child, err := importNode(childrenSrc[0], t.ElemField().Type, alloc)
		if err != nil {
			return nil, fmt.Errorf("list values: %w", err)
		}
		if err := checkListChildLength(buffers[1], src, child, 4); err != nil {
			child.Release()
			return nil, err
		}
		children = append(children, child)
	case *arrow.LargeListType:
		child, err := importNode(childrenSrc[0], t.ElemField().Type, alloc)
		if err != nil {
			return nil, fmt.Errorf("large list values: %w", err)
		}
		if err := checkListChildLength(buffers[1], src, child, 8); err != nil {
			child.Release()
			return nil, err
		}
		children = append(children, child)
	case *arrow.FixedSizeListType:
		child, err := importNode(childrenSrc[0], t.ElemField().Type, alloc)
		if err != nil {
			return nil, fmt.Errorf("fixed-size list values: %w", err)
		}
		want, err := checkedMul(src.Length, int64(t.Len()))
		if err != nil {
			child.Release()
			return nil, err
		}
		if int64(child.Len()) < want {
			child.Release()
			return nil, invalidf("fixed-size list child length %d is shorter than %d", child.Len(), want)
		}
		children = append(children, child)
	case *arrow.StructType:
		if int(src.NChildren) != t.NumFields() {
			return nil, invalidf("struct has %d children, want %d", src.NChildren, t.NumFields())
		}
		for i := 0; i < t.NumFields(); i++ {
			child, err := importNode(childrenSrc[i], t.Field(i).Type, alloc)
			if err != nil {
				return nil, fmt.Errorf("struct field %d: %w", i, err)
			}
			if int64(child.Len()) < src.Length {
				child.Release()
				return nil, invalidf("struct field %d length %d is shorter than %d", i, child.Len(), src.Length)
			}
			children = append(children, child)
		}
	case *arrow.MapType:
		child, err := importNode(childrenSrc[0], t.Elem(), alloc)
		if err != nil {
			return nil, fmt.Errorf("map entries: %w", err)
		}
		if err := checkListChildLength(buffers[1], src, child, 4); err != nil {
			child.Release()
			return nil, err
		}
		children = append(children, child)
	case *arrow.DictionaryType:
		if src.Dictionary == nil {
			return nil, invalidf("dictionary array has no dictionary")
		}
		dict, err = importNode(src.Dictionary, t.ValueType, alloc)
		if err != nil {
			return nil, fmt.Errorf("dictionary values: %w", err)
		}
	}

	childData := make([]arrow.ArrayData, len(children))
	for i, child := range children {
		childData[i] = child.Data()
	}
	data, err := newImportedData(dt, length, buffers, childData, int(src.NullCount), offset)
	if err != nil {
		return nil, err
	}
	if dict != nil {
		data.SetDictionary(dict.Data())
	}
	arr := array.MakeFromData(data)
	data.Release()
	memory.ReleaseBuffers(buffers)
	for _, child := range children {
		child.Release()
	}
	if dict != nil {
		dict.Release()
	}
	ok = true
	return arr, nil
}

func newImportedData(dt arrow.DataType, length int64, buffers []*memory.Buffer, children []arrow.ArrayData, nulls, offset int) (data *array.Data, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = invalidf("constructing Arrow array: %v", r)
			data = nil
		}
	}()
	n, err := toInt(length, "array length")
	if err != nil {
		return nil, err
	}
	return array.NewData(dt, n, buffers, children, nulls, offset), nil
}

func checkListChildLength(offsets *memory.Buffer, src *ArrowArray, child arrow.Array, width int64) error {
	if offsets == nil {
		return invalidf("nil list offsets")
	}
	last, err := lastOffset(offsets, src.Offset+src.Length, width)
	if err != nil {
		return err
	}
	if last < 0 || last > int64(child.Len()) {
		return invalidf("list last offset %d is outside child length %d", last, child.Len())
	}
	return nil
}

func validateArrayHeader(src *ArrowArray, dt arrow.DataType) (int64, error) {
	if src.Length < 0 {
		return 0, invalidf("negative array length %d", src.Length)
	}
	if src.Offset < 0 {
		return 0, invalidf("negative array offset %d", src.Offset)
	}
	if src.NullCount < -1 || src.NullCount > src.Length {
		return 0, invalidf("null count %d is outside [-1,%d]", src.NullCount, src.Length)
	}
	if _, err := checkedAdd(src.Length, src.Offset); err != nil {
		return 0, err
	}
	n, err := expectedBufferCount(dt)
	if err != nil {
		return 0, err
	}
	if src.NBuffers != int64(n) {
		return 0, invalidf("%s has %d buffers, want %d", dt, src.NBuffers, n)
	}
	if n > 0 && src.Buffers == nil {
		return 0, invalidf("array has %d buffers but a nil buffers pointer", n)
	}
	c, err := expectedChildCount(dt)
	if err != nil {
		return 0, err
	}
	if src.NChildren != int64(c) {
		return 0, invalidf("%s has %d children, want %d", dt, src.NChildren, c)
	}
	if c > 0 && src.Children == nil {
		return 0, invalidf("array has %d children but a nil children pointer", c)
	}
	return src.Length, nil
}

func importBuffers(src *ArrowArray, dt arrow.DataType, alloc *importAllocator) ([]*memory.Buffer, error) {
	n, err := expectedBufferCount(dt)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	buffers := make([]*memory.Buffer, n)
	ok := false
	defer func() {
		if !ok {
			memory.ReleaseBuffers(buffers)
		}
	}()

	validLen, err := checkedAdd(src.Length, src.Offset)
	if err != nil {
		return nil, err
	}
	srcBuffers := arrayBuffers(src)
	if len(srcBuffers) != n {
		return nil, invalidf("%s has %d buffers, want %d", dt, len(srcBuffers), n)
	}
	validBytes, err := checkedBytesForBits(validLen)
	if err != nil {
		return nil, err
	}
	if src.NullCount > 0 && srcBuffers[0] == nil {
		return nil, invalidf("array has %d nulls but no validity bitmap", src.NullCount)
	}
	if srcBuffers[0] != nil {
		buffers[0], err = importBuffer(srcBuffers[0], validBytes, alloc)
		if err != nil {
			return nil, fmt.Errorf("validity bitmap: %w", err)
		}
	}

	switch t := dt.(type) {
	case *arrow.NullType:
		// No buffers; the branch is unreachable because n == 0 above.
	case *arrow.DictionaryType:
		index, ok := t.IndexType.(arrow.FixedWidthDataType)
		if !ok || index.BitWidth()%8 != 0 {
			return nil, invalidf("dictionary index type %s has no byte-aligned fixed width", t.IndexType)
		}
		values, err := checkedMul(validLen, int64(index.BitWidth()/8))
		if err != nil {
			return nil, err
		}
		buffers[1], err = importBuffer(srcBuffers[1], values, alloc)
		if err != nil {
			return nil, fmt.Errorf("dictionary indices: %w", err)
		}
	case arrow.FixedWidthDataType:
		if t.ID() == arrow.BOOL {
			bytes, err := checkedBytesForBits(validLen)
			if err != nil {
				return nil, err
			}
			buffers[1], err = importBuffer(srcBuffers[1], bytes, alloc)
			if err != nil {
				return nil, fmt.Errorf("boolean values: %w", err)
			}
		} else {
			if t.BitWidth()%8 != 0 {
				return nil, unsupportedf("fixed width type %s with %d bits", dt, t.BitWidth())
			}
			values, err := checkedMul(validLen, int64(t.BitWidth()/8))
			if err != nil {
				return nil, err
			}
			buffers[1], err = importBuffer(srcBuffers[1], values, alloc)
			if err != nil {
				return nil, fmt.Errorf("values: %w", err)
			}
		}
	case *arrow.BinaryType, *arrow.StringType, *arrow.LargeBinaryType, *arrow.LargeStringType:
		width := int64(4)
		switch dt.ID() {
		case arrow.LARGE_BINARY, arrow.LARGE_STRING:
			width = 8
		}
		offsetBytes, err := checkedAdd(validLen, 1)
		if err != nil {
			return nil, err
		}
		offsetBytes, err = checkedMul(offsetBytes, width)
		if err != nil {
			return nil, err
		}
		buffers[1], err = importBuffer(srcBuffers[1], offsetBytes, alloc)
		if err != nil {
			return nil, fmt.Errorf("binary offsets: %w", err)
		}
		last, err := lastOffset(buffers[1], validLen, width)
		if err != nil {
			return nil, err
		}
		buffers[2], err = importBuffer(srcBuffers[2], last, alloc)
		if err != nil {
			return nil, fmt.Errorf("binary values: %w", err)
		}
	case *arrow.ListType, *arrow.LargeListType, *arrow.MapType:
		width := int64(4)
		switch dt.ID() {
		case arrow.LARGE_LIST:
			width = 8
		}
		offsetBytes, err := checkedAdd(validLen, 1)
		if err != nil {
			return nil, err
		}
		offsetBytes, err = checkedMul(offsetBytes, width)
		if err != nil {
			return nil, err
		}
		buffers[1], err = importBuffer(srcBuffers[1], offsetBytes, alloc)
		if err != nil {
			return nil, fmt.Errorf("list offsets: %w", err)
		}
		if err := validateOffsets(buffers[1], validLen, width); err != nil {
			return nil, err
		}
	case *arrow.StructType, *arrow.FixedSizeListType:
		// Only the validity bitmap is part of the array layout.
	default:
		return nil, unsupportedf("%s", dt)
	}
	ok = true
	return buffers, nil
}

func importBuffer(ptr unsafe.Pointer, size int64, alloc *importAllocator) (*memory.Buffer, error) {
	if size < 0 {
		return nil, invalidf("negative buffer size %d", size)
	}
	if size == 0 {
		return memory.NewBufferBytes([]byte{}), nil
	}
	if ptr == nil {
		return nil, invalidf("nil pointer for a %d-byte buffer", size)
	}
	n, err := toInt(size, "buffer size")
	if err != nil {
		return nil, err
	}
	alloc.pending.Add(1)
	return memory.NewBufferWithAllocator(unsafe.Slice((*byte)(ptr), n), alloc), nil
}

func lastOffset(buf *memory.Buffer, index, width int64) (int64, error) {
	if buf == nil {
		return 0, invalidf("nil offsets buffer")
	}
	data := buf.Bytes()
	if width == 4 {
		if index < 0 || index >= int64(len(data))/4 {
			return 0, invalidf("offset index %d is outside a %d-byte buffer", index, len(data))
		}
		return int64(binary.NativeEndian.Uint32(data[index*4:])), nil
	}
	if index < 0 || index >= int64(len(data))/8 {
		return 0, invalidf("offset index %d is outside a %d-byte buffer", index, len(data))
	}
	return int64(binary.NativeEndian.Uint64(data[index*8:])), nil
}

func validateOffsets(buf *memory.Buffer, lastIndex, width int64) error {
	if buf == nil {
		return invalidf("nil offsets buffer")
	}
	prev := int64(0)
	for i := int64(0); i <= lastIndex; i++ {
		off, err := lastOffset(buf, i, width)
		if err != nil {
			return err
		}
		if off < prev {
			return invalidf("offsets are not monotonic at index %d: %d after %d", i, off, prev)
		}
		prev = off
	}
	return nil
}

// ExportArray exports arr into dst. On success dst owns the release callback
// and the caller must release it exactly once. The Go array and its buffers
// are retained and pinned until that release. On error dst is zeroed.
func ExportArray(arr arrow.Array, dst *ArrowArray) error {
	if dst == nil {
		return invalidf("nil destination ArrowArray")
	}
	*dst = ArrowArray{}
	if arr == nil || arr.Data() == nil {
		return invalidf("nil array")
	}
	data := arr.Data()
	if err := validateDataType(data.DataType()); err != nil {
		return err
	}
	e, err := exportNode(data, data.DataType())
	if err != nil {
		return err
	}
	*dst = e.value
	return nil
}

// ExportRecordBatch exports batch as a struct array with one child per column.
// The batch is retained until the destination's release callback is invoked.
func ExportRecordBatch(batch arrow.RecordBatch, dst *ArrowArray) error {
	if dst == nil {
		return invalidf("nil destination ArrowArray")
	}
	*dst = ArrowArray{}
	if batch == nil {
		return invalidf("nil record batch")
	}
	batch.Retain()
	e := newArrayExporter(nil, batch)
	e.value.Length = batch.NumRows()
	e.value.NullCount = 0
	e.value.Offset = 0
	e.value.NBuffers = 1
	e.cBuffers = make([]unsafe.Pointer, 1)
	e.pinner.Pin(&e.cBuffers[0])
	e.value.Buffers = &e.cBuffers[0]

	n := batch.NumCols()
	if n > 0 {
		if n > int64(maxIntValue()) {
			return invalidf("record batch has too many columns: %d", n)
		}
		count := int(n)
		e.children = make([]*arrayExporter, count)
		e.cChildren = make([]*ArrowArray, count)
		e.pinner.Pin(&e.cChildren[0])
		e.value.Children = &e.cChildren[0]
		for i := int64(0); i < n; i++ {
			col := batch.Column(int(i)).Data()
			child, err := exportNode(col, col.DataType())
			if err != nil {
				e.freeTree()
				return fmt.Errorf("cdata: export record batch column %d: %w", i, err)
			}
			e.children[int(i)] = child
			e.cChildren[int(i)] = &child.value
		}
	}
	e.value.NChildren = int64(n)
	e.register()
	*dst = e.value
	return nil
}

type arrayExporter struct {
	value     ArrowArray
	box       *handleBox
	pinner    runtime.Pinner
	cBuffers  []unsafe.Pointer
	cChildren []*ArrowArray
	children  []*arrayExporter
	dict      *arrayExporter
	data      arrow.ArrayData
	batch     arrow.RecordBatch
	ownsData  bool
	pinned    bool
}

func newArrayExporter(data arrow.ArrayData, batch arrow.RecordBatch) *arrayExporter {
	e := &arrayExporter{data: data, batch: batch}
	e.pinner.Pin(&e.value)
	e.pinned = true
	return e
}

func exportNode(data arrow.ArrayData, dt arrow.DataType) (*arrayExporter, error) {
	if data == nil {
		return nil, invalidf("nil array data for %s", dt)
	}
	if err := validateDataType(dt); err != nil {
		return nil, err
	}
	if !arrow.TypeEqual(data.DataType(), dt) {
		return nil, invalidf("array data type %s does not match schema type %s", data.DataType(), dt)
	}
	n, err := expectedBufferCount(dt)
	if err != nil {
		return nil, err
	}
	buffers := data.Buffers()
	if len(buffers) < n {
		return nil, invalidf("%s has %d buffers, want at least %d", dt, len(buffers), n)
	}
	c, err := expectedChildCount(dt)
	if err != nil {
		return nil, err
	}
	children := data.Children()
	if len(children) != c {
		return nil, invalidf("%s has %d children, want %d", dt, len(children), c)
	}

	// Every exported node retains its own data. A consumer may move a child
	// structure out of the parent and release the parent immediately; the
	// child must keep its buffers alive until its own release callback runs.
	data.Retain()
	e := newArrayExporter(data, nil)
	e.ownsData = true
	e.value.Length = int64(data.Len())
	e.value.NullCount = int64(data.NullN())
	e.value.Offset = int64(data.Offset())
	e.value.NBuffers = int64(n)
	if n > 0 {
		e.cBuffers = make([]unsafe.Pointer, n)
		e.pinner.Pin(&e.cBuffers[0])
		e.value.Buffers = &e.cBuffers[0]
		for i := 0; i < n; i++ {
			b := buffers[i]
			if b == nil || b.Len() == 0 {
				if i == 0 {
					if data.NullN() != 0 {
						e.freeTree()
						return nil, invalidf("%s has %d nulls but no validity bitmap", dt, data.NullN())
					}
					e.cBuffers[i] = nil
					continue
				}
				e.cBuffers[i] = zeroBufferPointer()
				continue
			}
			bytes := b.Bytes()
			e.pinner.Pin(&bytes[0])
			e.cBuffers[i] = unsafe.Pointer(&bytes[0])
		}
	}

	if c > 0 {
		e.children = make([]*arrayExporter, c)
		e.cChildren = make([]*ArrowArray, c)
		e.pinner.Pin(&e.cChildren[0])
		e.value.Children = &e.cChildren[0]
		for i := 0; i < c; i++ {
			childType, err := childDataType(dt, i)
			if err != nil {
				e.freeTree()
				return nil, err
			}
			child, err := exportNode(children[i], childType)
			if err != nil {
				e.freeTree()
				return nil, err
			}
			e.children[int(i)] = child
			e.cChildren[int(i)] = &child.value
		}
	}
	if dictData := dictionaryData(data); dictData != nil {
		dictType, ok := dt.(*arrow.DictionaryType)
		if !ok {
			e.freeTree()
			return nil, invalidf("array has a dictionary but type %s is not dictionary", dt)
		}
		dict, err := exportNode(dictData, dictType.ValueType)
		if err != nil {
			e.freeTree()
			return nil, err
		}
		e.dict = dict
		e.value.Dictionary = &dict.value
	}
	e.value.NChildren = int64(c)
	e.register()
	return e, nil
}

// dictionaryData returns data.Dictionary with the typed-nil interface
// normalized to nil. *array.Data implements arrow.ArrayData, so a nil
// *array.Data stored in the interface compares non-nil.
func dictionaryData(data arrow.ArrayData) arrow.ArrayData {
	dict := data.Dictionary()
	if dict == nil {
		return nil
	}
	if concrete, ok := dict.(*array.Data); ok && concrete == nil {
		return nil
	}
	return dict
}

func childDataType(dt arrow.DataType, i int) (arrow.DataType, error) {
	switch t := dt.(type) {
	case *arrow.ListType:
		return t.ElemField().Type, nil
	case *arrow.LargeListType:
		return t.ElemField().Type, nil
	case *arrow.FixedSizeListType:
		return t.ElemField().Type, nil
	case *arrow.StructType:
		return t.Field(i).Type, nil
	case *arrow.MapType:
		return t.Elem(), nil
	default:
		return nil, invalidf("%s has no child %d", dt, i)
	}
}

func (e *arrayExporter) register() {
	if e.box == nil {
		e.box = newHandleBox(e)
		e.pinner.Pin(e.box)
	}
	e.value.Release = callbackArrayRelease
	e.value.PrivateData = unsafe.Pointer(e.box)
}

func (e *arrayExporter) freeSelf() {
	if !e.pinned {
		return
	}
	e.pinned = false
	if e.ownsData && e.data != nil {
		e.data.Release()
	}
	if e.batch != nil {
		e.batch.Release()
	}
	e.pinner.Unpin()
	if e.box != nil {
		handle.Remove(e.box.id)
	}
	e.cBuffers = nil
	e.cChildren = nil
	e.children = nil
	e.dict = nil
	e.data = nil
	e.batch = nil
	e.value = ArrowArray{}
}

func (e *arrayExporter) freeTree() {
	if e == nil || !e.pinned {
		return
	}
	for _, child := range e.children {
		child.freeTree()
	}
	e.dict.freeTree()
	e.freeSelf()
}

func (e *arrayExporter) releaseArray(_ *ArrowArray) {
	for _, child := range e.children {
		if child == nil {
			continue
		}
		c := &child.value
		if c.Release != 0 {
			_, _, _ = purego.SyscallN(c.Release, uintptr(unsafe.Pointer(c)))
		}
		// A zero release means the consumer moved this child and will
		// release the moved copy itself. The child retained its own data,
		// so the parent must not free it here.
	}
	if e.dict != nil && e.dict.value.Release != 0 {
		c := &e.dict.value
		_, _, _ = purego.SyscallN(c.Release, uintptr(unsafe.Pointer(c)))
	}
	e.freeSelf()
}
