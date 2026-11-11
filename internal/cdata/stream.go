package cdata

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/handle"
)

type streamExporter struct {
	value     ArrowArrayStream
	box       *handleBox
	pinner    runtime.Pinner
	errPinner runtime.Pinner
	reader    array.RecordReader
	lastError []byte
	errPinned bool
	pinned    bool
}

// ExportRecordReader exports reader as an Arrow C stream. On success dst owns
// the stream and the caller must invoke its release callback exactly once,
// possibly after moving the structure. The reader is retained until release.
// On error dst is zeroed.
func ExportRecordReader(reader array.RecordReader, dst *ArrowArrayStream) error {
	if dst == nil {
		return invalidf("nil destination ArrowArrayStream")
	}
	*dst = ArrowArrayStream{}
	if reader == nil {
		return invalidf("nil record reader")
	}
	if reader.Schema() == nil {
		return invalidf("record reader has a nil schema")
	}
	reader.Retain()
	e := &streamExporter{reader: reader}
	e.pinner.Pin(&e.value)
	e.pinned = true
	e.register()
	*dst = e.value
	return nil
}

func (e *streamExporter) register() {
	if e.box == nil {
		e.box = newHandleBox(e)
		e.pinner.Pin(e.box)
	}
	e.value.GetSchema = callbackStreamGetSchema
	e.value.GetNext = callbackStreamGetNext
	e.value.GetLastError = callbackStreamGetLastError
	e.value.Release = callbackStreamRelease
	e.value.PrivateData = unsafe.Pointer(e.box)
}

func (e *streamExporter) setError(err error) {
	if e.errPinned {
		e.errPinner.Unpin()
		e.errPinned = false
	}
	if err == nil {
		e.lastError = nil
		return
	}
	e.lastError = cString(err.Error())
	e.errPinner.Pin(&e.lastError[0])
	e.errPinned = true
}

func (e *streamExporter) releaseStream(_ *ArrowArrayStream) {
	if e.errPinned {
		e.errPinner.Unpin()
		e.errPinned = false
	}
	if e.reader != nil {
		e.reader.Release()
		e.reader = nil
	}
	if !e.pinned {
		return
	}
	e.pinned = false
	e.pinner.Unpin()
	if e.box != nil {
		handle.Remove(e.box.id)
	}
	e.lastError = nil
	e.value = ArrowArrayStream{}
}

func streamState(ptr uintptr) (*streamExporter, bool) {
	if ptr == 0 {
		return nil, false
	}
	s := (*ArrowArrayStream)(unsafe.Pointer(ptr))
	v, ok := handle.Get(handleID(s.PrivateData))
	if !ok {
		return nil, false
	}
	e, ok := v.(*streamExporter)
	return e, ok
}

func streamGetSchemaCallback(streamPtr, outPtr uintptr) (ret uintptr) {
	defer func() {
		if r := recover(); r != nil {
			if outPtr != 0 {
				out := (*ArrowSchema)(unsafe.Pointer(outPtr))
				if out.Release != 0 {
					ReleaseSchema(out)
				}
				*out = ArrowSchema{}
			}
			ret = uintptr(syscall.EINVAL)
		}
	}()
	if streamPtr == 0 || outPtr == 0 {
		return uintptr(syscall.EINVAL)
	}
	e, ok := streamState(streamPtr)
	if !ok || e.reader == nil {
		return uintptr(syscall.EINVAL)
	}
	out := (*ArrowSchema)(unsafe.Pointer(outPtr))
	*out = ArrowSchema{}
	if err := ExportSchema(e.reader.Schema(), out); err != nil {
		e.setError(err)
		*out = ArrowSchema{}
		return uintptr(syscall.EINVAL)
	}
	return 0
}

func streamGetNextCallback(streamPtr, outPtr uintptr) (ret uintptr) {
	defer func() {
		if r := recover(); r != nil {
			if outPtr != 0 {
				out := (*ArrowArray)(unsafe.Pointer(outPtr))
				if out.Release != 0 {
					ReleaseArray(out)
				}
				*out = ArrowArray{}
			}
			ret = uintptr(syscall.EIO)
		}
	}()
	if streamPtr == 0 || outPtr == 0 {
		return uintptr(syscall.EINVAL)
	}
	e, ok := streamState(streamPtr)
	if !ok || e.reader == nil {
		return uintptr(syscall.EINVAL)
	}
	out := (*ArrowArray)(unsafe.Pointer(outPtr))
	*out = ArrowArray{}
	if !e.reader.Next() {
		if err := e.reader.Err(); err != nil {
			e.setError(err)
			return uintptr(syscall.EIO)
		}
		return 0
	}
	rec := e.reader.RecordBatch()
	if rec == nil {
		e.setError(errors.New("record reader returned a nil batch"))
		return uintptr(syscall.EIO)
	}
	defer rec.Release()
	if err := ExportRecordBatch(rec, out); err != nil {
		e.setError(err)
		*out = ArrowArray{}
		return uintptr(syscall.EINVAL)
	}
	return 0
}

func streamGetLastErrorCallback(streamPtr uintptr) (ret uintptr) {
	defer func() {
		if r := recover(); r != nil {
			ret = 0
		}
	}()
	e, ok := streamState(streamPtr)
	if !ok || len(e.lastError) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&e.lastError[0]))
}
