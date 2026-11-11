package lance

import (
	"errors"
	"fmt"

	"github.com/lance-format/lance-go/internal/ffi"
	"github.com/lance-format/lance-go/namespace"
)

// Code mirrors LanceErrorCode.
type Code int32

const (
	CodeOK              Code = 0
	CodeInvalidArgument Code = 1
	CodeIO              Code = 2
	CodeNotFound        Code = 3
	CodeAlreadyExists   Code = 4
	CodeIndex           Code = 5
	CodeInternal        Code = 6
	CodeNotSupported    Code = 7
	CodeCommitConflict  Code = 8
	CodePanic           Code = 9
)

// String returns the C enumerator name.
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
	case CodeAlreadyExists:
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

// Sentinel errors classify native and argument failures for errors.Is.
var (
	ErrClosed          = errors.New("lance: closed")
	ErrInvalidArgument = errors.New("lance: invalid argument")
	ErrIO              = errors.New("lance: io error")
	ErrNotFound        = errors.New("lance: not found")
	ErrAlreadyExists   = errors.New("lance: already exists")
	ErrIndex           = errors.New("lance: index error")
	ErrInternal        = errors.New("lance: internal error")
	ErrNotSupported    = errors.New("lance: not supported")
	ErrCommitConflict  = errors.New("lance: commit conflict")
	ErrPanic           = errors.New("lance: native panic")
)

// Error is returned by dataset, writer, scanner and session operations.
type Error struct {
	Op      string
	URI     string
	Code    Code
	Message string
}

// ErrorCode returns the native Lance error code. It lets callback adapters
// preserve a typed callback failure without depending on this package.
func (e *Error) ErrorCode() int32 {
	return int32(e.Code)
}

func (e *Error) Error() string {
	message := e.Message
	if message == "" {
		message = e.Code.String()
	}
	if e.Op == "" && e.URI == "" {
		return fmt.Sprintf("lance: %s", message)
	}
	if e.URI == "" {
		return fmt.Sprintf("lance: %s: %s", e.Op, message)
	}
	if e.Op == "" {
		return fmt.Sprintf("lance: %s: %s", e.URI, message)
	}
	return fmt.Sprintf("lance: %s %s: %s", e.Op, e.URI, message)
}

// Unwrap maps the native code to its sentinel error.
func (e *Error) Unwrap() error {
	switch e.Code {
	case CodeInvalidArgument:
		return ErrInvalidArgument
	case CodeIO:
		return ErrIO
	case CodeNotFound:
		return ErrNotFound
	case CodeAlreadyExists:
		return ErrAlreadyExists
	case CodeIndex:
		return ErrIndex
	case CodeInternal:
		return ErrInternal
	case CodeNotSupported:
		return ErrNotSupported
	case CodeCommitConflict:
		return ErrCommitConflict
	case CodePanic:
		return ErrPanic
	default:
		return nil
	}
}

func invalidError(op, uri, format string, args ...any) error {
	return &Error{Op: op, URI: uri, Code: CodeInvalidArgument, Message: fmt.Sprintf(format, args...)}
}

func closedError(op, uri string) error {
	if uri == "" {
		return fmt.Errorf("lance: %s: %w", op, ErrClosed)
	}
	return fmt.Errorf("lance: %s %s: %w", op, uri, ErrClosed)
}

func wrapError(op, uri string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ffi.ErrClosed) {
		return closedError(op, uri)
	}
	if errors.Is(err, namespace.ErrClosed) {
		return closedError(op, uri)
	}
	var namespaceErr *namespace.NamespaceError
	if errors.As(err, &namespaceErr) {
		return &Error{
			Op:      op,
			URI:     uri,
			Code:    namespaceErrorCode(namespaceErr.Code),
			Message: fmt.Sprintf("namespace error %d: %s", namespaceErr.Code, namespaceErr.Message),
		}
	}
	var namespaceNative *namespace.NativeError
	if errors.As(err, &namespaceNative) {
		return &Error{
			Op:      op,
			URI:     uri,
			Code:    Code(namespaceNative.Code),
			Message: namespaceNative.Message,
		}
	}
	var native *ffi.Error
	if errors.As(err, &native) {
		code := Code(native.Code)
		if native.Code == ffi.CodeDatasetAlreadyExists {
			code = CodeAlreadyExists
		}
		return &Error{Op: op, URI: uri, Code: code, Message: native.Message}
	}
	if uri == "" {
		return fmt.Errorf("lance: %s: %w", op, err)
	}
	return fmt.Errorf("lance: %s %s: %w", op, uri, err)
}

func namespaceErrorCode(code namespace.NamespaceErrorCode) Code {
	switch code {
	case namespace.ErrorCodeUnsupported:
		return CodeNotSupported
	case namespace.ErrorCodeNamespaceNotFound, namespace.ErrorCodeTableNotFound,
		namespace.ErrorCodeTableIndexNotFound, namespace.ErrorCodeTableTagNotFound,
		namespace.ErrorCodeTransactionNotFound, namespace.ErrorCodeTableVersionNotFound,
		namespace.ErrorCodeTableBranchNotFound:
		return CodeNotFound
	case namespace.ErrorCodeNamespaceAlreadyExists, namespace.ErrorCodeTableAlreadyExists,
		namespace.ErrorCodeTableIndexAlreadyExists, namespace.ErrorCodeTableTagAlreadyExists,
		namespace.ErrorCodeTableBranchAlreadyExists:
		return CodeAlreadyExists
	case namespace.ErrorCodeInvalidInput, namespace.ErrorCodeTableColumnNotFound,
		namespace.ErrorCodeInvalidTableState, namespace.ErrorCodeTableSchemaValidationError,
		namespace.ErrorCodeNamespaceNotEmpty:
		return CodeInvalidArgument
	case namespace.ErrorCodeConcurrentModification:
		return CodeCommitConflict
	case namespace.ErrorCodeServiceUnavailable, namespace.ErrorCodeThrottling:
		return CodeIO
	case namespace.ErrorCodePermissionDenied, namespace.ErrorCodeUnauthenticated:
		return CodeInternal
	default:
		return CodeInternal
	}
}
