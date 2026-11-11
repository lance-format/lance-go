package ffi

import (
	"fmt"
	"unsafe"
)

// copyNativeByteBuffer copies and releases a native LanceByteBuffer.
func copyNativeByteBuffer(b *bindings, buffer uintptr) ([]byte, error) {
	defer b.byteBufferClose(buffer)
	length := b.byteBufferLen(buffer)
	if length == 0 {
		return []byte{}, nil
	}
	if length > uint64(maxIntValue()) {
		return nil, fmt.Errorf("ffi: byte buffer length exceeds Go slice limit: %d", length)
	}
	ptr := b.byteBufferData(buffer)
	if ptr == nil {
		return nil, fmt.Errorf("ffi: byte buffer has length %d but NULL data", length)
	}
	return append([]byte(nil), unsafe.Slice(ptr, int(length))...), nil
}
