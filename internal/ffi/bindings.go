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

	datasetCreateVectorIndexWithOptions func(dataset uintptr, column string, name uintptr, options *vectorIndexOptions, replace bool) int32
	datasetCreateScalarIndexWithOptions func(dataset uintptr, column string, name uintptr, indexType int32, paramsJSON uintptr, options *scalarIndexOptions, replace bool) int32
	datasetDropIndex                    func(dataset uintptr, name string) int32
	datasetIndexCount                   func(dataset uintptr) uint64
	datasetIndexListJSON                func(dataset uintptr) *byte
	datasetDescribeIndicesJSON          func(dataset uintptr) *byte
	datasetIndexStatisticsJSON          func(dataset uintptr, name string) *byte
	datasetIndexSegmentsJSON            func(dataset uintptr) *byte
	datasetIndexSegmentCount            func(dataset uintptr, name string) uint64
	datasetIndexSegments                func(dataset uintptr, name string, outUUIDs []byte, capacity uint64, outCount *uint64) int32
	datasetPrewarmIndex                 func(dataset uintptr, name string) int32
	datasetPrewarmIndexWithOptions      func(dataset uintptr, name string, options *indexPrewarmOptions) int32

	ftsMatchOptionsInit          func(options *ftsMatchOptions)
	ftsPhraseOptionsInit         func(options *ftsPhraseOptions)
	ftsMultiMatchOptionsInit     func(options *ftsMultiMatchOptions)
	ftsCombinedFieldsOptionsInit func(options *ftsCombinedFieldsOptions)
	ftsBoostOptionsInit          func(options *ftsBoostOptions)
	ftsQueryMatchNew             func(terms string, column uintptr, options *ftsMatchOptions) uintptr
	ftsQueryPhraseNew            func(terms string, column uintptr, options *ftsPhraseOptions) uintptr
	ftsQueryMultiMatchNew        func(terms string, columns []uintptr, count uint64, options *ftsMultiMatchOptions) uintptr
	ftsQueryCombinedFieldsNew    func(terms string, columns []uintptr, count uint64, options *ftsCombinedFieldsOptions) uintptr
	ftsQueryBooleanNew           func(occurs []int32, queries []uintptr, count uint64) uintptr
	ftsQueryBoostNew             func(positive, negative uintptr, options *ftsBoostOptions) uintptr
	ftsQueryClose                func(query uintptr)
	ftsQueryContextClose         func(context uintptr)
	datasetPrepareFTSMatchQuery  func(dataset uintptr, column, query string, matchOperator int32, maxFuzzyDistance uint32, coverage int32) uintptr
	datasetPrepareFTSPhraseQuery func(dataset uintptr, column, query string, slop int32, coverage int32) uintptr

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
	scannerSetFTSQuery           func(scanner uintptr, query uintptr) int32
	scannerSetFTSQueryContext    func(scanner uintptr, context uintptr) int32
	scannerSetMinHashQuery       func(scanner uintptr, column, text string) int32
	scannerSetFTSIndexSegments   func(scanner uintptr, uuids []byte, count uint64) int32
	scannerSchema                func(scanner uintptr, out *cdata.ArrowSchema) int32
	scannerPollNext              func(scanner uintptr, waker uintptr, wakerCtx uintptr, out *uintptr) int32

	batchToArrow func(batch uintptr, outArray *cdata.ArrowArray, outSchema *cdata.ArrowSchema) int32
	batchFree    func(batch uintptr)
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
		{"lance_dataset_create_vector_index_with_options", &b.datasetCreateVectorIndexWithOptions},
		{"lance_dataset_create_scalar_index_with_options", &b.datasetCreateScalarIndexWithOptions},
		{"lance_dataset_drop_index", &b.datasetDropIndex},
		{"lance_dataset_index_count", &b.datasetIndexCount},
		{"lance_dataset_index_list_json", &b.datasetIndexListJSON},
		{"lance_dataset_describe_indices_json", &b.datasetDescribeIndicesJSON},
		{"lance_dataset_index_statistics_json", &b.datasetIndexStatisticsJSON},
		{"lance_dataset_index_segments_json", &b.datasetIndexSegmentsJSON},
		{"lance_dataset_index_segment_count", &b.datasetIndexSegmentCount},
		{"lance_dataset_index_segments", &b.datasetIndexSegments},
		{"lance_dataset_prewarm_index", &b.datasetPrewarmIndex},
		{"lance_dataset_prewarm_index_with_options", &b.datasetPrewarmIndexWithOptions},
		{"lance_fts_query_match_options_init", &b.ftsMatchOptionsInit},
		{"lance_fts_query_phrase_options_init", &b.ftsPhraseOptionsInit},
		{"lance_fts_query_multi_match_options_init", &b.ftsMultiMatchOptionsInit},
		{"lance_fts_query_combined_fields_options_init", &b.ftsCombinedFieldsOptionsInit},
		{"lance_fts_query_boost_options_init", &b.ftsBoostOptionsInit},
		{"lance_fts_query_match_new", &b.ftsQueryMatchNew},
		{"lance_fts_query_phrase_new", &b.ftsQueryPhraseNew},
		{"lance_fts_query_multi_match_new", &b.ftsQueryMultiMatchNew},
		{"lance_fts_query_combined_fields_new", &b.ftsQueryCombinedFieldsNew},
		{"lance_fts_query_boolean_new", &b.ftsQueryBooleanNew},
		{"lance_fts_query_boost_new", &b.ftsQueryBoostNew},
		{"lance_fts_query_close", &b.ftsQueryClose},
		{"lance_fts_query_context_close", &b.ftsQueryContextClose},
		{"lance_dataset_prepare_fts_match_query", &b.datasetPrepareFTSMatchQuery},
		{"lance_dataset_prepare_fts_phrase_query", &b.datasetPrepareFTSPhraseQuery},

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
		{"lance_scanner_set_fts_query", &b.scannerSetFTSQuery},
		{"lance_scanner_set_fts_query_context", &b.scannerSetFTSQueryContext},
		{"lance_scanner_set_minhash_query", &b.scannerSetMinHashQuery},
		{"lance_scanner_set_fts_index_segments", &b.scannerSetFTSIndexSegments},
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
