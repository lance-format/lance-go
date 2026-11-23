package namespace

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/lance-format/lance-go/internal/ffi"
)

// categorySentinels is every category sentinel exposed by the package.
var categorySentinels = []error{
	ErrNotFound,
	ErrAlreadyExists,
	ErrInvalidInput,
	ErrPermissionDenied,
	ErrUnauthenticated,
	ErrConflict,
	ErrUnsupported,
	ErrThrottled,
	ErrUnavailable,
	ErrInternal,
}

func TestWrapNativeErrorKeepsUnsupportedCode(t *testing.T) {
	wrapped := wrapNativeError("namespace_exists", &ffi.NamespaceError{Code: 0, Message: "unsupported"})
	var namespaceErr *Error
	if !errors.As(wrapped, &namespaceErr) {
		t.Fatalf("wrapped error type = %T, want *Error", wrapped)
	}
	if namespaceErr.Code != ErrorCodeUnsupported {
		t.Fatalf("wrapped code = %d, want %d", namespaceErr.Code, ErrorCodeUnsupported)
	}
	if namespaceErr.Operation != "namespace_exists" {
		t.Fatalf("wrapped operation = %q", namespaceErr.Operation)
	}
	if !errors.Is(wrapped, ErrUnsupported) {
		t.Fatalf("wrapped error = %v, want ErrUnsupported", wrapped)
	}
}

func TestWrapNativeErrorKeepsNativeErrorDistinct(t *testing.T) {
	wrapped := wrapNativeError("get_table_stats", &ffi.Error{Code: ffi.CodeNotFound, Message: "table not found"})
	var nativeErr *NativeError
	if !errors.As(wrapped, &nativeErr) {
		t.Fatalf("wrapped error type = %T, want *NativeError", wrapped)
	}
	if nativeErr.Code != int32(ffi.CodeNotFound) {
		t.Fatalf("wrapped code = %d, want %d", nativeErr.Code, ffi.CodeNotFound)
	}
	var namespaceErr *Error
	if errors.As(wrapped, &namespaceErr) {
		t.Fatalf("native error %v matched *Error", wrapped)
	}
	if !strings.Contains(wrapped.Error(), "native error LANCE_ERR_NOT_FOUND") {
		t.Fatalf("native diagnostic = %q, want the lance-c code name", wrapped.Error())
	}
}

func TestWrapNativeErrorClosed(t *testing.T) {
	wrapped := wrapNativeError("list_tables", ffi.ErrClosed)
	if !errors.Is(wrapped, ErrClosed) {
		t.Fatalf("wrapped closed error = %v, want ErrClosed", wrapped)
	}
}

func TestErrorCodeString(t *testing.T) {
	codes := map[ErrorCode]string{
		ErrorCodeUnsupported:                "Unsupported",
		ErrorCodeNamespaceNotFound:          "NamespaceNotFound",
		ErrorCodeNamespaceAlreadyExists:     "NamespaceAlreadyExists",
		ErrorCodeNamespaceNotEmpty:          "NamespaceNotEmpty",
		ErrorCodeTableNotFound:              "TableNotFound",
		ErrorCodeTableAlreadyExists:         "TableAlreadyExists",
		ErrorCodeTableIndexNotFound:         "TableIndexNotFound",
		ErrorCodeTableIndexAlreadyExists:    "TableIndexAlreadyExists",
		ErrorCodeTableTagNotFound:           "TableTagNotFound",
		ErrorCodeTableTagAlreadyExists:      "TableTagAlreadyExists",
		ErrorCodeTransactionNotFound:        "TransactionNotFound",
		ErrorCodeTableVersionNotFound:       "TableVersionNotFound",
		ErrorCodeTableColumnNotFound:        "TableColumnNotFound",
		ErrorCodeInvalidInput:               "InvalidInput",
		ErrorCodeConcurrentModification:     "ConcurrentModification",
		ErrorCodePermissionDenied:           "PermissionDenied",
		ErrorCodeUnauthenticated:            "Unauthenticated",
		ErrorCodeServiceUnavailable:         "ServiceUnavailable",
		ErrorCodeInternal:                   "Internal",
		ErrorCodeInvalidTableState:          "InvalidTableState",
		ErrorCodeTableSchemaValidationError: "TableSchemaValidationError",
		ErrorCodeThrottling:                 "Throttling",
		ErrorCodeTableBranchNotFound:        "TableBranchNotFound",
		ErrorCodeTableBranchAlreadyExists:   "TableBranchAlreadyExists",
	}
	if len(codes) != 24 {
		t.Fatalf("code table has %d entries, want 24", len(codes))
	}
	for code, want := range codes {
		if got := code.String(); got != want {
			t.Errorf("ErrorCode(%d).String() = %q, want %q", uint32(code), got, want)
		}
	}
	for _, code := range []ErrorCode{24, 99, ErrorCode(^uint32(0))} {
		want := fmt.Sprintf("ErrorCode(%d)", uint32(code))
		if got := code.String(); got != want {
			t.Errorf("unknown code String() = %q, want %q", got, want)
		}
	}
}

func TestErrorUnwrapClassifiesNativeCodes(t *testing.T) {
	categories := map[ErrorCode]error{
		ErrorCodeUnsupported:                ErrUnsupported,
		ErrorCodeNamespaceNotFound:          ErrNotFound,
		ErrorCodeNamespaceAlreadyExists:     ErrAlreadyExists,
		ErrorCodeNamespaceNotEmpty:          ErrInvalidInput,
		ErrorCodeTableNotFound:              ErrNotFound,
		ErrorCodeTableAlreadyExists:         ErrAlreadyExists,
		ErrorCodeTableIndexNotFound:         ErrNotFound,
		ErrorCodeTableIndexAlreadyExists:    ErrAlreadyExists,
		ErrorCodeTableTagNotFound:           ErrNotFound,
		ErrorCodeTableTagAlreadyExists:      ErrAlreadyExists,
		ErrorCodeTransactionNotFound:        ErrNotFound,
		ErrorCodeTableVersionNotFound:       ErrNotFound,
		ErrorCodeTableColumnNotFound:        ErrNotFound,
		ErrorCodeInvalidInput:               ErrInvalidInput,
		ErrorCodeConcurrentModification:     ErrConflict,
		ErrorCodePermissionDenied:           ErrPermissionDenied,
		ErrorCodeUnauthenticated:            ErrUnauthenticated,
		ErrorCodeServiceUnavailable:         ErrUnavailable,
		ErrorCodeInternal:                   ErrInternal,
		ErrorCodeInvalidTableState:          ErrInvalidInput,
		ErrorCodeTableSchemaValidationError: ErrInvalidInput,
		ErrorCodeThrottling:                 ErrThrottled,
		ErrorCodeTableBranchNotFound:        ErrNotFound,
		ErrorCodeTableBranchAlreadyExists:   ErrAlreadyExists,
	}
	if len(categories) != 24 {
		t.Fatalf("category table has %d entries, want 24", len(categories))
	}
	for code, want := range categories {
		err := &Error{Operation: "describe_table", Code: code, Message: "native message"}
		if !errors.Is(err, want) {
			t.Errorf("code %s: errors.Is(%v) = false, want true", code, want)
		}
		for _, sentinel := range categorySentinels {
			if matches := errors.Is(err, sentinel); matches != (sentinel == want) {
				t.Errorf("code %s matched %v = %v, want %v", code, sentinel, matches, sentinel == want)
			}
		}
		var namespaceErr *Error
		if !errors.As(err, &namespaceErr) || namespaceErr.Code != code {
			t.Errorf("code %s: errors.As lost the exact code", code)
		}
	}
}

func TestErrorUnknownCodeStaysInspectable(t *testing.T) {
	const unknownCode ErrorCode = 99
	err := &Error{Operation: "describe_table", Code: unknownCode, Message: "newer native code"}
	if err.Unwrap() != nil {
		t.Fatalf("Unwrap = %v, want nil", err.Unwrap())
	}
	for _, sentinel := range categorySentinels {
		if errors.Is(err, sentinel) {
			t.Errorf("unknown code matched %v", sentinel)
		}
	}
	if IsNotFound(err) {
		t.Fatal("unknown code was treated as not found")
	}
	var namespaceErr *Error
	if !errors.As(err, &namespaceErr) || namespaceErr.Code != unknownCode {
		t.Fatalf("errors.As lost the unknown code: %v", namespaceErr)
	}
	if got, want := err.Error(), "namespace describe_table: namespace error ErrorCode(99): newer native code"; got != want {
		t.Errorf("diagnostic = %q, want %q", got, want)
	}
}

func TestErrorDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "namespace code",
			err:  &Error{Operation: "describe_table", Code: ErrorCodeTableNotFound, Message: "table users not found"},
			want: "namespace describe_table: namespace error TableNotFound: table users not found",
		},
		{
			name: "namespace code without message or operation",
			err:  &Error{Code: ErrorCodeUnsupported},
			want: "namespace: namespace error Unsupported",
		},
		{
			name: "native code",
			err:  &NativeError{Operation: "open", Code: int32(ffi.CodeNotFound), Message: "dataset not found"},
			want: "namespace open: native error LANCE_ERR_NOT_FOUND: dataset not found",
		},
		{
			name: "unknown native code",
			err:  &NativeError{Operation: "open", Code: 42, Message: "newer native code"},
			want: "namespace open: native error LanceErrorCode(42): newer native code",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorDiagnosticsDistinguishCodeSpaces(t *testing.T) {
	// The same number means different things in the two code spaces. The text
	// must show which one reported the failure.
	namespaceErr := (&Error{Operation: "describe_table", Code: 4, Message: "not found"}).Error()
	nativeErr := (&NativeError{Operation: "describe_table", Code: 4, Message: "not found"}).Error()
	if namespaceErr == nativeErr {
		t.Fatalf("namespace and native diagnostics are identical: %q", namespaceErr)
	}
	if !strings.Contains(namespaceErr, "namespace error TableNotFound") {
		t.Fatalf("namespace diagnostic = %q", namespaceErr)
	}
	if !strings.Contains(nativeErr, "native error LANCE_ERR_DATASET_ALREADY_EXISTS") {
		t.Fatalf("native diagnostic = %q", nativeErr)
	}
}

func TestIsNotFound(t *testing.T) {
	notFound := []ErrorCode{
		ErrorCodeNamespaceNotFound,
		ErrorCodeTableNotFound,
		ErrorCodeTableIndexNotFound,
		ErrorCodeTableTagNotFound,
		ErrorCodeTransactionNotFound,
		ErrorCodeTableVersionNotFound,
		ErrorCodeTableColumnNotFound,
		ErrorCodeTableBranchNotFound,
	}
	for _, code := range notFound {
		if !IsNotFound(&Error{Code: code}) {
			t.Fatalf("code %s was not treated as not found", code)
		}
		if !errors.Is(&Error{Code: code}, ErrNotFound) {
			t.Fatalf("code %s did not match ErrNotFound", code)
		}
	}
	notFoundSet := make(map[ErrorCode]bool, len(notFound))
	for _, code := range notFound {
		notFoundSet[code] = true
	}
	for code := ErrorCode(0); code <= ErrorCode(23); code++ {
		if notFoundSet[code] {
			continue
		}
		if IsNotFound(&Error{Code: code}) {
			t.Fatalf("code %s was treated as not found", code)
		}
	}
	if IsNotFound(&Error{Code: 99}) {
		t.Fatal("unknown code was treated as not found")
	}
	if IsNotFound(errors.New("other")) {
		t.Fatal("unrelated error was treated as not found")
	}
}

func TestDeprecatedTypeAliasesPreserveIdentity(t *testing.T) {
	if reflect.TypeOf(NamespaceError{}) != reflect.TypeOf(Error{}) {
		t.Fatal("NamespaceError is not an alias of Error")
	}
	if reflect.TypeOf(NamespaceErrorCode(0)) != reflect.TypeOf(ErrorCode(0)) {
		t.Fatal("NamespaceErrorCode is not an alias of ErrorCode")
	}
	if got, want := reflect.TypeOf(Error{}).Name(), "Error"; got != want {
		t.Fatalf("canonical type name = %q, want %q", got, want)
	}
	if got, want := reflect.TypeOf(Error{}).PkgPath(), "github.com/lance-format/lance-go/namespace"; got != want {
		t.Fatalf("canonical type package path = %q, want %q", got, want)
	}
	if got, want := fmt.Sprintf("%T", Error{}), "namespace.Error"; got != want {
		t.Fatalf("canonical value %%T = %q, want %q", got, want)
	}
	if got, want := fmt.Sprintf("%T", &Error{}), "*namespace.Error"; got != want {
		t.Fatalf("canonical pointer %%T = %q, want %q", got, want)
	}
	// The alias cannot restore the old reflected spelling.
	if got, want := reflect.TypeOf(NamespaceError{}).Name(), "Error"; got != want {
		t.Fatalf("alias type name = %q, want %q", got, want)
	}
	if got, want := fmt.Sprintf("%T", NamespaceError{}), "namespace.Error"; got != want {
		t.Fatalf("alias value %%T = %q, want %q", got, want)
	}

	// Composite literals and errors.As targets using the old names must keep
	// working because they name the same type.
	oldNameErr := &NamespaceError{Operation: "drop_table", Code: ErrorCodeTableNotFound, Message: "missing"}
	var canonical *Error
	if !errors.As(oldNameErr, &canonical) {
		t.Fatal("errors.As into *Error failed for a *NamespaceError value")
	}
	newNameErr := &Error{Operation: "drop_table", Code: ErrorCodeTableNotFound, Message: "missing"}
	var deprecated *NamespaceError
	if !errors.As(newNameErr, &deprecated) {
		t.Fatal("errors.As into *NamespaceError failed for a *Error value")
	}
	oldCode := NamespaceErrorCode(ErrorCodeThrottling)
	if !errors.Is(&Error{Code: oldCode}, ErrThrottled) {
		t.Fatal("deprecated code alias lost category classification")
	}
}
