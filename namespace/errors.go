package namespace

import (
	"errors"
	"fmt"

	"github.com/lance-format/lance-go/internal/ffi"
)

// ErrClosed is returned when a namespace or adapter is already closed.
var ErrClosed = errors.New("namespace: closed")

// NamespaceErrorCode is the error code reported by the native namespace.
type NamespaceErrorCode uint32

// Native namespace error codes. Unsupported is zero and is still an error.
const (
	ErrorCodeUnsupported                NamespaceErrorCode = 0
	ErrorCodeNamespaceNotFound          NamespaceErrorCode = 1
	ErrorCodeNamespaceAlreadyExists     NamespaceErrorCode = 2
	ErrorCodeNamespaceNotEmpty          NamespaceErrorCode = 3
	ErrorCodeTableNotFound              NamespaceErrorCode = 4
	ErrorCodeTableAlreadyExists         NamespaceErrorCode = 5
	ErrorCodeTableIndexNotFound         NamespaceErrorCode = 6
	ErrorCodeTableIndexAlreadyExists    NamespaceErrorCode = 7
	ErrorCodeTableTagNotFound           NamespaceErrorCode = 8
	ErrorCodeTableTagAlreadyExists      NamespaceErrorCode = 9
	ErrorCodeTransactionNotFound        NamespaceErrorCode = 10
	ErrorCodeTableVersionNotFound       NamespaceErrorCode = 11
	ErrorCodeTableColumnNotFound        NamespaceErrorCode = 12
	ErrorCodeInvalidInput               NamespaceErrorCode = 13
	ErrorCodeConcurrentModification     NamespaceErrorCode = 14
	ErrorCodePermissionDenied           NamespaceErrorCode = 15
	ErrorCodeUnauthenticated            NamespaceErrorCode = 16
	ErrorCodeServiceUnavailable         NamespaceErrorCode = 17
	ErrorCodeInternal                   NamespaceErrorCode = 18
	ErrorCodeInvalidTableState          NamespaceErrorCode = 19
	ErrorCodeTableSchemaValidationError NamespaceErrorCode = 20
	ErrorCodeThrottling                 NamespaceErrorCode = 21
	ErrorCodeTableBranchNotFound        NamespaceErrorCode = 22
	ErrorCodeTableBranchAlreadyExists   NamespaceErrorCode = 23
)

// NamespaceError reports an operation error produced by the native namespace.
type NamespaceError struct {
	Operation string
	Code      NamespaceErrorCode
	Message   string
}

func (e *NamespaceError) Error() string {
	return fmt.Sprintf("namespace %s: native error %d: %s", e.Operation, e.Code, e.Message)
}

// NativeError reports a non-namespace native error, such as an invalid
// argument or a panic caught by the C API.
type NativeError struct {
	Operation string
	Code      int32
	Message   string
}

func (e *NativeError) Error() string {
	return fmt.Sprintf("namespace %s: native error %d: %s", e.Operation, e.Code, e.Message)
}

// ProtocolError reports a malformed native response or an unencodable request.
type ProtocolError struct {
	Operation string
	Err       error
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("namespace %s: protocol error: %v", e.Operation, e.Err)
}

func (e *ProtocolError) Unwrap() error { return e.Err }

func wrapNativeError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var namespaceErr *ffi.NamespaceError
	if errors.As(err, &namespaceErr) {
		return &NamespaceError{
			Operation: operation,
			Code:      NamespaceErrorCode(namespaceErr.Code),
			Message:   namespaceErr.Message,
		}
	}
	var nativeErr *ffi.Error
	if errors.As(err, &nativeErr) {
		return &NativeError{Operation: operation, Code: int32(nativeErr.Code), Message: nativeErr.Message}
	}
	if errors.Is(err, ffi.ErrClosed) {
		return fmt.Errorf("namespace %s: %w", operation, ErrClosed)
	}
	return fmt.Errorf("namespace %s: %w", operation, err)
}

// IsNotFound reports whether err is a native not-found namespace error.
func IsNotFound(err error) bool {
	var target *NamespaceError
	if !errors.As(err, &target) {
		return false
	}
	switch target.Code {
	case ErrorCodeNamespaceNotFound, ErrorCodeTableNotFound,
		ErrorCodeTableIndexNotFound, ErrorCodeTableTagNotFound,
		ErrorCodeTransactionNotFound, ErrorCodeTableVersionNotFound,
		ErrorCodeTableBranchNotFound:
		return true
	default:
		return false
	}
}
