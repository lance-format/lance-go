package ffi

import (
	"fmt"
	"runtime"
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

func internalf(format string, args ...any) error {
	return &Error{Code: CodeInternal, Message: fmt.Sprintf(format, args...)}
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

// takeErrorOr consumes the pending error on the calling OS thread and returns
// it. When a native entry point reported failure without setting one, it
// returns a fallback that names what failed, for example
//
//	b.takeErrorOr("lance_dataset_open returned NULL")
//
// reports "ffi: lance_dataset_open returned NULL without setting an error".
//
// takeErrorOr has the same requirements as takeError: the caller must hold the
// OS-thread lock taken for the failing native call, and the pending error must
// not have been consumed or overwritten since. The fallback is only a last
// resort and never replaces an error the native layer set.
func (b *bindings) takeErrorOr(description string) error {
	if err := b.takeError(); err != nil {
		return err
	}
	return fmt.Errorf("ffi: %s without setting an error", description)
}

// callPinned runs call with the calling goroutine locked to its OS thread and
// returns the thread-local error the native entry point set. The lock covers
// both the call and the error capture, so the two cannot be split across OS
// threads.
//
// callPinned does not clear a stale error before call and consumes the pending
// error even when the native entry point reports success. Use it only for
// entry points whose failure signal is the thread-local error alone; entry
// points that also report a status return or a NULL handle keep their explicit
// sentinel check, because consuming the error unconditionally could attribute
// a stale error to a successful call.
func callPinned[T any](b *bindings, call func() T) (T, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	value := call()
	if err := b.takeError(); err != nil {
		var zero T
		return zero, err
	}
	return value, nil
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
