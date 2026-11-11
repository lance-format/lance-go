package ffi

import (
	"fmt"
	"runtime"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
)

// Schema is an owned native Lance schema value. It preserves Lance field IDs,
// nested metadata, encodings and dictionary values that an ArrowSchema view
// cannot represent.
type Schema struct {
	b      *bindings
	handle atomic.Uintptr
}

func wrapSchema(b *bindings, handle uintptr) *Schema {
	s := &Schema{b: b}
	s.handle.Store(handle)
	return s
}

// NativeSchema clones the dataset's native logical schema.
func (d *Dataset) NativeSchema() (*Schema, error) {
	handleValue := d.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}

	var out uintptr
	runtime.LockOSThread()
	rc := d.b.datasetSchemaClone(handleValue, &out)
	if rc != 0 {
		nativeErr := d.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_schema_clone failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_dataset_schema_clone returned NULL without setting an error")
	}
	return wrapSchema(d.b, out), nil
}

// Close releases the native schema handle. It is idempotent.
func (s *Schema) Close() {
	handle := s.handle.Swap(0)
	if handle == 0 {
		return
	}
	runtime.LockOSThread()
	s.b.schemaClose(handle)
	runtime.UnlockOSThread()
}

// Clone returns an independently owned copy.
func (s *Schema) Clone() (*Schema, error) {
	handleValue := s.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}

	runtime.LockOSThread()
	out := s.b.schemaClone(handleValue)
	if out == 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_schema_clone returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return wrapSchema(s.b, out), nil
}

// ToArrow exports a lossy Arrow view of the native schema.
func (s *Schema) ToArrow() (*arrow.Schema, error) {
	handleValue := s.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}

	var raw cdata.ArrowSchema
	runtime.LockOSThread()
	rc := s.b.schemaToArrow(handleValue, &raw)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_schema_to_arrow failed without setting an error")
	}
	runtime.UnlockOSThread()

	defer cdata.ReleaseSchema(&raw)
	schema, err := cdata.ImportSchema(&raw)
	if err != nil {
		return nil, fmt.Errorf("ffi: import native schema: %w", err)
	}
	return schema, nil
}

// Project selects an ordered list of column paths without rebuilding through
// Arrow. A nil or empty list selects native Schema::project's empty schema.
func (s *Schema) Project(columns []string) (*Schema, error) {
	handleValue := s.handle.Load()
	if handleValue == 0 {
		return nil, ErrClosed
	}
	list, err := newCStringList(columns)
	if err != nil {
		return nil, err
	}

	var out uintptr
	runtime.LockOSThread()
	rc := s.b.schemaProject(handleValue, list.ptrs, uint64(len(columns)), &out)
	list.keepAlive()
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_schema_project failed without setting an error")
	}
	runtime.UnlockOSThread()
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_schema_project returned NULL without setting an error")
	}
	return wrapSchema(s.b, out), nil
}

// FieldID resolves a column path to its native Lance field ID.
func (s *Schema) FieldID(column string) (int32, error) {
	handleValue := s.handle.Load()
	if handleValue == 0 {
		return 0, ErrClosed
	}
	if err := validateCString(column); err != nil {
		return 0, err
	}

	var fieldID int32
	runtime.LockOSThread()
	rc := s.b.schemaFieldID(handleValue, column, &fieldID)
	if rc != 0 {
		nativeErr := s.b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_schema_field_id failed without setting an error")
	}
	runtime.UnlockOSThread()
	return fieldID, nil
}
