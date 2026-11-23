package lance

import (
	"fmt"

	"github.com/lance-format/lance-go/internal/ffi"
)

// This file holds the option values that more than one operation accepts. Each
// type implements the interfaces of every operation that supports it and no
// others, so passing a shared option to an unsupported operation fails to
// compile.
//
// The types are opaque: construct them with the With... function in this file.
// A nil pointer is invalid input and is reported as ErrInvalidArgument when
// the option is applied.

// SessionOption shares a session's caches. Construct it with WithSession.
type SessionOption struct {
	session *Session
}

// WithSession shares a session's caches with Open, Write, Create, Append,
// Overwrite, WriteUncommitted and CommitTransaction. It is not accepted by
// CreateBranch or NewTransaction.
func WithSession(session *Session) *SessionOption {
	return &SessionOption{session: session}
}

func (o *SessionOption) sessionOrError() (*Session, error) {
	if o == nil {
		return nil, fmt.Errorf("nil SessionOption")
	}
	if o.session == nil {
		return nil, fmt.Errorf("session must not be nil")
	}
	return o.session, nil
}

func (o *SessionOption) applyOpen(config *openConfig) error {
	session, err := o.sessionOrError()
	if err != nil {
		return err
	}
	config.session = session
	return nil
}

func (o *SessionOption) applyWrite(config *writeConfig) error {
	session, err := o.sessionOrError()
	if err != nil {
		return err
	}
	config.session = session
	return nil
}

func (o *SessionOption) applyCommit(config *commitConfig) error {
	session, err := o.sessionOrError()
	if err != nil {
		return err
	}
	config.session = session
	return nil
}

// StorageOptionsOption passes a static object-store option set. Construct it
// with WithStorageOptions.
type StorageOptionsOption struct {
	options map[string]string
	set     bool
}

// WithStorageOptions passes object-store options to Open, Write, Create,
// Append, Overwrite, WriteUncommitted, CommitTransaction,
// CommitTransactionBatch and CreateBranch.
//
// A nil map is absent: native uses the store parameters it would use without
// the option. A non-nil map, including an empty one, is an explicit option
// set. The option snapshots the map when it is applied.
//
// WithStorageOptions and WithStorageOptionsAccessor are mutually exclusive.
//
// CreateBranch differs in what absence means: a nil map inherits the source
// dataset's store parameters, while a non-nil map, including an empty one,
// passes an explicit parameter set.
func WithStorageOptions(options map[string]string) *StorageOptionsOption {
	return &StorageOptionsOption{options: options, set: options != nil}
}

// staticOptions returns the presence-aware option set used by Open, Write and
// Commit. A nil map collapses to native absence, matching the historical
// option constructors.
func (o *StorageOptionsOption) staticOptions() (map[string]string, bool, error) {
	if o == nil {
		return nil, false, fmt.Errorf("nil StorageOptionsOption")
	}
	if !o.set {
		return nil, false, nil
	}
	copied, err := cloneStorageOptions(o.options)
	if err != nil {
		return nil, false, err
	}
	return copied, true, nil
}

func (o *StorageOptionsOption) applyOpen(config *openConfig) error {
	options, set, err := o.staticOptions()
	if err != nil {
		return err
	}
	config.storageOptions = options
	config.storageOptionsSet = set
	return nil
}

func (o *StorageOptionsOption) applyWrite(config *writeConfig) error {
	options, set, err := o.staticOptions()
	if err != nil {
		return err
	}
	config.storageOptions = options
	config.storageOptionsSet = set
	return nil
}

func (o *StorageOptionsOption) applyCommit(config *commitConfig) error {
	options, set, err := o.staticOptions()
	if err != nil {
		return err
	}
	config.storageOptions = options
	config.storageOptionsSet = set
	return nil
}

func (o *StorageOptionsOption) applyBranch(config *branchOptions) error {
	if o == nil {
		return fmt.Errorf("nil StorageOptionsOption")
	}
	if !o.set {
		return nil
	}
	copied, err := copyStorageOptions(o.options)
	if err != nil {
		return err
	}
	config.storage = &ffi.BranchOptions{StorageOptions: copied}
	return nil
}

// StorageOptionsAccessorOption supplies a reusable dynamic storage-options
// accessor. Construct it with WithStorageOptionsAccessor.
type StorageOptionsAccessorOption struct {
	accessor *StorageOptionsAccessor
}

// WithStorageOptionsAccessor supplies a reusable dynamic storage-options
// accessor to Open, Write, Create, Append, Overwrite, WriteUncommitted,
// CommitTransaction and CommitTransactionBatch.
//
// The accessor is mutually exclusive with WithStorageOptions and may be closed
// after the operation returns; native datasets and stores retain their own
// reference. Namespace entry points reject the option because the native
// namespace provider already supplies credentials.
func WithStorageOptionsAccessor(accessor *StorageOptionsAccessor) *StorageOptionsAccessorOption {
	return &StorageOptionsAccessorOption{accessor: accessor}
}

func (o *StorageOptionsAccessorOption) accessorOrError() (*StorageOptionsAccessor, error) {
	if o == nil {
		return nil, fmt.Errorf("nil StorageOptionsAccessorOption")
	}
	if o.accessor == nil {
		return nil, fmt.Errorf("storage options accessor must not be nil")
	}
	return o.accessor, nil
}

func (o *StorageOptionsAccessorOption) applyOpen(config *openConfig) error {
	accessor, err := o.accessorOrError()
	if err != nil {
		return err
	}
	config.storageOptionsAccessor = accessor
	return nil
}

func (o *StorageOptionsAccessorOption) applyWrite(config *writeConfig) error {
	accessor, err := o.accessorOrError()
	if err != nil {
		return err
	}
	config.storageOptionsAccessor = accessor
	return nil
}

func (o *StorageOptionsAccessorOption) applyCommit(config *commitConfig) error {
	accessor, err := o.accessorOrError()
	if err != nil {
		return err
	}
	config.storageOptionsAccessor = accessor
	return nil
}

// CommitLockOption routes an operation through an external commit lock.
// Construct it with WithCommitLock.
type CommitLockOption struct {
	lock *CommitLock
}

// WithCommitLock routes Open, ordinary and uncommitted writes, and
// CommitTransaction and CommitTransactionBatch through an external commit
// lock. The lock may be closed after the operation returns.
//
// It is rejected by namespace operations and fragment creation because those
// native entry points have no commit-lock parameter.
func WithCommitLock(lock *CommitLock) *CommitLockOption {
	return &CommitLockOption{lock: lock}
}

func (o *CommitLockOption) lockOrError() (*CommitLock, error) {
	if o == nil {
		return nil, fmt.Errorf("nil CommitLockOption")
	}
	if o.lock == nil {
		return nil, fmt.Errorf("commit lock must not be nil")
	}
	return o.lock, nil
}

func (o *CommitLockOption) applyOpen(config *openConfig) error {
	lock, err := o.lockOrError()
	if err != nil {
		return err
	}
	config.commitLock = lock
	return nil
}

func (o *CommitLockOption) applyWrite(config *writeConfig) error {
	lock, err := o.lockOrError()
	if err != nil {
		return err
	}
	config.commitLock = lock
	return nil
}

func (o *CommitLockOption) applyCommit(config *commitConfig) error {
	lock, err := o.lockOrError()
	if err != nil {
		return err
	}
	config.commitLock = lock
	return nil
}

// SkipAutoCleanupOption disables native old-version cleanup. Construct it with
// WithSkipAutoCleanup.
type SkipAutoCleanupOption struct {
	skip bool
}

// WithSkipAutoCleanup disables native old-version cleanup for a write or a
// commit. Fragment creation rejects it because creation does not commit.
func WithSkipAutoCleanup(skip bool) *SkipAutoCleanupOption {
	return &SkipAutoCleanupOption{skip: skip}
}

func (o *SkipAutoCleanupOption) applyWrite(config *writeConfig) error {
	if o == nil {
		return fmt.Errorf("nil SkipAutoCleanupOption")
	}
	config.skipAutoCleanup = o.skip
	return nil
}

func (o *SkipAutoCleanupOption) applyCommit(config *commitConfig) error {
	if o == nil {
		return fmt.Errorf("nil SkipAutoCleanupOption")
	}
	config.skipAutoCleanup = o.skip
	return nil
}

// TransactionPropertiesOption attaches transaction properties to a write or a
// new transaction. Construct it with WithTransactionProperties.
type TransactionPropertiesOption struct {
	properties map[string]string
}

// WithTransactionProperties attaches properties to the transaction created by
// an ordinary write or WriteUncommitted, or constructed by NewTransaction.
// Passing nil records a present, empty property map. Fragment creation and
// namespace writes reject the option.
func WithTransactionProperties(properties map[string]string) *TransactionPropertiesOption {
	return &TransactionPropertiesOption{properties: properties}
}

func (o *TransactionPropertiesOption) propertiesOrError() (map[string]string, error) {
	if o == nil {
		return nil, fmt.Errorf("nil TransactionPropertiesOption")
	}
	copied := make(map[string]string, len(o.properties))
	for key, value := range o.properties {
		if err := validateCString(key); err != nil {
			return nil, fmt.Errorf("transaction property key: %w", err)
		}
		if err := validateCString(value); err != nil {
			return nil, fmt.Errorf("transaction property %q: %w", key, err)
		}
		copied[key] = value
	}
	return copied, nil
}

func (o *TransactionPropertiesOption) applyWrite(config *writeConfig) error {
	copied, err := o.propertiesOrError()
	if err != nil {
		return err
	}
	config.transactionProperties = copied
	config.hasTransactionProperties = true
	return nil
}

func (o *TransactionPropertiesOption) applyTransaction(config *transactionConfig) error {
	copied, err := o.propertiesOrError()
	if err != nil {
		return err
	}
	config.properties = copied
	config.hasProperties = true
	return nil
}
