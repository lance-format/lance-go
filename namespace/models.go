// Code generated from lance-namespace-reqwest-client 0.13.0 OpenAPI models; DO NOT EDIT.
// The upstream schema is part of the Lance Namespace specification. Field tags and
// optional semantics must match the native serde models exactly. The Nullable
// helper and models_nullable.go are maintained manually after generation.

package namespace

import (
	"bytes"
	"encoding/json"
	"reflect"
)

// Nullable represents a field that distinguishes an absent value (nil pointer),
// an explicit JSON null (non-nil Nullable with a nil Value), and a concrete value.
// It mirrors Rust's Option<Option<T>> with serde double_option.
type Nullable[T any] struct {
	Value *T
}

// MarshalJSON preserves an explicit null instead of omitting the field.
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}
	// A nil Go slice or map represents an empty Rust Vec or HashMap in this
	// model layer; encoding it as JSON null would change Option<Option<T>>.
	value := reflect.ValueOf(*n.Value)
	switch value.Kind() {
	case reflect.Slice:
		if value.IsNil() {
			return []byte("[]"), nil
		}
	case reflect.Map:
		if value.IsNil() {
			return []byte("{}"), nil
		}
	}
	return json.Marshal(*n.Value)
}

// UnmarshalJSON records both JSON null and a concrete value as present.
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		n.Value = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	n.Value = &value
	return nil
}

// AddColumnsEntry mirrors the Lance Namespace `AddColumnsEntry` schema.
type AddColumnsEntry struct {
	Name          string                           `json:"name"`
	Expression    *Nullable[string]                `json:"expression,omitempty"`
	Computed      *Nullable[string]                `json:"computed,omitempty"`
	VirtualColumn *Nullable[AddVirtualColumnEntry] `json:"virtual_column,omitempty"`
}

// AddVirtualColumnEntry mirrors the Lance Namespace `AddVirtualColumnEntry` schema.
type AddVirtualColumnEntry struct {
	InputColumns     []string                      `json:"input_columns"`
	Outputs          []AddVirtualColumnOutputEntry `json:"outputs"`
	Image            string                        `json:"image"`
	UDF              string                        `json:"udf"`
	UDFName          string                        `json:"udf_name"`
	UDFVersion       string                        `json:"udf_version"`
	UDFBackend       *Nullable[string]             `json:"udf_backend,omitempty"`
	AutoBackfill     *Nullable[bool]               `json:"auto_backfill,omitempty"`
	Manifest         *Nullable[string]             `json:"manifest,omitempty"`
	ManifestChecksum *Nullable[string]             `json:"manifest_checksum,omitempty"`
	FieldMetadata    *map[string]string            `json:"field_metadata,omitempty"`
}

// AddVirtualColumnOutputEntry mirrors the Lance Namespace `AddVirtualColumnOutputEntry` schema.
type AddVirtualColumnOutputEntry struct {
	Column      string             `json:"column"`
	StructField string             `json:"struct_field"`
	DataType    any                `json:"data_type"`
	Nullable    bool               `json:"nullable"`
	Metadata    *map[string]string `json:"metadata,omitempty"`
}

// AlterColumnsEntry mirrors the Lance Namespace `AlterColumnsEntry` schema.
type AlterColumnsEntry struct {
	Path          string                             `json:"path"`
	DataType      *any                               `json:"data_type,omitempty"`
	Rename        *Nullable[string]                  `json:"rename,omitempty"`
	Nullable      *Nullable[bool]                    `json:"nullable,omitempty"`
	VirtualColumn *Nullable[AlterVirtualColumnEntry] `json:"virtual_column,omitempty"`
}

// AlterTableAddColumnsRequest mirrors the Lance Namespace `AlterTableAddColumnsRequest` schema.
type AlterTableAddColumnsRequest struct {
	Identity   *Identity          `json:"identity,omitempty"`
	Context    *map[string]string `json:"context,omitempty"`
	ID         *[]string          `json:"id,omitempty"`
	Branch     *string            `json:"branch,omitempty"`
	NewColumns []AddColumnsEntry  `json:"new_columns"`
}

// AlterTableAddColumnsResponse mirrors the Lance Namespace `AlterTableAddColumnsResponse` schema.
type AlterTableAddColumnsResponse struct {
	Context *map[string]string `json:"context,omitempty"`
	Version int64              `json:"version"`
}

// AlterTableAlterColumnsRequest mirrors the Lance Namespace `AlterTableAlterColumnsRequest` schema.
type AlterTableAlterColumnsRequest struct {
	Identity    *Identity           `json:"identity,omitempty"`
	Context     *map[string]string  `json:"context,omitempty"`
	ID          *[]string           `json:"id,omitempty"`
	Branch      *string             `json:"branch,omitempty"`
	Alterations []AlterColumnsEntry `json:"alterations"`
}

// AlterTableAlterColumnsResponse mirrors the Lance Namespace `AlterTableAlterColumnsResponse` schema.
type AlterTableAlterColumnsResponse struct {
	Context *map[string]string `json:"context,omitempty"`
	Version int64              `json:"version"`
}

// AlterTableBackfillColumnsRequest mirrors the Lance Namespace `AlterTableBackfillColumnsRequest` schema.
type AlterTableBackfillColumnsRequest struct {
	Identity                            *Identity          `json:"identity,omitempty"`
	Context                             *map[string]string `json:"context,omitempty"`
	ID                                  *[]string          `json:"id,omitempty"`
	Branch                              *string            `json:"branch,omitempty"`
	Column                              string             `json:"column"`
	Where                               *Nullable[string]  `json:"where,omitempty"`
	Concurrency                         *Nullable[int32]   `json:"concurrency,omitempty"`
	IntraApplierConcurrency             *Nullable[int32]   `json:"intra_applier_concurrency,omitempty"`
	MinCheckpointSize                   *Nullable[int32]   `json:"min_checkpoint_size,omitempty"`
	MaxCheckpointSize                   *Nullable[int32]   `json:"max_checkpoint_size,omitempty"`
	BatchCheckpointFlushIntervalSeconds *Nullable[float64] `json:"batch_checkpoint_flush_interval_seconds,omitempty"`
	ReadVersion                         *Nullable[int32]   `json:"read_version,omitempty"`
	TaskSize                            *Nullable[int32]   `json:"task_size,omitempty"`
	NumFrags                            *Nullable[int32]   `json:"num_frags,omitempty"`
	CheckpointSize                      *Nullable[int32]   `json:"checkpoint_size,omitempty"`
	CommitGranularity                   *Nullable[int32]   `json:"commit_granularity,omitempty"`
	Cluster                             *Nullable[string]  `json:"cluster,omitempty"`
	Manifest                            *Nullable[string]  `json:"manifest,omitempty"`
}

// AlterTableBackfillColumnsResponse mirrors the Lance Namespace `AlterTableBackfillColumnsResponse` schema.
type AlterTableBackfillColumnsResponse struct {
	Context *map[string]string `json:"context,omitempty"`
	JobID   string             `json:"job_id"`
}

// AlterTableDropColumnsRequest mirrors the Lance Namespace `AlterTableDropColumnsRequest` schema.
type AlterTableDropColumnsRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Branch   *string            `json:"branch,omitempty"`
	Columns  []string           `json:"columns"`
}

// AlterTableDropColumnsResponse mirrors the Lance Namespace `AlterTableDropColumnsResponse` schema.
type AlterTableDropColumnsResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
	Version       int64              `json:"version"`
}

// AlterTransactionAction mirrors the Lance Namespace `AlterTransactionAction` schema.
type AlterTransactionAction struct {
	SetStatusAction     *AlterTransactionSetStatus     `json:"setStatusAction,omitempty"`
	SetPropertyAction   *AlterTransactionSetProperty   `json:"setPropertyAction,omitempty"`
	UnsetPropertyAction *AlterTransactionUnsetProperty `json:"unsetPropertyAction,omitempty"`
}

// AlterTransactionRequest mirrors the Lance Namespace `AlterTransactionRequest` schema.
type AlterTransactionRequest struct {
	Identity *Identity                `json:"identity,omitempty"`
	Context  *map[string]string       `json:"context,omitempty"`
	ID       *[]string                `json:"id,omitempty"`
	Actions  []AlterTransactionAction `json:"actions"`
}

// AlterTransactionResponse mirrors the Lance Namespace `AlterTransactionResponse` schema.
type AlterTransactionResponse struct {
	Context    *map[string]string `json:"context,omitempty"`
	Status     string             `json:"status"`
	Properties *map[string]string `json:"properties,omitempty"`
}

// AlterTransactionSetProperty mirrors the Lance Namespace `AlterTransactionSetProperty` schema.
type AlterTransactionSetProperty struct {
	Key   *string `json:"key,omitempty"`
	Value *string `json:"value,omitempty"`
	Mode  *string `json:"mode,omitempty"`
}

// AlterTransactionSetStatus mirrors the Lance Namespace `AlterTransactionSetStatus` schema.
type AlterTransactionSetStatus struct {
	Status *string `json:"status,omitempty"`
}

// AlterTransactionUnsetProperty mirrors the Lance Namespace `AlterTransactionUnsetProperty` schema.
type AlterTransactionUnsetProperty struct {
	Key  *string `json:"key,omitempty"`
	Mode *string `json:"mode,omitempty"`
}

// AlterVirtualColumnEntry mirrors the Lance Namespace `AlterVirtualColumnEntry` schema.
type AlterVirtualColumnEntry struct {
	InputColumns     *Nullable[[]string] `json:"input_columns,omitempty"`
	Image            *Nullable[string]   `json:"image,omitempty"`
	UDF              *Nullable[string]   `json:"udf,omitempty"`
	UDFName          *Nullable[string]   `json:"udf_name,omitempty"`
	UDFVersion       *Nullable[string]   `json:"udf_version,omitempty"`
	UDFBackend       *Nullable[string]   `json:"udf_backend,omitempty"`
	AutoBackfill     *Nullable[bool]     `json:"auto_backfill,omitempty"`
	Manifest         *Nullable[string]   `json:"manifest,omitempty"`
	ManifestChecksum *Nullable[string]   `json:"manifest_checksum,omitempty"`
	FieldMetadata    *map[string]string  `json:"field_metadata,omitempty"`
}

// AnalyzeTableQueryPlanRequest mirrors the Lance Namespace `AnalyzeTableQueryPlanRequest` schema.
type AnalyzeTableQueryPlanRequest struct {
	Identity          *Identity                       `json:"identity,omitempty"`
	Context           *map[string]string              `json:"context,omitempty"`
	ID                *[]string                       `json:"id,omitempty"`
	Branch            *string                         `json:"branch,omitempty"`
	BypassVectorIndex *bool                           `json:"bypass_vector_index,omitempty"`
	Columns           *QueryTableRequestColumns       `json:"columns,omitempty"`
	DistanceType      *string                         `json:"distance_type,omitempty"`
	Ef                *int32                          `json:"ef,omitempty"`
	FastSearch        *bool                           `json:"fast_search,omitempty"`
	Filter            *string                         `json:"filter,omitempty"`
	FullTextQuery     *QueryTableRequestFullTextQuery `json:"full_text_query,omitempty"`
	K                 int32                           `json:"k"`
	LowerBound        *float32                        `json:"lower_bound,omitempty"`
	MaximumNprobes    *int32                          `json:"maximum_nprobes,omitempty"`
	MinimumNprobes    *int32                          `json:"minimum_nprobes,omitempty"`
	Nprobes           *int32                          `json:"nprobes,omitempty"`
	Offset            *int32                          `json:"offset,omitempty"`
	Prefilter         *bool                           `json:"prefilter,omitempty"`
	RefineFactor      *int32                          `json:"refine_factor,omitempty"`
	UpperBound        *float32                        `json:"upper_bound,omitempty"`
	Vector            QueryTableRequestVector         `json:"vector"`
	VectorColumn      *string                         `json:"vector_column,omitempty"`
	Version           *int64                          `json:"version,omitempty"`
	WithRowID         *bool                           `json:"with_row_id,omitempty"`
}

// AnalyzeTableQueryPlanResponse mirrors the Lance Namespace `AnalyzeTableQueryPlanResponse` schema.
type AnalyzeTableQueryPlanResponse struct {
	Context  *map[string]string `json:"context,omitempty"`
	Analysis string             `json:"analysis"`
}

// BatchCommitTablesRequest mirrors the Lance Namespace `BatchCommitTablesRequest` schema.
type BatchCommitTablesRequest struct {
	Identity   *Identity              `json:"identity,omitempty"`
	Context    *map[string]string     `json:"context,omitempty"`
	Operations []CommitTableOperation `json:"operations"`
}

// BatchCommitTablesResponse mirrors the Lance Namespace `BatchCommitTablesResponse` schema.
type BatchCommitTablesResponse struct {
	Context       *map[string]string  `json:"context,omitempty"`
	TransactionID *string             `json:"transaction_id,omitempty"`
	Results       []CommitTableResult `json:"results"`
}

// BatchCreateTableVersionsRequest mirrors the Lance Namespace `BatchCreateTableVersionsRequest` schema.
type BatchCreateTableVersionsRequest struct {
	Identity *Identity                 `json:"identity,omitempty"`
	Context  *map[string]string        `json:"context,omitempty"`
	Entries  []CreateTableVersionEntry `json:"entries"`
}

// BatchCreateTableVersionsResponse mirrors the Lance Namespace `BatchCreateTableVersionsResponse` schema.
type BatchCreateTableVersionsResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
	Versions      []TableVersion     `json:"versions"`
}

// BatchDeleteTableVersionsRequest mirrors the Lance Namespace `BatchDeleteTableVersionsRequest` schema.
type BatchDeleteTableVersionsRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Branch   *string            `json:"branch,omitempty"`
	Ranges   []VersionRange     `json:"ranges"`
}

// BatchDeleteTableVersionsResponse mirrors the Lance Namespace `BatchDeleteTableVersionsResponse` schema.
type BatchDeleteTableVersionsResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	DeletedCount  *int64             `json:"deleted_count,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// BooleanQuery mirrors the Lance Namespace `BooleanQuery` schema.
type BooleanQuery struct {
	Must    []FtsQuery `json:"must"`
	MustNot []FtsQuery `json:"must_not"`
	Should  []FtsQuery `json:"should"`
}

// BoostQuery mirrors the Lance Namespace `BoostQuery` schema.
type BoostQuery struct {
	Positive      FtsQuery `json:"positive"`
	Negative      FtsQuery `json:"negative"`
	NegativeBoost *float32 `json:"negative_boost,omitempty"`
}

// BranchContents mirrors the Lance Namespace `BranchContents` schema.
type BranchContents struct {
	ParentBranch  *string            `json:"parentBranch,omitempty"`
	ParentVersion int64              `json:"parentVersion"`
	CreateAt      int64              `json:"createAt"`
	ManifestSize  int64              `json:"manifestSize"`
	Metadata      *map[string]string `json:"metadata,omitempty"`
}

// CommitTableOperation mirrors the Lance Namespace `CommitTableOperation` schema.
type CommitTableOperation struct {
	DeclareTable        *DeclareTableRequest             `json:"declare_table,omitempty"`
	CreateTableVersion  *CreateTableVersionRequest       `json:"create_table_version,omitempty"`
	DeleteTableVersions *BatchDeleteTableVersionsRequest `json:"delete_table_versions,omitempty"`
	DeregisterTable     *DeregisterTableRequest          `json:"deregister_table,omitempty"`
}

// CommitTableResult mirrors the Lance Namespace `CommitTableResult` schema.
type CommitTableResult struct {
	DeclareTable        *DeclareTableResponse             `json:"declare_table,omitempty"`
	CreateTableVersion  *CreateTableVersionResponse       `json:"create_table_version,omitempty"`
	DeleteTableVersions *BatchDeleteTableVersionsResponse `json:"delete_table_versions,omitempty"`
	DeregisterTable     *DeregisterTableResponse          `json:"deregister_table,omitempty"`
}

// CountTableRowsRequest mirrors the Lance Namespace `CountTableRowsRequest` schema.
type CountTableRowsRequest struct {
	Identity  *Identity          `json:"identity,omitempty"`
	Context   *map[string]string `json:"context,omitempty"`
	ID        *[]string          `json:"id,omitempty"`
	Version   *int64             `json:"version,omitempty"`
	Branch    *string            `json:"branch,omitempty"`
	Predicate *string            `json:"predicate,omitempty"`
}

// CountTableRowsResponse mirrors the Lance Namespace `CountTableRowsResponse` schema.
type CountTableRowsResponse struct {
	Context *map[string]string `json:"context,omitempty"`
	Count   *int64             `json:"count,omitempty"`
}

// CreateMaterializedViewRequest mirrors the Lance Namespace `CreateMaterializedViewRequest` schema.
type CreateMaterializedViewRequest struct {
	Identity     *Identity                            `json:"identity,omitempty"`
	Context      *map[string]string                   `json:"context,omitempty"`
	ID           *[]string                            `json:"id,omitempty"`
	Kind         CreateMaterializedViewKind           `json:"kind"`
	SourceQuery  string                               `json:"source_query"`
	OutputSchema string                               `json:"output_schema"`
	UDTFSpec     *Nullable[MaterializedViewUdtfEntry] `json:"udtf_spec,omitempty"`
	WithNoData   *bool                                `json:"with_no_data,omitempty"`
	AutoRefresh  *Nullable[bool]                      `json:"auto_refresh,omitempty"`
}

// CreateMaterializedViewResponse mirrors the Lance Namespace `CreateMaterializedViewResponse` schema.
type CreateMaterializedViewResponse struct {
	Context *map[string]string `json:"context,omitempty"`
	Version int64              `json:"version"`
	JobID   *Nullable[string]  `json:"job_id,omitempty"`
}

// CreateNamespaceRequest mirrors the Lance Namespace `CreateNamespaceRequest` schema.
type CreateNamespaceRequest struct {
	Identity   *Identity          `json:"identity,omitempty"`
	Context    *map[string]string `json:"context,omitempty"`
	ID         *[]string          `json:"id,omitempty"`
	Mode       *string            `json:"mode,omitempty"`
	Properties *map[string]string `json:"properties,omitempty"`
}

// CreateNamespaceResponse mirrors the Lance Namespace `CreateNamespaceResponse` schema.
type CreateNamespaceResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
	Properties    *map[string]string `json:"properties,omitempty"`
}

// CreateTableBranchRequest mirrors the Lance Namespace `CreateTableBranchRequest` schema.
type CreateTableBranchRequest struct {
	Identity    *Identity          `json:"identity,omitempty"`
	Context     *map[string]string `json:"context,omitempty"`
	ID          *[]string          `json:"id,omitempty"`
	Name        string             `json:"name"`
	FromBranch  *string            `json:"from_branch,omitempty"`
	FromVersion *int64             `json:"from_version,omitempty"`
}

// CreateTableBranchResponse mirrors the Lance Namespace `CreateTableBranchResponse` schema.
type CreateTableBranchResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// CreateTableIndexRequest mirrors the Lance Namespace `CreateTableIndexRequest` schema.
type CreateTableIndexRequest struct {
	Identity            *Identity          `json:"identity,omitempty"`
	Context             *map[string]string `json:"context,omitempty"`
	ID                  *[]string          `json:"id,omitempty"`
	Branch              *string            `json:"branch,omitempty"`
	Column              string             `json:"column"`
	IndexType           string             `json:"index_type"`
	Name                *string            `json:"name,omitempty"`
	DistanceType        *string            `json:"distance_type,omitempty"`
	NumPartitions       *int32             `json:"num_partitions,omitempty"`
	NumSubVectors       *int32             `json:"num_sub_vectors,omitempty"`
	NumBits             *int32             `json:"num_bits,omitempty"`
	SampleRate          *int32             `json:"sample_rate,omitempty"`
	MaxIterations       *int32             `json:"max_iterations,omitempty"`
	TargetPartitionSize *int32             `json:"target_partition_size,omitempty"`
	M                   *int32             `json:"m,omitempty"`
	EfConstruction      *int32             `json:"ef_construction,omitempty"`
	WithPosition        *bool              `json:"with_position,omitempty"`
	BaseTokenizer       *string            `json:"base_tokenizer,omitempty"`
	Language            *string            `json:"language,omitempty"`
	MaxTokenLength      *int32             `json:"max_token_length,omitempty"`
	LowerCase           *bool              `json:"lower_case,omitempty"`
	Stem                *bool              `json:"stem,omitempty"`
	RemoveStopWords     *bool              `json:"remove_stop_words,omitempty"`
	AsciiFolding        *bool              `json:"ascii_folding,omitempty"`
}

// CreateTableIndexResponse mirrors the Lance Namespace `CreateTableIndexResponse` schema.
type CreateTableIndexResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// CreateTableRequest mirrors the Lance Namespace `CreateTableRequest` schema.
type CreateTableRequest struct {
	Identity       *Identity          `json:"identity,omitempty"`
	Context        *map[string]string `json:"context,omitempty"`
	ID             *[]string          `json:"id,omitempty"`
	Mode           *string            `json:"mode,omitempty"`
	Properties     *map[string]string `json:"properties,omitempty"`
	StorageOptions *map[string]string `json:"storage_options,omitempty"`
}

// CreateTableResponse mirrors the Lance Namespace `CreateTableResponse` schema.
type CreateTableResponse struct {
	Context        *map[string]string `json:"context,omitempty"`
	TransactionID  *string            `json:"transaction_id,omitempty"`
	Location       *string            `json:"location,omitempty"`
	Version        *int64             `json:"version,omitempty"`
	StorageOptions *map[string]string `json:"storage_options,omitempty"`
	Properties     *map[string]string `json:"properties,omitempty"`
}

// CreateTableScalarIndexResponse mirrors the Lance Namespace `CreateTableScalarIndexResponse` schema.
type CreateTableScalarIndexResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// CreateTableTagRequest mirrors the Lance Namespace `CreateTableTagRequest` schema.
type CreateTableTagRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Tag      string             `json:"tag"`
	Version  int64              `json:"version"`
	Branch   *string            `json:"branch,omitempty"`
}

// CreateTableTagResponse mirrors the Lance Namespace `CreateTableTagResponse` schema.
type CreateTableTagResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// CreateTableVersionEntry mirrors the Lance Namespace `CreateTableVersionEntry` schema.
type CreateTableVersionEntry struct {
	ID           []string           `json:"id"`
	Version      int64              `json:"version"`
	Branch       *string            `json:"branch,omitempty"`
	ManifestPath string             `json:"manifest_path"`
	ManifestSize *int64             `json:"manifest_size,omitempty"`
	ETag         *string            `json:"e_tag,omitempty"`
	Metadata     *map[string]string `json:"metadata,omitempty"`
	NamingScheme *string            `json:"naming_scheme,omitempty"`
}

// CreateTableVersionRequest mirrors the Lance Namespace `CreateTableVersionRequest` schema.
type CreateTableVersionRequest struct {
	Identity     *Identity          `json:"identity,omitempty"`
	Context      *map[string]string `json:"context,omitempty"`
	ID           *[]string          `json:"id,omitempty"`
	Version      int64              `json:"version"`
	Branch       *string            `json:"branch,omitempty"`
	ManifestPath string             `json:"manifest_path"`
	ManifestSize *int64             `json:"manifest_size,omitempty"`
	ETag         *string            `json:"e_tag,omitempty"`
	Metadata     *map[string]string `json:"metadata,omitempty"`
	NamingScheme *string            `json:"naming_scheme,omitempty"`
}

// CreateTableVersionResponse mirrors the Lance Namespace `CreateTableVersionResponse` schema.
type CreateTableVersionResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
	Version       *TableVersion      `json:"version,omitempty"`
}

// DeclareTableRequest mirrors the Lance Namespace `DeclareTableRequest` schema.
type DeclareTableRequest struct {
	Identity        *Identity          `json:"identity,omitempty"`
	Context         *map[string]string `json:"context,omitempty"`
	ID              *[]string          `json:"id,omitempty"`
	Location        *string            `json:"location,omitempty"`
	VendCredentials *bool              `json:"vend_credentials,omitempty"`
	Properties      *map[string]string `json:"properties,omitempty"`
}

// DeclareTableResponse mirrors the Lance Namespace `DeclareTableResponse` schema.
type DeclareTableResponse struct {
	Context           *map[string]string `json:"context,omitempty"`
	TransactionID     *string            `json:"transaction_id,omitempty"`
	Location          *string            `json:"location,omitempty"`
	StorageOptions    *map[string]string `json:"storage_options,omitempty"`
	Properties        *map[string]string `json:"properties,omitempty"`
	ManagedVersioning *bool              `json:"managed_versioning,omitempty"`
}

// DeleteFromTableRequest mirrors the Lance Namespace `DeleteFromTableRequest` schema.
type DeleteFromTableRequest struct {
	Identity  *Identity          `json:"identity,omitempty"`
	Context   *map[string]string `json:"context,omitempty"`
	ID        *[]string          `json:"id,omitempty"`
	Branch    *string            `json:"branch,omitempty"`
	Predicate string             `json:"predicate"`
}

// DeleteFromTableResponse mirrors the Lance Namespace `DeleteFromTableResponse` schema.
type DeleteFromTableResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
	Version       *int64             `json:"version,omitempty"`
}

// DeleteTableBranchRequest mirrors the Lance Namespace `DeleteTableBranchRequest` schema.
type DeleteTableBranchRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Name     string             `json:"name"`
}

// DeleteTableBranchResponse mirrors the Lance Namespace `DeleteTableBranchResponse` schema.
type DeleteTableBranchResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// DeleteTableTagRequest mirrors the Lance Namespace `DeleteTableTagRequest` schema.
type DeleteTableTagRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Tag      string             `json:"tag"`
}

// DeleteTableTagResponse mirrors the Lance Namespace `DeleteTableTagResponse` schema.
type DeleteTableTagResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// DeregisterTableRequest mirrors the Lance Namespace `DeregisterTableRequest` schema.
type DeregisterTableRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
}

// DeregisterTableResponse mirrors the Lance Namespace `DeregisterTableResponse` schema.
type DeregisterTableResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
	ID            *[]string          `json:"id,omitempty"`
	Location      *string            `json:"location,omitempty"`
	Properties    *map[string]string `json:"properties,omitempty"`
}

// DescribeNamespaceRequest mirrors the Lance Namespace `DescribeNamespaceRequest` schema.
type DescribeNamespaceRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
}

// DescribeNamespaceResponse mirrors the Lance Namespace `DescribeNamespaceResponse` schema.
type DescribeNamespaceResponse struct {
	Context    *map[string]string `json:"context,omitempty"`
	Properties *map[string]string `json:"properties,omitempty"`
}

// DescribeTableIndexStatsRequest mirrors the Lance Namespace `DescribeTableIndexStatsRequest` schema.
type DescribeTableIndexStatsRequest struct {
	Identity  *Identity          `json:"identity,omitempty"`
	Context   *map[string]string `json:"context,omitempty"`
	ID        *[]string          `json:"id,omitempty"`
	Version   *int64             `json:"version,omitempty"`
	Branch    *string            `json:"branch,omitempty"`
	IndexName *string            `json:"index_name,omitempty"`
}

// DescribeTableIndexStatsResponse mirrors the Lance Namespace `DescribeTableIndexStatsResponse` schema.
type DescribeTableIndexStatsResponse struct {
	Context          *map[string]string `json:"context,omitempty"`
	DistanceType     *string            `json:"distance_type,omitempty"`
	IndexType        *string            `json:"index_type,omitempty"`
	NumIndexedRows   *int64             `json:"num_indexed_rows,omitempty"`
	NumUnindexedRows *int64             `json:"num_unindexed_rows,omitempty"`
	NumIndices       *int32             `json:"num_indices,omitempty"`
}

// DescribeTableRequest mirrors the Lance Namespace `DescribeTableRequest` schema.
type DescribeTableRequest struct {
	Identity             *Identity          `json:"identity,omitempty"`
	Context              *map[string]string `json:"context,omitempty"`
	ID                   *[]string          `json:"id,omitempty"`
	Version              *int64             `json:"version,omitempty"`
	Tag                  *string            `json:"tag,omitempty"`
	Branch               *string            `json:"branch,omitempty"`
	WithTableURI         *bool              `json:"with_table_uri,omitempty"`
	LoadDetailedMetadata *bool              `json:"load_detailed_metadata,omitempty"`
	CheckDeclared        *bool              `json:"check_declared,omitempty"`
	VendCredentials      *bool              `json:"vend_credentials,omitempty"`
}

// DescribeTableResponse mirrors the Lance Namespace `DescribeTableResponse` schema.
type DescribeTableResponse struct {
	Context           *map[string]string `json:"context,omitempty"`
	Table             *string            `json:"table,omitempty"`
	Namespace         *[]string          `json:"namespace,omitempty"`
	Version           *int64             `json:"version,omitempty"`
	Location          *string            `json:"location,omitempty"`
	TableURI          *string            `json:"table_uri,omitempty"`
	Schema            *JsonArrowSchema   `json:"schema,omitempty"`
	StorageOptions    *map[string]string `json:"storage_options,omitempty"`
	Stats             *TableBasicStats   `json:"stats,omitempty"`
	Metadata          *map[string]string `json:"metadata,omitempty"`
	Properties        *map[string]string `json:"properties,omitempty"`
	ManagedVersioning *bool              `json:"managed_versioning,omitempty"`
	IsOnlyDeclared    *bool              `json:"is_only_declared,omitempty"`
}

// DescribeTableVersionRequest mirrors the Lance Namespace `DescribeTableVersionRequest` schema.
type DescribeTableVersionRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Version  *int64             `json:"version,omitempty"`
	Branch   *string            `json:"branch,omitempty"`
}

// DescribeTableVersionResponse mirrors the Lance Namespace `DescribeTableVersionResponse` schema.
type DescribeTableVersionResponse struct {
	Context *map[string]string `json:"context,omitempty"`
	Version TableVersion       `json:"version"`
}

// DescribeTransactionRequest mirrors the Lance Namespace `DescribeTransactionRequest` schema.
type DescribeTransactionRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
}

// DescribeTransactionResponse mirrors the Lance Namespace `DescribeTransactionResponse` schema.
type DescribeTransactionResponse struct {
	Context    *map[string]string `json:"context,omitempty"`
	Status     string             `json:"status"`
	Properties *map[string]string `json:"properties,omitempty"`
}

// DropNamespaceRequest mirrors the Lance Namespace `DropNamespaceRequest` schema.
type DropNamespaceRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Mode     *string            `json:"mode,omitempty"`
	Behavior *string            `json:"behavior,omitempty"`
}

// DropNamespaceResponse mirrors the Lance Namespace `DropNamespaceResponse` schema.
type DropNamespaceResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	Properties    *map[string]string `json:"properties,omitempty"`
	TransactionID *[]string          `json:"transaction_id,omitempty"`
}

// DropTableIndexRequest mirrors the Lance Namespace `DropTableIndexRequest` schema.
type DropTableIndexRequest struct {
	Identity  *Identity          `json:"identity,omitempty"`
	Context   *map[string]string `json:"context,omitempty"`
	ID        *[]string          `json:"id,omitempty"`
	Branch    *string            `json:"branch,omitempty"`
	IndexName *string            `json:"index_name,omitempty"`
}

// DropTableIndexResponse mirrors the Lance Namespace `DropTableIndexResponse` schema.
type DropTableIndexResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// DropTableRequest mirrors the Lance Namespace `DropTableRequest` schema.
type DropTableRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
}

// DropTableResponse mirrors the Lance Namespace `DropTableResponse` schema.
type DropTableResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
	ID            *[]string          `json:"id,omitempty"`
	Location      *string            `json:"location,omitempty"`
	Properties    *map[string]string `json:"properties,omitempty"`
}

// ErrorResponse mirrors the Lance Namespace `ErrorResponse` schema.
type ErrorResponse struct {
	Context  *map[string]string `json:"context,omitempty"`
	Error    *string            `json:"error,omitempty"`
	Code     int32              `json:"code"`
	Detail   *string            `json:"detail,omitempty"`
	Instance *string            `json:"instance,omitempty"`
}

// ExplainTableQueryPlanRequest mirrors the Lance Namespace `ExplainTableQueryPlanRequest` schema.
type ExplainTableQueryPlanRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Branch   *string            `json:"branch,omitempty"`
	Query    QueryTableRequest  `json:"query"`
	Verbose  *bool              `json:"verbose,omitempty"`
}

// ExplainTableQueryPlanResponse mirrors the Lance Namespace `ExplainTableQueryPlanResponse` schema.
type ExplainTableQueryPlanResponse struct {
	Context *map[string]string `json:"context,omitempty"`
	Plan    string             `json:"plan"`
}

// FragmentStats mirrors the Lance Namespace `FragmentStats` schema.
type FragmentStats struct {
	NumFragments      int64           `json:"num_fragments"`
	NumSmallFragments int64           `json:"num_small_fragments"`
	Lengths           FragmentSummary `json:"lengths"`
}

// FragmentSummary mirrors the Lance Namespace `FragmentSummary` schema.
type FragmentSummary struct {
	Min  int64 `json:"min"`
	Max  int64 `json:"max"`
	Mean int64 `json:"mean"`
	P25  int64 `json:"p25"`
	P50  int64 `json:"p50"`
	P75  int64 `json:"p75"`
	P99  int64 `json:"p99"`
}

// FtsQuery mirrors the Lance Namespace `FtsQuery` schema.
type FtsQuery struct {
	Match      *MatchQuery      `json:"match,omitempty"`
	Phrase     *PhraseQuery     `json:"phrase,omitempty"`
	Boost      *BoostQuery      `json:"boost,omitempty"`
	MultiMatch *MultiMatchQuery `json:"multi_match,omitempty"`
	Boolean    *BooleanQuery    `json:"boolean,omitempty"`
}

// GetTableStatsRequest mirrors the Lance Namespace `GetTableStatsRequest` schema.
type GetTableStatsRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Branch   *string            `json:"branch,omitempty"`
}

// GetTableStatsResponse mirrors the Lance Namespace `GetTableStatsResponse` schema.
type GetTableStatsResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TotalBytes    int64              `json:"total_bytes"`
	NumRows       int64              `json:"num_rows"`
	NumIndices    int64              `json:"num_indices"`
	FragmentStats FragmentStats      `json:"fragment_stats"`
}

// GetTableTagVersionRequest mirrors the Lance Namespace `GetTableTagVersionRequest` schema.
type GetTableTagVersionRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Tag      string             `json:"tag"`
}

// GetTableTagVersionResponse mirrors the Lance Namespace `GetTableTagVersionResponse` schema.
type GetTableTagVersionResponse struct {
	Context *map[string]string `json:"context,omitempty"`
	Version int64              `json:"version"`
	Branch  *string            `json:"branch,omitempty"`
}

// Identity mirrors the Lance Namespace `Identity` schema.
type Identity struct {
	APIKey    *string `json:"api_key,omitempty"`
	AuthToken *string `json:"auth_token,omitempty"`
}

// IndexContent mirrors the Lance Namespace `IndexContent` schema.
type IndexContent struct {
	IndexName        string   `json:"index_name"`
	IndexUUID        string   `json:"index_uuid"`
	Columns          []string `json:"columns"`
	Status           string   `json:"status"`
	IndexType        *string  `json:"index_type,omitempty"`
	TypeURL          *string  `json:"type_url,omitempty"`
	NumIndexedRows   *int64   `json:"num_indexed_rows,omitempty"`
	NumUnindexedRows *int64   `json:"num_unindexed_rows,omitempty"`
	SizeBytes        *int64   `json:"size_bytes,omitempty"`
	NumSegments      *int32   `json:"num_segments,omitempty"`
	CreatedAt        *string  `json:"created_at,omitempty"`
	IndexVersion     *int32   `json:"index_version,omitempty"`
	IndexDetails     *string  `json:"index_details,omitempty"`
}

// InsertIntoTableRequest mirrors the Lance Namespace `InsertIntoTableRequest` schema.
type InsertIntoTableRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Branch   *string            `json:"branch,omitempty"`
	Mode     *string            `json:"mode,omitempty"`
}

// InsertIntoTableResponse mirrors the Lance Namespace `InsertIntoTableResponse` schema.
type InsertIntoTableResponse struct {
	Context         *map[string]string `json:"context,omitempty"`
	TransactionID   *string            `json:"transaction_id,omitempty"`
	NumInsertedRows *int64             `json:"num_inserted_rows,omitempty"`
	Version         *int64             `json:"version,omitempty"`
}

// JsonArrowDataType mirrors the Lance Namespace `JsonArrowDataType` schema.
type JsonArrowDataType struct {
	Fields *[]JsonArrowField `json:"fields,omitempty"`
	Length *int64            `json:"length,omitempty"`
	Type   string            `json:"type"`
}

// JsonArrowField mirrors the Lance Namespace `JsonArrowField` schema.
type JsonArrowField struct {
	Metadata *map[string]string `json:"metadata,omitempty"`
	Name     string             `json:"name"`
	Nullable bool               `json:"nullable"`
	Type     JsonArrowDataType  `json:"type"`
}

// JsonArrowSchema mirrors the Lance Namespace `JsonArrowSchema` schema.
type JsonArrowSchema struct {
	Fields   []JsonArrowField   `json:"fields"`
	Metadata *map[string]string `json:"metadata,omitempty"`
}

// ListNamespacesRequest mirrors the Lance Namespace `ListNamespacesRequest` schema.
type ListNamespacesRequest struct {
	Identity  *Identity          `json:"identity,omitempty"`
	Context   *map[string]string `json:"context,omitempty"`
	ID        *[]string          `json:"id,omitempty"`
	PageToken *string            `json:"page_token,omitempty"`
	Limit     *int32             `json:"limit,omitempty"`
}

// ListNamespacesResponse mirrors the Lance Namespace `ListNamespacesResponse` schema.
type ListNamespacesResponse struct {
	Context    *map[string]string `json:"context,omitempty"`
	Namespaces []string           `json:"namespaces"`
	PageToken  *string            `json:"page_token,omitempty"`
}

// ListTableBranchesRequest mirrors the Lance Namespace `ListTableBranchesRequest` schema.
type ListTableBranchesRequest struct {
	Identity  *Identity          `json:"identity,omitempty"`
	Context   *map[string]string `json:"context,omitempty"`
	ID        *[]string          `json:"id,omitempty"`
	PageToken *string            `json:"page_token,omitempty"`
	Limit     *int32             `json:"limit,omitempty"`
}

// ListTableBranchesResponse mirrors the Lance Namespace `ListTableBranchesResponse` schema.
type ListTableBranchesResponse struct {
	Context   *map[string]string        `json:"context,omitempty"`
	Branches  map[string]BranchContents `json:"branches"`
	PageToken *string                   `json:"page_token,omitempty"`
}

// ListTableIndicesRequest mirrors the Lance Namespace `ListTableIndicesRequest` schema.
type ListTableIndicesRequest struct {
	Identity  *Identity          `json:"identity,omitempty"`
	Context   *map[string]string `json:"context,omitempty"`
	ID        *[]string          `json:"id,omitempty"`
	Version   *int64             `json:"version,omitempty"`
	Branch    *string            `json:"branch,omitempty"`
	PageToken *string            `json:"page_token,omitempty"`
	Limit     *int32             `json:"limit,omitempty"`
}

// ListTableIndicesResponse mirrors the Lance Namespace `ListTableIndicesResponse` schema.
type ListTableIndicesResponse struct {
	Context   *map[string]string `json:"context,omitempty"`
	Indexes   []IndexContent     `json:"indexes"`
	PageToken *string            `json:"page_token,omitempty"`
}

// ListTableTagsRequest mirrors the Lance Namespace `ListTableTagsRequest` schema.
type ListTableTagsRequest struct {
	Identity  *Identity          `json:"identity,omitempty"`
	Context   *map[string]string `json:"context,omitempty"`
	ID        *[]string          `json:"id,omitempty"`
	PageToken *string            `json:"page_token,omitempty"`
	Limit     *int32             `json:"limit,omitempty"`
}

// ListTableTagsResponse mirrors the Lance Namespace `ListTableTagsResponse` schema.
type ListTableTagsResponse struct {
	Context   *map[string]string     `json:"context,omitempty"`
	Tags      map[string]TagContents `json:"tags"`
	PageToken *string                `json:"page_token,omitempty"`
}

// ListTableVersionsRequest mirrors the Lance Namespace `ListTableVersionsRequest` schema.
type ListTableVersionsRequest struct {
	Identity   *Identity          `json:"identity,omitempty"`
	Context    *map[string]string `json:"context,omitempty"`
	ID         *[]string          `json:"id,omitempty"`
	Branch     *string            `json:"branch,omitempty"`
	PageToken  *string            `json:"page_token,omitempty"`
	Limit      *int32             `json:"limit,omitempty"`
	Descending *bool              `json:"descending,omitempty"`
}

// ListTableVersionsResponse mirrors the Lance Namespace `ListTableVersionsResponse` schema.
type ListTableVersionsResponse struct {
	Context   *map[string]string `json:"context,omitempty"`
	Versions  []TableVersion     `json:"versions"`
	PageToken *string            `json:"page_token,omitempty"`
}

// ListTablesRequest mirrors the Lance Namespace `ListTablesRequest` schema.
type ListTablesRequest struct {
	Identity        *Identity          `json:"identity,omitempty"`
	Context         *map[string]string `json:"context,omitempty"`
	ID              *[]string          `json:"id,omitempty"`
	PageToken       *string            `json:"page_token,omitempty"`
	Limit           *int32             `json:"limit,omitempty"`
	IncludeDeclared *bool              `json:"include_declared,omitempty"`
}

// ListTablesResponse mirrors the Lance Namespace `ListTablesResponse` schema.
type ListTablesResponse struct {
	Context   *map[string]string `json:"context,omitempty"`
	Tables    []string           `json:"tables"`
	PageToken *string            `json:"page_token,omitempty"`
}

// MatchQuery mirrors the Lance Namespace `MatchQuery` schema.
type MatchQuery struct {
	Boost         *float32 `json:"boost,omitempty"`
	Column        *string  `json:"column,omitempty"`
	Fuzziness     *int32   `json:"fuzziness,omitempty"`
	MaxExpansions *int32   `json:"max_expansions,omitempty"`
	Operator      *string  `json:"operator,omitempty"`
	PrefixLength  *int32   `json:"prefix_length,omitempty"`
	Terms         string   `json:"terms"`
}

// MaterializedViewUdtfEntry mirrors the Lance Namespace `MaterializedViewUdtfEntry` schema.
type MaterializedViewUdtfEntry struct {
	Kind                     MaterializedViewUdtfKind `json:"kind"`
	UDTF                     string                   `json:"udtf"`
	UDTFSHA                  string                   `json:"udtf_sha"`
	UDTFName                 string                   `json:"udtf_name"`
	UDTFVersion              string                   `json:"udtf_version"`
	InputColumns             *Nullable[[]string]      `json:"input_columns,omitempty"`
	PartitionBy              *Nullable[string]        `json:"partition_by,omitempty"`
	PartitionByIndexedColumn *Nullable[string]        `json:"partition_by_indexed_column,omitempty"`
	NumCpus                  *Nullable[float64]       `json:"num_cpus,omitempty"`
	NumGpus                  *Nullable[float64]       `json:"num_gpus,omitempty"`
	Memory                   *Nullable[int32]         `json:"memory,omitempty"`
	ErrorHandling            *Nullable[any]           `json:"error_handling,omitempty"`
	Batch                    *Nullable[bool]          `json:"batch,omitempty"`
	Manifest                 *Nullable[string]        `json:"manifest,omitempty"`
	ManifestChecksum         *Nullable[string]        `json:"manifest_checksum,omitempty"`
}

// MergeInsertIntoTableRequest mirrors the Lance Namespace `MergeInsertIntoTableRequest` schema.
type MergeInsertIntoTableRequest struct {
	Identity                         *Identity          `json:"identity,omitempty"`
	Context                          *map[string]string `json:"context,omitempty"`
	ID                               *[]string          `json:"id,omitempty"`
	Branch                           *string            `json:"branch,omitempty"`
	On                               *[]string          `json:"on,omitempty"`
	WhenMatchedUpdateAll             *bool              `json:"when_matched_update_all,omitempty"`
	WhenMatchedUpdateAllFilt         *string            `json:"when_matched_update_all_filt,omitempty"`
	WhenNotMatchedInsertAll          *bool              `json:"when_not_matched_insert_all,omitempty"`
	WhenNotMatchedBySourceDelete     *bool              `json:"when_not_matched_by_source_delete,omitempty"`
	WhenNotMatchedBySourceDeleteFilt *string            `json:"when_not_matched_by_source_delete_filt,omitempty"`
	Timeout                          *string            `json:"timeout,omitempty"`
	UseIndex                         *bool              `json:"use_index,omitempty"`
}

// MergeInsertIntoTableResponse mirrors the Lance Namespace `MergeInsertIntoTableResponse` schema.
type MergeInsertIntoTableResponse struct {
	Context         *map[string]string `json:"context,omitempty"`
	TransactionID   *string            `json:"transaction_id,omitempty"`
	NumUpdatedRows  *int64             `json:"num_updated_rows,omitempty"`
	NumInsertedRows *int64             `json:"num_inserted_rows,omitempty"`
	NumDeletedRows  *int64             `json:"num_deleted_rows,omitempty"`
	Version         *int64             `json:"version,omitempty"`
}

// MultiMatchQuery mirrors the Lance Namespace `MultiMatchQuery` schema.
type MultiMatchQuery struct {
	MatchQueries []MatchQuery `json:"match_queries"`
}

// NamespaceExistsRequest mirrors the Lance Namespace `NamespaceExistsRequest` schema.
type NamespaceExistsRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
}

// NamespaceExistsResponse mirrors the Lance Namespace `NamespaceExistsResponse` schema.
type NamespaceExistsResponse struct {
	Context *map[string]string `json:"context,omitempty"`
}

// PartitionField mirrors the Lance Namespace `PartitionField` schema.
type PartitionField struct {
	FieldID    string              `json:"field_id"`
	SourceIds  []int32             `json:"source_ids"`
	Transform  *PartitionTransform `json:"transform,omitempty"`
	Expression *string             `json:"expression,omitempty"`
	ResultType JsonArrowDataType   `json:"result_type"`
}

// PartitionSpec mirrors the Lance Namespace `PartitionSpec` schema.
type PartitionSpec struct {
	ID     int32            `json:"id"`
	Fields []PartitionField `json:"fields"`
}

// PartitionTransform mirrors the Lance Namespace `PartitionTransform` schema.
type PartitionTransform struct {
	Type       string `json:"type"`
	NumBuckets *int32 `json:"num_buckets,omitempty"`
	Width      *int32 `json:"width,omitempty"`
}

// PhraseQuery mirrors the Lance Namespace `PhraseQuery` schema.
type PhraseQuery struct {
	Column *string `json:"column,omitempty"`
	Slop   *int32  `json:"slop,omitempty"`
	Terms  string  `json:"terms"`
}

// QueryTableRequest mirrors the Lance Namespace `QueryTableRequest` schema.
type QueryTableRequest struct {
	Identity          *Identity                       `json:"identity,omitempty"`
	Context           *map[string]string              `json:"context,omitempty"`
	ID                *[]string                       `json:"id,omitempty"`
	Branch            *string                         `json:"branch,omitempty"`
	BypassVectorIndex *bool                           `json:"bypass_vector_index,omitempty"`
	Columns           *QueryTableRequestColumns       `json:"columns,omitempty"`
	DistanceType      *string                         `json:"distance_type,omitempty"`
	Ef                *int32                          `json:"ef,omitempty"`
	FastSearch        *bool                           `json:"fast_search,omitempty"`
	Filter            *string                         `json:"filter,omitempty"`
	FullTextQuery     *QueryTableRequestFullTextQuery `json:"full_text_query,omitempty"`
	K                 int32                           `json:"k"`
	LowerBound        *float32                        `json:"lower_bound,omitempty"`
	MaximumNprobes    *int32                          `json:"maximum_nprobes,omitempty"`
	MinimumNprobes    *int32                          `json:"minimum_nprobes,omitempty"`
	Nprobes           *int32                          `json:"nprobes,omitempty"`
	Offset            *int32                          `json:"offset,omitempty"`
	Prefilter         *bool                           `json:"prefilter,omitempty"`
	RefineFactor      *int32                          `json:"refine_factor,omitempty"`
	UpperBound        *float32                        `json:"upper_bound,omitempty"`
	Vector            QueryTableRequestVector         `json:"vector"`
	VectorColumn      *string                         `json:"vector_column,omitempty"`
	Version           *int64                          `json:"version,omitempty"`
	WithRowID         *bool                           `json:"with_row_id,omitempty"`
}

// QueryTableRequestColumns mirrors the Lance Namespace `QueryTableRequestColumns` schema.
type QueryTableRequestColumns struct {
	ColumnNames   *[]string          `json:"column_names,omitempty"`
	ColumnAliases *map[string]string `json:"column_aliases,omitempty"`
}

// QueryTableRequestFullTextQuery mirrors the Lance Namespace `QueryTableRequestFullTextQuery` schema.
type QueryTableRequestFullTextQuery struct {
	StringQuery     *StringFtsQuery     `json:"string_query,omitempty"`
	StructuredQuery *StructuredFtsQuery `json:"structured_query,omitempty"`
}

// QueryTableRequestVector mirrors the Lance Namespace `QueryTableRequestVector` schema.
type QueryTableRequestVector struct {
	SingleVector *[]float32   `json:"single_vector,omitempty"`
	MultiVector  *[][]float32 `json:"multi_vector,omitempty"`
}

// QueryTableResponse mirrors the Lance Namespace `QueryTableResponse` schema.
type QueryTableResponse struct {
	Context *map[string]string `json:"context,omitempty"`
	Data    *[]byte            `json:"data,omitempty"`
}

// RefreshMaterializedViewRequest mirrors the Lance Namespace `RefreshMaterializedViewRequest` schema.
type RefreshMaterializedViewRequest struct {
	Identity                *Identity          `json:"identity,omitempty"`
	Context                 *map[string]string `json:"context,omitempty"`
	ID                      *[]string          `json:"id,omitempty"`
	SrcVersion              *Nullable[int32]   `json:"src_version,omitempty"`
	MaxRowsPerFragment      *Nullable[int32]   `json:"max_rows_per_fragment,omitempty"`
	Concurrency             *Nullable[int32]   `json:"concurrency,omitempty"`
	IntraApplierConcurrency *Nullable[int32]   `json:"intra_applier_concurrency,omitempty"`
	SourceTaskSize          *Nullable[int32]   `json:"source_task_size,omitempty"`
	Cluster                 *Nullable[string]  `json:"cluster,omitempty"`
	OutputLimit             *Nullable[int32]   `json:"output_limit,omitempty"`
	Manifest                *Nullable[string]  `json:"manifest,omitempty"`
}

// RefreshMaterializedViewResponse mirrors the Lance Namespace `RefreshMaterializedViewResponse` schema.
type RefreshMaterializedViewResponse struct {
	Context *map[string]string `json:"context,omitempty"`
	JobID   string             `json:"job_id"`
}

// RegisterTableRequest mirrors the Lance Namespace `RegisterTableRequest` schema.
type RegisterTableRequest struct {
	Identity   *Identity          `json:"identity,omitempty"`
	Context    *map[string]string `json:"context,omitempty"`
	ID         *[]string          `json:"id,omitempty"`
	Location   string             `json:"location"`
	Mode       *string            `json:"mode,omitempty"`
	Properties *map[string]string `json:"properties,omitempty"`
}

// RegisterTableResponse mirrors the Lance Namespace `RegisterTableResponse` schema.
type RegisterTableResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
	Location      *string            `json:"location,omitempty"`
	Properties    *map[string]string `json:"properties,omitempty"`
}

// RenameTableRequest mirrors the Lance Namespace `RenameTableRequest` schema.
type RenameTableRequest struct {
	Identity       *Identity          `json:"identity,omitempty"`
	Context        *map[string]string `json:"context,omitempty"`
	ID             *[]string          `json:"id,omitempty"`
	NewTableName   string             `json:"new_table_name"`
	NewNamespaceID *[]string          `json:"new_namespace_id,omitempty"`
}

// RenameTableResponse mirrors the Lance Namespace `RenameTableResponse` schema.
type RenameTableResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// RestoreTableRequest mirrors the Lance Namespace `RestoreTableRequest` schema.
type RestoreTableRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Version  int64              `json:"version"`
	Branch   *string            `json:"branch,omitempty"`
}

// RestoreTableResponse mirrors the Lance Namespace `RestoreTableResponse` schema.
type RestoreTableResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// StringFtsQuery mirrors the Lance Namespace `StringFtsQuery` schema.
type StringFtsQuery struct {
	Columns *[]string `json:"columns,omitempty"`
	Query   string    `json:"query"`
}

// StructuredFtsQuery mirrors the Lance Namespace `StructuredFtsQuery` schema.
type StructuredFtsQuery struct {
	Query FtsQuery `json:"query"`
}

// TableBasicStats mirrors the Lance Namespace `TableBasicStats` schema.
type TableBasicStats struct {
	NumDeletedRows int32 `json:"num_deleted_rows"`
	NumFragments   int32 `json:"num_fragments"`
}

// TableExistsRequest mirrors the Lance Namespace `TableExistsRequest` schema.
type TableExistsRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Version  *int64             `json:"version,omitempty"`
}

// TableExistsResponse mirrors the Lance Namespace `TableExistsResponse` schema.
type TableExistsResponse struct {
	Context *map[string]string `json:"context,omitempty"`
}

// TableVersion mirrors the Lance Namespace `TableVersion` schema.
type TableVersion struct {
	Version         int64              `json:"version"`
	ManifestPath    string             `json:"manifest_path"`
	ManifestSize    *int64             `json:"manifest_size,omitempty"`
	ETag            *string            `json:"e_tag,omitempty"`
	TimestampMillis *int64             `json:"timestamp_millis,omitempty"`
	Metadata        *map[string]string `json:"metadata,omitempty"`
}

// TagContents mirrors the Lance Namespace `TagContents` schema.
type TagContents struct {
	Branch       *string `json:"branch,omitempty"`
	Version      int64   `json:"version"`
	ManifestSize int64   `json:"manifestSize"`
}

// UpdateFieldMetadataEntry mirrors the Lance Namespace `UpdateFieldMetadataEntry` schema.
type UpdateFieldMetadataEntry struct {
	Path     string            `json:"path"`
	Metadata map[string]string `json:"metadata"`
	Replace  *Nullable[bool]   `json:"replace,omitempty"`
}

// UpdateFieldMetadataRequest mirrors the Lance Namespace `UpdateFieldMetadataRequest` schema.
type UpdateFieldMetadataRequest struct {
	Identity *Identity                  `json:"identity,omitempty"`
	Context  *map[string]string         `json:"context,omitempty"`
	ID       *[]string                  `json:"id,omitempty"`
	Branch   *string                    `json:"branch,omitempty"`
	Updates  []UpdateFieldMetadataEntry `json:"updates"`
}

// UpdateFieldMetadataResponse mirrors the Lance Namespace `UpdateFieldMetadataResponse` schema.
type UpdateFieldMetadataResponse struct {
	Context *map[string]string            `json:"context,omitempty"`
	Version int64                         `json:"version"`
	Fields  *map[string]map[string]string `json:"fields,omitempty"`
}

// UpdateTableRequest mirrors the Lance Namespace `UpdateTableRequest` schema.
type UpdateTableRequest struct {
	Identity   *Identity          `json:"identity,omitempty"`
	Context    *map[string]string `json:"context,omitempty"`
	ID         *[]string          `json:"id,omitempty"`
	Branch     *string            `json:"branch,omitempty"`
	Predicate  *string            `json:"predicate,omitempty"`
	Updates    [][]string         `json:"updates"`
	Properties *map[string]string `json:"properties,omitempty"`
}

// UpdateTableResponse mirrors the Lance Namespace `UpdateTableResponse` schema.
type UpdateTableResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
	UpdatedRows   int64              `json:"updated_rows"`
	Version       int64              `json:"version"`
	Properties    *map[string]string `json:"properties,omitempty"`
}

// UpdateTableSchemaMetadataRequest mirrors the Lance Namespace `UpdateTableSchemaMetadataRequest` schema.
type UpdateTableSchemaMetadataRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Branch   *string            `json:"branch,omitempty"`
	Metadata *map[string]string `json:"metadata,omitempty"`
}

// UpdateTableSchemaMetadataResponse mirrors the Lance Namespace `UpdateTableSchemaMetadataResponse` schema.
type UpdateTableSchemaMetadataResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	Metadata      *map[string]string `json:"metadata,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// UpdateTableTagRequest mirrors the Lance Namespace `UpdateTableTagRequest` schema.
type UpdateTableTagRequest struct {
	Identity *Identity          `json:"identity,omitempty"`
	Context  *map[string]string `json:"context,omitempty"`
	ID       *[]string          `json:"id,omitempty"`
	Tag      string             `json:"tag"`
	Version  int64              `json:"version"`
	Branch   *string            `json:"branch,omitempty"`
}

// UpdateTableTagResponse mirrors the Lance Namespace `UpdateTableTagResponse` schema.
type UpdateTableTagResponse struct {
	Context       *map[string]string `json:"context,omitempty"`
	TransactionID *string            `json:"transaction_id,omitempty"`
}

// VersionRange mirrors the Lance Namespace `VersionRange` schema.
type VersionRange struct {
	StartVersion int64 `json:"start_version"`
	EndVersion   int64 `json:"end_version"`
}

// CreateMaterializedViewKind identifies the native materialized view kind for
// CreateMaterializedViewRequest.
type CreateMaterializedViewKind string

const (
	CreateMaterializedViewKindQuery   CreateMaterializedViewKind = "query"
	CreateMaterializedViewKindUDTF    CreateMaterializedViewKind = "udtf"
	CreateMaterializedViewKindChunker CreateMaterializedViewKind = "chunker"
)

// MaterializedViewUdtfKind identifies the native UDTF kind for a materialized
// view UDTF entry.
type MaterializedViewUdtfKind string

const (
	MaterializedViewUdtfKindUDTF    MaterializedViewUdtfKind = "udtf"
	MaterializedViewUdtfKindChunker MaterializedViewUdtfKind = "chunker"
)
