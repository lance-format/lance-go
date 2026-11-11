// Code generated from the native namespace operation table; DO NOT EDIT.

package namespace

import (
	"context"
	"encoding/json"
	"fmt"
)

// ListNamespaces calls the native list_namespaces operation.
func (n *nativeNamespace) ListNamespaces(ctx context.Context, request *ListNamespacesRequest) (*ListNamespacesResponse, error) {
	var response ListNamespacesResponse
	if err := n.do(ctx, "list_namespaces", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DescribeNamespace calls the native describe_namespace operation.
func (n *nativeNamespace) DescribeNamespace(ctx context.Context, request *DescribeNamespaceRequest) (*DescribeNamespaceResponse, error) {
	var response DescribeNamespaceResponse
	if err := n.do(ctx, "describe_namespace", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateNamespace calls the native create_namespace operation.
func (n *nativeNamespace) CreateNamespace(ctx context.Context, request *CreateNamespaceRequest) (*CreateNamespaceResponse, error) {
	var response CreateNamespaceResponse
	if err := n.do(ctx, "create_namespace", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DropNamespace calls the native drop_namespace operation.
func (n *nativeNamespace) DropNamespace(ctx context.Context, request *DropNamespaceRequest) (*DropNamespaceResponse, error) {
	var response DropNamespaceResponse
	if err := n.do(ctx, "drop_namespace", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// NamespaceExists calls the native namespace_exists operation.
func (n *nativeNamespace) NamespaceExists(ctx context.Context, request *NamespaceExistsRequest) error {
	if err := n.do(ctx, "namespace_exists", request, nil, nil); err != nil {
		return err
	}
	return nil
}

// ListTables calls the native list_tables operation.
func (n *nativeNamespace) ListTables(ctx context.Context, request *ListTablesRequest) (*ListTablesResponse, error) {
	var response ListTablesResponse
	if err := n.do(ctx, "list_tables", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DescribeTable calls the native describe_table operation.
func (n *nativeNamespace) DescribeTable(ctx context.Context, request *DescribeTableRequest) (*DescribeTableResponse, error) {
	var response DescribeTableResponse
	if err := n.do(ctx, "describe_table", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// RegisterTable calls the native register_table operation.
func (n *nativeNamespace) RegisterTable(ctx context.Context, request *RegisterTableRequest) (*RegisterTableResponse, error) {
	var response RegisterTableResponse
	if err := n.do(ctx, "register_table", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// TableExists calls the native table_exists operation.
func (n *nativeNamespace) TableExists(ctx context.Context, request *TableExistsRequest) error {
	if err := n.do(ctx, "table_exists", request, nil, nil); err != nil {
		return err
	}
	return nil
}

// DropTable calls the native drop_table operation.
func (n *nativeNamespace) DropTable(ctx context.Context, request *DropTableRequest) (*DropTableResponse, error) {
	var response DropTableResponse
	if err := n.do(ctx, "drop_table", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DeregisterTable calls the native deregister_table operation.
func (n *nativeNamespace) DeregisterTable(ctx context.Context, request *DeregisterTableRequest) (*DeregisterTableResponse, error) {
	var response DeregisterTableResponse
	if err := n.do(ctx, "deregister_table", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CountTableRows calls the native count_table_rows operation.
func (n *nativeNamespace) CountTableRows(ctx context.Context, request *CountTableRowsRequest) (int64, error) {
	var value int64
	if err := n.do(ctx, "count_table_rows", request, nil, &value); err != nil {
		return 0, err
	}
	return value, nil
}

// CreateTable calls the native create_table operation.
func (n *nativeNamespace) CreateTable(ctx context.Context, request *CreateTableRequest, payload []byte) (*CreateTableResponse, error) {
	var response CreateTableResponse
	if err := n.do(ctx, "create_table", request, payload, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DeclareTable calls the native declare_table operation.
func (n *nativeNamespace) DeclareTable(ctx context.Context, request *DeclareTableRequest) (*DeclareTableResponse, error) {
	var response DeclareTableResponse
	if err := n.do(ctx, "declare_table", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// InsertIntoTable calls the native insert_into_table operation.
func (n *nativeNamespace) InsertIntoTable(ctx context.Context, request *InsertIntoTableRequest, payload []byte) (*InsertIntoTableResponse, error) {
	var response InsertIntoTableResponse
	if err := n.do(ctx, "insert_into_table", request, payload, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// MergeInsertIntoTable calls the native merge_insert_into_table operation.
func (n *nativeNamespace) MergeInsertIntoTable(ctx context.Context, request *MergeInsertIntoTableRequest, payload []byte) (*MergeInsertIntoTableResponse, error) {
	var response MergeInsertIntoTableResponse
	if err := n.do(ctx, "merge_insert_into_table", request, payload, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// UpdateTable calls the native update_table operation.
func (n *nativeNamespace) UpdateTable(ctx context.Context, request *UpdateTableRequest) (*UpdateTableResponse, error) {
	var response UpdateTableResponse
	if err := n.do(ctx, "update_table", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DeleteFromTable calls the native delete_from_table operation.
func (n *nativeNamespace) DeleteFromTable(ctx context.Context, request *DeleteFromTableRequest) (*DeleteFromTableResponse, error) {
	var response DeleteFromTableResponse
	if err := n.do(ctx, "delete_from_table", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// QueryTable calls the native query_table operation.
func (n *nativeNamespace) QueryTable(ctx context.Context, request *QueryTableRequest) (*QueryTableResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	requestJSON, err := encodeRequest(request)
	if err != nil {
		return nil, requestError("query_table", err)
	}
	var response QueryTableResponse
	lease, err := n.acquire(ctx, "query_table")
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("namespace query_table: %w", err)
	}
	result, err := lease.Call("query_table", requestJSON, nil)
	if err != nil {
		return nil, wrapNativeError("query_table", err)
	}
	if len(result.JSON) > 0 {
		if err := json.Unmarshal(result.JSON, &response); err != nil {
			return nil, &ProtocolError{Operation: "query_table", Err: err}
		}
	}
	if result.Bytes != nil {
		data := append([]byte(nil), result.Bytes...)
		response.Data = &data
	}
	return &response, nil
}

// CreateTableIndex calls the native create_table_index operation.
func (n *nativeNamespace) CreateTableIndex(ctx context.Context, request *CreateTableIndexRequest) (*CreateTableIndexResponse, error) {
	var response CreateTableIndexResponse
	if err := n.do(ctx, "create_table_index", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// ListTableIndices calls the native list_table_indices operation.
func (n *nativeNamespace) ListTableIndices(ctx context.Context, request *ListTableIndicesRequest) (*ListTableIndicesResponse, error) {
	var response ListTableIndicesResponse
	if err := n.do(ctx, "list_table_indices", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DescribeTableIndexStats calls the native describe_table_index_stats operation.
func (n *nativeNamespace) DescribeTableIndexStats(ctx context.Context, request *DescribeTableIndexStatsRequest) (*DescribeTableIndexStatsResponse, error) {
	var response DescribeTableIndexStatsResponse
	if err := n.do(ctx, "describe_table_index_stats", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DescribeTransaction calls the native describe_transaction operation.
func (n *nativeNamespace) DescribeTransaction(ctx context.Context, request *DescribeTransactionRequest) (*DescribeTransactionResponse, error) {
	var response DescribeTransactionResponse
	if err := n.do(ctx, "describe_transaction", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// AlterTransaction calls the native alter_transaction operation.
func (n *nativeNamespace) AlterTransaction(ctx context.Context, request *AlterTransactionRequest) (*AlterTransactionResponse, error) {
	var response AlterTransactionResponse
	if err := n.do(ctx, "alter_transaction", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateTableScalarIndex calls the native create_table_scalar_index operation.
func (n *nativeNamespace) CreateTableScalarIndex(ctx context.Context, request *CreateTableIndexRequest) (*CreateTableScalarIndexResponse, error) {
	var response CreateTableScalarIndexResponse
	if err := n.do(ctx, "create_table_scalar_index", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DropTableIndex calls the native drop_table_index operation.
func (n *nativeNamespace) DropTableIndex(ctx context.Context, request *DropTableIndexRequest) (*DropTableIndexResponse, error) {
	var response DropTableIndexResponse
	if err := n.do(ctx, "drop_table_index", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// ListAllTables calls the native list_all_tables operation.
func (n *nativeNamespace) ListAllTables(ctx context.Context, request *ListTablesRequest) (*ListTablesResponse, error) {
	var response ListTablesResponse
	if err := n.do(ctx, "list_all_tables", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// RestoreTable calls the native restore_table operation.
func (n *nativeNamespace) RestoreTable(ctx context.Context, request *RestoreTableRequest) (*RestoreTableResponse, error) {
	var response RestoreTableResponse
	if err := n.do(ctx, "restore_table", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// RenameTable calls the native rename_table operation.
func (n *nativeNamespace) RenameTable(ctx context.Context, request *RenameTableRequest) (*RenameTableResponse, error) {
	var response RenameTableResponse
	if err := n.do(ctx, "rename_table", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// ListTableVersions calls the native list_table_versions operation.
func (n *nativeNamespace) ListTableVersions(ctx context.Context, request *ListTableVersionsRequest) (*ListTableVersionsResponse, error) {
	var response ListTableVersionsResponse
	if err := n.do(ctx, "list_table_versions", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateTableVersion calls the native create_table_version operation.
func (n *nativeNamespace) CreateTableVersion(ctx context.Context, request *CreateTableVersionRequest) (*CreateTableVersionResponse, error) {
	var response CreateTableVersionResponse
	if err := n.do(ctx, "create_table_version", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DescribeTableVersion calls the native describe_table_version operation.
func (n *nativeNamespace) DescribeTableVersion(ctx context.Context, request *DescribeTableVersionRequest) (*DescribeTableVersionResponse, error) {
	var response DescribeTableVersionResponse
	if err := n.do(ctx, "describe_table_version", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// BatchDeleteTableVersions calls the native batch_delete_table_versions operation.
func (n *nativeNamespace) BatchDeleteTableVersions(ctx context.Context, request *BatchDeleteTableVersionsRequest) (*BatchDeleteTableVersionsResponse, error) {
	var response BatchDeleteTableVersionsResponse
	if err := n.do(ctx, "batch_delete_table_versions", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// BatchCreateTableVersions calls the native batch_create_table_versions operation.
func (n *nativeNamespace) BatchCreateTableVersions(ctx context.Context, request *BatchCreateTableVersionsRequest) (*BatchCreateTableVersionsResponse, error) {
	var response BatchCreateTableVersionsResponse
	if err := n.do(ctx, "batch_create_table_versions", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// BatchCommitTables calls the native batch_commit_tables operation.
func (n *nativeNamespace) BatchCommitTables(ctx context.Context, request *BatchCommitTablesRequest) (*BatchCommitTablesResponse, error) {
	var response BatchCommitTablesResponse
	if err := n.do(ctx, "batch_commit_tables", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// UpdateTableSchemaMetadata calls the native update_table_schema_metadata operation.
func (n *nativeNamespace) UpdateTableSchemaMetadata(ctx context.Context, request *UpdateTableSchemaMetadataRequest) (*UpdateTableSchemaMetadataResponse, error) {
	var response UpdateTableSchemaMetadataResponse
	if err := n.do(ctx, "update_table_schema_metadata", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// UpdateFieldMetadata calls the native update_field_metadata operation.
func (n *nativeNamespace) UpdateFieldMetadata(ctx context.Context, request *UpdateFieldMetadataRequest) (*UpdateFieldMetadataResponse, error) {
	var response UpdateFieldMetadataResponse
	if err := n.do(ctx, "update_field_metadata", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// GetTableStats calls the native get_table_stats operation.
func (n *nativeNamespace) GetTableStats(ctx context.Context, request *GetTableStatsRequest) (*GetTableStatsResponse, error) {
	var response GetTableStatsResponse
	if err := n.do(ctx, "get_table_stats", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// ExplainTableQueryPlan calls the native explain_table_query_plan operation.
func (n *nativeNamespace) ExplainTableQueryPlan(ctx context.Context, request *ExplainTableQueryPlanRequest) (string, error) {
	var value string
	if err := n.do(ctx, "explain_table_query_plan", request, nil, &value); err != nil {
		return "", err
	}
	return value, nil
}

// AnalyzeTableQueryPlan calls the native analyze_table_query_plan operation.
func (n *nativeNamespace) AnalyzeTableQueryPlan(ctx context.Context, request *AnalyzeTableQueryPlanRequest) (string, error) {
	var value string
	if err := n.do(ctx, "analyze_table_query_plan", request, nil, &value); err != nil {
		return "", err
	}
	return value, nil
}

// AlterTableAddColumns calls the native alter_table_add_columns operation.
func (n *nativeNamespace) AlterTableAddColumns(ctx context.Context, request *AlterTableAddColumnsRequest) (*AlterTableAddColumnsResponse, error) {
	var response AlterTableAddColumnsResponse
	if err := n.do(ctx, "alter_table_add_columns", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// AlterTableAlterColumns calls the native alter_table_alter_columns operation.
func (n *nativeNamespace) AlterTableAlterColumns(ctx context.Context, request *AlterTableAlterColumnsRequest) (*AlterTableAlterColumnsResponse, error) {
	var response AlterTableAlterColumnsResponse
	if err := n.do(ctx, "alter_table_alter_columns", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// AlterTableDropColumns calls the native alter_table_drop_columns operation.
func (n *nativeNamespace) AlterTableDropColumns(ctx context.Context, request *AlterTableDropColumnsRequest) (*AlterTableDropColumnsResponse, error) {
	var response AlterTableDropColumnsResponse
	if err := n.do(ctx, "alter_table_drop_columns", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// AlterTableBackfillColumns calls the native alter_table_backfill_columns operation.
func (n *nativeNamespace) AlterTableBackfillColumns(ctx context.Context, request *AlterTableBackfillColumnsRequest) (*AlterTableBackfillColumnsResponse, error) {
	var response AlterTableBackfillColumnsResponse
	if err := n.do(ctx, "alter_table_backfill_columns", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// RefreshMaterializedView calls the native refresh_materialized_view operation.
func (n *nativeNamespace) RefreshMaterializedView(ctx context.Context, request *RefreshMaterializedViewRequest) (*RefreshMaterializedViewResponse, error) {
	var response RefreshMaterializedViewResponse
	if err := n.do(ctx, "refresh_materialized_view", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateMaterializedView calls the native create_materialized_view operation.
func (n *nativeNamespace) CreateMaterializedView(ctx context.Context, request *CreateMaterializedViewRequest) (*CreateMaterializedViewResponse, error) {
	var response CreateMaterializedViewResponse
	if err := n.do(ctx, "create_materialized_view", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// ListTableTags calls the native list_table_tags operation.
func (n *nativeNamespace) ListTableTags(ctx context.Context, request *ListTableTagsRequest) (*ListTableTagsResponse, error) {
	var response ListTableTagsResponse
	if err := n.do(ctx, "list_table_tags", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// GetTableTagVersion calls the native get_table_tag_version operation.
func (n *nativeNamespace) GetTableTagVersion(ctx context.Context, request *GetTableTagVersionRequest) (*GetTableTagVersionResponse, error) {
	var response GetTableTagVersionResponse
	if err := n.do(ctx, "get_table_tag_version", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateTableTag calls the native create_table_tag operation.
func (n *nativeNamespace) CreateTableTag(ctx context.Context, request *CreateTableTagRequest) (*CreateTableTagResponse, error) {
	var response CreateTableTagResponse
	if err := n.do(ctx, "create_table_tag", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DeleteTableTag calls the native delete_table_tag operation.
func (n *nativeNamespace) DeleteTableTag(ctx context.Context, request *DeleteTableTagRequest) (*DeleteTableTagResponse, error) {
	var response DeleteTableTagResponse
	if err := n.do(ctx, "delete_table_tag", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// UpdateTableTag calls the native update_table_tag operation.
func (n *nativeNamespace) UpdateTableTag(ctx context.Context, request *UpdateTableTagRequest) (*UpdateTableTagResponse, error) {
	var response UpdateTableTagResponse
	if err := n.do(ctx, "update_table_tag", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateTableBranch calls the native create_table_branch operation.
func (n *nativeNamespace) CreateTableBranch(ctx context.Context, request *CreateTableBranchRequest) (*CreateTableBranchResponse, error) {
	var response CreateTableBranchResponse
	if err := n.do(ctx, "create_table_branch", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// ListTableBranches calls the native list_table_branches operation.
func (n *nativeNamespace) ListTableBranches(ctx context.Context, request *ListTableBranchesRequest) (*ListTableBranchesResponse, error) {
	var response ListTableBranchesResponse
	if err := n.do(ctx, "list_table_branches", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DeleteTableBranch calls the native delete_table_branch operation.
func (n *nativeNamespace) DeleteTableBranch(ctx context.Context, request *DeleteTableBranchRequest) (*DeleteTableBranchResponse, error) {
	var response DeleteTableBranchResponse
	if err := n.do(ctx, "delete_table_branch", request, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
