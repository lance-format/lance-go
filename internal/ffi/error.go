package ffi

import (
	"fmt"
	"unsafe"
)

// Code mirrors LanceErrorCode from include/lance/lance.h.
type Code int32

// Error codes defined by the pinned lance-c header.
const (
	CodeOK                   Code = 0
	CodeInvalidArgument      Code = 1
	CodeIO                   Code = 2
	CodeNotFound             Code = 3
	CodeDatasetAlreadyExists Code = 4
	CodeIndex                Code = 5
	CodeInternal             Code = 6
	CodeNotSupported         Code = 7
	CodeCommitConflict       Code = 8
	CodePanic                Code = 9
)

// String returns the C enumerator name, or LanceErrorCode(N) for codes added
// by a newer lance-c.
func (c Code) String() string {
	switch c {
	case CodeOK:
		return "LANCE_OK"
	case CodeInvalidArgument:
		return "LANCE_ERR_INVALID_ARGUMENT"
	case CodeIO:
		return "LANCE_ERR_IO"
	case CodeNotFound:
		return "LANCE_ERR_NOT_FOUND"
	case CodeDatasetAlreadyExists:
		return "LANCE_ERR_DATASET_ALREADY_EXISTS"
	case CodeIndex:
		return "LANCE_ERR_INDEX"
	case CodeInternal:
		return "LANCE_ERR_INTERNAL"
	case CodeNotSupported:
		return "LANCE_ERR_NOT_SUPPORTED"
	case CodeCommitConflict:
		return "LANCE_ERR_COMMIT_CONFLICT"
	case CodePanic:
		return "LANCE_ERR_PANIC"
	default:
		return fmt.Sprintf("LanceErrorCode(%d)", int32(c))
	}
}

// Error reports a Lance error code and message.
type Error struct {
	Code    Code
	Message string
}

func invalidf(format string, args ...any) error {
	return &Error{Code: CodeInvalidArgument, Message: fmt.Sprintf(format, args...)}
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("lance-c: %s", e.Code)
	}
	return fmt.Sprintf("lance-c: %s: %s", e.Code, e.Message)
}

// takeError consumes the pending error on the calling OS thread.
//
// The code must be read before the message: lance_last_error_message takes the
// error out of Rust TLS, so reading the code afterwards would always return
// LANCE_OK. The message is copied before lance_free_string releases it.
//
// takeError must be called while the goroutine is locked to the same OS thread
// that performed the failing native call. Callers are responsible for the
// lock; OpenDataset and Dataset.Close show the pattern.
func (b *bindings) takeError() *Error {
	code := b.lastErrorCode()
	if code == int32(CodeOK) {
		return nil
	}

	var message string
	if ptr := b.lastErrorMessage(); ptr != nil {
		message = goString(ptr)
		b.freeString(ptr)
	}
	return &Error{Code: Code(code), Message: message}
}

// goString copies a NUL-terminated C string into Go memory. The copy is
// independent of the C allocation, so the caller can free the C string
// immediately afterwards.
func goString(ptr *byte) string {
	if ptr == nil {
		return ""
	}

	length := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(ptr), length)) != 0 {
		length++
	}
	return string(unsafe.Slice(ptr, length))
}
