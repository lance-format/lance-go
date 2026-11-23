package ffi

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

// cStringList owns NUL-terminated copies of Go strings and the pointer array
// used for C array parameters. The backing buffers and the array itself are
// pinned so native code may keep their addresses for the duration of a call:
// release must run after the native call returns. Ownership is registered as
// soon as an object is pinned, so a failed partial construction cannot leak a
// pin.
//
// A nil list pointer array is passed as NULL to C.
//
// A cStringList must not be copied after construction.
type cStringList struct {
	ptrs []*byte
	bufs [][]byte
	pin  runtime.Pinner
	pins int
}

// pinnedObjects counts the objects currently pinned by live cStringList
// values. Tests compare it with a baseline to prove that an exit path released
// its pins; a missing unpin otherwise only surfaces as a fatal runtime.Pinner
// panic from a finalizer.
var pinnedObjects atomic.Int64

// pinPointer pins one object and records it for release.
func (l *cStringList) pinPointer(pointer any) {
	l.pin.Pin(pointer)
	l.pins++
	pinnedObjects.Add(1)
}

func newCStringList(values []string) (*cStringList, error) {
	if len(values) == 0 {
		return &cStringList{}, nil
	}

	list := &cStringList{
		ptrs: make([]*byte, len(values)+1),
		bufs: make([][]byte, len(values)),
	}
	for i, value := range values {
		buf, err := cStringBuffer(value)
		if err != nil {
			list.release()
			return nil, err
		}
		list.bufs[i] = buf
		list.pinPointer(&list.bufs[i][0])
		list.ptrs[i] = &list.bufs[i][0]
	}
	list.pinPointer(&list.ptrs[0])
	return list, nil
}

// newNullableCStringList is the pointer-valued form used by metadata updates,
// where a nil value means deletion and an empty non-nil string is a real
// value. The list itself still has a trailing NULL pointer entry.
func newNullableCStringList(values []*string) (*cStringList, error) {
	if len(values) == 0 {
		return &cStringList{}, nil
	}

	list := &cStringList{
		ptrs: make([]*byte, len(values)+1),
		bufs: make([][]byte, len(values)),
	}
	for i, value := range values {
		if value == nil {
			continue
		}
		buf, err := cStringBuffer(*value)
		if err != nil {
			list.release()
			return nil, err
		}
		list.bufs[i] = buf
		list.pinPointer(&list.bufs[i][0])
		list.ptrs[i] = &list.bufs[i][0]
	}
	list.pinPointer(&list.ptrs[0])
	return list, nil
}

func newCStringPtr(value string) ([]byte, *byte, error) {
	buf, err := cStringBuffer(value)
	if err != nil {
		return nil, nil, err
	}
	return buf, &buf[0], nil
}

// newPresentCStringList is like newCStringList but always produces a non-NULL
// pointer array, including for an empty input. Use it for native parameters
// where NULL means "inherit" and a non-NULL empty list means "explicitly
// empty".
func newPresentCStringList(values []string) (*cStringList, error) {
	list, err := newCStringList(values)
	if err != nil {
		return nil, err
	}
	if list.ptrs == nil {
		list.ptrs = []*byte{nil}
		list.pinPointer(&list.ptrs[0])
	}
	return list, nil
}

// first returns the first string pointer, or nil when the list is absent or
// empty.
func (l *cStringList) first() *byte {
	if l == nil || len(l.ptrs) == 0 {
		return nil
	}
	return l.ptrs[0]
}

// data returns the address of the first pointer entry for use in a C struct.
// A nil pointer array is passed as NULL to C.
func (l *cStringList) data() **byte {
	if l == nil || len(l.ptrs) == 0 {
		return nil
	}
	return &l.ptrs[0]
}

// release unpins the backing buffers and the pointer array. It must run after
// the native call that consumed them returns. It is safe to call repeatedly,
// including on a nil list.
func (l *cStringList) release() {
	if l == nil || l.pins == 0 {
		return
	}
	l.pin.Unpin()
	pinnedObjects.Add(int64(-l.pins))
	l.pins = 0
}

func cStringBuffer(value string) ([]byte, error) {
	if err := validateCString(value); err != nil {
		return nil, err
	}
	buf := make([]byte, len(value)+1)
	copy(buf, value)
	return buf, nil
}

func validateCString(value string) error {
	for i := 0; i < len(value); i++ {
		if value[i] == 0 {
			return fmt.Errorf("ffi: string contains an embedded NUL byte")
		}
	}
	return nil
}

func validateCStrings(values []string) error {
	for _, value := range values {
		if err := validateCString(value); err != nil {
			return err
		}
	}
	return nil
}
