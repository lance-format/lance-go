package lance

import (
	"math"
	"strings"
	"testing"
)

// The assertions below pin the operation support matrix of the shared option
// values at compile time: a shared value must implement the interface of every
// operation that accepts it, and the package must not add unsupported
// interfaces silently. Passing a value to an unsupported operation fails to
// compile at the call site.
var (
	_ OpenOption           = (*SessionOption)(nil)
	_ WriteOption          = (*SessionOption)(nil)
	_ CommitOption         = (*SessionOption)(nil)
	_ OpenOption           = (*StorageOptionsOption)(nil)
	_ WriteOption          = (*StorageOptionsOption)(nil)
	_ CommitOption         = (*StorageOptionsOption)(nil)
	_ BranchOption         = (*StorageOptionsOption)(nil)
	_ OpenOption           = (*StorageOptionsAccessorOption)(nil)
	_ WriteOption          = (*StorageOptionsAccessorOption)(nil)
	_ CommitOption         = (*StorageOptionsAccessorOption)(nil)
	_ OpenOption           = (*CommitLockOption)(nil)
	_ WriteOption          = (*CommitLockOption)(nil)
	_ CommitOption         = (*CommitLockOption)(nil)
	_ WriteOption          = (*SkipAutoCleanupOption)(nil)
	_ CommitOption         = (*SkipAutoCleanupOption)(nil)
	_ WriteOption          = (*TransactionPropertiesOption)(nil)
	_ TransactionOption    = (*TransactionPropertiesOption)(nil)
	_ AddColumnsOption     = WithAddColumnsBatchSize(1)
	_ FragmentCreateOption = (*SessionOption)(nil)
)

func TestSessionOptionAppliesToEverySupportedOperation(t *testing.T) {
	session := &Session{}

	var open openConfig
	if err := WithSession(session).applyOpen(&open); err != nil {
		t.Fatalf("applyOpen: %v", err)
	}
	if open.session != session {
		t.Fatal("open session was not applied")
	}

	var write writeConfig
	if err := WithSession(session).applyWrite(&write); err != nil {
		t.Fatalf("applyWrite: %v", err)
	}
	if write.session != session {
		t.Fatal("write session was not applied")
	}

	var commit commitConfig
	if err := WithSession(session).applyCommit(&commit); err != nil {
		t.Fatalf("applyCommit: %v", err)
	}
	if commit.session != session {
		t.Fatal("commit session was not applied")
	}
}

func TestSharedOptionsRejectNilValues(t *testing.T) {
	tests := []struct {
		name  string
		apply func() error
		want  string
	}{
		{
			name:  "nil SessionOption pointer",
			apply: func() error { return (*SessionOption)(nil).applyOpen(&openConfig{}) },
			want:  "nil SessionOption",
		},
		{
			name:  "nil session",
			apply: func() error { return WithSession(nil).applyWrite(&writeConfig{}) },
			want:  "session must not be nil",
		},
		{
			name:  "nil StorageOptionsOption pointer",
			apply: func() error { return (*StorageOptionsOption)(nil).applyOpen(&openConfig{}) },
			want:  "nil StorageOptionsOption",
		},
		{
			name:  "nil StorageOptionsAccessorOption pointer",
			apply: func() error { return (*StorageOptionsAccessorOption)(nil).applyCommit(&commitConfig{}) },
			want:  "nil StorageOptionsAccessorOption",
		},
		{
			name:  "nil accessor",
			apply: func() error { return WithStorageOptionsAccessor(nil).applyOpen(&openConfig{}) },
			want:  "storage options accessor must not be nil",
		},
		{
			name:  "nil CommitLockOption pointer",
			apply: func() error { return (*CommitLockOption)(nil).applyOpen(&openConfig{}) },
			want:  "nil CommitLockOption",
		},
		{
			name:  "nil commit lock",
			apply: func() error { return WithCommitLock(nil).applyCommit(&commitConfig{}) },
			want:  "commit lock must not be nil",
		},
		{
			name:  "nil SkipAutoCleanupOption pointer",
			apply: func() error { return (*SkipAutoCleanupOption)(nil).applyWrite(&writeConfig{}) },
			want:  "nil SkipAutoCleanupOption",
		},
		{
			name:  "nil TransactionPropertiesOption pointer",
			apply: func() error { return (*TransactionPropertiesOption)(nil).applyTransaction(&transactionConfig{}) },
			want:  "nil TransactionPropertiesOption",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.apply()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

// TestStorageOptionsPresence protects the presence rules: a nil map is native
// absence, while an explicitly empty map keeps the native Some(empty)
// distinction. The clone collapses an empty map to nil, so presence must be
// tracked separately.
func TestStorageOptionsPresence(t *testing.T) {
	var absent openConfig
	if err := WithStorageOptions(nil).applyOpen(&absent); err != nil {
		t.Fatalf("nil map: %v", err)
	}
	if absent.storageOptionsSet || absent.storageOptions != nil {
		t.Fatalf("nil map = %v/%v, want absent", absent.storageOptions, absent.storageOptionsSet)
	}

	var empty openConfig
	if err := WithStorageOptions(map[string]string{}).applyOpen(&empty); err != nil {
		t.Fatalf("empty map: %v", err)
	}
	if !empty.storageOptionsSet || empty.storageOptions != nil {
		t.Fatalf("empty map = %v/%v, want present with no entries", empty.storageOptions, empty.storageOptionsSet)
	}

	original := map[string]string{"aws_region": "us-east-2"}
	var copied openConfig
	if err := WithStorageOptions(original).applyOpen(&copied); err != nil {
		t.Fatalf("non-empty map: %v", err)
	}
	original["aws_region"] = "changed"
	if copied.storageOptions["aws_region"] != "us-east-2" {
		t.Fatalf("storage options were not copied at apply time: %v", copied.storageOptions)
	}
}

// TestStorageOptionsBranchPresence protects the CreateBranch meaning of
// absence: a nil map inherits the source dataset's store parameters, while a
// non-nil empty map passes an explicit empty set.
func TestStorageOptionsBranchPresence(t *testing.T) {
	var inherited branchOptions
	if err := WithStorageOptions(nil).applyBranch(&inherited); err != nil {
		t.Fatalf("nil map: %v", err)
	}
	if inherited.storage != nil {
		t.Fatalf("nil map storage = %v, want inherit", inherited.storage)
	}

	var explicit branchOptions
	if err := WithStorageOptions(map[string]string{}).applyBranch(&explicit); err != nil {
		t.Fatalf("empty map: %v", err)
	}
	if explicit.storage == nil || explicit.storage.StorageOptions == nil || len(explicit.storage.StorageOptions) != 0 {
		t.Fatalf("empty map storage = %#v, want explicit empty", explicit.storage)
	}
}

func TestSkipAutoCleanupAppliesToWriteAndCommit(t *testing.T) {
	var write writeConfig
	if err := WithSkipAutoCleanup(true).applyWrite(&write); err != nil {
		t.Fatalf("applyWrite: %v", err)
	}
	if !write.skipAutoCleanup {
		t.Fatal("write skip-auto-cleanup was not applied")
	}

	var commit commitConfig
	if err := WithSkipAutoCleanup(true).applyCommit(&commit); err != nil {
		t.Fatalf("applyCommit: %v", err)
	}
	if !commit.skipAutoCleanup {
		t.Fatal("commit skip-auto-cleanup was not applied")
	}
}

// TestTransactionPropertiesPresence protects the documented nil meaning:
// passing nil records a present, empty property map rather than native
// absence, on both the write and the explicit transaction path.
func TestTransactionPropertiesPresence(t *testing.T) {
	var write writeConfig
	if err := WithTransactionProperties(nil).applyWrite(&write); err != nil {
		t.Fatalf("applyWrite: %v", err)
	}
	if !write.hasTransactionProperties || write.transactionProperties == nil || len(write.transactionProperties) != 0 {
		t.Fatalf("write properties = %v/%v, want present empty", write.transactionProperties, write.hasTransactionProperties)
	}

	var transaction transactionConfig
	if err := WithTransactionProperties(nil).applyTransaction(&transaction); err != nil {
		t.Fatalf("applyTransaction: %v", err)
	}
	if !transaction.hasProperties || transaction.properties == nil || len(transaction.properties) != 0 {
		t.Fatalf("transaction properties = %v/%v, want present empty", transaction.properties, transaction.hasProperties)
	}
}

func TestAddColumnsOptions(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		config, err := parseAddColumnsOptions(nil)
		if err != nil {
			t.Fatalf("parseAddColumnsOptions: %v", err)
		}
		if config.batchSizeSet || config.batchSize != 0 {
			t.Fatalf("default config = %+v, want unset", config)
		}
	})

	t.Run("explicit zero keeps presence", func(t *testing.T) {
		config, err := parseAddColumnsOptions([]AddColumnsOption{WithAddColumnsBatchSize(0)})
		if err != nil {
			t.Fatalf("parseAddColumnsOptions: %v", err)
		}
		if !config.batchSizeSet || config.batchSize != 0 {
			t.Fatalf("config = %+v, want explicit zero", config)
		}
	})

	t.Run("last value wins", func(t *testing.T) {
		config, err := parseAddColumnsOptions([]AddColumnsOption{
			WithAddColumnsBatchSize(7),
			WithAddColumnsBatchSize(9),
		})
		if err != nil {
			t.Fatalf("parseAddColumnsOptions: %v", err)
		}
		if config.batchSize != 9 {
			t.Fatalf("batch size = %d, want 9", config.batchSize)
		}
	})

	t.Run("nil option", func(t *testing.T) {
		if _, err := parseAddColumnsOptions([]AddColumnsOption{nil}); err == nil || !strings.Contains(err.Error(), "nil AddColumnsOption") {
			t.Fatalf("error = %v, want nil AddColumnsOption", err)
		}
	})
}

// TestAddColumnsFragmentBatchSize protects the Fragment native contract: the
// option unset maps to None, an explicit value maps to Some(uint32), and a
// value above uint32 is rejected before any native call.
func TestAddColumnsFragmentBatchSize(t *testing.T) {
	unset, err := addColumnsConfig{}.fragmentBatchSize()
	if err != nil || unset != nil {
		t.Fatalf("unset = %v/%v, want None", unset, err)
	}

	zero, err := addColumnsConfig{batchSize: 0, batchSizeSet: true}.fragmentBatchSize()
	if err != nil || zero == nil || *zero != 0 {
		t.Fatalf("explicit zero = %v/%v, want Some(0)", zero, err)
	}

	max, err := addColumnsConfig{batchSize: math.MaxUint32, batchSizeSet: true}.fragmentBatchSize()
	if err != nil || max == nil || *max != math.MaxUint32 {
		t.Fatalf("MaxUint32 = %v/%v, want Some(MaxUint32)", max, err)
	}

	tooLarge, err := addColumnsConfig{batchSize: math.MaxUint32 + 1, batchSizeSet: true}.fragmentBatchSize()
	if err == nil || !strings.Contains(err.Error(), "exceeds uint32") {
		t.Fatalf("above uint32 = %v/%v, want exceeds uint32", tooLarge, err)
	}
}
