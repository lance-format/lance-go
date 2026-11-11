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

	compactionOptionsNew                              func() uintptr
	compactionOptionsFree                             func(uintptr)
	compactionOptionsSetTargetRowsPerFragment         func(uintptr, uint64) int32
	compactionOptionsSetMaxRowsPerGroup               func(uintptr, uint64) int32
	compactionOptionsSetMaxBytesPerFile               func(uintptr, bool, uint64) int32
	compactionOptionsSetMaterializeDeletions          func(uintptr, bool) int32
	compactionOptionsSetMaterializeDeletionsThreshold func(uintptr, float32) int32
	compactionOptionsSetNumThreads                    func(uintptr, bool, uint64) int32
	compactionOptionsSetBatchSize                     func(uintptr, bool, uint64) int32
	compactionOptionsSetIOBufferSize                  func(uintptr, bool, uint64) int32
	compactionOptionsSetDeferIndexRemap               func(uintptr, bool) int32
	compactionOptionsSetIndexRemapMode                func(uintptr, int32) int32
	compactionOptionsSetCompactionMode                func(uintptr, bool, int32) int32
	compactionOptionsSetBinaryCopyReadBatchBytes      func(uintptr, bool, uint64) int32
	compactionOptionsSetMaxSourceFragments            func(uintptr, bool, uint64) int32
	compactionOptionsSetMaxSourceRows                 func(uintptr, bool, uint64) int32
	compactionOptionsSetMaxSourceBytes                func(uintptr, bool, uint64) int32
	compactionOptionsSetExcludedFragmentIDs           func(uintptr, []uint32, uint64) int32
	compactionOptionsSetMaxOverlaysPerFragment        func(uintptr, bool, uint64) int32
	compactionOptionsSetDataStorageVersion            func(uintptr, *byte) int32
	compactionOptionsSetTransactionProperties         func(uintptr, *transactionProperties) int32

	datasetCompactFilesV2 func(uintptr, uintptr, *compactionMetrics) int32
	datasetPlanCompaction func(uintptr, uintptr) uintptr

	compactionPlanReadVersion func(uintptr) uint64
	compactionPlanTaskCount   func(uintptr) uint64
	compactionPlanTaskAt      func(uintptr, uint64) uintptr
	compactionPlanJSON        func(uintptr) *byte
	compactionPlanFromJSON    func(string) uintptr
	compactionPlanClose       func(uintptr)

	compactionTaskReadVersion            func(uintptr) uint64
	compactionTaskFragmentCount          func(uintptr) uint64
	compactionTaskFragmentMetadataJSONAt func(uintptr, uint64) *byte
	compactionTaskExecute                func(uintptr, uintptr) uintptr
	compactionTaskJSON                   func(uintptr) *byte
	compactionTaskFromJSON               func(string) uintptr
	compactionTaskClose                  func(uintptr)

	rewriteResultReadVersion                    func(uintptr) uint64
	rewriteResultMetrics                        func(uintptr, *compactionMetrics) int32
	rewriteResultOriginalFragmentCount          func(uintptr) uint64
	rewriteResultNewFragmentCount               func(uintptr) uint64
	rewriteResultOriginalFragmentMetadataJSONAt func(uintptr, uint64) *byte
	rewriteResultNewFragmentMetadataJSONAt      func(uintptr, uint64) *byte
	rewriteResultJSON                           func(uintptr) *byte
	rewriteResultFromJSON                       func(string) uintptr
	rewriteResultClose                          func(uintptr)

	datasetCommitCompaction func(uintptr, []uintptr, uint64, uintptr, *compactionMetrics) int32

	datasetCleanupOldVersions func(uintptr, *cleanupOldVersionsOptions, *removalStats) int32
	datasetCleanup            func(uintptr, *cleanupPolicy, *cleanupOperationOptions, *removalStats) int32
	datasetCleanupExplain     func(uintptr, *cleanupPolicy, *cleanupOperationOptions) uintptr

	cleanupExplanationReadVersion                        func(uintptr) uint64
	cleanupExplanationStats                              func(uintptr, *removalStats) int32
	cleanupExplanationCandidateCount                     func(uintptr) uint64
	cleanupExplanationCandidateFilesTruncated            func(uintptr) int32
	cleanupExplanationCandidateFileLimit                 func(uintptr) uint64
	cleanupExplanationCandidatePathAt                    func(uintptr, uint64) *byte
	cleanupExplanationCandidateKindAt                    func(uintptr, uint64) int32
	cleanupExplanationCandidateUnverifiedAt              func(uintptr, uint64) int32
	cleanupExplanationCandidateSizeBytesAt               func(uintptr, uint64) uint64
	cleanupExplanationReferencedBranchCount              func(uintptr) uint64
	cleanupExplanationReferencedBranchNameAt             func(uintptr, uint64) *byte
	cleanupExplanationReferencedBranchVersionAt          func(uintptr, uint64) uint64
	cleanupExplanationReferencedBranchCleanupCandidateAt func(uintptr, uint64) int32
	cleanupExplanationWarningCount                       func(uintptr) uint64
	cleanupExplanationWarningAt                          func(uintptr, uint64) *byte
	cleanupExplanationClose                              func(uintptr)
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

		{"lance_compaction_options_v2_new", &b.compactionOptionsNew},
		{"lance_compaction_options_v2_free", &b.compactionOptionsFree},
		{"lance_compaction_options_v2_set_target_rows_per_fragment", &b.compactionOptionsSetTargetRowsPerFragment},
		{"lance_compaction_options_v2_set_max_rows_per_group", &b.compactionOptionsSetMaxRowsPerGroup},
		{"lance_compaction_options_v2_set_max_bytes_per_file", &b.compactionOptionsSetMaxBytesPerFile},
		{"lance_compaction_options_v2_set_materialize_deletions", &b.compactionOptionsSetMaterializeDeletions},
		{"lance_compaction_options_v2_set_materialize_deletions_threshold", &b.compactionOptionsSetMaterializeDeletionsThreshold},
		{"lance_compaction_options_v2_set_num_threads", &b.compactionOptionsSetNumThreads},
		{"lance_compaction_options_v2_set_batch_size", &b.compactionOptionsSetBatchSize},
		{"lance_compaction_options_v2_set_io_buffer_size", &b.compactionOptionsSetIOBufferSize},
		{"lance_compaction_options_v2_set_defer_index_remap", &b.compactionOptionsSetDeferIndexRemap},
		{"lance_compaction_options_v2_set_index_remap_mode", &b.compactionOptionsSetIndexRemapMode},
		{"lance_compaction_options_v2_set_compaction_mode", &b.compactionOptionsSetCompactionMode},
		{"lance_compaction_options_v2_set_binary_copy_read_batch_bytes", &b.compactionOptionsSetBinaryCopyReadBatchBytes},
		{"lance_compaction_options_v2_set_max_source_fragments", &b.compactionOptionsSetMaxSourceFragments},
		{"lance_compaction_options_v2_set_max_source_rows", &b.compactionOptionsSetMaxSourceRows},
		{"lance_compaction_options_v2_set_max_source_bytes", &b.compactionOptionsSetMaxSourceBytes},
		{"lance_compaction_options_v2_set_excluded_fragment_ids", &b.compactionOptionsSetExcludedFragmentIDs},
		{"lance_compaction_options_v2_set_max_overlays_per_fragment", &b.compactionOptionsSetMaxOverlaysPerFragment},
		{"lance_compaction_options_v2_set_data_storage_version", &b.compactionOptionsSetDataStorageVersion},
		{"lance_compaction_options_v2_set_transaction_properties", &b.compactionOptionsSetTransactionProperties},

		{"lance_dataset_compact_files_v2", &b.datasetCompactFilesV2},
		{"lance_dataset_plan_compaction", &b.datasetPlanCompaction},

		{"lance_compaction_plan_read_version", &b.compactionPlanReadVersion},
		{"lance_compaction_plan_task_count", &b.compactionPlanTaskCount},
		{"lance_compaction_plan_task_at", &b.compactionPlanTaskAt},
		{"lance_compaction_plan_json", &b.compactionPlanJSON},
		{"lance_compaction_plan_from_json", &b.compactionPlanFromJSON},
		{"lance_compaction_plan_close", &b.compactionPlanClose},

		{"lance_compaction_task_read_version", &b.compactionTaskReadVersion},
		{"lance_compaction_task_fragment_count", &b.compactionTaskFragmentCount},
		{"lance_compaction_task_fragment_metadata_json_at", &b.compactionTaskFragmentMetadataJSONAt},
		{"lance_compaction_task_execute", &b.compactionTaskExecute},
		{"lance_compaction_task_json", &b.compactionTaskJSON},
		{"lance_compaction_task_from_json", &b.compactionTaskFromJSON},
		{"lance_compaction_task_close", &b.compactionTaskClose},

		{"lance_rewrite_result_read_version", &b.rewriteResultReadVersion},
		{"lance_rewrite_result_metrics", &b.rewriteResultMetrics},
		{"lance_rewrite_result_original_fragment_count", &b.rewriteResultOriginalFragmentCount},
		{"lance_rewrite_result_new_fragment_count", &b.rewriteResultNewFragmentCount},
		{"lance_rewrite_result_original_fragment_metadata_json_at", &b.rewriteResultOriginalFragmentMetadataJSONAt},
		{"lance_rewrite_result_new_fragment_metadata_json_at", &b.rewriteResultNewFragmentMetadataJSONAt},
		{"lance_rewrite_result_json", &b.rewriteResultJSON},
		{"lance_rewrite_result_from_json", &b.rewriteResultFromJSON},
		{"lance_rewrite_result_close", &b.rewriteResultClose},

		{"lance_dataset_commit_compaction", &b.datasetCommitCompaction},

		{"lance_dataset_cleanup_old_versions", &b.datasetCleanupOldVersions},
		{"lance_dataset_cleanup", &b.datasetCleanup},
		{"lance_dataset_cleanup_explain", &b.datasetCleanupExplain},

		{"lance_cleanup_explanation_read_version", &b.cleanupExplanationReadVersion},
		{"lance_cleanup_explanation_stats", &b.cleanupExplanationStats},
		{"lance_cleanup_explanation_candidate_count", &b.cleanupExplanationCandidateCount},
		{"lance_cleanup_explanation_candidate_files_truncated", &b.cleanupExplanationCandidateFilesTruncated},
		{"lance_cleanup_explanation_candidate_file_limit", &b.cleanupExplanationCandidateFileLimit},
		{"lance_cleanup_explanation_candidate_path_at", &b.cleanupExplanationCandidatePathAt},
		{"lance_cleanup_explanation_candidate_kind_at", &b.cleanupExplanationCandidateKindAt},
		{"lance_cleanup_explanation_candidate_unverified_at", &b.cleanupExplanationCandidateUnverifiedAt},
		{"lance_cleanup_explanation_candidate_size_bytes_at", &b.cleanupExplanationCandidateSizeBytesAt},
		{"lance_cleanup_explanation_referenced_branch_count", &b.cleanupExplanationReferencedBranchCount},
		{"lance_cleanup_explanation_referenced_branch_name_at", &b.cleanupExplanationReferencedBranchNameAt},
		{"lance_cleanup_explanation_referenced_branch_version_at", &b.cleanupExplanationReferencedBranchVersionAt},
		{"lance_cleanup_explanation_referenced_branch_cleanup_candidate_at", &b.cleanupExplanationReferencedBranchCleanupCandidateAt},
		{"lance_cleanup_explanation_warning_count", &b.cleanupExplanationWarningCount},
		{"lance_cleanup_explanation_warning_at", &b.cleanupExplanationWarningAt},
		{"lance_cleanup_explanation_close", &b.cleanupExplanationClose},

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
