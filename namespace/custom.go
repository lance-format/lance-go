package namespace

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/lance-format/lance-go/internal/ffi"
	"github.com/lance-format/lance-go/internal/nshandle"
)

// CustomClient implements the four managed-table operations a custom native
// namespace can dispatch. Implementations must be safe for concurrent use:
// native code may invoke the same client from multiple goroutines.
//
// Requests and responses use the existing typed namespace models. Returned
// responses are encoded before the native call returns; the client must not
// mutate a response after returning it.
type CustomClient interface {
	DescribeTable(request *DescribeTableRequest) (*DescribeTableResponse, error)
	ListTableVersions(request *ListTableVersionsRequest) (*ListTableVersionsResponse, error)
	CreateTableVersion(request *CreateTableVersionRequest) (*CreateTableVersionResponse, error)
	DescribeTableVersion(request *DescribeTableVersionRequest) (*DescribeTableVersionResponse, error)
}

// CustomClientErrorCode is a typed error category reported by a CustomClient.
// The values match the native custom-dispatch callback contract. A custom
// client must use one of the declared constants; values without a mapped
// namespace error, including zero, are reported by native code as internal
// errors.
type CustomClientErrorCode int32

const (
	CustomClientErrorInvalidInput  CustomClientErrorCode = 1
	CustomClientErrorIO            CustomClientErrorCode = 2
	CustomClientErrorNotFound      CustomClientErrorCode = 3
	CustomClientErrorAlreadyExists CustomClientErrorCode = 4
	CustomClientErrorInternal      CustomClientErrorCode = 6
	CustomClientErrorUnsupported   CustomClientErrorCode = 7
	CustomClientErrorConflict      CustomClientErrorCode = 8
)

// CustomClientError reports a typed failure from a CustomClient back to native
// code. A returned CustomClientError is translated to the matching namespace
// error code; any other error is reported as an internal error. The error
// message is preserved in the resulting NamespaceError.
type CustomClientError struct {
	Code    CustomClientErrorCode
	Message string
}

// Error implements error.
func (e *CustomClientError) Error() string {
	if e == nil {
		return "custom namespace client error"
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("custom namespace client error code %d", e.Code)
}

// ErrorCode reports the native callback error code.
func (e *CustomClientError) ErrorCode() int32 {
	if e == nil {
		return int32(CustomClientErrorInternal)
	}
	return int32(e.Code)
}

type customClientAdapter struct {
	client CustomClient
}

// DispatchNamespace decodes the native request, calls exactly one typed client
// method, and encodes the typed response. The operation string is authoritative:
// several request models have overlapping JSON fields, so the operation is what
// distinguishes them.
func (a *customClientAdapter) DispatchNamespace(operation string, requestJSON []byte) ([]byte, error) {
	if a.client == nil {
		return nil, fmt.Errorf("custom namespace client is no longer available")
	}
	switch operation {
	case "describe_table":
		var request DescribeTableRequest
		if err := decodeCustomRequest(operation, requestJSON, &request); err != nil {
			return nil, err
		}
		response, err := a.client.DescribeTable(&request)
		if err != nil {
			return nil, err
		}
		return encodeCustomResponse(operation, response)
	case "list_table_versions":
		var request ListTableVersionsRequest
		if err := decodeCustomRequest(operation, requestJSON, &request); err != nil {
			return nil, err
		}
		response, err := a.client.ListTableVersions(&request)
		if err != nil {
			return nil, err
		}
		return encodeCustomResponse(operation, response)
	case "create_table_version":
		var request CreateTableVersionRequest
		if err := decodeCustomRequest(operation, requestJSON, &request); err != nil {
			return nil, err
		}
		response, err := a.client.CreateTableVersion(&request)
		if err != nil {
			return nil, err
		}
		return encodeCustomResponse(operation, response)
	case "describe_table_version":
		var request DescribeTableVersionRequest
		if err := decodeCustomRequest(operation, requestJSON, &request); err != nil {
			return nil, err
		}
		response, err := a.client.DescribeTableVersion(&request)
		if err != nil {
			return nil, err
		}
		return encodeCustomResponse(operation, response)
	default:
		return nil, &CustomClientError{
			Code:    CustomClientErrorUnsupported,
			Message: fmt.Sprintf("custom namespace does not support operation %q", operation),
		}
	}
}

func decodeCustomRequest(operation string, data []byte, request any) error {
	if len(data) == 0 {
		data = []byte("{}")
	}
	if err := json.Unmarshal(data, request); err != nil {
		return fmt.Errorf("custom namespace %s request: %w", operation, err)
	}
	return nil
}

func encodeCustomResponse(operation string, response any) ([]byte, error) {
	if response == nil {
		return nil, fmt.Errorf("custom namespace %s returned a nil response", operation)
	}
	value := reflect.ValueOf(response)
	if value.Kind() == reflect.Ptr && value.IsNil() {
		return nil, fmt.Errorf("custom namespace %s returned a nil response", operation)
	}
	data, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("custom namespace %s response: %w", operation, err)
	}
	return data, nil
}

func isNilCustomClient(client CustomClient) bool {
	if client == nil {
		return true
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// NewCustom creates a namespace backed by a Go CustomClient.
//
// Native code dispatches only DescribeTable, ListTableVersions,
// CreateTableVersion, and DescribeTableVersion to the client. Every other
// namespace operation returns the native unsupported error. The namespace
// retains the client until native code drops the final owner; NewCustom does
// not close the client, so the caller remains responsible for its resources.
//
// The client is called synchronously from the native caller's goroutine.
// context.Context values are not propagated because the dispatch callback has
// no context field. A callback must not close the namespace that invoked it,
// because Close waits for in-flight callbacks.
func NewCustom(client CustomClient, namespaceID string) (Namespace, error) {
	if isNilCustomClient(client) {
		return nil, fmt.Errorf("namespace: custom client is nil")
	}
	native, err := ffi.NewCustomNamespace(&customClientAdapter{client: client}, namespaceID)
	if err != nil {
		return nil, err
	}
	return &nativeNamespace{owner: nshandle.New(native)}, nil
}
