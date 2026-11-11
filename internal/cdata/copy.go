package cdata

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go/internal/handle"
)

// CopyBorrowedRecordBatch copies a borrowed C record batch into Go-owned
// memory.
//
// The mapper callback receives an ArrowArray/ArrowSchema pair that is valid
// only for the duration of the callback. ImportRecordBatch normally takes
// ownership of its source, which is not allowed for a borrowed producer
// value. This helper imports a shallow copy of the array structure whose
// release callbacks are neutralized, then copies every Arrow buffer into Go
// memory. The producer's array and release callback are left untouched.
//
// The returned batch owns all of its buffers and remains valid after the
// native callback returns. The schema is imported before the array so the
// existing import validation is reused for both layouts.
func CopyBorrowedRecordBatch(src *ArrowArray, srcSchema *ArrowSchema) (arrow.RecordBatch, error) {
	if src == nil {
		return nil, invalidf("nil borrowed ArrowArray")
	}
	if srcSchema == nil {
		return nil, invalidf("nil borrowed ArrowSchema")
	}
	if src.Release == 0 {
		return nil, invalidf("borrowed ArrowArray has no release callback")
	}

	schema, err := ImportSchema(srcSchema)
	if err != nil {
		return nil, fmt.Errorf("cdata: import borrowed schema: %w", err)
	}
	owner := &borrowedClone{}
	root, err := owner.clone(src)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, invalidf("nil borrowed ArrowArray")
	}
	owner.pin()
	// The clone has no producer release. Give the import allocator an inert
	// root callback that releases the clone's pins once the imported buffers
	// no longer reference this borrowed structure.
	root.Release = callbackArrayRelease
	root.PrivateData = unsafe.Pointer(owner.box)
	imported, err := ImportRecordBatch(root, schema)
	runtime.KeepAlive(owner)
	if err != nil {
		return nil, fmt.Errorf("cdata: import borrowed record batch: %w", err)
	}
	defer imported.Release()

	copied, err := copyRecordBatch(imported)
	if err != nil {
		return nil, err
	}
	return copied, nil
}

// borrowedClone owns the copied C array structures for the lifetime of an
// import that aliases the producer's buffers. The root release callback runs
// when those imported buffers are released, at which point the copied
// structures and their pointer arrays can be unpinned.
type borrowedClone struct {
	box         *handleBox
	pinner      runtime.Pinner
	nodes       []*ArrowArray
	childSlices [][]*ArrowArray
	pinned      bool
}

func (c *borrowedClone) pin() {
	if c.pinned {
		return
	}
	for _, node := range c.nodes {
		c.pinner.Pin(node)
	}
	for _, children := range c.childSlices {
		if len(children) > 0 {
			c.pinner.Pin(&children[0])
		}
	}
	c.box = newHandleBox(c)
	c.pinned = true
}

func (c *borrowedClone) releaseArray(*ArrowArray) {
	if !c.pinned {
		return
	}
	c.pinner.Unpin()
	c.pinned = false
	if c.box != nil {
		handle.Remove(c.box.id)
		c.box = nil
	}
}

// clone copies the C structures (not the buffers) so the borrowed source can
// be imported without moving or releasing it. Buffer and dictionary pointers
// still refer to the producer's memory, which remains valid for the duration
// of the callback and is copied before it returns.
func (c *borrowedClone) clone(src *ArrowArray) (*ArrowArray, error) {
	if src == nil {
		return nil, nil
	}
	if src.NBuffers < 0 || src.NChildren < 0 {
		return nil, invalidf("array has negative buffer or child count")
	}
	node := *src
	node.Release = 0
	node.PrivateData = nil
	if src.NChildren > 0 {
		if src.Children == nil {
			return nil, invalidf("array has %d children but a nil children pointer", src.NChildren)
		}
		children := make([]*ArrowArray, src.NChildren)
		for i := range children {
			child, err := c.clone(src.Children[i])
			if err != nil {
				return nil, err
			}
			children[i] = child
		}
		node.Children = &children[0]
		c.childSlices = append(c.childSlices, children)
	} else {
		node.Children = nil
	}
	if src.Dictionary != nil {
		dict, err := c.clone(src.Dictionary)
		if err != nil {
			return nil, err
		}
		node.Dictionary = dict
	}
	c.nodes = append(c.nodes, &node)
	return &node, nil
}

func copyRecordBatch(batch arrow.RecordBatch) (arrow.RecordBatch, error) {
	if batch == nil {
		return nil, invalidf("nil record batch")
	}
	schema := batch.Schema()
	if schema == nil {
		return nil, invalidf("record batch has a nil schema")
	}
	columns := make([]arrow.Array, 0, batch.NumCols())
	ok := false
	defer func() {
		if ok {
			return
		}
		for _, col := range columns {
			col.Release()
		}
	}()

	for i := 0; i < int(batch.NumCols()); i++ {
		data, err := copyArrayData(batch.Column(i).Data())
		if err != nil {
			return nil, fmt.Errorf("cdata: copy record batch column %d: %w", i, err)
		}
		col := array.MakeFromData(data)
		data.Release()
		columns = append(columns, col)
	}
	copied, err := newRecordBatch(schema, columns, batch.NumRows())
	if err != nil {
		return nil, err
	}
	for _, col := range columns {
		col.Release()
	}
	columns = nil
	ok = true
	return copied, nil
}

func copyArrayData(data arrow.ArrayData) (*array.Data, error) {
	if data == nil {
		return nil, invalidf("nil array data")
	}
	dt := data.DataType()
	if dt == nil {
		return nil, invalidf("array data has a nil data type")
	}

	buffers := make([]*memory.Buffer, len(data.Buffers()))
	for i, buf := range data.Buffers() {
		if buf == nil {
			continue
		}
		// Bytes exposes the complete underlying buffer, including the part
		// before a sliced array's offset. Preserving the whole buffer and the
		// original offset keeps sliced validity, offsets and values aligned.
		buffers[i] = memory.NewBufferBytes(copyBytes(buf.Bytes()))
	}
	children := make([]arrow.ArrayData, len(data.Children()))
	ok := false
	defer func() {
		if ok {
			return
		}
		for _, buf := range buffers {
			if buf != nil {
				buf.Release()
			}
		}
		for _, child := range children {
			if child != nil {
				child.Release()
			}
		}
	}()

	for i, child := range data.Children() {
		if child == nil {
			continue
		}
		copiedChild, err := copyArrayData(child)
		if err != nil {
			return nil, fmt.Errorf("child %d: %w", i, err)
		}
		children[i] = copiedChild
	}

	copied := array.NewData(dt, data.Len(), buffers, children, data.NullN(), data.Offset())
	if dict := data.Dictionary(); dict != nil {
		dictData, err := copyArrayData(dict)
		if err != nil {
			copied.Release()
			return nil, fmt.Errorf("dictionary: %w", err)
		}
		copied.SetDictionary(dictData)
		dictData.Release()
	}
	// NewData retains every buffer and child. Release the references that
	// this function created after the new data has taken its own.
	for _, buf := range buffers {
		if buf != nil {
			buf.Release()
		}
	}
	for _, child := range children {
		if child != nil {
			child.Release()
		}
	}
	ok = true
	return copied, nil
}

func copyBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}
