package lance

import (
	"fmt"
	"strings"
	"time"
)

func validateCString(value string) error {
	if strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("string contains an embedded NUL byte")
	}
	return nil
}

func cloneStorageOptions(options map[string]string) (map[string]string, error) {
	if len(options) == 0 {
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
	version        uint64
	storageOptions map[string]string
	session        *Session
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
type WriteParams struct {
	MaxRowsPerFile     uint64
	MaxRowsPerGroup    uint64
	MaxBytesPerFile    uint64
	DataStorageVersion string
	EnableStableRowIDs bool
}

// AutoCleanupParams configures native old-version cleanup.
type AutoCleanupParams struct {
	Interval  uint64
	OlderThan time.Duration
}

type writeConfig struct {
	mode            WriteMode
	params          WriteParams
	autoCleanup     *AutoCleanupParams
	skipAutoCleanup bool
	storageOptions  map[string]string
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
		if err := validateCString(params.DataStorageVersion); err != nil {
			return fmt.Errorf("data storage version: %w", err)
		}
		config.params = params
		return nil
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

// WithWriteStorageOptions passes object-store options to the write.
func WithWriteStorageOptions(options map[string]string) WriteOption {
	return func(config *writeConfig) error {
		copied, err := cloneStorageOptions(options)
		if err != nil {
			return err
		}
		config.storageOptions = copied
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

type scanConfig struct {
	columns              []string
	filter               *string
	limit                *int64
	offset               *int64
	batchSize            *int64
	batchSizeBytes       *uint64
	projection           []ffiProjection
	orderBy              []ffiOrdering
	rowID                *bool
	rowAddress           *bool
	substraitFilter      []byte
	substraitAggregate   []byte
	additionalSQLFilters []string
	statistics           func(ScanStatistics)
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
