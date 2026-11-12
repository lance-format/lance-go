//go:build linux || darwin || windows

package namespace_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lance-format/lance-go/internal/loader"
	"github.com/lance-format/lance-go/namespace"
)

func newDirectoryNamespace(t *testing.T, opts ...namespace.Option) namespace.Namespace {
	t.Helper()
	ns, err := namespace.NewDirectory(map[string]string{"root": t.TempDir()}, opts...)
	if errors.Is(err, loader.ErrNotFound) {
		t.Skip("lance-c is not available; set LANCE_LIB_PATH to a built liblance_c")
	}
	if err != nil {
		t.Fatalf("new directory namespace: %v", err)
	}
	t.Cleanup(func() { _ = ns.Close() })
	return ns
}

func TestNativeDirectoryNamespaceLifecycle(t *testing.T) {
	ns := newDirectoryNamespace(t)
	ctx := context.Background()

	id, err := ns.ID()
	if err != nil {
		t.Fatalf("ID: %v", err)
	}
	if id == "" {
		t.Fatal("ID returned an empty namespace identifier")
	}

	list, err := ns.ListNamespaces(ctx, &namespace.ListNamespacesRequest{})
	if err != nil {
		t.Fatalf("ListNamespaces: %v", err)
	}
	if len(list.Namespaces) != 0 {
		t.Fatalf("root namespace list = %v, want empty", list.Namespaces)
	}

	if err := ns.NamespaceExists(ctx, &namespace.NamespaceExistsRequest{}); err != nil {
		t.Fatalf("NamespaceExists root: %v", err)
	}
	missingTable := []string{"missing_table"}
	var namespaceErr *namespace.NamespaceError
	err = ns.TableExists(ctx, &namespace.TableExistsRequest{ID: &missingTable})
	if !errors.As(err, &namespaceErr) || namespaceErr.Code != namespace.ErrorCodeTableNotFound {
		t.Fatalf("TableExists missing = %v, want TableNotFound", err)
	}

	metrics, err := ns.OpsMetrics()
	if err != nil {
		t.Fatalf("OpsMetrics: %v", err)
	}
	if metrics == nil {
		t.Fatal("OpsMetrics returned a nil map")
	}
	if err := ns.ResetOpsMetrics(); err != nil {
		t.Fatalf("ResetOpsMetrics: %v", err)
	}

	if err := ns.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := ns.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := ns.ID(); !errors.Is(err, namespace.ErrClosed) {
		t.Fatalf("ID after Close = %v, want ErrClosed", err)
	}
	if _, err := ns.ListNamespaces(ctx, &namespace.ListNamespacesRequest{}); !errors.Is(err, namespace.ErrClosed) {
		t.Fatalf("ListNamespaces after Close = %v, want ErrClosed", err)
	}
}

type contextProviderFunc func(string, string) (map[string]string, error)

func (f contextProviderFunc) ProvideContext(operation, objectID string) (map[string]string, error) {
	return f(operation, objectID)
}

type contextRecorder struct {
	mu          sync.Mutex
	operations  []string
	objectIDs   []string
	headerValue string
}

func (r *contextRecorder) ProvideContext(operation, objectID string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.operations = append(r.operations, operation)
	r.objectIDs = append(r.objectIDs, objectID)
	return map[string]string{"headers.X-Namespace-Test": r.headerValue}, nil
}

func (r *contextRecorder) called() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.operations) == 0 {
		return "", ""
	}
	return r.operations[len(r.operations)-1], r.objectIDs[len(r.objectIDs)-1]
}

func TestNativeRestNamespaceDynamicContext(t *testing.T) {
	recorder := &contextRecorder{headerValue: "from-provider"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Namespace-Test"); got != recorder.headerValue {
			t.Errorf("X-Namespace-Test = %q, want %q", got, recorder.headerValue)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tables":[]}`)
	}))
	defer server.Close()

	ns, err := namespace.NewRest(
		map[string]string{"uri": server.URL},
		namespace.WithDynamicContextProvider(recorder),
	)
	if errors.Is(err, loader.ErrNotFound) {
		t.Skip("lance-c is not available; set LANCE_LIB_PATH to a built liblance_c")
	}
	if err != nil {
		t.Fatalf("new REST namespace: %v", err)
	}
	defer ns.Close()

	id := []string{"workspace"}
	response, err := ns.ListTables(context.Background(), &namespace.ListTablesRequest{ID: &id})
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(response.Tables) != 0 {
		t.Fatalf("tables = %v, want empty", response.Tables)
	}
	operation, objectID := recorder.called()
	if operation != "list_tables" {
		t.Fatalf("provider operation = %q, want list_tables", operation)
	}
	if objectID != "workspace" {
		t.Fatalf("provider object ID = %q, want workspace", objectID)
	}
}

func TestNativeRestNamespaceDynamicContextErrorContinues(t *testing.T) {
	for _, mode := range []string{"error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			provider := contextProviderFunc(func(string, string) (map[string]string, error) {
				calls.Add(1)
				runtime.GC()
				if mode == "panic" {
					panic("provider panic")
				}
				return nil, errors.New("provider failed")
			})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("X-Namespace-Test"); got != "" {
					t.Errorf("X-Namespace-Test = %q, want no header after provider failure", got)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"namespaces":[]}`)
			}))
			defer server.Close()
			ns, err := namespace.NewRest(map[string]string{"uri": server.URL}, namespace.WithDynamicContextProvider(provider))
			if errors.Is(err, loader.ErrNotFound) {
				t.Skip("lance-c is not available; set LANCE_LIB_PATH to a built liblance_c")
			}
			if err != nil {
				t.Fatalf("new REST namespace: %v", err)
			}
			defer ns.Close()
			runtime.GC()
			rootID := []string{}
			response, err := ns.ListNamespaces(context.Background(), &namespace.ListNamespacesRequest{ID: &rootID})
			if err != nil {
				t.Fatalf("ListNamespaces after provider failure: %v", err)
			}
			if len(response.Namespaces) != 0 {
				t.Fatalf("namespaces = %v, want empty", response.Namespaces)
			}
			if calls.Load() == 0 {
				t.Fatal("native request did not invoke the provider")
			}
		})
	}
}

func TestNativeRESTAdapterOutlivesNamespace(t *testing.T) {
	ns := newDirectoryNamespace(t)
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
		t.Fatalf("close namespace: %v", err)
	}

	client, err := namespace.NewRest(map[string]string{"uri": fmt.Sprintf("http://127.0.0.1:%d", port)})
	if err != nil {
		t.Fatalf("new adapter client: %v", err)
	}
	defer client.Close()
	rootID := []string{}
	response, err := client.ListNamespaces(context.Background(), &namespace.ListNamespacesRequest{ID: &rootID})
	if err != nil {
		t.Fatalf("request after namespace Close: %v", err)
	}
	if len(response.Namespaces) != 0 {
		t.Fatalf("namespaces after owner Close = %v, want empty", response.Namespaces)
	}

	adapter.Shutdown()
	adapter.Shutdown()
	if _, err := adapter.Port(); err == nil {
		t.Fatal("Port succeeded after Shutdown")
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("adapter Close: %v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("second adapter Close: %v", err)
	}
}
