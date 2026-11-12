//go:build linux || darwin || windows

package lance_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/loader"
	"github.com/lance-format/lance-go/namespace"
)

type customClient struct {
	mu                   sync.Mutex
	calls                []string
	describeTable        func(*namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error)
	listTableVersions    func(*namespace.ListTableVersionsRequest) (*namespace.ListTableVersionsResponse, error)
	createTableVersion   func(*namespace.CreateTableVersionRequest) (*namespace.CreateTableVersionResponse, error)
	describeTableVersion func(*namespace.DescribeTableVersionRequest) (*namespace.DescribeTableVersionResponse, error)
}

func (c *customClient) record(operation string) {
	c.mu.Lock()
	c.calls = append(c.calls, operation)
	c.mu.Unlock()
}

func (c *customClient) recordedCalls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

func (c *customClient) DescribeTable(request *namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error) {
	c.record("describe_table")
	if c.describeTable == nil {
		return &namespace.DescribeTableResponse{}, nil
	}
	return c.describeTable(request)
}

func (c *customClient) ListTableVersions(request *namespace.ListTableVersionsRequest) (*namespace.ListTableVersionsResponse, error) {
	c.record("list_table_versions")
	if c.listTableVersions == nil {
		return &namespace.ListTableVersionsResponse{}, nil
	}
	return c.listTableVersions(request)
}

func (c *customClient) CreateTableVersion(request *namespace.CreateTableVersionRequest) (*namespace.CreateTableVersionResponse, error) {
	c.record("create_table_version")
	if c.createTableVersion == nil {
		return &namespace.CreateTableVersionResponse{}, nil
	}
	return c.createTableVersion(request)
}

func (c *customClient) DescribeTableVersion(request *namespace.DescribeTableVersionRequest) (*namespace.DescribeTableVersionResponse, error) {
	c.record("describe_table_version")
	if c.describeTableVersion == nil {
		return &namespace.DescribeTableVersionResponse{}, nil
	}
	return c.describeTableVersion(request)
}

func newCustomNamespace(t *testing.T, client namespace.CustomClient, namespaceID string) namespace.Namespace {
	t.Helper()
	ns, err := namespace.NewCustom(client, namespaceID)
	if errors.Is(err, loader.ErrNotFound) {
		t.Skip("lance-c is not available; set LANCE_LIB_PATH to a built liblance_c")
	}
	if err != nil {
		t.Fatalf("new custom namespace: %v", err)
	}
	t.Cleanup(func() { _ = ns.Close() })
	return ns
}

func customTableID(t *testing.T, value *[]string, want []string) error {
	t.Helper()
	if value == nil || len(*value) != len(want) {
		return fmt.Errorf("table id = %v, want %v", value, want)
	}
	for i := range want {
		if (*value)[i] != want[i] {
			return fmt.Errorf("table id = %v, want %v", *value, want)
		}
	}
	return nil
}

func TestNativeCustomNamespaceDispatchesFourOperations(t *testing.T) {
	ctx := context.Background()
	tableID := []string{"custom_events"}
	client := &customClient{}
	client.describeTable = func(request *namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error) {
		if err := customTableID(t, request.ID, tableID); err != nil {
			return nil, err
		}
		return &namespace.DescribeTableResponse{
			Table:    ptr("custom_events"),
			Version:  ptr(int64(7)),
			Location: ptr("s3://bucket/custom_events"),
		}, nil
	}
	client.listTableVersions = func(request *namespace.ListTableVersionsRequest) (*namespace.ListTableVersionsResponse, error) {
		if err := customTableID(t, request.ID, tableID); err != nil {
			return nil, err
		}
		if request.Limit == nil || *request.Limit != 3 {
			return nil, fmt.Errorf("limit = %v, want 3", request.Limit)
		}
		return &namespace.ListTableVersionsResponse{
			Versions:  []namespace.TableVersion{{Version: 7, ManifestPath: "manifest-7"}},
			PageToken: ptr("next-page"),
		}, nil
	}
	client.createTableVersion = func(request *namespace.CreateTableVersionRequest) (*namespace.CreateTableVersionResponse, error) {
		if err := customTableID(t, request.ID, tableID); err != nil {
			return nil, err
		}
		if request.Version != 8 || request.ManifestPath != "manifest-8" {
			return nil, fmt.Errorf("create version request = %+v", request)
		}
		return &namespace.CreateTableVersionResponse{Version: &namespace.TableVersion{Version: 8, ManifestPath: "manifest-8"}}, nil
	}
	client.describeTableVersion = func(request *namespace.DescribeTableVersionRequest) (*namespace.DescribeTableVersionResponse, error) {
		if err := customTableID(t, request.ID, tableID); err != nil {
			return nil, err
		}
		if request.Version == nil || *request.Version != 7 {
			return nil, fmt.Errorf("version = %v, want 7", request.Version)
		}
		return &namespace.DescribeTableVersionResponse{Version: namespace.TableVersion{Version: 7, ManifestPath: "manifest-7"}}, nil
	}

	ns := newCustomNamespace(t, client, "custom-dispatch")
	runtime.GC()

	description, err := ns.DescribeTable(ctx, &namespace.DescribeTableRequest{ID: &tableID, Version: ptr(int64(7))})
	if err != nil {
		t.Fatalf("DescribeTable: %v", err)
	}
	if description.Version == nil || *description.Version != 7 || description.Location == nil || *description.Location != "s3://bucket/custom_events" {
		t.Fatalf("DescribeTable response = %+v", description)
	}

	versions, err := ns.ListTableVersions(ctx, &namespace.ListTableVersionsRequest{ID: &tableID, Limit: ptr(int32(3))})
	if err != nil {
		t.Fatalf("ListTableVersions: %v", err)
	}
	if len(versions.Versions) != 1 || versions.Versions[0].Version != 7 || versions.PageToken == nil || *versions.PageToken != "next-page" {
		t.Fatalf("ListTableVersions response = %+v", versions)
	}

	created, err := ns.CreateTableVersion(ctx, &namespace.CreateTableVersionRequest{ID: &tableID, Version: 8, ManifestPath: "manifest-8"})
	if err != nil {
		t.Fatalf("CreateTableVersion: %v", err)
	}
	if created.Version == nil || created.Version.Version != 8 {
		t.Fatalf("CreateTableVersion response = %+v", created)
	}

	version, err := ns.DescribeTableVersion(ctx, &namespace.DescribeTableVersionRequest{ID: &tableID, Version: ptr(int64(7))})
	if err != nil {
		t.Fatalf("DescribeTableVersion: %v", err)
	}
	if version.Version.Version != 7 || version.Version.ManifestPath != "manifest-7" {
		t.Fatalf("DescribeTableVersion response = %+v", version)
	}

	calls := client.recordedCalls()
	wantCalls := []string{"describe_table", "list_table_versions", "create_table_version", "describe_table_version"}
	if len(calls) != len(wantCalls) {
		t.Fatalf("client calls = %v, want %v", calls, wantCalls)
	}
	for i := range wantCalls {
		if calls[i] != wantCalls[i] {
			t.Fatalf("client calls = %v, want %v", calls, wantCalls)
		}
	}

	var nativeErr *namespace.NativeError
	_, err = ns.ListNamespaces(ctx, &namespace.ListNamespacesRequest{})
	if !errors.As(err, &nativeErr) || nativeErr.Code != int32(lance.CodeNotSupported) {
		t.Fatalf("ListNamespaces = %v, want native CodeNotSupported", err)
	}
	if got := len(client.recordedCalls()); got != len(wantCalls) {
		t.Fatalf("unsupported operation reached the client: %v", client.recordedCalls())
	}
}

func TestNativeCustomNamespaceIdentity(t *testing.T) {
	var typedNil *customClient
	if _, err := namespace.NewCustom(typedNil, ""); err == nil {
		t.Fatal("NewCustom accepted a typed nil client")
	}

	implicit := newCustomNamespace(t, &customClient{}, "")
	id1, err := implicit.ID()
	if err != nil {
		t.Fatalf("ID: %v", err)
	}
	id2, err := implicit.ID()
	if err != nil {
		t.Fatalf("second ID: %v", err)
	}
	if id1 == "" || id1 != id2 {
		t.Fatalf("implicit identity = %q then %q, want a non-empty stable value", id1, id2)
	}

	other := newCustomNamespace(t, &customClient{}, "")
	otherID, err := other.ID()
	if err != nil {
		t.Fatalf("other ID: %v", err)
	}
	if otherID == id1 {
		t.Fatalf("two implicit namespaces share identity %q", id1)
	}

	explicit := newCustomNamespace(t, &customClient{}, "custom-explicit")
	explicitID, err := explicit.ID()
	if err != nil {
		t.Fatalf("explicit ID: %v", err)
	}
	if explicitID != "custom-explicit" {
		t.Fatalf("explicit identity = %q, want custom-explicit", explicitID)
	}
}

func TestNativeCustomNamespaceTypedErrors(t *testing.T) {
	ctx := context.Background()
	tableID := []string{"custom_events"}
	cases := []struct {
		name     string
		response *namespace.DescribeTableResponse
		err      error
		code     namespace.NamespaceErrorCode
		message  string
	}{
		{
			name:     "not found",
			response: &namespace.DescribeTableResponse{Location: ptr("must-not-be-used")},
			err:      &namespace.CustomClientError{Code: namespace.CustomClientErrorNotFound, Message: "missing table"},
			code:     namespace.ErrorCodeTableNotFound,
			message:  "missing table",
		},
		{
			name:    "wrapped conflict",
			err:     fmt.Errorf("remote: %w", &namespace.CustomClientError{Code: namespace.CustomClientErrorConflict, Message: "version changed"}),
			code:    namespace.ErrorCodeConcurrentModification,
			message: "version changed",
		},
		{
			name:    "already exists",
			err:     &namespace.CustomClientError{Code: namespace.CustomClientErrorAlreadyExists, Message: "table exists"},
			code:    namespace.ErrorCodeTableAlreadyExists,
			message: "table exists",
		},
		{
			name:    "invalid input",
			err:     &namespace.CustomClientError{Code: namespace.CustomClientErrorInvalidInput, Message: "bad request"},
			code:    namespace.ErrorCodeInvalidInput,
			message: "bad request",
		},
		{
			name:    "unsupported",
			err:     &namespace.CustomClientError{Code: namespace.CustomClientErrorUnsupported, Message: "not implemented"},
			code:    namespace.ErrorCodeUnsupported,
			message: "not implemented",
		},
		{
			name:    "io becomes internal",
			err:     &namespace.CustomClientError{Code: namespace.CustomClientErrorIO, Message: "backend unavailable"},
			code:    namespace.ErrorCodeInternal,
			message: "backend unavailable",
		},
		{
			name:    "generic error",
			err:     errors.New("backend exploded"),
			code:    namespace.ErrorCodeInternal,
			message: "backend exploded",
		},
		{
			name:    "nil response",
			code:    namespace.ErrorCodeInternal,
			message: "nil response",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client := &customClient{describeTable: func(*namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error) {
				return test.response, test.err
			}}
			ns := newCustomNamespace(t, client, "custom-errors")
			_, err := ns.DescribeTable(ctx, &namespace.DescribeTableRequest{ID: &tableID})
			var namespaceErr *namespace.NamespaceError
			if !errors.As(err, &namespaceErr) {
				t.Fatalf("error type = %T (%v), want *namespace.NamespaceError", err, err)
			}
			if namespaceErr.Code != test.code {
				t.Fatalf("code = %d, want %d (message %q)", namespaceErr.Code, test.code, namespaceErr.Message)
			}
			if !strings.Contains(namespaceErr.Message, test.message) {
				t.Fatalf("message = %q, want substring %q", namespaceErr.Message, test.message)
			}
		})
	}
}

func TestNativeCustomNamespacePanicBecomesInternalError(t *testing.T) {
	client := &customClient{describeTable: func(*namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error) {
		panic("custom backend panic")
	}}
	ns := newCustomNamespace(t, client, "custom-panic")
	tableID := []string{"custom_events"}
	_, err := ns.DescribeTable(context.Background(), &namespace.DescribeTableRequest{ID: &tableID})
	var namespaceErr *namespace.NamespaceError
	if !errors.As(err, &namespaceErr) || namespaceErr.Code != namespace.ErrorCodeInternal {
		t.Fatalf("DescribeTable = %v, want Internal", err)
	}
	if !strings.Contains(namespaceErr.Message, "custom backend panic") {
		t.Fatalf("message = %q, want panic text", namespaceErr.Message)
	}
}

func TestNativeCustomNamespaceSurvivesGC(t *testing.T) {
	client := &customClient{describeTable: func(*namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error) {
		response := &namespace.DescribeTableResponse{Location: ptr("s3://bucket/after-gc")}
		runtime.GC()
		return response, nil
	}}
	ns := newCustomNamespace(t, client, "custom-gc")
	runtime.GC()
	tableID := []string{"custom_events"}
	response, err := ns.DescribeTable(context.Background(), &namespace.DescribeTableRequest{ID: &tableID})
	if err != nil {
		t.Fatalf("DescribeTable: %v", err)
	}
	if response.Location == nil || *response.Location != "s3://bucket/after-gc" {
		t.Fatalf("response = %+v", response)
	}
}

func TestNativeCustomNamespaceConcurrentCalls(t *testing.T) {
	client := &customClient{describeTable: func(request *namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error) {
		if request.ID == nil || len(*request.ID) != 1 {
			return nil, fmt.Errorf("id = %v", request.ID)
		}
		location := "s3://bucket/" + (*request.ID)[0]
		return &namespace.DescribeTableResponse{Location: &location}, nil
	}}
	ns := newCustomNamespace(t, client, "custom-concurrent")

	const workers = 8
	var wait sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		tableID := []string{fmt.Sprintf("table_%d", i)}
		wait.Add(1)
		go func() {
			defer wait.Done()
			response, err := ns.DescribeTable(context.Background(), &namespace.DescribeTableRequest{ID: &tableID})
			if err != nil {
				errs <- err
				return
			}
			if response.Location == nil || *response.Location != "s3://bucket/"+tableID[0] {
				errs <- fmt.Errorf("location = %v, want table %q", response.Location, tableID[0])
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestNativeCustomNamespaceCloseWaitsForCallback(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()

	client := &customClient{describeTable: func(*namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error) {
		enteredOnce.Do(func() { close(entered) })
		<-release
		return &namespace.DescribeTableResponse{Location: ptr("s3://bucket/blocked")}, nil
	}}
	ns := newCustomNamespace(t, client, "custom-close")
	tableID := []string{"custom_events"}

	callDone := make(chan error, 1)
	go func() {
		_, err := ns.DescribeTable(context.Background(), &namespace.DescribeTableRequest{ID: &tableID})
		callDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("client callback did not start")
	}

	closeStarted := make(chan struct{})
	closeDone := make(chan error, 1)
	go func() {
		close(closeStarted)
		closeDone <- ns.Close()
	}()
	<-closeStarted

	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := ns.ID()
		if errors.Is(err, namespace.ErrClosed) {
			break
		}
		if err != nil {
			t.Fatalf("ID while closing = %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("Close did not mark the namespace closed")
		}
		runtime.Gosched()
	}
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before the in-flight callback finished: %v", err)
	default:
	}

	unblock()
	if err := <-callDone; err != nil {
		t.Fatalf("in-flight DescribeTable: %v", err)
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not finish after the in-flight callback returned")
	}
}

type directoryCustomClient struct {
	ns    namespace.Namespace
	mu    sync.Mutex
	calls []string
}

func (c *directoryCustomClient) record(operation string) {
	c.mu.Lock()
	c.calls = append(c.calls, operation)
	c.mu.Unlock()
}

func (c *directoryCustomClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *directoryCustomClient) DescribeTable(request *namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error) {
	c.record("describe_table")
	return c.ns.DescribeTable(context.Background(), request)
}

func (c *directoryCustomClient) ListTableVersions(request *namespace.ListTableVersionsRequest) (*namespace.ListTableVersionsResponse, error) {
	c.record("list_table_versions")
	return c.ns.ListTableVersions(context.Background(), request)
}

func (c *directoryCustomClient) CreateTableVersion(request *namespace.CreateTableVersionRequest) (*namespace.CreateTableVersionResponse, error) {
	c.record("create_table_version")
	return c.ns.CreateTableVersion(context.Background(), request)
}

func (c *directoryCustomClient) DescribeTableVersion(request *namespace.DescribeTableVersionRequest) (*namespace.DescribeTableVersionResponse, error) {
	c.record("describe_table_version")
	return c.ns.DescribeTableVersion(context.Background(), request)
}

func TestNativeCustomNamespaceRESTAdapterOutlivesOwner(t *testing.T) {
	ctx := context.Background()
	tableID := []string{"custom_events"}
	client := &customClient{describeTable: func(request *namespace.DescribeTableRequest) (*namespace.DescribeTableResponse, error) {
		if err := customTableID(t, request.ID, tableID); err != nil {
			return nil, err
		}
		return &namespace.DescribeTableResponse{
			Table:    ptr("custom_events"),
			Location: ptr("s3://bucket/custom_events"),
		}, nil
	}}
	ns := newCustomNamespace(t, client, "custom-rest")
	adapter, err := ns.StartRESTAdapter("127.0.0.1", 0)
	if err != nil {
		t.Fatalf("StartRESTAdapter: %v", err)
	}
	defer adapter.Close()
	port, err := adapter.Port()
	if err != nil {
		t.Fatalf("Port: %v", err)
	}
	if port == 0 {
		t.Fatal("Port returned zero for a running adapter")
	}

	if err := ns.Close(); err != nil {
		t.Fatalf("close custom namespace: %v", err)
	}
	rest, err := namespace.NewRest(map[string]string{"uri": fmt.Sprintf("http://127.0.0.1:%d", port)})
	if err != nil {
		t.Fatalf("new adapter client: %v", err)
	}
	defer rest.Close()

	callsBefore := len(client.recordedCalls())
	response, err := rest.DescribeTable(ctx, &namespace.DescribeTableRequest{ID: &tableID})
	if err != nil {
		t.Fatalf("DescribeTable through REST adapter after owner Close: %v", err)
	}
	if response.Location == nil || *response.Location != "s3://bucket/custom_events" {
		t.Fatalf("DescribeTable response = %+v", response)
	}
	calls := client.recordedCalls()[callsBefore:]
	if len(calls) != 1 || calls[0] != "describe_table" {
		t.Fatalf("client calls after owner Close = %v, want [describe_table]", calls)
	}
}

func TestNativeCustomNamespaceManagedDataset(t *testing.T) {
	ctx := context.Background()
	directory, err := namespace.NewDirectory(map[string]string{
		"root":                           t.TempDir(),
		"manifest_enabled":               "true",
		"table_version_tracking_enabled": "true",
	})
	if errors.Is(err, loader.ErrNotFound) {
		t.Skip("lance-c is not available; set LANCE_LIB_PATH to a built liblance_c")
	}
	if err != nil {
		t.Fatalf("new directory namespace: %v", err)
	}
	defer directory.Close()

	tableID := []string{"custom_managed_events"}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
	}, nil)
	created, err := lance.WriteToNamespace(ctx, directory, tableID, makeReader(t, schema,
		makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true}),
	))
	if err != nil {
		t.Fatalf("seed directory table: %v", err)
	}
	if err := created.Close(); err != nil {
		t.Fatalf("close seeded dataset: %v", err)
	}

	proxy := &directoryCustomClient{ns: directory}
	custom := newCustomNamespace(t, proxy, "custom-managed")
	opened, err := lance.OpenFromNamespace(ctx, custom, tableID)
	if err != nil {
		t.Fatalf("OpenFromNamespace through custom namespace: %v", err)
	}
	if ids := scanManagedIDs(t, ctx, opened); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("opened ids = %v, want [1 2]", ids)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close opened dataset: %v", err)
	}

	versions, err := custom.ListTableVersions(ctx, &namespace.ListTableVersionsRequest{ID: &tableID})
	if err != nil {
		t.Fatalf("ListTableVersions through custom namespace: %v", err)
	}
	if len(versions.Versions) == 0 {
		t.Fatal("custom namespace returned no table versions")
	}
	version, err := custom.DescribeTableVersion(ctx, &namespace.DescribeTableVersionRequest{ID: &tableID, Version: ptr(int64(1))})
	if err != nil {
		t.Fatalf("DescribeTableVersion through custom namespace: %v", err)
	}
	if version.Version.Version != 1 {
		t.Fatalf("version = %d, want 1", version.Version.Version)
	}

	description, err := custom.DescribeTable(ctx, &namespace.DescribeTableRequest{ID: &tableID})
	if err != nil {
		t.Fatalf("DescribeTable through custom namespace: %v", err)
	}
	if description.Location == nil || *description.Location == "" {
		t.Fatalf("custom DescribeTable location = %v", description.Location)
	}
	location := *description.Location

	tx, err := lance.WriteUncommitted(ctx, location, makeReader(t, schema,
		makeRecord(t, schema, []int64{3, 4}, []string{"c", "d"}, []bool{true, true}),
	), lance.WithWriteMode(lance.WriteModeAppend))
	if err != nil {
		t.Fatalf("WriteUncommitted: %v", err)
	}
	defer tx.Close()
	committed, err := lance.CommitTransactionToNamespace(ctx, custom, tableID, location, tx)
	if err != nil {
		t.Fatalf("CommitTransactionToNamespace: %v", err)
	}
	if rows, err := committed.CountRows(ctx); err != nil || rows != 4 {
		t.Fatalf("committed CountRows = %d, %v; want 4, nil", rows, err)
	}
	if err := committed.Close(); err != nil {
		t.Fatalf("close committed dataset: %v", err)
	}

	survivor, err := lance.OpenFromNamespace(ctx, custom, tableID)
	if err != nil {
		t.Fatalf("OpenFromNamespace before custom Close: %v", err)
	}
	defer survivor.Close()
	if err := custom.Close(); err != nil {
		t.Fatalf("close custom namespace: %v", err)
	}
	if rows, err := survivor.CountRows(ctx); err != nil || rows != 4 {
		t.Fatalf("dataset CountRows after custom Close = %d, %v; want 4, nil", rows, err)
	}
	callsBeforeDelete := proxy.count()
	afterDelete, removed, err := survivor.Delete(ctx, "id = 1")
	if err != nil {
		t.Fatalf("Delete after custom Close: %v", err)
	}
	defer afterDelete.Close()
	if removed != 1 {
		t.Fatalf("deleted rows = %d, want 1", removed)
	}
	if proxy.count() == callsBeforeDelete {
		t.Fatal("Delete after custom Close did not dispatch through the retained custom client")
	}
	if ids := scanManagedIDs(t, ctx, afterDelete); len(ids) != 3 || ids[0] != 2 || ids[2] != 4 {
		t.Fatalf("ids after delete = %v, want [2 3 4]", ids)
	}
}
