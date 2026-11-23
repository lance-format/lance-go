package ffi

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

// cStringList owns NUL-terminated copies of Go strings and the pointer array
// used for C array parameters. The backing buffers and the array itself are
// pinned so native code may keep their addresses for the duration of a call:
// release must run after the native call returns. The list takes ownership of
// its pins before the first Pin, so a failed partial construction can release
// them.
//
// A nil list pointer array is passed as NULL to C.
//
// A cStringList must not be copied after construction.
type cStringList struct {
	ptrs     []*byte
	bufs     [][]byte
	pin      runtime.Pinner
	acquired bool
}

// acquiredLists counts string lists whose pins are still held. It changes once
// per list, never per pinned string, and exists so tests can prove that every
// exit path released its list; a missing unpin otherwise only surfaces as a
// fatal runtime.Pinner panic from a finalizer.
var acquiredLists atomic.Int64

// acquire records list ownership before the first Pin. release is the single
// place that clears it, so a partially built list can be released on error.
func (l *cStringList) acquire() {
	if l.acquired {
		return
	}
	l.acquired = true
	acquiredLists.Add(1)
}

func newCStringList(values []string) (*cStringList, error) {
	if len(values) == 0 {
		return &cStringList{}, nil
	}

	list := &cStringList{
		ptrs: make([]*byte, len(values)+1),
		bufs: make([][]byte, len(values)),
	}
	list.acquire()
	for i, value := range values {
		buf, err := cStringBuffer(value)
		if err != nil {
			list.release()
			return nil, err
		}
		list.bufs[i] = buf
		list.pin.Pin(&list.bufs[i][0])
		list.ptrs[i] = &list.bufs[i][0]
	}
	list.pin.Pin(&list.ptrs[0])
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
	list.acquire()
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
		list.pin.Pin(&list.bufs[i][0])
		list.ptrs[i] = &list.bufs[i][0]
	}
	list.pin.Pin(&list.ptrs[0])
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
		list.acquire()
		list.pin.Pin(&list.ptrs[0])
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
	if l == nil || !l.acquired {
		return
	}
	l.pin.Unpin()
	l.acquired = false
	acquiredLists.Add(-1)
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
