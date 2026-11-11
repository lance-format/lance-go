package ffi

import (
	"fmt"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/cdata"
)

// importArrowStream drains a caller-owned native ArrowArrayStream and releases
// it exactly once. The returned batches are independent Arrow Go values and
// must be released by the caller.
func importArrowStream(stream *cdata.ArrowArrayStream) ([]arrow.RecordBatch, error) {
	if stream.Release == 0 || stream.GetSchema == 0 || stream.GetNext == 0 {
		return nil, fmt.Errorf("ffi: native Arrow stream is missing required callbacks")
	}
	defer cdata.ReleaseStream(stream)

	var rawSchema cdata.ArrowSchema
	defer cdata.ReleaseSchema(&rawSchema)
	if rc := callStreamGetSchema(stream, &rawSchema); rc != 0 {
		return nil, streamError(stream, "get_schema")
	}

	schema, err := cdata.ImportSchema(&rawSchema)
	if err != nil {
		return nil, fmt.Errorf("ffi: import Arrow stream schema: %w", err)
	}

	var batches []arrow.RecordBatch
	for {
		var rawArray cdata.ArrowArray
		rc := callStreamGetNext(stream, &rawArray)
		if rc != 0 {
			if rawArray.Release != 0 {
				cdata.ReleaseArray(&rawArray)
			}
			releaseRecordBatches(batches)
			return nil, streamError(stream, "get_next")
		}
		if rawArray.Release == 0 {
			break
		}

		batch, err := cdata.ImportRecordBatch(&rawArray, schema)
		if err != nil {
			// ImportRecordBatch consumes the array on every return path.
			releaseRecordBatches(batches)
			return nil, fmt.Errorf("ffi: import Arrow stream batch: %w", err)
		}
		batches = append(batches, batch)
	}
	return batches, nil
}

func releaseRecordBatches(batches []arrow.RecordBatch) {
	for _, batch := range batches {
		batch.Release()
	}
}

func callStreamGetSchema(stream *cdata.ArrowArrayStream, out *cdata.ArrowSchema) uintptr {
	result, _, _ := purego.SyscallN(
		stream.GetSchema,
		uintptr(unsafe.Pointer(stream)),
		uintptr(unsafe.Pointer(out)),
	)
	return result
}

func callStreamGetNext(stream *cdata.ArrowArrayStream, out *cdata.ArrowArray) uintptr {
	result, _, _ := purego.SyscallN(
		stream.GetNext,
		uintptr(unsafe.Pointer(stream)),
		uintptr(unsafe.Pointer(out)),
	)
	return result
}

func streamError(stream *cdata.ArrowArrayStream, operation string) error {
	message := "native Arrow stream reported an error"
	if stream.GetLastError != 0 {
		var getLastError func(*cdata.ArrowArrayStream) *byte
		purego.RegisterFunc(&getLastError, stream.GetLastError)
		if ptr := getLastError(stream); ptr != nil {
			message = goString(ptr)
		}
	}
	return fmt.Errorf("ffi: Arrow stream %s: %s", operation, message)
}
