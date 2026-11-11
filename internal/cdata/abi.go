// Package cdata implements the Arrow C Data Interface and the Arrow C Stream
// Interface in pure Go.
//
// The structs in this file are the ABI shared with foreign producers and
// consumers. Their field order, sizes, and alignment must match the C
// definitions in the Arrow specification. Release fields are function
// pointers represented as uintptr so they can be installed with purego
// callbacks and invoked with purego.SyscallN without cgo.
//
// All ownership is explicit:
//
//   - Import operations take ownership of the source structure, move it into Go
//     owned memory, and invoke the producer's release callback exactly once
//     when the last imported Arrow buffer is released.
//   - Export operations transfer ownership of the emitted structure to the
//     caller. The caller must invoke the release callback exactly once, either
//     on the original structure or on a bitwise copy that has been moved out of
//     the original location.
//   - Release callbacks may receive the address of a bitwise copy of the
//     structure. They must therefore not infer ownership from the address and
//     must keep their bookkeeping in PrivateData.
package cdata

import "unsafe"

// ArrowSchema mirrors struct ArrowSchema.
type ArrowSchema struct {
	Format      *byte
	Name        *byte
	Metadata    *byte
	Flags       int64
	NChildren   int64
	Children    **ArrowSchema
	Dictionary  *ArrowSchema
	Release     uintptr
	PrivateData unsafe.Pointer
}

// ArrowArray mirrors struct ArrowArray.
type ArrowArray struct {
	Length      int64
	NullCount   int64
	Offset      int64
	NBuffers    int64
	NChildren   int64
	Buffers     *unsafe.Pointer
	Children    **ArrowArray
	Dictionary  *ArrowArray
	Release     uintptr
	PrivateData unsafe.Pointer
}

// ArrowArrayStream mirrors struct ArrowArrayStream.
type ArrowArrayStream struct {
	GetSchema    uintptr
	GetNext      uintptr
	GetLastError uintptr
	Release      uintptr
	PrivateData  unsafe.Pointer
}

// ArrowSchema flags from the C Data Interface specification.
const (
	FlagDictionaryOrdered int64 = 1 << 0
	FlagNullable          int64 = 1 << 1
	FlagMapKeysSorted     int64 = 1 << 2
)

// The C Data Interface permits NULL only for zero-sized buffers. Some
// implementations still read the pointer, so export a stable non-NULL address
// for those buffers.
var zeroRegion [64]byte

func zeroBufferPointer() unsafe.Pointer {
	return unsafe.Pointer(&zeroRegion[0])
}

func schemaChildren(s *ArrowSchema) []*ArrowSchema {
	if s == nil || s.Children == nil || s.NChildren <= 0 {
		return nil
	}
	return unsafe.Slice(s.Children, int(s.NChildren))
}

func arrayChildren(a *ArrowArray) []*ArrowArray {
	if a == nil || a.Children == nil || a.NChildren <= 0 {
		return nil
	}
	return unsafe.Slice(a.Children, int(a.NChildren))
}

func arrayBuffers(a *ArrowArray) []unsafe.Pointer {
	if a == nil || a.Buffers == nil || a.NBuffers <= 0 {
		return nil
	}
	return unsafe.Slice(a.Buffers, int(a.NBuffers))
}
