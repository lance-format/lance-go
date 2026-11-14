package lance

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/lance-format/lance-go/index"
)

func validateCString(value string) error {
	if strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("string contains an embedded NUL byte")
	}
	return nil
}

func cloneStorageOptions(options map[string]string) (map[string]string, error) {
	copied, err := copyStorageOptions(options)
	if err != nil || len(copied) == 0 {
		return nil, err
	}
	return copied, nil
}

func copyStorageOptions(options map[string]string) (map[string]string, error) {
	if options == nil {
		return nil, nil
	}
	result := make(map[string]string, len(options))
	for key, value := range options {
		if key == "" {
			return nil, fmt.Errorf("storage option key must not be empty")
		}
		if err := validateCString(key); err != nil {
			return nil, fmt.Errorf("storage option key: %w", err)
		}
		if err := validateCString(value); err != nil {
			return nil, fmt.Errorf("storage option %q: %w", key, err)
		}
		result[key] = value
	}
	return result, nil
}

type openConfig struct {
	version                uint64
	storageOptions         map[string]string
	storageOptionsSet      bool
	storageOptionsAccessor *StorageOptionsAccessor
	session                *Session
	commitLock             *CommitLock
	baseStoreParams        map[string]BaseStoreParams
}

// OpenOption configures Open.
type OpenOption func(*openConfig) error

// WithVersion selects a dataset version. Zero selects the latest version.
func WithVersion(version uint64) OpenOption {
	return func(config *openConfig) error {
		config.version = version
		return nil
	}
}

// WithStorageOptions passes object-store options to Lance.
func WithStorageOptions(options map[string]string) OpenOption {
	return func(config *openConfig) error {
		copied, err := cloneStorageOptions(options)
		if err != nil {
			return err
		}
		config.storageOptions = copied
		config.storageOptionsSet = options != nil
		return nil
	}
}

// WithStorageOptionsAccessor supplies a reusable dynamic storage-options
// accessor. It is mutually exclusive with WithStorageOptions and is safe to
// close after Open returns; native datasets retain their own reference.
func WithStorageOptionsAccessor(accessor *StorageOptionsAccessor) OpenOption {
	return func(config *openConfig) error {
		if accessor == nil {
			return fmt.Errorf("storage options accessor must not be nil")
		}
		config.storageOptionsAccessor = accessor
		return nil
	}
}

// WithBaseStoreParams binds per-base storage parameters to exact base-path
// keys from the dataset manifest. Use the path strings returned by
// BasePaths, normally BasePaths()[id].Path; IDs, names and URI forms are not
// translated or normalized.
//
// A nil map preserves the ordinary Open path. A non-nil map, including an
// empty one, uses the optional native per-base open entry point and preserves
// the distinction between native None and Some(empty). The option snapshots
// the map and every pointed scalar value before Open starts. Storage option
// accessors are pinned for the native call and may be closed after Open
// returns; the native dataset retains its own reference.
// Dataset-level storage options, WithStorageOptionsAccessor, WithVersion and
// WithSession still apply; an exact per-base binding takes precedence over
// the dataset-level storage options for that base.
// Bindings are runtime-only; native does not persist them, so a later reopen
// must supply them again.
//
// WithBaseStoreParams cannot be combined with WithOpenCommitLock because the
// per-base open entry has no commit-lock parameter. OpenFromNamespace also
// rejects a non-nil map because namespace opens do not accept per-base store
// parameters. Libraries that predate the optional symbol return
// ErrNotSupported naming lance_dataset_open_with_base_store_params.
func WithBaseStoreParams(params map[string]BaseStoreParams) OpenOption {
	return func(config *openConfig) error {
		copied, err := cloneBaseStoreParams(params)
		if err != nil {
			return err
		}
		config.baseStoreParams = copied
		return nil
	}
}

// WithSession shares a session's caches with the dataset.
func WithSession(session *Session) OpenOption {
	return func(config *openConfig) error {
		if session == nil {
			return fmt.Errorf("session must not be nil")
		}
		config.session = session
		return nil
	}
}

// WriteMode selects create, append or overwrite behavior.
type WriteMode int32

const (
	WriteModeCreate    WriteMode = 0
	WriteModeAppend    WriteMode = 1
	WriteModeOverwrite WriteMode = 2
)

// WriteParams controls file layout and stable row IDs. Zero values select
// native Lance defaults.
//
// The native-only fields below are routed through the owned native write
// options handle. Entry points that still pass the older static write options
// structure, including namespace writes and fragment creation, reject
// EnableV2ManifestPaths, InitialBases, TargetBases, TargetBaseNamesOrPaths,
// TargetAllBases, BaseStoreParams, AllowExternalBlobOutsideBases,
// ExternalBlobMode, BlobPackFileSizeThreshold, FileWriterOptions and Progress
// instead of silently dropping them. A commit lock is supplied separately
// with WithWriteCommitLock and does not change the presence semantics of these
// fields.
type WriteParams struct {
	MaxRowsPerFile     uint64
	MaxRowsPerGroup    uint64
	MaxBytesPerFile    uint64
	DataStorageVersion string
	EnableStableRowIDs bool

	EnableV2ManifestPaths         *bool
	InitialBases                  []BasePath
	TargetBases                   []uint32
	TargetBaseNamesOrPaths        []string
	TargetAllBases                *bool
	BaseStoreParams               map[string]BaseStoreParams
	AllowExternalBlobOutsideBases bool
	ExternalBlobMode              ExternalBlobMode
	BlobPackFileSizeThreshold     *uint64
	FileWriterOptions             *DatasetFileWriterOptions
	Progress                      *WriteProgress
}

// ExternalBlobMode selects how external blob references are written.
type ExternalBlobMode int32

const (
	// ExternalBlobModeReference stores external references without uploading.
	ExternalBlobModeReference ExternalBlobMode = 0
	// ExternalBlobModeIngest uploads referenced payloads into managed storage.
	ExternalBlobModeIngest ExternalBlobMode = 1
)

// BaseStoreParams configures representable storage parameters for one base
// path. It is used by WriteParams.BaseStoreParams and WithBaseStoreParams.
// Pointer fields preserve native absence versus explicit false or zero.
type BaseStoreParams struct {
	StorageOptionsAccessor     *StorageOptionsAccessor
	BlockSize                  *uint64
	S3CredentialsRefreshOffset *time.Duration
	UseConstantSizeUploadParts *bool
	ListIsLexicallyOrdered     *bool
}

// DatasetFileWriterOptions controls representable dataset file-writer settings.
type DatasetFileWriterOptions struct {
	DataCacheBytes    *uint64
	MaxPageBytes      *uint64
	KeepOriginalArray *bool
}

// WriteFragmentProgress is one begin- or complete-fragment progress event.
type WriteFragmentProgress struct {
	FragmentID   uint64
	PhysicalRows *uint64
	FileCount    uint64
}

// WriteStats is a native cumulative write-statistics snapshot.
type WriteStats struct {
	BytesWritten uint64
	RowsWritten  uint64
	FilesWritten uint32
}

// WriteProgress receives native write callbacks. Callbacks run on native worker
// threads and must be safe for concurrent use.
type WriteProgress struct {
	BeginFragment    func(WriteFragmentProgress) error
	CompleteFragment func(WriteFragmentProgress) error
	WriteStats       func(WriteStats)
}

// AutoCleanupParams configures native old-version cleanup.
type AutoCleanupParams struct {
	Interval  uint64
	OlderThan time.Duration
}

type writeConfig struct {
	mode                     WriteMode
	params                   WriteParams
	schema                   *NativeSchema
	autoCleanup              *AutoCleanupParams
	skipAutoCleanup          bool
	storageOptions           map[string]string
	storageOptionsSet        bool
	storageOptionsAccessor   *StorageOptionsAccessor
	session                  *Session
	commitLock               *CommitLock
	transactionProperties    map[string]string
	hasTransactionProperties bool
	branch                   string
}

// WriteOption configures Write, Create, Append and Overwrite.
type WriteOption func(*writeConfig) error

// WithWriteMode selects the write mode.
func WithWriteMode(mode WriteMode) WriteOption {
	return func(config *writeConfig) error {
		if mode < WriteModeCreate || mode > WriteModeOverwrite {
			return fmt.Errorf("invalid write mode %d", mode)
		}
		config.mode = mode
		return nil
	}
}

// WithWriteParams sets native write parameters.
func WithWriteParams(params WriteParams) WriteOption {
	return func(config *writeConfig) error {
		copied, err := cloneWriteParams(params)
		if err != nil {
			return err
		}
		config.params = copied
		return nil
	}
}

func cloneWriteParams(params WriteParams) (WriteParams, error) {
	copied := params
	if err := validateCString(params.DataStorageVersion); err != nil {
		return WriteParams{}, fmt.Errorf("data storage version: %w", err)
	}

	if params.EnableV2ManifestPaths != nil {
		value := *params.EnableV2ManifestPaths
		copied.EnableV2ManifestPaths = &value
	}
	if params.InitialBases != nil {
		copied.InitialBases = make([]BasePath, len(params.InitialBases))
		for i, base := range params.InitialBases {
			if err := validateCString(base.Path); err != nil {
				return WriteParams{}, fmt.Errorf("initial base %d path: %w", i, err)
			}
			copied.InitialBases[i] = base
			if base.Name != nil {
				if err := validateCString(*base.Name); err != nil {
					return WriteParams{}, fmt.Errorf("initial base %d name: %w", i, err)
				}
				name := *base.Name
				copied.InitialBases[i].Name = &name
			}
		}
	}
	if params.TargetBases != nil {
		copied.TargetBases = make([]uint32, len(params.TargetBases))
		copy(copied.TargetBases, params.TargetBases)
	}
	if params.TargetBaseNamesOrPaths != nil {
		copied.TargetBaseNamesOrPaths = make([]string, len(params.TargetBaseNamesOrPaths))
		copy(copied.TargetBaseNamesOrPaths, params.TargetBaseNamesOrPaths)
		for i, target := range copied.TargetBaseNamesOrPaths {
			if err := validateCString(target); err != nil {
				return WriteParams{}, fmt.Errorf("target base %d: %w", i, err)
			}
		}
	}
	if params.TargetAllBases != nil {
		value := *params.TargetAllBases
		copied.TargetAllBases = &value
	}
	if params.BaseStoreParams != nil {
		var err error
		copied.BaseStoreParams, err = cloneBaseStoreParams(params.BaseStoreParams)
		if err != nil {
			return WriteParams{}, err
		}
	}
	if params.BlobPackFileSizeThreshold != nil {
		value := *params.BlobPackFileSizeThreshold
		copied.BlobPackFileSizeThreshold = &value
	}
	if params.FileWriterOptions != nil {
		fileOptions := *params.FileWriterOptions
		if params.FileWriterOptions.DataCacheBytes != nil {
			value := *params.FileWriterOptions.DataCacheBytes
			fileOptions.DataCacheBytes = &value
		}
		if params.FileWriterOptions.MaxPageBytes != nil {
			value := *params.FileWriterOptions.MaxPageBytes
			fileOptions.MaxPageBytes = &value
		}
		if params.FileWriterOptions.KeepOriginalArray != nil {
			value := *params.FileWriterOptions.KeepOriginalArray
			fileOptions.KeepOriginalArray = &value
		}
		copied.FileWriterOptions = &fileOptions
	}
	if params.Progress != nil {
		progress := *params.Progress
		copied.Progress = &progress
	}
	return copied, nil
}

// cloneBaseStoreParams snapshots the map and scalar pointers shared by write
// parameters and WithBaseStoreParams.
func cloneBaseStoreParams(params map[string]BaseStoreParams) (map[string]BaseStoreParams, error) {
	if params == nil {
		return nil, nil
	}
	copied := make(map[string]BaseStoreParams, len(params))
	for path, base := range params {
		if err := validateCString(path); err != nil {
			return nil, fmt.Errorf("base store params path: %w", err)
		}
		clone := base
		if base.BlockSize != nil {
			value := *base.BlockSize
			clone.BlockSize = &value
		}
		if base.S3CredentialsRefreshOffset != nil {
			if *base.S3CredentialsRefreshOffset < 0 {
				return nil, fmt.Errorf("base store params %q: S3 credentials refresh offset must not be negative", path)
			}
			value := *base.S3CredentialsRefreshOffset
			clone.S3CredentialsRefreshOffset = &value
		}
		if base.UseConstantSizeUploadParts != nil {
			value := *base.UseConstantSizeUploadParts
			clone.UseConstantSizeUploadParts = &value
		}
		if base.ListIsLexicallyOrdered != nil {
			value := *base.ListIsLexicallyOrdered
			clone.ListIsLexicallyOrdered = &value
		}
		copied[path] = clone
	}
	return copied, nil
}

// hasNativeOptions reports whether params requires the native-options entry
// point. Explicit empty slices and maps still count as present.
func (params WriteParams) hasNativeOptions() bool {
	return params.firstNativeOptionField() != ""
}

func (params WriteParams) firstNativeOptionField() string {
	switch {
	case params.EnableV2ManifestPaths != nil:
		return "EnableV2ManifestPaths"
	case params.InitialBases != nil:
		return "InitialBases"
	case params.TargetBases != nil:
		return "TargetBases"
	case params.TargetBaseNamesOrPaths != nil:
		return "TargetBaseNamesOrPaths"
	case params.TargetAllBases != nil:
		return "TargetAllBases"
	case params.BaseStoreParams != nil:
		return "BaseStoreParams"
	case params.AllowExternalBlobOutsideBases:
		return "AllowExternalBlobOutsideBases"
	case params.ExternalBlobMode != ExternalBlobModeReference:
		return "ExternalBlobMode"
	case params.BlobPackFileSizeThreshold != nil:
		return "BlobPackFileSizeThreshold"
	case params.FileWriterOptions != nil:
		return "FileWriterOptions"
	case params.Progress != nil:
		return "Progress"
	default:
		return ""
	}
}

// WithAutoCleanup configures old-version cleanup for create writes.
func WithAutoCleanup(params AutoCleanupParams) WriteOption {
	return func(config *writeConfig) error {
		if params.OlderThan < 0 {
			return fmt.Errorf("auto-cleanup older-than duration must not be negative")
		}
		copied := params
		config.autoCleanup = &copied
		return nil
	}
}

// WithSkipAutoCleanup disables native old-version cleanup.
func WithSkipAutoCleanup(skip bool) WriteOption {
	return func(config *writeConfig) error {
		config.skipAutoCleanup = skip
		return nil
	}
}

// WithWriteSession pins the write to a session. It is valid for ordinary
// writes, WriteUncommitted, WriteToNamespace, CreateFragment and WriteFragments.
func WithWriteSession(session *Session) WriteOption {
	return func(config *writeConfig) error {
		if session == nil {
			return fmt.Errorf("session must not be nil")
		}
		config.session = session
		return nil
	}
}

// WithWriteBranch selects a table branch for WriteToNamespace. It is invalid
// for URI writes.
func WithWriteBranch(branch string) WriteOption {
	return func(config *writeConfig) error {
		if err := validateCString(branch); err != nil {
			return fmt.Errorf("branch: %w", err)
		}
		config.branch = branch
		return nil
	}
}

// WithWriteTransactionProperties attaches properties to the transaction
// created by an ordinary write or WriteUncommitted. Passing nil records a
// present, empty map.
func WithWriteTransactionProperties(properties map[string]string) WriteOption {
	return func(config *writeConfig) error {
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
		config.transactionProperties = copied
		config.hasTransactionProperties = true
		return nil
	}
}

// WithFragmentSchema sets the schema used by CreateFragment and WriteFragments.
// The native schema preserves field IDs that are not represented in an Arrow
// schema. It is invalid for Write, Create, Append, Overwrite and
// WriteUncommitted or WriteToNamespace.
func WithFragmentSchema(schema *NativeSchema) WriteOption {
	return func(config *writeConfig) error {
		if schema == nil {
			return fmt.Errorf("schema must not be nil")
		}
		config.schema = schema
		return nil
	}
}

// WithWriteStorageOptions passes object-store options to the write.
func WithWriteStorageOptions(options map[string]string) WriteOption {
	return func(config *writeConfig) error {
		copied, err := cloneStorageOptions(options)
		if err != nil {
			return err
		}
		config.storageOptions = copied
		config.storageOptionsSet = options != nil
		return nil
	}
}

// WithWriteStorageOptionsAccessor supplies a reusable dynamic accessor for a
// dataset write. It is mutually exclusive with WithWriteStorageOptions.
func WithWriteStorageOptionsAccessor(accessor *StorageOptionsAccessor) WriteOption {
	return func(config *writeConfig) error {
		if accessor == nil {
			return fmt.Errorf("storage options accessor must not be nil")
		}
		config.storageOptionsAccessor = accessor
		return nil
	}
}

// OrderDirection is an ascending or descending sort direction.
type OrderDirection int32

const (
	Ascending  OrderDirection = 0
	Descending OrderDirection = 1
)

// NullOrder places nulls first or last.
type NullOrder int32

const (
	NullsLast  NullOrder = 0
	NullsFirst NullOrder = 1
)

// Ordering is one scanner sort key.
type Ordering struct {
	Column    string
	Direction OrderDirection
	NullOrder NullOrder
}

// ProjectionColumn selects an output column by SQL expression.
type ProjectionColumn struct {
	Name       string
	Expression string
}

// BlobHandling controls how blob columns are represented by a scan.
type BlobHandling int32

const (
	// BlobHandlingDescriptors returns blob columns as descriptors and leaves
	// other binary columns inline. This is the native default.
	BlobHandlingDescriptors BlobHandling = 0
	// BlobHandlingAllBinary materializes every blob column as binary payloads.
	BlobHandlingAllBinary BlobHandling = 1
	// BlobHandlingAllDescriptions returns every string or binary column with
	// blob metadata as a descriptor.
	BlobHandlingAllDescriptions BlobHandling = 2
)

// MaterializationStyle controls when projected columns are loaded.
type MaterializationStyle int32

const (
	// MaterializationHeuristic lets the native planner choose per column.
	MaterializationHeuristic MaterializationStyle = 0
	// MaterializationAllLate loads projected columns as late as possible.
	MaterializationAllLate MaterializationStyle = 1
	// MaterializationAllEarly loads projected columns as early as possible.
	MaterializationAllEarly MaterializationStyle = 2
	// MaterializationAllEarlyExcept loads all projected columns early except
	// the columns named in WithMaterializationStyle.
	MaterializationAllEarlyExcept MaterializationStyle = 3
)

type scanConfig struct {
	columns              []string
	filter               *string
	limit                *int64
	offset               *int64
	batchSize            *int64
	batchSizeBytes       *uint64
	ioBufferSize         *uint64
	batchReadahead       *uint64
	fragmentReadahead    *uint64
	targetParallelism    *uint64
	scanInOrder          *bool
	useScalarIndex       *bool
	strictBatchSize      *bool
	useStats             *bool
	includeDeletedRows   *bool
	blobHandling         *BlobHandling
	materializationStyle *MaterializationStyle
	materializationEarly []string
	fragmentIDs          []uint64
	fragmentIDsSet       bool
	disableScoring       bool
	scalarIndexSegment   *index.UUID
	projection           []ffiProjection
	orderBy              []ffiOrdering
	rowID                *bool
	rowAddress           *bool
	substraitFilter      []byte
	substraitAggregate   []byte
	additionalSQLFilters []string
	statistics           func(ScanStatistics)
	fts                  ftsScanConfig
	vector               vectorConfig
}

type ffiProjection struct {
	name       string
	expression string
}

type ffiOrdering struct {
	column    string
	direction OrderDirection
	nullOrder NullOrder
}

// ScanOption configures a scanner.
type ScanOption func(*scanConfig) error

// WithColumns selects columns by name.
func WithColumns(columns ...string) ScanOption {
	return func(config *scanConfig) error {
		for _, column := range columns {
			if column == "" {
				return fmt.Errorf("column name must not be empty")
			}
			if err := validateCString(column); err != nil {
				return fmt.Errorf("column name: %w", err)
			}
		}
		config.columns = append([]string(nil), columns...)
		return nil
	}
}

// WithProjection selects output columns by SQL expression.
func WithProjection(columns ...ProjectionColumn) ScanOption {
	return func(config *scanConfig) error {
		projection := make([]ffiProjection, 0, len(columns))
		for _, column := range columns {
			if column.Name == "" {
				return fmt.Errorf("projection output name must not be empty")
			}
			if column.Expression == "" {
				return fmt.Errorf("projection expression must not be empty")
			}
			if err := validateCString(column.Name); err != nil {
				return fmt.Errorf("projection output name: %w", err)
			}
			if err := validateCString(column.Expression); err != nil {
				return fmt.Errorf("projection expression: %w", err)
			}
			projection = append(projection, ffiProjection{name: column.Name, expression: column.Expression})
		}
		config.projection = projection
		return nil
	}
}

// WithSQLFilter sets the scanner SQL filter.
func WithSQLFilter(filter string) ScanOption {
	return func(config *scanConfig) error {
		if err := validateCString(filter); err != nil {
			return fmt.Errorf("SQL filter: %w", err)
		}
		copied := filter
		config.filter = &copied
		return nil
	}
}

// WithAdditionalSQLFilter combines another SQL predicate with the filter.
func WithAdditionalSQLFilter(filter string) ScanOption {
	return func(config *scanConfig) error {
		if err := validateCString(filter); err != nil {
			return fmt.Errorf("additional SQL filter: %w", err)
		}
		config.additionalSQLFilters = append(config.additionalSQLFilters, filter)
		return nil
	}
}

// WithSubstraitFilter sets a serialized Substrait filter.
func WithSubstraitFilter(data []byte) ScanOption {
	return func(config *scanConfig) error {
		config.substraitFilter = append([]byte(nil), data...)
		return nil
	}
}

// WithSubstraitAggregate sets a serialized Substrait aggregate expression.
func WithSubstraitAggregate(data []byte) ScanOption {
	return func(config *scanConfig) error {
		config.substraitAggregate = append([]byte(nil), data...)
		return nil
	}
}

// WithOrderBy appends native sort keys.
func WithOrderBy(orderings ...Ordering) ScanOption {
	return func(config *scanConfig) error {
		for _, ordering := range orderings {
			if ordering.Column == "" {
				return fmt.Errorf("order-by column must not be empty")
			}
			if err := validateCString(ordering.Column); err != nil {
				return fmt.Errorf("order-by column: %w", err)
			}
			if ordering.Direction != Ascending && ordering.Direction != Descending {
				return fmt.Errorf("invalid order direction %d", ordering.Direction)
			}
			if ordering.NullOrder != NullsLast && ordering.NullOrder != NullsFirst {
				return fmt.Errorf("invalid null order %d", ordering.NullOrder)
			}
			config.orderBy = append(config.orderBy, ffiOrdering{
				column:    ordering.Column,
				direction: ordering.Direction,
				nullOrder: ordering.NullOrder,
			})
		}
		return nil
	}
}

// WithLimit limits the number of returned rows.
func WithLimit(limit int64) ScanOption {
	return func(config *scanConfig) error {
		if limit < 0 {
			return fmt.Errorf("limit must not be negative")
		}
		copied := limit
		config.limit = &copied
		return nil
	}
}

// WithOffset skips leading rows.
func WithOffset(offset int64) ScanOption {
	return func(config *scanConfig) error {
		if offset < 0 {
			return fmt.Errorf("offset must not be negative")
		}
		copied := offset
		config.offset = &copied
		return nil
	}
}

// WithBatchSize sets the target row count per batch.
func WithBatchSize(size int64) ScanOption {
	return func(config *scanConfig) error {
		if size <= 0 {
			return fmt.Errorf("batch size must be positive")
		}
		copied := size
		config.batchSize = &copied
		return nil
	}
}

// WithBatchSizeBytes sets the target byte size per batch.
func WithBatchSizeBytes(size uint64) ScanOption {
	return func(config *scanConfig) error {
		if size == 0 {
			return fmt.Errorf("batch size in bytes must be positive")
		}
		copied := size
		config.batchSizeBytes = &copied
		return nil
	}
}

// WithIOBufferSize sets the native scanner I/O buffer size in bytes.
func WithIOBufferSize(size uint64) ScanOption {
	return func(config *scanConfig) error {
		if size == 0 || size > math.MaxInt64 {
			return fmt.Errorf("I/O buffer size must be in [1, %d]", int64(math.MaxInt64))
		}
		copied := size
		config.ioBufferSize = &copied
		return nil
	}
}

// WithBatchReadahead sets how many batches the native scanner reads ahead.
func WithBatchReadahead(batches uint64) ScanOption {
	return func(config *scanConfig) error {
		if batches == 0 {
			return fmt.Errorf("batch readahead must be positive")
		}
		copied := batches
		config.batchReadahead = &copied
		return nil
	}
}

// WithFragmentReadahead sets how many fragments the native scanner reads
// ahead. It has no effect when WithScanInOrder(true).
func WithFragmentReadahead(fragments uint64) ScanOption {
	return func(config *scanConfig) error {
		if fragments == 0 {
			return fmt.Errorf("fragment readahead must be positive")
		}
		copied := fragments
		config.fragmentReadahead = &copied
		return nil
	}
}

// WithTargetParallelism sets the native scanner's target task parallelism.
func WithTargetParallelism(tasks uint64) ScanOption {
	return func(config *scanConfig) error {
		if tasks == 0 {
			return fmt.Errorf("target parallelism must be positive")
		}
		copied := tasks
		config.targetParallelism = &copied
		return nil
	}
}

// WithScanInOrder requires fragments to be returned in dataset order. It is
// enabled by default.
func WithScanInOrder(enabled bool) ScanOption {
	return func(config *scanConfig) error {
		config.scanInOrder = &enabled
		return nil
	}
}

// WithUseScalarIndex controls whether scalar indexes may optimize filters. It
// is enabled by default and is independent of WithVectorUseIndex.
func WithUseScalarIndex(enabled bool) ScanOption {
	return func(config *scanConfig) error {
		config.useScalarIndex = &enabled
		return nil
	}
}

// WithStrictBatchSize requires every batch except the last to contain exactly
// the configured batch size. It cannot be combined with WithBatchSizeBytes.
func WithStrictBatchSize(enabled bool) ScanOption {
	return func(config *scanConfig) error {
		config.strictBatchSize = &enabled
		return nil
	}
}

// WithUseStats controls whether file statistics may optimize the scan. It is
// enabled by default.
func WithUseStats(enabled bool) ScanOption {
	return func(config *scanConfig) error {
		config.useStats = &enabled
		return nil
	}
}

// WithIncludeDeletedRows includes rows deleted from the scanned snapshot.
// WithRowID(true) is required. Deleted rows have a null _rowid. For filtered
// scans, use WithUseScalarIndex(false) to prevent index plans that omit
// tombstoned rows. Explicit scalar index segments are incompatible.
func WithIncludeDeletedRows(enabled bool) ScanOption {
	return func(config *scanConfig) error {
		config.includeDeletedRows = &enabled
		return nil
	}
}

// WithBlobHandling selects how blob columns are represented.
func WithBlobHandling(handling BlobHandling) ScanOption {
	return func(config *scanConfig) error {
		switch handling {
		case BlobHandlingDescriptors, BlobHandlingAllBinary, BlobHandlingAllDescriptions:
		default:
			return fmt.Errorf("invalid blob handling %d", handling)
		}
		copied := handling
		config.blobHandling = &copied
		return nil
	}
}

// WithMaterializationStyle selects when projected columns are loaded.
// earlyExcept names Lance schema paths to load late when style is
// MaterializationAllEarlyExcept. An empty exception list with that style loads
// every projected column early.
func WithMaterializationStyle(style MaterializationStyle, earlyExcept ...string) ScanOption {
	early := append([]string(nil), earlyExcept...)
	return func(config *scanConfig) error {
		switch style {
		case MaterializationHeuristic, MaterializationAllLate, MaterializationAllEarly, MaterializationAllEarlyExcept:
		default:
			return fmt.Errorf("invalid materialization style %d", style)
		}
		if style != MaterializationAllEarlyExcept && len(early) > 0 {
			return fmt.Errorf("materialization style %d does not accept early-except columns", style)
		}
		columns := make([]string, len(early))
		for i, column := range early {
			if column == "" {
				return fmt.Errorf("early-except column must not be empty")
			}
			if err := validateCString(column); err != nil {
				return fmt.Errorf("early-except column: %w", err)
			}
			columns[i] = column
		}
		copied := style
		config.materializationStyle = &copied
		config.materializationEarly = columns
		return nil
	}
}

// WithFragmentIDs restricts the scan to the given fragment IDs. Calling it
// with no IDs selects no fragments. It replaces the metadata selection staged
// by Fragment.Scan, including staged deletion metadata.
func WithFragmentIDs(fragmentIDs ...uint64) ScanOption {
	copied := append([]uint64(nil), fragmentIDs...)
	return func(config *scanConfig) error {
		config.fragmentIDs = append([]uint64(nil), copied...)
		config.fragmentIDsSet = true
		return nil
	}
}

// WithoutScoringAutoprojection disables automatic _score and _distance output
// columns for FTS and vector scans. After this option, those columns are
// returned only when explicitly projected. The setting cannot be undone on
// the same scanner.
func WithoutScoringAutoprojection() ScanOption {
	return func(config *scanConfig) error {
		config.disableScoring = true
		return nil
	}
}

// WithScalarIndexSegment restricts a scalar-index search to one index segment.
// The scan must also set at least one explicit fragment ID; those fragments
// define both the segment read domain and the fallback domain for rows not
// covered by the segment.
func WithScalarIndexSegment(segment index.UUID) ScanOption {
	return func(config *scanConfig) error {
		copied := segment
		config.scalarIndexSegment = &copied
		return nil
	}
}

// WithRowID includes Lance row IDs in scanned batches.
func WithRowID(enabled bool) ScanOption {
	return func(config *scanConfig) error {
		config.rowID = &enabled
		return nil
	}
}

// WithRowAddress includes Lance row addresses in scanned batches.
func WithRowAddress(enabled bool) ScanOption {
	return func(config *scanConfig) error {
		config.rowAddress = &enabled
		return nil
	}
}

// WithScanStatistics registers a callback for completed scans.
func WithScanStatistics(callback func(ScanStatistics)) ScanOption {
	return func(config *scanConfig) error {
		config.statistics = callback
		return nil
	}
}
