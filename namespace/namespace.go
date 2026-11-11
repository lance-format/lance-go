// Package namespace provides typed access to Lance namespaces through the
// native lance C API.
//
// Namespace and REST adapter objects own independent native handles. Closing a
// namespace does not close datasets or adapters that were already acquired from
// it, and closing an object waits for in-flight native calls.
//
// Every operation checks ctx before acquiring the native handle. The native
// call itself is synchronous and cannot be cancelled once it has started.
// A nil request sends an empty JSON object. Operations that return data also
// expose the raw Arrow IPC payload when the native protocol carries one.
package namespace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	_ "github.com/lance-format/lance-go/internal/bundled"
	"github.com/lance-format/lance-go/internal/ffi"
	"github.com/lance-format/lance-go/internal/nshandle"
)

// Namespace is a sealed native Lance namespace. Use NewDirectory, NewRest, or
// NewConnect to construct one.
type Namespace interface {
	// nativeNamespace seals the interface to this package's native adapter.
	nativeNamespace()

	// ID returns the native namespace identifier.
	ID() (string, error)
	// OpsMetrics returns native operation metrics. Directory and REST
	// namespaces support metrics; other implementations may return a native
	// unsupported error.
	OpsMetrics() (map[string]uint64, error)
	// ResetOpsMetrics resets native operation metrics.
	ResetOpsMetrics() error
	// Close releases the native namespace. It waits for in-flight calls and is
	// idempotent.
	Close() error
	// StartRESTAdapter starts a native REST adapter over this namespace. The
	// adapter owns an independent native lifetime.
	StartRESTAdapter(host string, port uint16) (*RESTAdapter, error)

	ListNamespaces(ctx context.Context, request *ListNamespacesRequest) (*ListNamespacesResponse, error)
	DescribeNamespace(ctx context.Context, request *DescribeNamespaceRequest) (*DescribeNamespaceResponse, error)
	CreateNamespace(ctx context.Context, request *CreateNamespaceRequest) (*CreateNamespaceResponse, error)
	DropNamespace(ctx context.Context, request *DropNamespaceRequest) (*DropNamespaceResponse, error)
	NamespaceExists(ctx context.Context, request *NamespaceExistsRequest) error
	ListTables(ctx context.Context, request *ListTablesRequest) (*ListTablesResponse, error)
	DescribeTable(ctx context.Context, request *DescribeTableRequest) (*DescribeTableResponse, error)
	RegisterTable(ctx context.Context, request *RegisterTableRequest) (*RegisterTableResponse, error)
	TableExists(ctx context.Context, request *TableExistsRequest) error
	DropTable(ctx context.Context, request *DropTableRequest) (*DropTableResponse, error)
	DeregisterTable(ctx context.Context, request *DeregisterTableRequest) (*DeregisterTableResponse, error)
	CountTableRows(ctx context.Context, request *CountTableRowsRequest) (int64, error)
	CreateTable(ctx context.Context, request *CreateTableRequest, payload []byte) (*CreateTableResponse, error)
	DeclareTable(ctx context.Context, request *DeclareTableRequest) (*DeclareTableResponse, error)
	InsertIntoTable(ctx context.Context, request *InsertIntoTableRequest, payload []byte) (*InsertIntoTableResponse, error)
	MergeInsertIntoTable(ctx context.Context, request *MergeInsertIntoTableRequest, payload []byte) (*MergeInsertIntoTableResponse, error)
	UpdateTable(ctx context.Context, request *UpdateTableRequest) (*UpdateTableResponse, error)
	DeleteFromTable(ctx context.Context, request *DeleteFromTableRequest) (*DeleteFromTableResponse, error)
	QueryTable(ctx context.Context, request *QueryTableRequest) (*QueryTableResponse, error)
	CreateTableIndex(ctx context.Context, request *CreateTableIndexRequest) (*CreateTableIndexResponse, error)
	ListTableIndices(ctx context.Context, request *ListTableIndicesRequest) (*ListTableIndicesResponse, error)
	DescribeTableIndexStats(ctx context.Context, request *DescribeTableIndexStatsRequest) (*DescribeTableIndexStatsResponse, error)
	DescribeTransaction(ctx context.Context, request *DescribeTransactionRequest) (*DescribeTransactionResponse, error)
	AlterTransaction(ctx context.Context, request *AlterTransactionRequest) (*AlterTransactionResponse, error)
	CreateTableScalarIndex(ctx context.Context, request *CreateTableIndexRequest) (*CreateTableScalarIndexResponse, error)
	DropTableIndex(ctx context.Context, request *DropTableIndexRequest) (*DropTableIndexResponse, error)
	ListAllTables(ctx context.Context, request *ListTablesRequest) (*ListTablesResponse, error)
	RestoreTable(ctx context.Context, request *RestoreTableRequest) (*RestoreTableResponse, error)
	RenameTable(ctx context.Context, request *RenameTableRequest) (*RenameTableResponse, error)
	ListTableVersions(ctx context.Context, request *ListTableVersionsRequest) (*ListTableVersionsResponse, error)
	CreateTableVersion(ctx context.Context, request *CreateTableVersionRequest) (*CreateTableVersionResponse, error)
	DescribeTableVersion(ctx context.Context, request *DescribeTableVersionRequest) (*DescribeTableVersionResponse, error)
	BatchDeleteTableVersions(ctx context.Context, request *BatchDeleteTableVersionsRequest) (*BatchDeleteTableVersionsResponse, error)
	BatchCreateTableVersions(ctx context.Context, request *BatchCreateTableVersionsRequest) (*BatchCreateTableVersionsResponse, error)
	BatchCommitTables(ctx context.Context, request *BatchCommitTablesRequest) (*BatchCommitTablesResponse, error)
	UpdateTableSchemaMetadata(ctx context.Context, request *UpdateTableSchemaMetadataRequest) (*UpdateTableSchemaMetadataResponse, error)
	UpdateFieldMetadata(ctx context.Context, request *UpdateFieldMetadataRequest) (*UpdateFieldMetadataResponse, error)
	GetTableStats(ctx context.Context, request *GetTableStatsRequest) (*GetTableStatsResponse, error)
	ExplainTableQueryPlan(ctx context.Context, request *ExplainTableQueryPlanRequest) (string, error)
	AnalyzeTableQueryPlan(ctx context.Context, request *AnalyzeTableQueryPlanRequest) (string, error)
	AlterTableAddColumns(ctx context.Context, request *AlterTableAddColumnsRequest) (*AlterTableAddColumnsResponse, error)
	AlterTableAlterColumns(ctx context.Context, request *AlterTableAlterColumnsRequest) (*AlterTableAlterColumnsResponse, error)
	AlterTableDropColumns(ctx context.Context, request *AlterTableDropColumnsRequest) (*AlterTableDropColumnsResponse, error)
	AlterTableBackfillColumns(ctx context.Context, request *AlterTableBackfillColumnsRequest) (*AlterTableBackfillColumnsResponse, error)
	RefreshMaterializedView(ctx context.Context, request *RefreshMaterializedViewRequest) (*RefreshMaterializedViewResponse, error)
	CreateMaterializedView(ctx context.Context, request *CreateMaterializedViewRequest) (*CreateMaterializedViewResponse, error)
	ListTableTags(ctx context.Context, request *ListTableTagsRequest) (*ListTableTagsResponse, error)
	GetTableTagVersion(ctx context.Context, request *GetTableTagVersionRequest) (*GetTableTagVersionResponse, error)
	CreateTableTag(ctx context.Context, request *CreateTableTagRequest) (*CreateTableTagResponse, error)
	DeleteTableTag(ctx context.Context, request *DeleteTableTagRequest) (*DeleteTableTagResponse, error)
	UpdateTableTag(ctx context.Context, request *UpdateTableTagRequest) (*UpdateTableTagResponse, error)
	CreateTableBranch(ctx context.Context, request *CreateTableBranchRequest) (*CreateTableBranchResponse, error)
	ListTableBranches(ctx context.Context, request *ListTableBranchesRequest) (*ListTableBranchesResponse, error)
	DeleteTableBranch(ctx context.Context, request *DeleteTableBranchRequest) (*DeleteTableBranchResponse, error)
}

// nativeNamespace is the only Namespace implementation in this package.
type nativeNamespace struct {
	owner *nshandle.Owner
}

func (*nativeNamespace) nativeNamespace() {}

// Option configures a namespace constructor.
type Option func(*config) error

type config struct {
	provider DynamicContextProvider
}

// DynamicContextProvider supplies request context for a native namespace
// operation. The native implementation calls it synchronously before routing
// the operation, possibly from multiple namespace operations concurrently. A
// provider error is logged by native code and the request continues with an
// empty context. A provider must not call Close on the namespace whose
// operation invoked it.
type DynamicContextProvider interface {
	ProvideContext(operation, objectID string) (map[string]string, error)
}

// WithDynamicContextProvider installs a provider for dynamic request context.
// A nil provider is rejected.
func WithDynamicContextProvider(provider DynamicContextProvider) Option {
	return func(cfg *config) error {
		if provider == nil {
			return errors.New("namespace: dynamic context provider must not be nil")
		}
		value := reflect.ValueOf(provider)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
			if value.IsNil() {
				return errors.New("namespace: dynamic context provider must not be nil")
			}
		}
		cfg.provider = provider
		return nil
	}
}

func applyOptions(opts []Option) (config, error) {
	var cfg config
	for _, opt := range opts {
		if opt == nil {
			return config{}, errors.New("namespace: nil option")
		}
		if err := opt(&cfg); err != nil {
			return config{}, err
		}
	}
	return cfg, nil
}

func providerOf(cfg config) ffi.ContextProvider {
	if cfg.provider == nil {
		return nil
	}
	return cfg.provider
}

// NewDirectory creates a directory-backed namespace.
func NewDirectory(properties map[string]string, opts ...Option) (Namespace, error) {
	cfg, err := applyOptions(opts)
	if err != nil {
		return nil, err
	}
	native, err := ffi.NewDirectoryNamespace(properties, providerOf(cfg))
	if err != nil {
		return nil, err
	}
	return &nativeNamespace{owner: nshandle.New(native)}, nil
}

// NewRest creates a namespace backed by a REST service.
func NewRest(properties map[string]string, opts ...Option) (Namespace, error) {
	cfg, err := applyOptions(opts)
	if err != nil {
		return nil, err
	}
	native, err := ffi.NewRestNamespace(properties, providerOf(cfg))
	if err != nil {
		return nil, err
	}
	return &nativeNamespace{owner: nshandle.New(native)}, nil
}

// NewConnect creates a namespace implemented by a native Lance Connect plugin.
func NewConnect(implName string, properties map[string]string, opts ...Option) (Namespace, error) {
	cfg, err := applyOptions(opts)
	if err != nil {
		return nil, err
	}
	native, err := ffi.NewConnectNamespace(implName, properties, providerOf(cfg))
	if err != nil {
		return nil, err
	}
	return &nativeNamespace{owner: nshandle.New(native)}, nil
}

// ID returns the native namespace identifier.
func (n *nativeNamespace) ID() (string, error) {
	if n == nil || n.owner == nil {
		return "", ErrClosed
	}
	lease, ok := n.owner.Acquire()
	if !ok {
		return "", ErrClosed
	}
	defer lease.Release()
	id, err := lease.ID()
	if err != nil {
		return "", wrapNativeError("id", err)
	}
	return id, nil
}

// OpsMetrics returns native operation metrics.
func (n *nativeNamespace) OpsMetrics() (map[string]uint64, error) {
	if n == nil || n.owner == nil {
		return nil, ErrClosed
	}
	lease, ok := n.owner.Acquire()
	if !ok {
		return nil, ErrClosed
	}
	defer lease.Release()
	raw, err := lease.OpsMetrics()
	if err != nil {
		return nil, wrapNativeError("ops_metrics", err)
	}
	var metrics map[string]uint64
	if err := json.Unmarshal(raw, &metrics); err != nil {
		return nil, &ProtocolError{Operation: "ops_metrics", Err: err}
	}
	return metrics, nil
}

// ResetOpsMetrics resets native operation metrics.
func (n *nativeNamespace) ResetOpsMetrics() error {
	if n == nil || n.owner == nil {
		return ErrClosed
	}
	lease, ok := n.owner.Acquire()
	if !ok {
		return ErrClosed
	}
	defer lease.Release()
	if err := lease.ResetOpsMetrics(); err != nil {
		return wrapNativeError("reset_ops_metrics", err)
	}
	return nil
}

// Close releases the native namespace and waits for in-flight calls.
func (n *nativeNamespace) Close() error {
	if n == nil || n.owner == nil {
		return nil
	}
	n.owner.Close()
	return nil
}

// RESTAdapter serves a namespace over the native REST adapter. It owns an
// independent native handle, so closing the namespace does not close it.
type RESTAdapter struct {
	owner *nshandle.RESTAdapter
}

// StartRESTAdapter starts a native REST adapter. An empty host uses the native
// default; port zero binds an ephemeral port.
func (n *nativeNamespace) StartRESTAdapter(host string, port uint16) (*RESTAdapter, error) {
	lease, err := n.acquire(context.Background(), "rest_adapter_start")
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	native, err := lease.StartRESTAdapter(host, port)
	if err != nil {
		return nil, wrapNativeError("rest_adapter_start", err)
	}
	return &RESTAdapter{owner: native}, nil
}

// Port returns the bound adapter port.
func (a *RESTAdapter) Port() (uint16, error) {
	if a == nil || a.owner == nil {
		return 0, ErrClosed
	}
	port, err := a.owner.Port()
	if err != nil {
		return 0, wrapNativeError("rest_adapter_port", err)
	}
	return port, nil
}

// Shutdown stops serving and is idempotent.
func (a *RESTAdapter) Shutdown() {
	if a == nil || a.owner == nil {
		return
	}
	a.owner.Shutdown()
}

// Close waits for in-flight calls and releases the native adapter.
func (a *RESTAdapter) Close() error {
	if a == nil || a.owner == nil {
		return nil
	}
	a.owner.Close()
	return nil
}

// AcquireNative is an internal bridge used by the root package.
func (n *nativeNamespace) AcquireNative() (*nshandle.Lease, bool) {
	if n == nil || n.owner == nil {
		return nil, false
	}
	return n.owner.Acquire()
}

func (n *nativeNamespace) acquire(ctx context.Context, operation string) (*nshandle.Lease, error) {
	if n == nil || n.owner == nil {
		return nil, fmt.Errorf("namespace %s: %w", operation, ErrClosed)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("namespace %s: %w", operation, err)
	}
	lease, ok := n.owner.Acquire()
	if !ok {
		return nil, fmt.Errorf("namespace %s: %w", operation, ErrClosed)
	}
	return lease, nil
}

func (n *nativeNamespace) do(ctx context.Context, operation string, request any, payload []byte, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	requestJSON, err := encodeRequest(request)
	if err != nil {
		return &ProtocolError{Operation: operation, Err: err}
	}
	lease, err := n.acquire(ctx, operation)
	if err != nil {
		return err
	}
	defer lease.Release()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("namespace %s: %w", operation, err)
	}
	result, err := lease.Call(operation, requestJSON, payload)
	if err != nil {
		return wrapNativeError(operation, err)
	}
	if out == nil {
		return nil
	}
	if len(result.JSON) == 0 {
		return &ProtocolError{Operation: operation, Err: errors.New("native namespace returned an empty JSON response")}
	}
	if err := json.Unmarshal(result.JSON, out); err != nil {
		return &ProtocolError{Operation: operation, Err: err}
	}
	return nil
}

func requestError(operation string, err error) error {
	return &ProtocolError{Operation: operation, Err: err}
}

func encodeRequest(request any) (string, error) {
	if request == nil {
		return "{}", nil
	}
	value := reflect.ValueOf(request)
	if value.Kind() == reflect.Ptr && value.IsNil() {
		return "{}", nil
	}
	data, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
