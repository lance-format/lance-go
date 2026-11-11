package ffi

import (
	"encoding/json"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/cdata"
)

// Namespace is an opaque native namespace handle. Directory, REST and
// Connected namespaces all share it; the native implementation owns discovery,
// retries, routing and credential refresh.
type Namespace struct {
	b      *bindings
	handle uintptr
}

// NamespaceError is a failure reported through LanceNamespaceResult. Its Code
// is the Lance Namespace ErrorCode and is meaningful even when it is zero
// (Unsupported).
type NamespaceError struct {
	Code    uint32
	Message string
}

func (e *NamespaceError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("namespace error code %d", e.Code)
	}
	return fmt.Sprintf("namespace error code %d: %s", e.Code, e.Message)
}

// RESTAdapter is an opaque native REST adapter handle.
type RESTAdapter struct {
	b      *bindings
	handle uintptr
}

func namespacePropertiesJSON(properties map[string]string) (string, error) {
	if len(properties) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(properties)
	if err != nil {
		return "", fmt.Errorf("encode namespace properties: %w", err)
	}
	if err := validateCString(string(encoded)); err != nil {
		return "", err
	}
	return string(encoded), nil
}

// NewDirectoryNamespace creates a native DirectoryNamespace from properties.
// A nil or empty map uses native defaults.
func NewDirectoryNamespace(properties map[string]string, provider ContextProvider) (*Namespace, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	encoded, err := namespacePropertiesJSON(properties)
	if err != nil {
		return nil, err
	}
	spec := newNamespaceContextProviderSpec(provider)
	runtime.LockOSThread()
	handle := b.namespaceDirectoryNew(encoded, 0, spec)
	runtime.KeepAlive(encoded)
	runtime.KeepAlive(spec)
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: namespace constructor returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &Namespace{b: b, handle: handle}, nil
}

// NewRestNamespace creates a native RestNamespace from properties.
func NewRestNamespace(properties map[string]string, provider ContextProvider) (*Namespace, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	encoded, err := namespacePropertiesJSON(properties)
	if err != nil {
		return nil, err
	}
	spec := newNamespaceContextProviderSpec(provider)
	runtime.LockOSThread()
	handle := b.namespaceRestNew(encoded, spec)
	runtime.KeepAlive(encoded)
	runtime.KeepAlive(spec)
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: namespace constructor returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &Namespace{b: b, handle: handle}, nil
}

// NewConnectNamespace creates a native ConnectedNamespace for implName.
func NewConnectNamespace(implName string, properties map[string]string, provider ContextProvider) (*Namespace, error) {
	if implName == "" {
		return nil, fmt.Errorf("ffi: impl_name must not be empty")
	}
	if err := validateCString(implName); err != nil {
		return nil, err
	}
	b, err := load()
	if err != nil {
		return nil, err
	}
	encoded, err := namespacePropertiesJSON(properties)
	if err != nil {
		return nil, err
	}
	spec := newNamespaceContextProviderSpec(provider)
	runtime.LockOSThread()
	handle := b.namespaceConnectNew(implName, encoded, 0, spec)
	runtime.KeepAlive(implName)
	runtime.KeepAlive(encoded)
	runtime.KeepAlive(spec)
	if handle == 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: namespace constructor returned NULL without setting an error")
	}
	runtime.UnlockOSThread()
	return &Namespace{b: b, handle: handle}, nil
}

// Close releases the native namespace. It must be called exactly once by the
// owner of the handle.
func (n *Namespace) Close() {
	if n == nil || n.handle == 0 {
		return
	}
	handle := n.handle
	n.handle = 0
	runtime.LockOSThread()
	n.b.namespaceClose(handle)
	_ = n.b.takeError()
	runtime.UnlockOSThread()
}

// NativeHandle returns the raw native handle for the internal owner boundary.
func (n *Namespace) NativeHandle() uintptr {
	if n == nil {
		return 0
	}
	return n.handle
}

// CallResult holds the two independent result channels of a namespace call.
// Both fields are copied out of native memory before the result is closed.
type CallResult struct {
	JSON  []byte
	Bytes []byte
}

// Call executes one namespace operation. Empty requestJSON is sent as "{}".
// The native call is synchronous and cannot be cancelled once entered.
func (n *Namespace) Call(operation, requestJSON string, requestBytes []byte) (*CallResult, error) {
	if n == nil || n.handle == 0 {
		return nil, ErrClosed
	}
	if operation == "" {
		return nil, fmt.Errorf("ffi: namespace operation must not be empty")
	}
	if err := validateCString(operation); err != nil {
		return nil, err
	}
	if requestJSON == "" {
		requestJSON = "{}"
	}
	if err := validateCString(requestJSON); err != nil {
		return nil, err
	}

	var result lanceNamespaceResult
	runtime.LockOSThread()
	rc := n.b.namespaceCall(n.handle, operation, requestJSON, requestBytes, uint64(len(requestBytes)), &result)
	runtime.KeepAlive(operation)
	runtime.KeepAlive(requestJSON)
	runtime.KeepAlive(requestBytes)
	nativeErr := n.b.takeError()
	namespaceErr := namespaceResultError(&result)
	jsonText := goString(result.ResultJSON)
	var copiedBytes []byte
	if result.ResultBytes != nil && result.ResultBytesLen > 0 {
		copiedBytes = append([]byte(nil), unsafe.Slice(result.ResultBytes, result.ResultBytesLen)...)
	}
	n.b.namespaceResultClose(&result)
	runtime.UnlockOSThread()

	if namespaceErr != nil {
		return nil, namespaceErr
	}
	if rc != 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: namespace operation %s failed without setting an error", operation)
	}
	if nativeErr != nil {
		return nil, nativeErr
	}
	out := &CallResult{}
	if jsonText != "" {
		out.JSON = []byte(jsonText)
	}
	out.Bytes = copiedBytes
	return out, nil
}

func namespaceResultError(result *lanceNamespaceResult) error {
	if result == nil || result.HasNamespaceError == 0 {
		return nil
	}
	return &NamespaceError{
		Code:    result.NamespaceErrorCode,
		Message: goString(result.NamespaceErrorMessage),
	}
}

// ID returns the native namespace identifier.
func (n *Namespace) ID() (string, error) {
	if n == nil || n.handle == 0 {
		return "", ErrClosed
	}
	runtime.LockOSThread()
	ptr := n.b.namespaceID(n.handle)
	nativeErr := n.b.takeError()
	if ptr == nil {
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_namespace_id returned NULL without setting an error")
	}
	id := goString(ptr)
	n.b.freeString(ptr)
	runtime.UnlockOSThread()
	return id, nil
}

// OpsMetrics returns the native operation counters as JSON. Directory and REST
// namespaces provide counters; Connected namespaces return an error.
func (n *Namespace) OpsMetrics() ([]byte, error) {
	if n == nil || n.handle == 0 {
		return nil, ErrClosed
	}
	runtime.LockOSThread()
	ptr := n.b.namespaceRetrieveOpsMetrics(n.handle)
	nativeErr := n.b.takeError()
	if ptr == nil {
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_namespace_retrieve_ops_metrics returned NULL without setting an error")
	}
	metrics := []byte(goString(ptr))
	n.b.freeString(ptr)
	runtime.UnlockOSThread()
	return metrics, nil
}

// ResetOpsMetrics resets the native operation counters.
func (n *Namespace) ResetOpsMetrics() error {
	if n == nil || n.handle == 0 {
		return ErrClosed
	}
	runtime.LockOSThread()
	rc := n.b.namespaceResetOpsMetrics(n.handle)
	nativeErr := n.b.takeError()
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_namespace_reset_ops_metrics failed without setting an error")
	}
	if nativeErr != nil {
		return nativeErr
	}
	return nil
}

func wrapNativeDataset(b *bindings, handle uintptr) *Dataset {
	ds := &Dataset{b: b}
	ds.handle.Store(handle)
	return ds
}

// OpenDataset opens a namespace-managed table. A zero version selects the
// latest version. storageOptions may be nil.
func (n *Namespace) OpenDataset(tableIDJSON string, storageOptions map[string]string, version uint64, session *Session) (*Dataset, error) {
	if n == nil || n.handle == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(tableIDJSON); err != nil {
		return nil, err
	}
	storageJSON, err := storageOptionsJSON(storageOptions)
	if err != nil {
		return nil, err
	}
	var sessionHandle uintptr
	if session != nil {
		sessionHandle = session.NativeHandle()
	}
	runtime.LockOSThread()
	handle := n.b.datasetOpenNamespace(n.handle, tableIDJSON, storageJSON, version, sessionHandle)
	runtime.KeepAlive(tableIDJSON)
	runtime.KeepAlive(storageJSON)
	nativeErr := n.b.takeError()
	runtime.UnlockOSThread()
	if handle == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_open_namespace returned NULL without setting an error")
	}
	return wrapNativeDataset(n.b, handle), nil
}

// WriteDataset writes reader through a namespace-managed destination. The
// stream is consumed on every return path after export.
func (n *Namespace) WriteDataset(tableIDJSON string, reader array.RecordReader, opts WriteOptions, storageOptions map[string]string, session *Session, branch string) (*Dataset, error) {
	if n == nil || n.handle == 0 {
		return nil, ErrClosed
	}
	if reader == nil {
		return nil, fmt.Errorf("ffi: record reader must not be nil")
	}
	if reader.Schema() == nil {
		return nil, fmt.Errorf("ffi: record reader schema must not be nil")
	}
	if opts.Mode < WriteModeCreate || opts.Mode > WriteModeOverwrite {
		return nil, fmt.Errorf("ffi: invalid write mode %d", opts.Mode)
	}
	if err := validateCString(tableIDJSON); err != nil {
		return nil, err
	}
	var branchBuf []byte
	var branchPtr *byte
	if branch != "" {
		var err error
		branchBuf, branchPtr, err = newCStringPtr(branch)
		if err != nil {
			return nil, err
		}
	}
	storageJSON, err := storageOptionsJSON(storageOptions)
	if err != nil {
		return nil, err
	}
	rawOptions, versionList, err := buildWriteParameters(opts)
	if err != nil {
		return nil, err
	}
	var rawSchema cdata.ArrowSchema
	if err := cdata.ExportSchema(reader.Schema(), &rawSchema); err != nil {
		return nil, err
	}
	defer cdata.ReleaseSchema(&rawSchema)
	var rawStream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &rawStream); err != nil {
		return nil, err
	}
	streamConsumed := false
	defer func() {
		if !streamConsumed {
			cdata.ReleaseStream(&rawStream)
		}
	}()

	var sessionHandle uintptr
	if session != nil {
		sessionHandle = session.NativeHandle()
	}
	var out uintptr
	runtime.LockOSThread()
	rc := n.b.datasetWriteNamespace(
		n.handle,
		tableIDJSON,
		&rawSchema,
		&rawStream,
		int32(opts.Mode),
		&rawOptions.Params,
		&rawOptions,
		storageJSON,
		sessionHandle,
		branchPtr,
		&out,
	)
	streamConsumed = true
	runtime.KeepAlive(tableIDJSON)
	runtime.KeepAlive(reader)
	versionList.keepAlive()
	runtime.KeepAlive(&rawOptions)
	runtime.KeepAlive(storageJSON)
	runtime.KeepAlive(branchBuf)
	nativeErr := n.b.takeError()
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_write_namespace failed without setting an error")
	}
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_dataset_write_namespace returned NULL dataset")
	}
	return wrapNativeDataset(n.b, out), nil
}

// CommitNamespaceURI commits tx to a namespace-managed publication URI.
func (n *Namespace) CommitNamespaceURI(tableIDJSON, uri string, tx *Transaction, opts CommitOptions, session *Session) (*Dataset, error) {
	if n == nil || n.handle == 0 {
		return nil, ErrClosed
	}
	if tx == nil {
		return nil, fmt.Errorf("ffi: transaction must not be nil")
	}
	txHandle := tx.NativeHandle()
	if txHandle == 0 {
		return nil, ErrClosed
	}
	if uri == "" {
		return nil, fmt.Errorf("ffi: uri must not be empty")
	}
	if err := validateCString(tableIDJSON); err != nil {
		return nil, err
	}
	if err := validateCString(uri); err != nil {
		return nil, err
	}
	raw, storageList, versionList, err := buildCommitOptions(opts)
	if err != nil {
		return nil, err
	}
	var sessionHandle uintptr
	if session != nil {
		sessionHandle = session.NativeHandle()
	}
	var out uintptr
	runtime.LockOSThread()
	rc := n.b.transactionCommitNamespaceURI(
		n.handle,
		tableIDJSON,
		uri,
		txHandle,
		raw,
		storageList.ptrs,
		sessionHandle,
		&out,
	)
	storageList.keepAlive()
	versionList.keepAlive()
	runtime.KeepAlive(tableIDJSON)
	runtime.KeepAlive(uri)
	runtime.KeepAlive(raw)
	nativeErr := n.b.takeError()
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_transaction_commit_namespace_uri failed without setting an error")
	}
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_transaction_commit_namespace_uri returned NULL dataset")
	}
	return wrapNativeDataset(n.b, out), nil
}

// StartRESTAdapter starts a native REST adapter in front of ns. host may be
// empty to use the native default; port zero selects an ephemeral port.
func (n *Namespace) StartRESTAdapter(host string, port uint16) (*RESTAdapter, error) {
	if n == nil || n.handle == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(host); err != nil {
		return nil, err
	}
	var out uintptr
	runtime.LockOSThread()
	rc := n.b.restAdapterStart(n.handle, host, port, &out)
	runtime.KeepAlive(host)
	nativeErr := n.b.takeError()
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_rest_adapter_start failed without setting an error")
	}
	if out == 0 {
		return nil, fmt.Errorf("ffi: lance_rest_adapter_start returned NULL adapter")
	}
	return &RESTAdapter{b: n.b, handle: out}, nil
}

// Port returns the bound adapter port, or zero when the adapter is stopped.
func (a *RESTAdapter) Port() (uint16, error) {
	if a == nil || a.handle == 0 {
		return 0, ErrClosed
	}
	runtime.LockOSThread()
	port := a.b.restAdapterPort(a.handle)
	nativeErr := a.b.takeError()
	runtime.UnlockOSThread()
	if port == 0 {
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: lance_rest_adapter_port is zero because the adapter is not running")
	}
	if nativeErr != nil {
		return 0, nativeErr
	}
	return port, nil
}

// Shutdown stops serving. It is idempotent.
func (a *RESTAdapter) Shutdown() {
	if a == nil || a.handle == 0 {
		return
	}
	runtime.LockOSThread()
	a.b.restAdapterShutdown(a.handle)
	_ = a.b.takeError()
	runtime.UnlockOSThread()
}

// Close stops the adapter when it is still serving and releases the native
// handle. It is safe after Shutdown and is idempotent at the owner boundary.
func (a *RESTAdapter) Close() {
	if a == nil || a.handle == 0 {
		return
	}
	handle := a.handle
	a.handle = 0
	runtime.LockOSThread()
	a.b.restAdapterClose(handle)
	_ = a.b.takeError()
	runtime.UnlockOSThread()
}

// NativeHandle returns the raw native handle for the internal owner boundary.
func (a *RESTAdapter) NativeHandle() uintptr {
	if a == nil {
		return 0
	}
	return a.handle
}
