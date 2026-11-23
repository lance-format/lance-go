package namespace

import (
	"errors"
	"fmt"

	"github.com/lance-format/lance-go/internal/ffi"
)

// ErrClosed is returned when a namespace or adapter is already closed.
var ErrClosed = errors.New("namespace: closed")

// Category sentinels classify native namespace failures for errors.Is.
//
// The namespace result channel preserves the exact native Lance Namespace
// code, so each category follows that code's meaning: the not-found codes,
// including TableColumnNotFound, are ErrNotFound; the already-exists codes are
// ErrAlreadyExists; ConcurrentModification is ErrConflict; InvalidInput,
// InvalidTableState, TableSchemaValidationError and NamespaceNotEmpty are
// ErrInvalidInput, matching the pinned lance-c mapping of those families; and
// the remaining codes keep their own category. Every code keeps its exact
// numeric value in Error.Code. A code added by a newer native library matches
// no sentinel; inspect Error.Code to handle it.
var (
	ErrNotFound         = errors.New("namespace: not found")
	ErrAlreadyExists    = errors.New("namespace: already exists")
	ErrInvalidInput     = errors.New("namespace: invalid input")
	ErrPermissionDenied = errors.New("namespace: permission denied")
	ErrUnauthenticated  = errors.New("namespace: unauthenticated")
	ErrConflict         = errors.New("namespace: conflict")
	ErrUnsupported      = errors.New("namespace: unsupported")
	ErrThrottled        = errors.New("namespace: throttled")
	ErrUnavailable      = errors.New("namespace: unavailable")
	ErrInternal         = errors.New("namespace: internal error")
)

// ErrorCode is the error code reported by the native namespace.
type ErrorCode uint32

// Native namespace error codes. Unsupported is zero and is still an error.
const (
	ErrorCodeUnsupported                ErrorCode = 0
	ErrorCodeNamespaceNotFound          ErrorCode = 1
	ErrorCodeNamespaceAlreadyExists     ErrorCode = 2
	ErrorCodeNamespaceNotEmpty          ErrorCode = 3
	ErrorCodeTableNotFound              ErrorCode = 4
	ErrorCodeTableAlreadyExists         ErrorCode = 5
	ErrorCodeTableIndexNotFound         ErrorCode = 6
	ErrorCodeTableIndexAlreadyExists    ErrorCode = 7
	ErrorCodeTableTagNotFound           ErrorCode = 8
	ErrorCodeTableTagAlreadyExists      ErrorCode = 9
	ErrorCodeTransactionNotFound        ErrorCode = 10
	ErrorCodeTableVersionNotFound       ErrorCode = 11
	ErrorCodeTableColumnNotFound        ErrorCode = 12
	ErrorCodeInvalidInput               ErrorCode = 13
	ErrorCodeConcurrentModification     ErrorCode = 14
	ErrorCodePermissionDenied           ErrorCode = 15
	ErrorCodeUnauthenticated            ErrorCode = 16
	ErrorCodeServiceUnavailable         ErrorCode = 17
	ErrorCodeInternal                   ErrorCode = 18
	ErrorCodeInvalidTableState          ErrorCode = 19
	ErrorCodeTableSchemaValidationError ErrorCode = 20
	ErrorCodeThrottling                 ErrorCode = 21
	ErrorCodeTableBranchNotFound        ErrorCode = 22
	ErrorCodeTableBranchAlreadyExists   ErrorCode = 23
)

// String returns the native Lance Namespace name for the code. A code added by
// a newer native library keeps its numeric value so it stays inspectable.
func (c ErrorCode) String() string {
	switch c {
	case ErrorCodeUnsupported:
		return "Unsupported"
	case ErrorCodeNamespaceNotFound:
		return "NamespaceNotFound"
	case ErrorCodeNamespaceAlreadyExists:
		return "NamespaceAlreadyExists"
	case ErrorCodeNamespaceNotEmpty:
		return "NamespaceNotEmpty"
	case ErrorCodeTableNotFound:
		return "TableNotFound"
	case ErrorCodeTableAlreadyExists:
		return "TableAlreadyExists"
	case ErrorCodeTableIndexNotFound:
		return "TableIndexNotFound"
	case ErrorCodeTableIndexAlreadyExists:
		return "TableIndexAlreadyExists"
	case ErrorCodeTableTagNotFound:
		return "TableTagNotFound"
	case ErrorCodeTableTagAlreadyExists:
		return "TableTagAlreadyExists"
	case ErrorCodeTransactionNotFound:
		return "TransactionNotFound"
	case ErrorCodeTableVersionNotFound:
		return "TableVersionNotFound"
	case ErrorCodeTableColumnNotFound:
		return "TableColumnNotFound"
	case ErrorCodeInvalidInput:
		return "InvalidInput"
	case ErrorCodeConcurrentModification:
		return "ConcurrentModification"
	case ErrorCodePermissionDenied:
		return "PermissionDenied"
	case ErrorCodeUnauthenticated:
		return "Unauthenticated"
	case ErrorCodeServiceUnavailable:
		return "ServiceUnavailable"
	case ErrorCodeInternal:
		return "Internal"
	case ErrorCodeInvalidTableState:
		return "InvalidTableState"
	case ErrorCodeTableSchemaValidationError:
		return "TableSchemaValidationError"
	case ErrorCodeThrottling:
		return "Throttling"
	case ErrorCodeTableBranchNotFound:
		return "TableBranchNotFound"
	case ErrorCodeTableBranchAlreadyExists:
		return "TableBranchAlreadyExists"
	default:
		return fmt.Sprintf("ErrorCode(%d)", uint32(c))
	}
}

// Error reports an operation error produced by the native namespace.
//
// Unwrap exposes the matching category sentinel, so errors.Is works without
// unpacking the value. The namespace code space is distinct from the lance-c
// code space reported by NativeError.
type Error struct {
	Operation string
	Code      ErrorCode
	Message   string
}

// NamespaceError is the pre-v0.3.0 name of Error.
//
// Deprecated: use Error. The alias names the same type, so existing
// errors.As targets and composite literals keep working.
type NamespaceError = Error

// NamespaceErrorCode is the pre-v0.3.0 name of ErrorCode.
//
// Deprecated: use ErrorCode. The alias names the same type, so existing
// errors.As targets and composite literals keep working.
type NamespaceErrorCode = ErrorCode

func (e *Error) Error() string {
	return namespaceDiagnostic(e.Operation, "namespace error "+e.Code.String(), e.Message)
}

// Unwrap returns the category sentinel for the native namespace code, or nil
// for a code this SDK does not know yet.
func (e *Error) Unwrap() error {
	switch e.Code {
	case ErrorCodeNamespaceNotFound, ErrorCodeTableNotFound,
		ErrorCodeTableIndexNotFound, ErrorCodeTableTagNotFound,
		ErrorCodeTransactionNotFound, ErrorCodeTableVersionNotFound,
		ErrorCodeTableColumnNotFound, ErrorCodeTableBranchNotFound:
		return ErrNotFound
	case ErrorCodeNamespaceAlreadyExists, ErrorCodeTableAlreadyExists,
		ErrorCodeTableIndexAlreadyExists, ErrorCodeTableTagAlreadyExists,
		ErrorCodeTableBranchAlreadyExists:
		return ErrAlreadyExists
	case ErrorCodeNamespaceNotEmpty, ErrorCodeInvalidInput,
		ErrorCodeInvalidTableState, ErrorCodeTableSchemaValidationError:
		return ErrInvalidInput
	case ErrorCodeConcurrentModification:
		return ErrConflict
	case ErrorCodePermissionDenied:
		return ErrPermissionDenied
	case ErrorCodeUnauthenticated:
		return ErrUnauthenticated
	case ErrorCodeServiceUnavailable:
		return ErrUnavailable
	case ErrorCodeThrottling:
		return ErrThrottled
	case ErrorCodeInternal:
		return ErrInternal
	case ErrorCodeUnsupported:
		return ErrUnsupported
	default:
		return nil
	}
}

// NativeError reports a non-namespace native error, such as an invalid
// argument or a panic caught by the C API. Its Code is a lance-c LanceErrorCode,
// not a namespace code.
type NativeError struct {
	Operation string
	Code      int32
	Message   string
}

func (e *NativeError) Error() string {
	return namespaceDiagnostic(e.Operation, "native error "+ffi.Code(e.Code).String(), e.Message)
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

// namespaceDiagnostic renders "namespace <operation>: <detail>: <message>" and
// omits empty parts.
func namespaceDiagnostic(operation, detail, message string) string {
	result := "namespace"
	if operation != "" {
		result += " " + operation
	}
	result += ": " + detail
	if message != "" {
		result += ": " + message
	}
	return result
}

func wrapNativeError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var namespaceErr *ffi.NamespaceError
	if errors.As(err, &namespaceErr) {
		return &Error{
			Operation: operation,
			Code:      ErrorCode(namespaceErr.Code),
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

// IsNotFound reports whether err is a native not-found namespace error. It is
// shorthand for errors.Is(err, ErrNotFound) and includes
// ErrorCodeTableColumnNotFound, which the pinned native implementation reports
// as not found.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
