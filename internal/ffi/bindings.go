package ffi

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ebitengine/purego"

	"github.com/lance-format/lance-go/internal/cdata"
	"github.com/lance-format/lance-go/internal/loader"
)

// bindings holds the resolved C function pointers. A nil field would panic on
// call, so bind returns a fully populated value or an error.
type bindings struct {
	lastErrorCode    func() int32
	lastErrorMessage func() *byte
	freeString       func(*byte)

	datasetOpen              func(uri string, storageOpts []uintptr, version uint64) uintptr
	datasetOpenWithSession   func(uri string, storageOpts []uintptr, version uint64, session uintptr) uintptr
	datasetClose             func(dataset uintptr)
	datasetVersion           func(dataset uintptr) uint64
	datasetCountRows         func(dataset uintptr) uint64
	datasetCountRowsFiltered func(dataset uintptr, filter string) uint64
	datasetLatestVersion     func(dataset uintptr) uint64
	datasetValidate          func(dataset uintptr) int32
	datasetSchema            func(dataset uintptr, out *cdata.ArrowSchema) int32
	datasetDataStats         func(dataset uintptr) uintptr
	dataStatsCount           func(stats uintptr) uint64
	dataStatsFieldIDAt       func(stats uintptr, index uint64) uint32
	dataStatsBytesAt         func(stats uintptr, index uint64) uint64
	dataStatsClose           func(stats uintptr)
	datasetTake              func(dataset uintptr, indices []uint64, numIndices uint64, columns []uintptr, out *cdata.ArrowArrayStream) int32
	datasetTakeRows          func(dataset uintptr, rowIDs []uint64, numRowIDs uint64, columns []uintptr, out *cdata.ArrowArrayStream) int32

	datasetCloneHandle   func(dataset uintptr) uintptr
	datasetDelete        func(dataset uintptr, predicate string, outNumDeleted *uint64) int32
	datasetUpdate        func(dataset uintptr, predicate uintptr, columns []uintptr, values []uintptr, numUpdates uint64, outNumUpdated *uint64) int32
	datasetMergeInsert   func(dataset uintptr, onColumns []uintptr, numOnColumns uint64, source *cdata.ArrowArrayStream, params *mergeInsertParams, outResult *mergeInsertResult) int32
	datasetMerge         func(dataset uintptr, leftOn, rightOn string, source *cdata.ArrowArrayStream) int32
	datasetTruncateTable func(dataset uintptr) int32

	sessionNew               func(indexCacheSize, metadataCacheSize uint64) uintptr
	sessionNewWithCacheSpecs func(indexCache, metadataCache *sessionCacheSpec) uintptr
	sessionClose             func(session uintptr)
	sessionGetCacheStats     func(session uintptr, out *sessionCacheStats) int32
	sessionGetSizeBytes      func(session uintptr, out *uint64) int32
	sessionGetApproxNumItems func(session uintptr, out *uint64) int32

	datasetWriteWithOptions func(uri string, schema *cdata.ArrowSchema, stream *cdata.ArrowArrayStream, mode int32, options *writeOptions, storageOpts []uintptr, outDataset *uintptr) int32

	scannerNew                   func(dataset uintptr, columns []uintptr, filter uintptr) uintptr
	scannerClose                 func(scanner uintptr)
	scannerSetLimit              func(scanner uintptr, limit int64) int32
	scannerSetOffset             func(scanner uintptr, offset int64) int32
	scannerSetBatchSize          func(scanner uintptr, size int64) int32
	scannerSetBatchSizeBytes     func(scanner uintptr, size uint64) int32
	scannerSetProjection         func(scanner uintptr, columns []projectionColumn, count uint64) int32
	scannerSetOrderBy            func(scanner uintptr, orderings []columnOrdering, count uint64) int32
	scannerWithRowID             func(scanner uintptr, enabled bool) int32
	scannerWithRowAddress        func(scanner uintptr, enabled bool) int32
	scannerSetSubstraitFilter    func(scanner uintptr, data []byte, length uint64) int32
	scannerSetSubstraitAggregate func(scanner uintptr, data []byte, length uint64) int32
	scannerAdditionalSQLFilter   func(scanner uintptr, filter string) int32
	scannerSetStatisticsCallback func(scanner uintptr, callback uintptr, ctx uintptr) int32
	scannerSchema                func(scanner uintptr, out *cdata.ArrowSchema) int32
	scannerPollNext              func(scanner uintptr, waker uintptr, wakerCtx uintptr, out *uintptr) int32

	batchToArrow func(batch uintptr, outArray *cdata.ArrowArray, outSchema *cdata.ArrowSchema) int32
	batchFree    func(batch uintptr)

	datasetSchemaClone func(dataset uintptr, out *uintptr) int32
	schemaClose        func(schema uintptr)
	schemaClone        func(schema uintptr) uintptr
	schemaToArrow      func(schema uintptr, out *cdata.ArrowSchema) int32
	schemaProject      func(schema uintptr, columns []uintptr, count uint64, out *uintptr) int32
	schemaFieldID      func(schema uintptr, column string, out *int32) int32

	datasetFragmentCount     func(dataset uintptr) uint64
	datasetFragmentIDs       func(dataset uintptr, outIDs []uint64) int32
	datasetOpenFragment      func(dataset uintptr, fragmentID uint64) uintptr
	fragmentClose            func(fragment uintptr)
	fragmentID               func(fragment uintptr) uint64
	fragmentDataFileCount    func(fragment uintptr) uint64
	fragmentIsBound          func(fragment uintptr, out *bool) int32
	fragmentDataFileAt       func(fragment uintptr, index uint64, out *uintptr) int32
	fragmentDataFileForField func(fragment uintptr, fieldID int32, out *uintptr) int32
	fragmentMetadataJSON     func(fragment uintptr) *byte
	fragmentSchema           func(fragment uintptr, out *cdata.ArrowSchema) int32
	fragmentSchemaClone      func(fragment uintptr, out *uintptr) int32
	fragmentPhysicalRows     func(fragment uintptr, out *uint64) int32
	fragmentDeletedRows      func(fragment uintptr, out *uint64) int32
	fragmentLogicalRows      func(fragment uintptr, out *uint64) int32
	fragmentScan             func(fragment uintptr) uintptr
	fragmentTake             func(fragment uintptr, indices []uint32, count uint64, columns []uintptr, out *cdata.ArrowArrayStream) int32
	fragmentCountRows        func(fragment uintptr, filter uintptr, out *uint64) int32
	fragmentDelete           func(fragment uintptr, predicate string, out *uintptr) int32

	datasetCreateDataFile    func(dataset uintptr, path string, baseID *uint32, out *uintptr) int32
	dataFileClose            func(file uintptr)
	dataFileClone            func(file uintptr) uintptr
	dataFilePath             func(file uintptr) *byte
	dataFileFieldCount       func(file uintptr) uint64
	dataFileFieldIDAt        func(file uintptr, index uint64, out *int32) int32
	dataFileColumnIndexCount func(file uintptr) uint64
	dataFileColumnIndexAt    func(file uintptr, index uint64, out *int32) int32
	dataFileFileVersion      func(file uintptr, outMajor, outMinor *uint32) int32
	dataFileFileSize         func(file uintptr, outSet *bool, outSize *uint64) int32
	dataFileBaseID           func(file uintptr, outSet *bool, outBaseID *uint32) int32

	datasetWriteUncommittedStream                func(uri string, schema *cdata.ArrowSchema, stream *cdata.ArrowArrayStream, mode int32, options *uncommittedWriteOptions, storageOpts []uintptr, session uintptr, out *uintptr) int32
	transactionClose                             func(transaction uintptr)
	operationClose                               func(operation uintptr)
	dataReplacementGroupClose                    func(group uintptr)
	dataReplacementGroupNew                      func(fragmentID uint64, file uintptr) uintptr
	dataReplacementGroupClone                    func(group uintptr) uintptr
	dataReplacementGroupFragmentID               func(group uintptr) uint64
	dataReplacementGroupFile                     func(group uintptr, out *uintptr) int32
	operationDataReplacementNew                  func(groups []uintptr, count uint64) uintptr
	transactionReadVersion                       func(transaction uintptr, out *uint64) int32
	operationDeleteNew                           func(updated []uintptr, updatedCount uint64, deletedIDs []uint64, deletedCount uint64, predicate string) uintptr
	operationAppendNew                           func(fragments []uintptr, count uint64) uintptr
	operationRestoreNew                          func(version uint64) uintptr
	operationReserveFragmentsNew                 func(count uint32) uintptr
	operationUpdateBasesNew                      func(bases *basePath, count uint64) uintptr
	transactionNew                               func(readVersion uint64, operation uintptr, options *transactionOptions) uintptr
	transactionUUID                              func(transaction uintptr) *byte
	transactionTag                               func(transaction uintptr, outTag **byte) int32
	transactionHasProperties                     func(transaction uintptr, out *bool) int32
	transactionPropertyCount                     func(transaction uintptr, out *uint64) int32
	transactionPropertyKeyAt                     func(transaction uintptr, index uint64) *byte
	transactionPropertyValueAt                   func(transaction uintptr, index uint64) *byte
	transactionOperation                         func(transaction uintptr, out *uintptr) int32
	operationKind                                func(operation uintptr, out *int32) int32
	operationFragmentCount                       func(operation uintptr, out *uint64) int32
	operationAppendFragmentCount                 func(operation uintptr, out *uint64) int32
	operationAppendFragmentAt                    func(operation uintptr, index uint64, out *uintptr) int32
	operationRestoreVersion                      func(operation uintptr, out *uint64) int32
	operationReserveFragmentsCount               func(operation uintptr, out *uint32) int32
	operationUpdateBasesCount                    func(operation uintptr, out *uint64) int32
	operationUpdateBasesAt                       func(operation uintptr, index uint64, out *basePathView) int32
	operationDeleteUpdatedFragmentCount          func(operation uintptr, out *uint64) int32
	operationDeleteUpdatedFragmentMetadataJSONAt func(operation uintptr, index uint64) *byte
	operationDeleteDeletedFragmentCount          func(operation uintptr, out *uint64) int32
	operationDeleteDeletedFragmentIDAt           func(operation uintptr, index uint64, out *uint64) int32
	operationDeletePredicate                     func(operation uintptr) *byte
	operationDataReplacementCount                func(operation uintptr, out *uint64) int32
	operationDataReplacementAt                   func(operation uintptr, index uint64, out *uintptr) int32
	operationSchemaClone                         func(operation uintptr, out *uintptr) int32
	operationProjectNew                          func(schema uintptr, preserves bool, out *uintptr) int32
	operationPreservesNullability                func(operation uintptr, out *bool) int32
	operationSchema                              func(operation uintptr, out *cdata.ArrowSchema) int32
	transactionToProto                           func(transaction uintptr, outBytes **byte, outLen *uint64) int32
	transactionFromProto                         func(bytes []byte, length uint64) uintptr
	datasetReadTransaction                       func(dataset uintptr, out *uintptr) int32
	datasetReadTransactionByVersion              func(dataset uintptr, version uint64, out *uintptr) int32
	transactionCommit                            func(uri string, transaction uintptr, options *commitOptions, storageOpts []uintptr, session uintptr, out *uintptr) int32
	transactionCommitBatch                       func(uri string, transactions []uintptr, count uint64, options *commitOptions, storageOpts []uintptr, session uintptr, outDataset *uintptr, outTransaction *uintptr) int32
	freeBytes                                    func(bytes *byte)
}

// symbol maps one C symbol to the Go function pointer that receives it.
type symbol struct {
	name   string
	target any
}

// symbols is the complete set of C entry points this package binds.
// scripts/check-abi-symbols.go parses this list and compares it with the
// pinned header; keep the literal form ([]symbol{{"name", &b.field}}) intact.
func (b *bindings) symbols() []symbol {
	return []symbol{
		{"lance_last_error_code", &b.lastErrorCode},
		{"lance_last_error_message", &b.lastErrorMessage},
		{"lance_free_string", &b.freeString},
		{"lance_dataset_open", &b.datasetOpen},
		{"lance_dataset_open_with_session", &b.datasetOpenWithSession},
		{"lance_dataset_close", &b.datasetClose},
		{"lance_dataset_version", &b.datasetVersion},
		{"lance_dataset_count_rows", &b.datasetCountRows},
		{"lance_dataset_count_rows_filtered", &b.datasetCountRowsFiltered},
		{"lance_dataset_latest_version", &b.datasetLatestVersion},
		{"lance_dataset_validate", &b.datasetValidate},
		{"lance_dataset_schema", &b.datasetSchema},
		{"lance_dataset_calculate_data_stats", &b.datasetDataStats},
		{"lance_data_statistics_count", &b.dataStatsCount},
		{"lance_data_statistics_field_id_at", &b.dataStatsFieldIDAt},
		{"lance_data_statistics_bytes_on_disk_at", &b.dataStatsBytesAt},
		{"lance_data_statistics_close", &b.dataStatsClose},
		{"lance_dataset_take", &b.datasetTake},
		{"lance_dataset_take_rows", &b.datasetTakeRows},

		{"lance_dataset_clone_handle", &b.datasetCloneHandle},
		{"lance_dataset_delete", &b.datasetDelete},
		{"lance_dataset_update", &b.datasetUpdate},
		{"lance_dataset_merge_insert", &b.datasetMergeInsert},
		{"lance_dataset_merge", &b.datasetMerge},
		{"lance_dataset_truncate_table", &b.datasetTruncateTable},

		{"lance_session_new", &b.sessionNew},
		{"lance_session_new_with_cache_specs", &b.sessionNewWithCacheSpecs},
		{"lance_session_close", &b.sessionClose},
		{"lance_session_get_cache_stats", &b.sessionGetCacheStats},
		{"lance_session_get_size_bytes", &b.sessionGetSizeBytes},
		{"lance_session_get_approx_num_items", &b.sessionGetApproxNumItems},

		{"lance_dataset_write_with_options", &b.datasetWriteWithOptions},

		{"lance_dataset_schema_clone", &b.datasetSchemaClone},
		{"lance_schema_close", &b.schemaClose},
		{"lance_schema_clone", &b.schemaClone},
		{"lance_schema_to_arrow", &b.schemaToArrow},
		{"lance_schema_project", &b.schemaProject},
		{"lance_schema_field_id", &b.schemaFieldID},

		{"lance_dataset_fragment_count", &b.datasetFragmentCount},
		{"lance_dataset_fragment_ids", &b.datasetFragmentIDs},
		{"lance_dataset_open_fragment", &b.datasetOpenFragment},
		{"lance_fragment_close", &b.fragmentClose},
		{"lance_fragment_id", &b.fragmentID},
		{"lance_fragment_data_file_count", &b.fragmentDataFileCount},
		{"lance_fragment_is_bound", &b.fragmentIsBound},
		{"lance_fragment_data_file_at", &b.fragmentDataFileAt},
		{"lance_fragment_data_file_for_field", &b.fragmentDataFileForField},
		{"lance_fragment_metadata_json", &b.fragmentMetadataJSON},
		{"lance_fragment_schema", &b.fragmentSchema},
		{"lance_fragment_schema_clone", &b.fragmentSchemaClone},
		{"lance_fragment_physical_rows", &b.fragmentPhysicalRows},
		{"lance_fragment_deleted_rows", &b.fragmentDeletedRows},
		{"lance_fragment_logical_rows", &b.fragmentLogicalRows},
		{"lance_fragment_scan", &b.fragmentScan},
		{"lance_fragment_take", &b.fragmentTake},
		{"lance_fragment_count_rows", &b.fragmentCountRows},
		{"lance_fragment_delete", &b.fragmentDelete},

		{"lance_dataset_create_data_file", &b.datasetCreateDataFile},
		{"lance_data_file_close", &b.dataFileClose},
		{"lance_data_file_clone", &b.dataFileClone},
		{"lance_data_file_path", &b.dataFilePath},
		{"lance_data_file_field_count", &b.dataFileFieldCount},
		{"lance_data_file_field_id_at", &b.dataFileFieldIDAt},
		{"lance_data_file_column_index_count", &b.dataFileColumnIndexCount},
		{"lance_data_file_column_index_at", &b.dataFileColumnIndexAt},
		{"lance_data_file_file_version", &b.dataFileFileVersion},
		{"lance_data_file_file_size", &b.dataFileFileSize},
		{"lance_data_file_base_id", &b.dataFileBaseID},

		{"lance_dataset_write_uncommitted_stream", &b.datasetWriteUncommittedStream},
		{"lance_transaction_close", &b.transactionClose},
		{"lance_operation_close", &b.operationClose},
		{"lance_data_replacement_group_close", &b.dataReplacementGroupClose},
		{"lance_data_replacement_group_new", &b.dataReplacementGroupNew},
		{"lance_data_replacement_group_clone", &b.dataReplacementGroupClone},
		{"lance_data_replacement_group_fragment_id", &b.dataReplacementGroupFragmentID},
		{"lance_data_replacement_group_file", &b.dataReplacementGroupFile},
		{"lance_operation_data_replacement_new", &b.operationDataReplacementNew},
		{"lance_transaction_read_version", &b.transactionReadVersion},
		{"lance_operation_delete_new", &b.operationDeleteNew},
		{"lance_operation_append_new", &b.operationAppendNew},
		{"lance_operation_restore_new", &b.operationRestoreNew},
		{"lance_operation_reserve_fragments_new", &b.operationReserveFragmentsNew},
		{"lance_operation_update_bases_new", &b.operationUpdateBasesNew},
		{"lance_transaction_new", &b.transactionNew},
		{"lance_transaction_uuid", &b.transactionUUID},
		{"lance_transaction_tag", &b.transactionTag},
		{"lance_transaction_has_properties", &b.transactionHasProperties},
		{"lance_transaction_property_count", &b.transactionPropertyCount},
		{"lance_transaction_property_key_at", &b.transactionPropertyKeyAt},
		{"lance_transaction_property_value_at", &b.transactionPropertyValueAt},
		{"lance_transaction_operation", &b.transactionOperation},
		{"lance_operation_kind", &b.operationKind},
		{"lance_operation_fragment_count", &b.operationFragmentCount},
		{"lance_operation_append_fragment_count", &b.operationAppendFragmentCount},
		{"lance_operation_append_fragment_at", &b.operationAppendFragmentAt},
		{"lance_operation_restore_version", &b.operationRestoreVersion},
		{"lance_operation_reserve_fragments_count", &b.operationReserveFragmentsCount},
		{"lance_operation_update_bases_count", &b.operationUpdateBasesCount},
		{"lance_operation_update_bases_at", &b.operationUpdateBasesAt},
		{"lance_operation_delete_updated_fragment_count", &b.operationDeleteUpdatedFragmentCount},
		{"lance_operation_delete_updated_fragment_metadata_json_at", &b.operationDeleteUpdatedFragmentMetadataJSONAt},
		{"lance_operation_delete_deleted_fragment_count", &b.operationDeleteDeletedFragmentCount},
		{"lance_operation_delete_deleted_fragment_id_at", &b.operationDeleteDeletedFragmentIDAt},
		{"lance_operation_delete_predicate", &b.operationDeletePredicate},
		{"lance_operation_data_replacement_count", &b.operationDataReplacementCount},
		{"lance_operation_data_replacement_at", &b.operationDataReplacementAt},
		{"lance_operation_schema_clone", &b.operationSchemaClone},
		{"lance_operation_project_new", &b.operationProjectNew},
		{"lance_operation_preserves_nullability", &b.operationPreservesNullability},
		{"lance_operation_schema", &b.operationSchema},
		{"lance_transaction_to_proto", &b.transactionToProto},
		{"lance_transaction_from_proto", &b.transactionFromProto},
		{"lance_dataset_read_transaction", &b.datasetReadTransaction},
		{"lance_dataset_read_transaction_by_version", &b.datasetReadTransactionByVersion},
		{"lance_transaction_commit", &b.transactionCommit},
		{"lance_transaction_commit_batch", &b.transactionCommitBatch},
		{"lance_free_bytes", &b.freeBytes},

		{"lance_scanner_new", &b.scannerNew},
		{"lance_scanner_close", &b.scannerClose},
		{"lance_scanner_set_limit", &b.scannerSetLimit},
		{"lance_scanner_set_offset", &b.scannerSetOffset},
		{"lance_scanner_set_batch_size", &b.scannerSetBatchSize},
		{"lance_scanner_set_batch_size_bytes", &b.scannerSetBatchSizeBytes},
		{"lance_scanner_set_projection", &b.scannerSetProjection},
		{"lance_scanner_set_order_by", &b.scannerSetOrderBy},
		{"lance_scanner_with_row_id", &b.scannerWithRowID},
		{"lance_scanner_with_row_address", &b.scannerWithRowAddress},
		{"lance_scanner_set_substrait_filter", &b.scannerSetSubstraitFilter},
		{"lance_scanner_set_substrait_aggregate", &b.scannerSetSubstraitAggregate},
		{"lance_scanner_additional_sql_filter", &b.scannerAdditionalSQLFilter},
		{"lance_scanner_set_statistics_callback", &b.scannerSetStatisticsCallback},
		{"lance_scanner_schema", &b.scannerSchema},
		{"lance_scanner_poll_next", &b.scannerPollNext},

		{"lance_batch_to_arrow", &b.batchToArrow},
		{"lance_batch_free", &b.batchFree},
	}
}

var (
	mu    sync.Mutex
	bound *bindings
)

// load returns the process-wide bindings, resolving symbols on first use.
// Failed attempts are not cached; a later call retries.
func load() (*bindings, error) {
	mu.Lock()
	defer mu.Unlock()

	if bound != nil {
		return bound, nil
	}

	lib, err := loader.Load()
	if err != nil {
		return nil, err
	}
	b, err := bind(lib)
	if err != nil {
		return nil, err
	}
	bound = b
	return b, nil
}

func bind(lib *loader.Library) (*bindings, error) {
	b := &bindings{}

	var missing []string
	for _, s := range b.symbols() {
		addr, err := lib.Lookup(s.name)
		if err != nil {
			missing = append(missing, s.name)
			continue
		}
		purego.RegisterFunc(s.target, addr)
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf(
			"ffi: %s does not export required symbol(s) %s; rebuild lance-c from %s (%s) or install a build that matches this binding",
			lib.Path(), strings.Join(missing, ", "), upstreamRepo, upstreamCommit,
		)
	}
	return b, nil
}
