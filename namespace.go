package lance

import (
	"context"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/array"

	_ "github.com/lance-format/lance-go/internal/bundled"
	"github.com/lance-format/lance-go/internal/nshandle"
	"github.com/lance-format/lance-go/namespace"
)

// nativeNamespaceAcquirer is implemented by the concrete namespace types in
// the namespace package. External types cannot provide a native lease.
type nativeNamespaceAcquirer interface {
	namespace.Namespace
	AcquireNative() (*nshandle.Lease, bool)
}

func acquireNamespace(namespaceName namespace.Namespace, operation string) (*nshandle.Lease, error) {
	if namespaceName == nil {
		return nil, invalidError(operation, "", "namespace must not be nil")
	}
	acquirer, ok := namespaceName.(nativeNamespaceAcquirer)
	if !ok {
		return nil, invalidError(operation, "", "namespace does not provide native access")
	}
	lease, ok := acquirer.AcquireNative()
	if !ok {
		return nil, closedError(operation, "")
	}
	return lease, nil
}

func namespaceTableURI(tableID []string) string {
	return strings.Join(tableID, "$")
}

// OpenFromNamespace opens a table managed by ns. tableID is the native table
// identifier path. A zero version selects the latest version.
func OpenFromNamespace(ctx context.Context, ns namespace.Namespace, tableID []string, opts ...OpenOption) (*Dataset, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(tableID) == 0 {
		return nil, invalidError("open namespace", "", "table identifier must contain at least one part")
	}
	config := openConfig{}
	for _, option := range opts {
		if option == nil {
			return nil, invalidError("open namespace", "", "nil OpenOption")
		}
		if err := option(&config); err != nil {
			return nil, invalidError("open namespace", "", "%v", err)
		}
	}
	session, releaseSession, err := acquireSession("open namespace", "", config.session)
	if err != nil {
		return nil, err
	}
	defer releaseSession()
	lease, err := acquireNamespace(ns, "open namespace")
	if err != nil {
		return nil, err
	}
	defer lease.Release()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	native, err := lease.OpenDataset(tableID, config.storageOptions, config.version, session)
	if err != nil {
		return nil, wrapError("open namespace", namespaceTableURI(tableID), err)
	}
	return newDataset(native, namespaceTableURI(tableID)), nil
}

// WriteToNamespace writes all batches from reader into a table managed by ns.
// The default mode is Create.
//
// reader is borrowed and advanced during the call. The caller retains
// ownership and must release it after the call. A canceled context is observed
// before the native call; if the write commits after cancellation, the
// committed Dataset is returned.
func WriteToNamespace(ctx context.Context, ns namespace.Namespace, tableID []string, reader array.RecordReader, opts ...WriteOption) (*Dataset, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(tableID) == 0 {
		return nil, invalidError("write namespace", "", "table identifier must contain at least one part")
	}
	if reader == nil {
		return nil, invalidError("write namespace", "", "record reader must not be nil")
	}
	config := writeConfig{mode: WriteModeCreate}
	for _, option := range opts {
		if option == nil {
			return nil, invalidError("write namespace", "", "nil WriteOption")
		}
		if err := option(&config); err != nil {
			return nil, invalidError("write namespace", "", "%v", err)
		}
	}
	if config.hasTransactionProperties {
		return nil, invalidError("write namespace", "", "WithWriteTransactionProperties is not supported")
	}
	session, releaseSession, err := acquireSession("write namespace", "", config.session)
	if err != nil {
		return nil, err
	}
	defer releaseSession()
	lease, err := acquireNamespace(ns, "write namespace")
	if err != nil {
		return nil, err
	}
	defer lease.Release()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	native, err := lease.WriteDataset(tableID, reader, nativeWriteOptions(config), config.storageOptions, session, config.branch)
	if err != nil {
		return nil, wrapError("write namespace", namespaceTableURI(tableID), err)
	}
	return newDataset(native, namespaceTableURI(tableID)), nil
}

// CommitTransactionToNamespace publishes tx through a namespace-managed table
// at uri. A canceled context is observed before the native call; if the commit
// succeeds after cancellation, the committed Dataset is returned.
func CommitTransactionToNamespace(ctx context.Context, ns namespace.Namespace, tableID []string, uri string, tx *Transaction, opts ...CommitOption) (*Dataset, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(tableID) == 0 {
		return nil, invalidError("commit namespace", "", "table identifier must contain at least one part")
	}
	if uri == "" {
		return nil, invalidError("commit namespace", "", "publication URI must not be empty")
	}
	if err := validateCString(uri); err != nil {
		return nil, invalidError("commit namespace", "", "%v", err)
	}
	if tx == nil {
		return nil, invalidError("commit namespace", "", "transaction must not be nil")
	}
	config, err := parseCommitOptions(opts)
	if err != nil {
		return nil, invalidError("commit namespace", "", "%v", err)
	}
	session, releaseSession, err := acquireSession("commit namespace", "", config.session)
	if err != nil {
		return nil, err
	}
	defer releaseSession()
	if !tx.res.acquire() {
		return nil, closedError("commit namespace", "")
	}
	defer tx.res.release()
	lease, err := acquireNamespace(ns, "commit namespace")
	if err != nil {
		return nil, err
	}
	defer lease.Release()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	native, err := lease.CommitNamespaceURI(tableID, uri, tx.native, ffiCommitOptions(config), session)
	if err != nil {
		return nil, wrapError("commit namespace", uri, err)
	}
	return newDataset(native, uri), nil
}
