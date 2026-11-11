package lance

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
)

// OperationKind identifies a native transaction operation variant.
type OperationKind int32

const (
	OperationUnknown           OperationKind = 0
	OperationAppend            OperationKind = 1
	OperationDelete            OperationKind = 2
	OperationOverwrite         OperationKind = 3
	OperationCreateIndex       OperationKind = 4
	OperationRewrite           OperationKind = 5
	OperationDataReplacement   OperationKind = 6
	OperationDataOverlay       OperationKind = 7
	OperationMerge             OperationKind = 8
	OperationRestore           OperationKind = 9
	OperationReserveFragments  OperationKind = 10
	OperationUpdate            OperationKind = 11
	OperationProject           OperationKind = 12
	OperationUpdateConfig      OperationKind = 13
	OperationUpdateMemWALState OperationKind = 14
	OperationClone             OperationKind = 15
	OperationUpdateBases       OperationKind = 16
)

// Transaction is an owned native Lance transaction.
type Transaction struct {
	native *ffi.Transaction
	res    *resource
	uri    string
}

func newTransaction(native *ffi.Transaction, uri string) *Transaction {
	tx := &Transaction{native: native, uri: uri}
	tx.res = newResource(native.Close)
	return tx
}

// Close releases the transaction. It is idempotent.
func (t *Transaction) Close() error {
	if t == nil {
		return nil
	}
	t.res.close()
	return nil
}

func (t *Transaction) uriOrEmpty() string {
	if t == nil {
		return ""
	}
	return t.uri
}

// ReadVersion returns the transaction's read version. Zero is valid for an
// uncommitted create.
func (t *Transaction) ReadVersion() (uint64, error) {
	if t == nil || !t.res.acquire() {
		return 0, closedError("transaction read version", t.uriOrEmpty())
	}
	defer t.res.release()
	version, err := t.native.ReadVersion()
	if err != nil {
		return 0, wrapError("transaction read version", t.uri, err)
	}
	return version, nil
}

// UUID returns the transaction UUID.
func (t *Transaction) UUID() (string, error) {
	if t == nil || !t.res.acquire() {
		return "", closedError("transaction uuid", t.uriOrEmpty())
	}
	defer t.res.release()
	uuid, err := t.native.UUID()
	if err != nil {
		return "", wrapError("transaction uuid", t.uri, err)
	}
	return uuid, nil
}

// Tag returns the optional transaction tag.
func (t *Transaction) Tag() (string, bool, error) {
	if t == nil || !t.res.acquire() {
		return "", false, closedError("transaction tag", t.uriOrEmpty())
	}
	defer t.res.release()
	tag, present, err := t.native.Tag()
	if err != nil {
		return "", false, wrapError("transaction tag", t.uri, err)
	}
	return tag, present, nil
}

// Properties returns the native property map. The boolean distinguishes an
// absent map from a present empty map.
func (t *Transaction) Properties() (map[string]string, bool, error) {
	if t == nil || !t.res.acquire() {
		return nil, false, closedError("transaction properties", t.uriOrEmpty())
	}
	defer t.res.release()
	properties, present, err := t.native.Properties()
	if err != nil {
		return nil, false, wrapError("transaction properties", t.uri, err)
	}
	return properties, present, nil
}

// Operation returns a cloned operation.
func (t *Transaction) Operation() (*Operation, error) {
	if t == nil || !t.res.acquire() {
		return nil, closedError("transaction operation", t.uriOrEmpty())
	}
	defer t.res.release()
	native, err := t.native.Operation()
	if err != nil {
		return nil, wrapError("transaction operation", t.uri, err)
	}
	return newOperation(native, t.uri), nil
}

// MarshalBinary serializes the transaction in Lance protobuf format.
func (t *Transaction) MarshalBinary() ([]byte, error) {
	if t == nil || !t.res.acquire() {
		return nil, closedError("marshal transaction", t.uriOrEmpty())
	}
	defer t.res.release()
	data, err := t.native.MarshalBinary()
	if err != nil {
		return nil, wrapError("marshal transaction", t.uri, err)
	}
	return data, nil
}

// Commit publishes the transaction to uri.
func (t *Transaction) Commit(ctx context.Context, uri string, opts ...CommitOption) (*Dataset, error) {
	return CommitTransaction(ctx, uri, t, opts...)
}

// ParseTransaction parses one complete Lance protobuf transaction.
func ParseTransaction(data []byte) (*Transaction, error) {
	native, err := ffi.ParseTransaction(data)
	if err != nil {
		return nil, wrapError("parse transaction", "", err)
	}
	return newTransaction(native, ""), nil
}

// ReadTransaction returns the current snapshot's transaction, or (nil, nil)
// when the manifest has no transaction file.
func (d *Dataset) ReadTransaction() (*Transaction, error) {
	if err := d.acquire("read transaction"); err != nil {
		return nil, err
	}
	defer d.res.release()
	native, err := d.native.ReadTransaction()
	if err != nil {
		return nil, wrapError("read transaction", d.uri, err)
	}
	if native == nil {
		return nil, nil
	}
	return newTransaction(native, d.uri), nil
}

// ReadTransactionByVersion reads a specific version's transaction. It returns
// (nil, nil) when the version exists but has no transaction file. Missing or
// invalid versions follow the same native errors as other versioned reads.
func (d *Dataset) ReadTransactionByVersion(version uint64) (*Transaction, error) {
	if err := d.acquire("read transaction"); err != nil {
		return nil, err
	}
	defer d.res.release()
	native, err := d.native.ReadTransactionByVersion(version)
	if err != nil {
		return nil, wrapError("read transaction", d.uri, err)
	}
	if native == nil {
		return nil, nil
	}
	return newTransaction(native, d.uri), nil
}

// Operation is a native transaction operation snapshot.
type Operation struct {
	native *ffi.Operation
	res    *resource
	uri    string
}

func newOperation(native *ffi.Operation, uri string) *Operation {
	operation := &Operation{native: native, uri: uri}
	operation.res = newResource(native.Close)
	return operation
}

// Close releases the operation. It is idempotent.
func (o *Operation) Close() error {
	if o == nil {
		return nil
	}
	o.res.close()
	return nil
}

// Kind returns the operation variant.
func (o *Operation) Kind() (OperationKind, error) {
	return operationValue(o, "operation kind", func(native *ffi.Operation) (OperationKind, error) {
		kind, err := native.Kind()
		return OperationKind(kind), err
	})
}

// FragmentCount returns the fragment count for Append or Overwrite.
func (o *Operation) FragmentCount() (uint64, error) {
	return operationValue(o, "operation fragment count", (*ffi.Operation).FragmentCount)
}

// AppendFragmentCount returns the number of fragments in an Append operation.
func (o *Operation) AppendFragmentCount() (uint64, error) {
	return operationValue(o, "operation append fragment count", (*ffi.Operation).AppendFragmentCount)
}

// AppendFragment returns one detached fragment from an Append operation.
func (o *Operation) AppendFragment(index uint64) (*Fragment, error) {
	if o == nil || !o.res.acquire() {
		return nil, closedError("operation append fragment", o.uriOrEmpty())
	}
	defer o.res.release()
	native, err := o.native.AppendFragmentAt(index)
	if err != nil {
		return nil, wrapError("operation append fragment", o.uri, err)
	}
	return newFragment(native, o.uri), nil
}

// AppendFragments returns every detached fragment from an Append operation.
func (o *Operation) AppendFragments() ([]*Fragment, error) {
	count, err := o.AppendFragmentCount()
	if err != nil {
		return nil, err
	}
	fragments := make([]*Fragment, 0, count)
	for i := uint64(0); i < count; i++ {
		fragment, err := o.AppendFragment(i)
		if err != nil {
			for _, opened := range fragments {
				_ = opened.Close()
			}
			return nil, err
		}
		fragments = append(fragments, fragment)
	}
	return fragments, nil
}

// RestoreVersion returns the target version of a Restore operation.
func (o *Operation) RestoreVersion() (uint64, error) {
	return operationValue(o, "operation restore version", (*ffi.Operation).RestoreVersion)
}

// ReserveFragmentsCount returns the reserved fragment count.
func (o *Operation) ReserveFragmentsCount() (uint32, error) {
	return operationValue(o, "operation reserve fragments count", (*ffi.Operation).ReserveFragmentsCount)
}

// UpdateBasesCount returns the number of new base paths.
func (o *Operation) UpdateBasesCount() (uint64, error) {
	return operationValue(o, "operation update bases count", (*ffi.Operation).UpdateBasesCount)
}

// UpdateBase returns one base path from an UpdateBases operation.
func (o *Operation) UpdateBase(index uint64) (BasePath, error) {
	if o == nil || !o.res.acquire() {
		return BasePath{}, closedError("operation update base", o.uriOrEmpty())
	}
	defer o.res.release()
	base, err := o.native.BasePathAt(index)
	if err != nil {
		return BasePath{}, wrapError("operation update base", o.uri, err)
	}
	return BasePath{ID: base.ID, Path: base.Path, Name: base.Name, IsDatasetRoot: base.IsDatasetRoot}, nil
}

// UpdateBases returns every base path from an UpdateBases operation.
func (o *Operation) UpdateBases() ([]BasePath, error) {
	count, err := o.UpdateBasesCount()
	if err != nil {
		return nil, err
	}
	bases := make([]BasePath, 0, count)
	for i := uint64(0); i < count; i++ {
		base, err := o.UpdateBase(i)
		if err != nil {
			return nil, err
		}
		bases = append(bases, base)
	}
	return bases, nil
}

// DeleteUpdatedFragmentCount returns the number of updated Delete fragments.
func (o *Operation) DeleteUpdatedFragmentCount() (uint64, error) {
	return operationValue(o, "operation delete updated fragment count", (*ffi.Operation).DeleteUpdatedFragmentCount)
}

// DeleteUpdatedFragmentMetadata returns one updated fragment's metadata JSON.
func (o *Operation) DeleteUpdatedFragmentMetadata(index uint64) (json.RawMessage, error) {
	if o == nil || !o.res.acquire() {
		return nil, closedError("operation delete updated fragment metadata", o.uriOrEmpty())
	}
	defer o.res.release()
	data, err := o.native.DeleteUpdatedFragmentMetadataJSONAt(index)
	if err != nil {
		return nil, wrapError("operation delete updated fragment metadata", o.uri, err)
	}
	return json.RawMessage(data), nil
}

// DeleteDeletedFragmentCount returns the number of fully deleted fragment IDs.
func (o *Operation) DeleteDeletedFragmentCount() (uint64, error) {
	return operationValue(o, "operation delete deleted fragment count", (*ffi.Operation).DeleteDeletedFragmentCount)
}

// DeleteDeletedFragmentID returns one fully deleted fragment ID.
func (o *Operation) DeleteDeletedFragmentID(index uint64) (uint64, error) {
	return operationValue(o, "operation delete deleted fragment id", func(native *ffi.Operation) (uint64, error) {
		return native.DeleteDeletedFragmentIDAt(index)
	})
}

// DeleteDeletedFragmentIDs returns every fully deleted fragment ID.
func (o *Operation) DeleteDeletedFragmentIDs() ([]uint64, error) {
	count, err := o.DeleteDeletedFragmentCount()
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, count)
	for i := uint64(0); i < count; i++ {
		id, err := o.DeleteDeletedFragmentID(i)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// DeletePredicate returns the Delete operation's SQL predicate.
func (o *Operation) DeletePredicate() (string, error) {
	return operationValue(o, "operation delete predicate", (*ffi.Operation).DeletePredicate)
}

// DataReplacementCount returns the number of data replacement groups.
func (o *Operation) DataReplacementCount() (uint64, error) {
	return operationValue(o, "operation data replacement count", (*ffi.Operation).DataReplacementCount)
}

// DataReplacement returns one cloned data replacement group.
func (o *Operation) DataReplacement(index uint64) (*DataReplacementGroup, error) {
	if o == nil || !o.res.acquire() {
		return nil, closedError("operation data replacement", o.uriOrEmpty())
	}
	defer o.res.release()
	native, err := o.native.DataReplacementAt(index)
	if err != nil {
		return nil, wrapError("operation data replacement", o.uri, err)
	}
	return newDataReplacementGroup(native), nil
}

// DataReplacements returns every data replacement group.
func (o *Operation) DataReplacements() ([]*DataReplacementGroup, error) {
	count, err := o.DataReplacementCount()
	if err != nil {
		return nil, err
	}
	groups := make([]*DataReplacementGroup, 0, count)
	for i := uint64(0); i < count; i++ {
		group, err := o.DataReplacement(i)
		if err != nil {
			for _, opened := range groups {
				_ = opened.Close()
			}
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, nil
}

// Schema exports a lossy Arrow view of an Overwrite, Merge or Project schema.
func (o *Operation) Schema() (*arrow.Schema, error) {
	if o == nil || !o.res.acquire() {
		return nil, closedError("operation schema", o.uriOrEmpty())
	}
	defer o.res.release()
	schema, err := o.native.Schema()
	if err != nil {
		return nil, wrapError("operation schema", o.uri, err)
	}
	return schema, nil
}

// NativeSchema clones the in-memory native operation schema.
func (o *Operation) NativeSchema() (*NativeSchema, error) {
	if o == nil || !o.res.acquire() {
		return nil, closedError("operation native schema", o.uriOrEmpty())
	}
	defer o.res.release()
	native, err := o.native.NativeSchema()
	if err != nil {
		return nil, wrapError("operation native schema", o.uri, err)
	}
	return newNativeSchema(native), nil
}

// PreservesNullability reads the assertion from a Merge or Project operation.
func (o *Operation) PreservesNullability() (bool, error) {
	return operationValue(o, "operation preserves nullability", (*ffi.Operation).PreservesNullability)
}

func operationValue[T any](o *Operation, op string, call func(*ffi.Operation) (T, error)) (T, error) {
	var zero T
	if o == nil || !o.res.acquire() {
		return zero, closedError(op, o.uriOrEmpty())
	}
	defer o.res.release()
	value, err := call(o.native)
	if err != nil {
		return zero, wrapError(op, o.uri, err)
	}
	return value, nil
}

func (o *Operation) uriOrEmpty() string {
	if o == nil {
		return ""
	}
	return o.uri
}

// NewAppendOperation clones fragments into an Append operation.
func NewAppendOperation(fragments ...*Fragment) (*Operation, error) {
	if err := acquireFragments(fragments); err != nil {
		return nil, err
	}
	defer releaseFragments(fragments)
	nativeFragments := make([]*ffi.Fragment, len(fragments))
	for i, fragment := range fragments {
		nativeFragments[i] = fragment.native
	}
	native, err := ffi.NewAppendOperation(nativeFragments)
	if err != nil {
		return nil, wrapError("new append operation", fragmentURI(fragments), err)
	}
	return newOperation(native, fragmentURI(fragments)), nil
}

// NewDeleteOperation clones updated fragments and deleted IDs into a Delete
// operation.
func NewDeleteOperation(updated []*Fragment, deletedFragmentIDs []uint64, predicate string) (*Operation, error) {
	if err := acquireFragments(updated); err != nil {
		return nil, err
	}
	defer releaseFragments(updated)
	nativeFragments := make([]*ffi.Fragment, len(updated))
	for i, fragment := range updated {
		nativeFragments[i] = fragment.native
	}
	native, err := ffi.NewDeleteOperation(nativeFragments, deletedFragmentIDs, predicate)
	if err != nil {
		return nil, wrapError("new delete operation", fragmentURI(updated), err)
	}
	return newOperation(native, fragmentURI(updated)), nil
}

// NewRestoreOperation constructs a Restore operation.
func NewRestoreOperation(version uint64) (*Operation, error) {
	native, err := ffi.NewRestoreOperation(version)
	if err != nil {
		return nil, wrapError("new restore operation", "", err)
	}
	return newOperation(native, ""), nil
}

// NewReserveFragmentsOperation constructs a ReserveFragments operation.
func NewReserveFragmentsOperation(count uint32) (*Operation, error) {
	native, err := ffi.NewReserveFragmentsOperation(count)
	if err != nil {
		return nil, wrapError("new reserve fragments operation", "", err)
	}
	return newOperation(native, ""), nil
}

// NewUpdateBasesOperation constructs an UpdateBases operation.
func NewUpdateBasesOperation(bases []BasePath) (*Operation, error) {
	nativeBases := make([]ffi.BasePath, len(bases))
	for i, base := range bases {
		nativeBases[i] = ffi.BasePath{
			ID:            base.ID,
			Path:          base.Path,
			Name:          base.Name,
			IsDatasetRoot: base.IsDatasetRoot,
		}
	}
	native, err := ffi.NewUpdateBasesOperation(nativeBases)
	if err != nil {
		return nil, wrapError("new update bases operation", "", err)
	}
	return newOperation(native, ""), nil
}

// DataReplacementGroup is a native fragment data file replacement.
type DataReplacementGroup struct {
	native *ffi.DataReplacementGroup
	res    *resource
}

func newDataReplacementGroup(native *ffi.DataReplacementGroup) *DataReplacementGroup {
	group := &DataReplacementGroup{native: native}
	group.res = newResource(native.Close)
	return group
}

// NewDataReplacementGroup clones newFile into a replacement group.
func NewDataReplacementGroup(fragmentID uint64, newFile *DataFile) (*DataReplacementGroup, error) {
	if newFile == nil {
		return nil, invalidError("new data replacement group", "", "data file must not be nil")
	}
	if !newFile.res.acquire() {
		return nil, closedError("new data replacement group", "")
	}
	defer newFile.res.release()
	native, err := ffi.NewDataReplacementGroup(fragmentID, newFile.native)
	if err != nil {
		return nil, wrapError("new data replacement group", "", err)
	}
	return newDataReplacementGroup(native), nil
}

// Close releases the group. It is idempotent.
func (g *DataReplacementGroup) Close() error {
	if g == nil {
		return nil
	}
	g.res.close()
	return nil
}

// Clone returns an independent group snapshot.
func (g *DataReplacementGroup) Clone() (*DataReplacementGroup, error) {
	if g == nil || !g.res.acquire() {
		return nil, closedError("clone data replacement group", "")
	}
	defer g.res.release()
	native, err := g.native.Clone()
	if err != nil {
		return nil, wrapError("clone data replacement group", "", err)
	}
	return newDataReplacementGroup(native), nil
}

// FragmentID returns the replaced fragment ID. Zero is valid.
func (g *DataReplacementGroup) FragmentID() (uint64, error) {
	if g == nil || !g.res.acquire() {
		return 0, closedError("data replacement fragment id", "")
	}
	defer g.res.release()
	id, err := g.native.FragmentID()
	if err != nil {
		return 0, wrapError("data replacement fragment id", "", err)
	}
	return id, nil
}

// File returns the replacement data file.
func (g *DataReplacementGroup) File() (*DataFile, error) {
	if g == nil || !g.res.acquire() {
		return nil, closedError("data replacement file", "")
	}
	defer g.res.release()
	native, err := g.native.File()
	if err != nil {
		return nil, wrapError("data replacement file", "", err)
	}
	return newDataFile(native), nil
}

// NewDataReplacementOperation constructs a DataReplacement operation.
func NewDataReplacementOperation(groups ...*DataReplacementGroup) (*Operation, error) {
	for i, group := range groups {
		if group == nil {
			return nil, invalidError("new data replacement operation", "", "group %d must not be nil", i)
		}
		if !group.res.acquire() {
			for _, acquired := range groups[:i] {
				acquired.res.release()
			}
			return nil, closedError("new data replacement operation", "")
		}
	}
	defer func() {
		for _, group := range groups {
			group.res.release()
		}
	}()
	nativeGroups := make([]*ffi.DataReplacementGroup, len(groups))
	for i, group := range groups {
		nativeGroups[i] = group.native
	}
	native, err := ffi.NewDataReplacementOperation(nativeGroups)
	if err != nil {
		return nil, wrapError("new data replacement operation", "", err)
	}
	return newOperation(native, ""), nil
}

// NewProjectOperation constructs a Project operation.
func NewProjectOperation(schema *NativeSchema, preservesNullability bool) (*Operation, error) {
	if schema == nil {
		return nil, invalidError("new project operation", "", "schema must not be nil")
	}
	if !schema.res.acquire() {
		return nil, closedError("new project operation", "")
	}
	defer schema.res.release()
	native, err := ffi.NewProjectOperation(schema.native, preservesNullability)
	if err != nil {
		return nil, wrapError("new project operation", "", err)
	}
	return newOperation(native, ""), nil
}

// TransactionOption configures NewTransaction.
type TransactionOption func(*transactionConfig) error

type transactionConfig struct {
	uuid          *string
	tag           *string
	properties    map[string]string
	hasProperties bool
}

// WithTransactionUUID sets an explicit transaction UUID.
func WithTransactionUUID(uuid string) TransactionOption {
	return func(config *transactionConfig) error {
		if err := validateCString(uuid); err != nil {
			return fmt.Errorf("transaction UUID: %w", err)
		}
		config.uuid = &uuid
		return nil
	}
}

// WithTransactionTag records an optional transaction tag.
func WithTransactionTag(tag string) TransactionOption {
	return func(config *transactionConfig) error {
		if err := validateCString(tag); err != nil {
			return fmt.Errorf("transaction tag: %w", err)
		}
		config.tag = &tag
		return nil
	}
}

// WithTransactionProperties attaches a property map. Passing nil records a
// present, empty map.
func WithTransactionProperties(properties map[string]string) TransactionOption {
	return func(config *transactionConfig) error {
		copied := make(map[string]string, len(properties))
		for key, value := range properties {
			if err := validateCString(key); err != nil {
				return fmt.Errorf("transaction property key: %w", err)
			}
			if err := validateCString(value); err != nil {
				return fmt.Errorf("transaction property %q: %w", key, err)
			}
			copied[key] = value
		}
		config.properties = copied
		config.hasProperties = true
		return nil
	}
}

// NewTransaction constructs a transaction from an operation. The operation is
// cloned and remains usable. Without WithTransactionUUID, native generates one.
func NewTransaction(readVersion uint64, operation *Operation, opts ...TransactionOption) (*Transaction, error) {
	if operation == nil {
		return nil, invalidError("new transaction", "", "operation must not be nil")
	}
	config := transactionConfig{}
	for _, option := range opts {
		if option == nil {
			return nil, invalidError("new transaction", "", "nil TransactionOption")
		}
		if err := option(&config); err != nil {
			return nil, invalidError("new transaction", "", "%v", err)
		}
	}
	if !operation.res.acquire() {
		return nil, closedError("new transaction", operation.uri)
	}
	defer operation.res.release()
	native, err := ffi.NewTransaction(readVersion, operation.native, ffi.TransactionOptions{
		UUID:          config.uuid,
		Tag:           config.tag,
		Properties:    config.properties,
		HasProperties: config.hasProperties,
	})
	if err != nil {
		return nil, wrapError("new transaction", operation.uri, err)
	}
	return newTransaction(native, operation.uri), nil
}

// CommitTimeoutMode selects the native overall commit timeout policy.
type CommitTimeoutMode int32

const (
	CommitTimeoutDefault  CommitTimeoutMode = 0
	CommitTimeoutDisabled CommitTimeoutMode = 1
	CommitTimeoutExplicit CommitTimeoutMode = 2
)

// CommitTimeout selects the native overall commit timeout. Duration is used
// only with CommitTimeoutExplicit.
type CommitTimeout struct {
	Mode     CommitTimeoutMode
	Duration time.Duration
}

// CommitOption configures CommitTransaction.
type CommitOption func(*commitConfig) error

type commitConfig struct {
	useStableRowIDs        *bool
	enableV2ManifestPaths  *bool
	maxRetries             *uint32
	retryTimeout           *time.Duration
	timeout                CommitTimeout
	detached               bool
	skipAutoCleanup        bool
	dataStorageVersion     *string
	storageOptions         map[string]string
	storageOptionsSet      bool
	storageOptionsAccessor *StorageOptionsAccessor
	session                *Session
}

// WithCommitStableRowIDs overrides stable row ID generation.
func WithCommitStableRowIDs(enabled bool) CommitOption {
	return func(config *commitConfig) error {
		config.useStableRowIDs = &enabled
		return nil
	}
}

// WithCommitV2ManifestPaths overrides v2 manifest path generation.
func WithCommitV2ManifestPaths(enabled bool) CommitOption {
	return func(config *commitConfig) error {
		config.enableV2ManifestPaths = &enabled
		return nil
	}
}

// WithCommitMaxRetries sets the commit conflict retry count.
func WithCommitMaxRetries(retries uint32) CommitOption {
	return func(config *commitConfig) error {
		config.maxRetries = &retries
		return nil
	}
}

// WithCommitRetryTimeout bounds the total time spent retrying conflict
// resolution.
func WithCommitRetryTimeout(timeout time.Duration) CommitOption {
	return func(config *commitConfig) error {
		if timeout < 0 {
			return fmt.Errorf("retry timeout must not be negative")
		}
		config.retryTimeout = &timeout
		return nil
	}
}

// WithCommitTimeout sets the native overall commit timeout policy.
func WithCommitTimeout(timeout CommitTimeout) CommitOption {
	return func(config *commitConfig) error {
		switch timeout.Mode {
		case CommitTimeoutDefault, CommitTimeoutDisabled:
		case CommitTimeoutExplicit:
			if timeout.Duration < 0 {
				return fmt.Errorf("commit timeout must not be negative")
			}
		default:
			return fmt.Errorf("invalid commit timeout mode %d", timeout.Mode)
		}
		config.timeout = timeout
		return nil
	}
}

// WithCommitDetached writes a detached manifest version without advancing the
// dataset's latest version.
func WithCommitDetached(detached bool) CommitOption {
	return func(config *commitConfig) error {
		config.detached = detached
		return nil
	}
}

// WithCommitSkipAutoCleanup controls native old-version cleanup on commit.
func WithCommitSkipAutoCleanup(skip bool) CommitOption {
	return func(config *commitConfig) error {
		config.skipAutoCleanup = skip
		return nil
	}
}

// WithCommitDataStorageVersion overrides the data storage version.
func WithCommitDataStorageVersion(version string) CommitOption {
	return func(config *commitConfig) error {
		if err := validateCString(version); err != nil {
			return fmt.Errorf("data storage version: %w", err)
		}
		config.dataStorageVersion = &version
		return nil
	}
}

// WithCommitStorageOptions passes object-store options to the commit.
func WithCommitStorageOptions(options map[string]string) CommitOption {
	return func(config *commitConfig) error {
		copied, err := cloneStorageOptions(options)
		if err != nil {
			return err
		}
		config.storageOptions = copied
		config.storageOptionsSet = options != nil
		return nil
	}
}

// WithCommitStorageOptionsAccessor supplies a reusable dynamic accessor for a
// commit. It is mutually exclusive with WithCommitStorageOptions.
func WithCommitStorageOptionsAccessor(accessor *StorageOptionsAccessor) CommitOption {
	return func(config *commitConfig) error {
		if accessor == nil {
			return fmt.Errorf("storage options accessor must not be nil")
		}
		config.storageOptionsAccessor = accessor
		return nil
	}
}

// WithCommitSession pins the commit to a session.
func WithCommitSession(session *Session) CommitOption {
	return func(config *commitConfig) error {
		if session == nil {
			return fmt.Errorf("session must not be nil")
		}
		config.session = session
		return nil
	}
}

// CommitTransaction publishes one transaction through CommitBuilder::execute.
// The native commit retries conflicts according to the commit options. A
// canceled context is observed before the native call; if the commit succeeds
// after cancellation, the committed Dataset is returned.
func CommitTransaction(ctx context.Context, uri string, tx *Transaction, opts ...CommitOption) (*Dataset, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uri == "" {
		return nil, invalidError("commit", uri, "dataset URI must not be empty")
	}
	if err := validateCString(uri); err != nil {
		return nil, invalidError("commit", uri, "%v", err)
	}
	if tx == nil {
		return nil, invalidError("commit", uri, "transaction must not be nil")
	}
	config, err := parseCommitOptions(opts)
	if err != nil {
		return nil, invalidError("commit", uri, "%v", err)
	}
	storageAccessor, releaseStorage, err := resolveStorageOptions("commit", uri, config.storageOptions, config.storageOptionsSet, config.storageOptionsAccessor)
	if err != nil {
		return nil, err
	}
	defer releaseStorage()
	session, releaseSession, err := acquireSession("commit", uri, config.session)
	if err != nil {
		return nil, err
	}
	defer releaseSession()
	if !tx.res.acquire() {
		return nil, closedError("commit", uri)
	}
	defer tx.res.release()

	var native *ffi.Dataset
	if storageAccessor != nil {
		native, err = ffi.CommitWithAccessor(uri, tx.native, ffiCommitOptions(config, session), storageAccessor)
	} else {
		native, err = ffi.Commit(uri, tx.native, ffiCommitOptions(config, session))
	}
	if err != nil {
		return nil, wrapError("commit", uri, err)
	}
	return newDataset(native, uri), nil
}

// CommitTransactionBatch publishes append transactions atomically through
// CommitBuilder::execute_batch.
func CommitTransactionBatch(ctx context.Context, uri string, txs []*Transaction, opts ...CommitOption) (*Dataset, *Transaction, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if uri == "" {
		return nil, nil, invalidError("commit batch", uri, "dataset URI must not be empty")
	}
	if err := validateCString(uri); err != nil {
		return nil, nil, invalidError("commit batch", uri, "%v", err)
	}
	config, err := parseCommitOptions(opts)
	if err != nil {
		return nil, nil, invalidError("commit batch", uri, "%v", err)
	}
	storageAccessor, releaseStorage, err := resolveStorageOptions("commit batch", uri, config.storageOptions, config.storageOptionsSet, config.storageOptionsAccessor)
	if err != nil {
		return nil, nil, err
	}
	defer releaseStorage()
	for i, tx := range txs {
		if tx == nil {
			return nil, nil, invalidError("commit batch", uri, "transaction %d must not be nil", i)
		}
		if !tx.res.acquire() {
			for _, acquired := range txs[:i] {
				acquired.res.release()
			}
			return nil, nil, closedError("commit batch", uri)
		}
	}
	defer func() {
		for _, tx := range txs {
			tx.res.release()
		}
	}()
	session, releaseSession, err := acquireSession("commit batch", uri, config.session)
	if err != nil {
		return nil, nil, err
	}
	defer releaseSession()

	nativeTxs := make([]*ffi.Transaction, len(txs))
	for i, tx := range txs {
		nativeTxs[i] = tx.native
	}
	var nativeDataset *ffi.Dataset
	var nativeTransaction *ffi.Transaction
	if storageAccessor != nil {
		nativeDataset, nativeTransaction, err = ffi.CommitBatchWithAccessor(uri, nativeTxs, ffiCommitOptions(config, session), storageAccessor)
	} else {
		nativeDataset, nativeTransaction, err = ffi.CommitBatch(uri, nativeTxs, ffiCommitOptions(config, session))
	}
	if err != nil {
		return nil, nil, wrapError("commit batch", uri, err)
	}
	return newDataset(nativeDataset, uri), newTransaction(nativeTransaction, uri), nil
}

func parseCommitOptions(opts []CommitOption) (commitConfig, error) {
	config := commitConfig{}
	for _, option := range opts {
		if option == nil {
			return commitConfig{}, fmt.Errorf("nil CommitOption")
		}
		if err := option(&config); err != nil {
			return commitConfig{}, err
		}
	}
	return config, nil
}

func ffiCommitOptions(config commitConfig, session *ffi.Session) ffi.CommitOptions {
	return ffi.CommitOptions{
		UseStableRowIDs:       config.useStableRowIDs,
		EnableV2ManifestPaths: config.enableV2ManifestPaths,
		MaxRetries:            config.maxRetries,
		RetryTimeout:          config.retryTimeout,
		Timeout: ffi.CommitTimeout{
			Mode:     ffi.CommitTimeoutMode(config.timeout.Mode),
			Duration: config.timeout.Duration,
		},
		Detached:           config.detached,
		SkipAutoCleanup:    config.skipAutoCleanup,
		DataStorageVersion: config.dataStorageVersion,
		StorageOptions:     config.storageOptions,
		Session:            session,
	}
}

// WriteUncommitted writes reader as uncommitted data files and returns the
// transaction. The caller owns the transaction and is responsible for
// committing it or leaving the data files orphaned. reader is borrowed exactly
// as in Write.
func WriteUncommitted(ctx context.Context, uri string, reader array.RecordReader, opts ...WriteOption) (*Transaction, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uri == "" {
		return nil, invalidError("write uncommitted", uri, "dataset URI must not be empty")
	}
	if err := validateCString(uri); err != nil {
		return nil, invalidError("write uncommitted", uri, "%v", err)
	}
	if reader == nil {
		return nil, invalidError("write uncommitted", uri, "record reader must not be nil")
	}
	config := writeConfig{mode: WriteModeCreate}
	for _, option := range opts {
		if option == nil {
			return nil, invalidError("write uncommitted", uri, "nil WriteOption")
		}
		if err := option(&config); err != nil {
			return nil, invalidError("write uncommitted", uri, "%v", err)
		}
	}
	if config.branch != "" {
		return nil, invalidError("write uncommitted", uri, "WithWriteBranch is valid only for WriteToNamespace")
	}
	storageAccessor, releaseStorage, err := resolveStorageOptions("write uncommitted", uri, config.storageOptions, config.storageOptionsSet, config.storageOptionsAccessor)
	if err != nil {
		return nil, err
	}
	defer releaseStorage()
	session, releaseSession, err := acquireSession("write uncommitted", uri, config.session)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	uncommittedOptions := ffi.WriteUncommittedOptions{
		Write:                 nativeWriteOptions(config),
		StorageOptions:        config.storageOptions,
		TransactionProperties: config.transactionProperties,
		HasProperties:         config.hasTransactionProperties,
		Session:               session,
	}
	var native *ffi.Transaction
	if storageAccessor != nil {
		native, err = ffi.WriteUncommittedWithAccessor(uri, reader, uncommittedOptions, storageAccessor)
	} else {
		native, err = ffi.WriteUncommitted(uri, reader, uncommittedOptions)
	}
	if err != nil {
		return nil, wrapError("write uncommitted", uri, err)
	}
	return newTransaction(native, uri), nil
}

func acquireFragments(fragments []*Fragment) error {
	for i, fragment := range fragments {
		if fragment == nil {
			for _, acquired := range fragments[:i] {
				acquired.res.release()
			}
			return invalidError("fragment operation", "", "fragment %d must not be nil", i)
		}
		if !fragment.res.acquire() {
			for _, acquired := range fragments[:i] {
				acquired.res.release()
			}
			return closedError("fragment operation", fragment.uri)
		}
	}
	return nil
}

func releaseFragments(fragments []*Fragment) {
	for _, fragment := range fragments {
		fragment.res.release()
	}
}

func fragmentURI(fragments []*Fragment) string {
	for _, fragment := range fragments {
		if fragment != nil {
			return fragment.uri
		}
	}
	return ""
}

func newDataset(native *ffi.Dataset, uri string) *Dataset {
	dataset := &Dataset{native: native, uri: uri}
	dataset.res = newResource(native.Close)
	return dataset
}
