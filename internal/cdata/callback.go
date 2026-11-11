package cdata

import (
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/handle"
)

// handleBox is the Go allocation named by a foreign PrivateData pointer. It is
// pinned for as long as the corresponding C structure is owned by the foreign
// side. This keeps the handle identifier valid even when a consumer copies the
// C structure to another address before releasing it.
type handleBox struct {
	id uint64
}

func newHandleBox(v any) *handleBox {
	return &handleBox{id: handle.New(v)}
}

func handleID(ptr unsafe.Pointer) uint64 {
	if ptr == nil {
		return 0
	}
	return (*handleBox)(ptr).id
}

// ReleaseSchema invokes src.Release if it is non-zero. The producer marks the
// structure released inside its callback. It is safe for nil or already released
// structures, foreign producers, and structures copied away from their
// original address. It does not clear src.PrivateData: the producer's release
// callback owns that state and may still need it.
func ReleaseSchema(src *ArrowSchema) {
	if src == nil || src.Release == 0 {
		return
	}
	fn := src.Release
	defer func() {
		_ = recover()
	}()
	_, _, _ = purego.SyscallN(fn, uintptr(unsafe.Pointer(src)))
}

// ReleaseArray invokes src.Release exactly once if it is non-zero. Its
// semantics match ReleaseSchema, including support for moved structures.
func ReleaseArray(src *ArrowArray) {
	if src == nil || src.Release == 0 {
		return
	}
	fn := src.Release
	defer func() {
		_ = recover()
	}()
	_, _, _ = purego.SyscallN(fn, uintptr(unsafe.Pointer(src)))
}

// ReleaseStream invokes src.Release exactly once if it is non-zero. Its
// semantics match ReleaseSchema, including support for moved structures.
func ReleaseStream(src *ArrowArrayStream) {
	if src == nil || src.Release == 0 {
		return
	}
	fn := src.Release
	defer func() {
		_ = recover()
	}()
	_, _, _ = purego.SyscallN(fn, uintptr(unsafe.Pointer(src)))
}

type schemaReleaser interface {
	releaseSchema(*ArrowSchema)
}

type arrayReleaser interface {
	releaseArray(*ArrowArray)
}

type streamReleaser interface {
	releaseStream(*ArrowArrayStream)
}

// The callbacks below are installed once at package initialization. purego
// callbacks are never freed and have a process-wide limit, so no export path
// is allowed to create callbacks dynamically.
//
// The C release hooks return void. The Go trampolines return a uintptr because
// syscall.NewCallback, which purego uses on Windows, requires callbacks to have
// exactly one uintptr-sized result. The value is ignored by C.
var (
	callbackSchemaRelease = purego.NewCallback(schemaReleaseCallback)
	callbackArrayRelease  = purego.NewCallback(arrayReleaseCallback)
	callbackStreamRelease = purego.NewCallback(streamReleaseCallback)

	callbackStreamGetSchema    = purego.NewCallback(streamGetSchemaCallback)
	callbackStreamGetNext      = purego.NewCallback(streamGetNextCallback)
	callbackStreamGetLastError = purego.NewCallback(streamGetLastErrorCallback)
)

func schemaReleaseCallback(s *ArrowSchema) (ret uintptr) {
	if s == nil {
		return
	}
	defer func() {
		_ = recover()
	}()

	s.Release = 0
	id := handleID(s.PrivateData)
	if id == 0 {
		return
	}
	v, ok := handle.Take(id)
	if !ok {
		return
	}
	if r, ok := v.(schemaReleaser); ok {
		r.releaseSchema(s)
	}
	return
}

func arrayReleaseCallback(a *ArrowArray) (ret uintptr) {
	if a == nil {
		return
	}
	defer func() {
		_ = recover()
	}()

	a.Release = 0
	id := handleID(a.PrivateData)
	if id == 0 {
		return
	}
	v, ok := handle.Take(id)
	if !ok {
		return
	}
	if r, ok := v.(arrayReleaser); ok {
		r.releaseArray(a)
	}
	return
}

func streamReleaseCallback(s *ArrowArrayStream) (ret uintptr) {
	if s == nil {
		return
	}
	defer func() {
		_ = recover()
	}()

	s.Release = 0
	id := handleID(s.PrivateData)
	if id == 0 {
		return
	}
	v, ok := handle.Take(id)
	if !ok {
		return
	}
	if r, ok := v.(streamReleaser); ok {
		r.releaseStream(s)
	}
	return
}
