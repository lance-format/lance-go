// Package nshandle is the narrow ownership boundary between the public
// namespace package, the root lance package, and the native FFI.
//
// An Owner owns one native namespace handle and its lifetime. Callers borrow a
// Lease for one operation; Close waits for every lease to be released and only
// then closes the native handle. A Lease must not be used after Release.
package nshandle

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
	"github.com/lance-format/lance-go/internal/lifetime"
)

// Owner owns one native namespace handle.
type Owner struct {
	native *ffi.Namespace
	life   *lifetime.Resource
}

// New takes ownership of native. It must not be nil.
func New(native *ffi.Namespace) *Owner {
	if native == nil {
		panic("nshandle: nil native namespace")
	}
	o := &Owner{native: native}
	o.life = lifetime.New(native.Close)
	return o
}

// Close stops new operations, waits for in-flight leases, and releases the
// native namespace exactly once.
func (o *Owner) Close() {
	if o == nil {
		return
	}
	o.life.Close()
}

// Acquire borrows the native namespace for one operation.
func (o *Owner) Acquire() (*Lease, bool) {
	if o == nil || !o.life.Acquire() {
		return nil, false
	}
	return &Lease{owner: o}, true
}

// Lease is one borrowed operation on an Owner.
type Lease struct {
	owner *Owner
	once  sync.Once
}

// Release returns the lease. It is idempotent.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.owner.life.Release()
	})
}

// Call executes one native namespace operation.
func (l *Lease) Call(operation, requestJSON string, requestBytes []byte) (*ffi.CallResult, error) {
	if l == nil || l.owner == nil {
		return nil, ffi.ErrClosed
	}
	return l.owner.native.Call(operation, requestJSON, requestBytes)
}

// ID returns the native namespace identifier.
func (l *Lease) ID() (string, error) {
	if l == nil || l.owner == nil {
		return "", ffi.ErrClosed
	}
	return l.owner.native.ID()
}

// OpsMetrics returns native operation counters as JSON.
func (l *Lease) OpsMetrics() ([]byte, error) {
	if l == nil || l.owner == nil {
		return nil, ffi.ErrClosed
	}
	return l.owner.native.OpsMetrics()
}

// ResetOpsMetrics resets native operation counters.
func (l *Lease) ResetOpsMetrics() error {
	if l == nil || l.owner == nil {
		return ffi.ErrClosed
	}
	return l.owner.native.ResetOpsMetrics()
}

// OpenDataset opens a namespace-managed table.
func (l *Lease) OpenDataset(tableID []string, storageOptions map[string]string, version uint64, session *ffi.Session) (*ffi.Dataset, error) {
	if l == nil || l.owner == nil {
		return nil, ffi.ErrClosed
	}
	tableIDJSON, err := encodeTableIDJSON(tableID)
	if err != nil {
		return nil, err
	}
	return l.owner.native.OpenDataset(tableIDJSON, storageOptions, version, session)
}

// WriteDataset writes reader into a namespace-managed table.
func (l *Lease) WriteDataset(tableID []string, reader array.RecordReader, opts ffi.WriteOptions, storageOptions map[string]string, session *ffi.Session, branch string) (*ffi.Dataset, error) {
	if l == nil || l.owner == nil {
		return nil, ffi.ErrClosed
	}
	tableIDJSON, err := encodeTableIDJSON(tableID)
	if err != nil {
		return nil, err
	}
	return l.owner.native.WriteDataset(tableIDJSON, reader, opts, storageOptions, session, branch)
}

// CommitNamespaceURI commits tx to a namespace-managed publication URI.
func (l *Lease) CommitNamespaceURI(tableID []string, uri string, tx *ffi.Transaction, opts ffi.CommitOptions, session *ffi.Session) (*ffi.Dataset, error) {
	if l == nil || l.owner == nil {
		return nil, ffi.ErrClosed
	}
	tableIDJSON, err := encodeTableIDJSON(tableID)
	if err != nil {
		return nil, err
	}
	return l.owner.native.CommitNamespaceURI(tableIDJSON, uri, tx, opts, session)
}

// NewStorageOptionsProvider creates a native namespace-backed storage options
// provider for tableID. The provider retains the native namespace, so it
// remains usable after the caller releases this lease or closes the public
// namespace handle.
func (l *Lease) NewStorageOptionsProvider(tableID []string) (*ffi.StorageOptionsProvider, error) {
	if l == nil || l.owner == nil {
		return nil, ffi.ErrClosed
	}
	tableIDJSON, err := encodeTableIDJSON(tableID)
	if err != nil {
		return nil, err
	}
	return ffi.NewStorageOptionsProviderFromNamespace(l.owner.native, tableIDJSON)
}

// StartRESTAdapter starts a native REST adapter for this namespace.
func (l *Lease) StartRESTAdapter(host string, port uint16) (*RESTAdapter, error) {
	if l == nil || l.owner == nil {
		return nil, ffi.ErrClosed
	}
	native, err := l.owner.native.StartRESTAdapter(host, port)
	if err != nil {
		return nil, err
	}
	return newRESTAdapter(native), nil
}

// RESTAdapter owns one native REST adapter handle independently of its
// namespace.
type RESTAdapter struct {
	native *ffi.RESTAdapter
	life   *lifetime.Resource
}

func newRESTAdapter(native *ffi.RESTAdapter) *RESTAdapter {
	a := &RESTAdapter{native: native}
	a.life = lifetime.New(native.Close)
	return a
}

// Port returns the bound adapter port.
func (a *RESTAdapter) Port() (uint16, error) {
	if a == nil {
		return 0, ffi.ErrClosed
	}
	if !a.life.Acquire() {
		return 0, ffi.ErrClosed
	}
	defer a.life.Release()
	return a.native.Port()
}

// Shutdown stops serving and is idempotent. It waits for in-flight Port calls.
func (a *RESTAdapter) Shutdown() {
	if a == nil {
		return
	}
	if !a.life.Acquire() {
		return
	}
	defer a.life.Release()
	a.native.Shutdown()
}

// Close waits for in-flight calls and releases the native adapter.
func (a *RESTAdapter) Close() {
	if a == nil {
		return
	}
	a.life.Close()
}

func encodeTableIDJSON(tableID []string) (string, error) {
	if len(tableID) == 0 {
		return "", fmt.Errorf("lance: table identifier must contain at least one part")
	}
	encoded, err := json.Marshal(tableID)
	if err != nil {
		return "", fmt.Errorf("lance: encode table identifier: %w", err)
	}
	return string(encoded), nil
}
