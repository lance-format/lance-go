package ffi

import (
	"fmt"
	"runtime"
	"unsafe"
)

// cStringList owns NUL-terminated copies of Go strings and the pointer array
// used for C array parameters. The buffers stay alive as long as the list is
// reachable. A nil list pointer array is passed as NULL to C.
type cStringList struct {
	ptrs  []uintptr
	bufs  [][]byte
	owned bool
}

func newCStringList(values []string) (*cStringList, error) {
	if len(values) == 0 {
		return &cStringList{}, nil
	}

	list := &cStringList{
		ptrs:  make([]uintptr, len(values)+1),
		bufs:  make([][]byte, len(values)),
		owned: true,
	}
	for i, value := range values {
		if err := validateCString(value); err != nil {
			return nil, err
		}
		buf := make([]byte, len(value)+1)
		copy(buf, value)
		list.bufs[i] = buf
		list.ptrs[i] = uintptr(unsafe.Pointer(&buf[0]))
	}
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
		ptrs:  make([]uintptr, len(values)+1),
		bufs:  make([][]byte, len(values)),
		owned: true,
	}
	for i, value := range values {
		if value == nil {
			continue
		}
		if err := validateCString(*value); err != nil {
			return nil, err
		}
		buf := make([]byte, len(*value)+1)
		copy(buf, *value)
		list.bufs[i] = buf
		list.ptrs[i] = uintptr(unsafe.Pointer(&buf[0]))
	}
	return list, nil
}

func newCStringPtr(value string) ([]byte, *byte, error) {
	if err := validateCString(value); err != nil {
		return nil, nil, err
	}
	buf := make([]byte, len(value)+1)
	copy(buf, value)
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
		list.ptrs = []uintptr{0}
	}
	return list, nil
}

// ptr returns the address of the first pointer entry for use in a C struct.
// A nil pointer array is passed as NULL to C.
func (l *cStringList) ptr() *uintptr {
	if l == nil || len(l.ptrs) == 0 {
		return nil
	}
	return &l.ptrs[0]
}

// keepAlive must run after the native call, directly or through defer: ptrs
// contains integer addresses that do not keep the backing string buffers live.
func (l *cStringList) keepAlive() {
	if l == nil {
		return
	}
	runtime.KeepAlive(l.ptrs)
	runtime.KeepAlive(l.bufs)
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
