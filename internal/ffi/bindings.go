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

	datasetCurrentVersion  func(dataset uintptr) uintptr
	versionID              func(version uintptr) uint64
	versionTimestampNS     func(version uintptr) int64
	versionMetadataCount   func(version uintptr) uint64
	versionMetadataKeyAt   func(version uintptr, index uint64) *byte
	versionMetadataValueAt func(version uintptr, index uint64) *byte
	versionClose           func(version uintptr)
	datasetVersions        func(dataset uintptr) uintptr
	versionsCount          func(versions uintptr) uint64
	versionsAt             func(versions uintptr, index uint64) uintptr
	versionsIDAt           func(versions uintptr, index uint64) uint64
	versionsTimestampMSAt  func(versions uintptr, index uint64) int64
	versionsClose          func(versions uintptr)
	datasetVersionRefs     func(dataset uintptr) uintptr
	versionRefsCount       func(refs uintptr) uint64
	versionRefsIDAt        func(refs uintptr, index uint64) uint64
	versionRefsClose       func(refs uintptr)
	refVersionNumber       func(version uint64) uintptr
	refVersion             func(branch string, hasVersion bool, version uint64) uintptr
	refTag                 func(tag string) uintptr
	refClose               func(ref uintptr)
	datasetCheckoutRef     func(dataset uintptr, ref uintptr) uintptr
	datasetCheckoutBranch  func(dataset uintptr, branch string) uintptr
	datasetRestore         func(dataset uintptr, version uint64) uintptr

	datasetTags               func(dataset uintptr) uintptr
	tagsCount                 func(tags uintptr) uint64
	tagsAt                    func(tags uintptr, index uint64) uintptr
	tagsClose                 func(tags uintptr)
	datasetTagGet             func(dataset uintptr, name string) uintptr
	datasetTagCreate          func(dataset uintptr, name string, ref uintptr) int32
	datasetTagUpdate          func(dataset uintptr, name string, ref uintptr) int32
	datasetTagDelete          func(dataset uintptr, name string) int32
	datasetTagReplaceMetadata func(dataset uintptr, name string, metadata []uintptr) int32
	tagName                   func(tag uintptr) *byte
	tagVersion                func(tag uintptr) uint64
	tagBranch                 func(tag uintptr, out **byte) int32
	tagCreatedAtNS            func(tag uintptr, out *int64) int32
	tagUpdatedAtNS            func(tag uintptr, out *int64) int32
	tagManifestSize           func(tag uintptr) uint64
	tagMetadataCount          func(tag uintptr) uint64
	tagMetadataKeyAt          func(tag uintptr, index uint64) *byte
	tagMetadataValueAt        func(tag uintptr, index uint64) *byte
	tagClose                  func(tag uintptr)

	datasetBranchesJSON          func(dataset uintptr) *byte
	datasetBranchGetJSON         func(dataset uintptr, name string) *byte
	datasetBranchIdentifierJSON  func(dataset uintptr) *byte
	datasetBranchCreate          func(dataset uintptr, name string, ref uintptr, storageOpts []uintptr) uintptr
	datasetBranchDelete          func(dataset uintptr, name string) int32
	datasetBranchForceDelete     func(dataset uintptr, name string) int32
	datasetBranchReplaceMetadata func(dataset uintptr, name string, metadata []uintptr) int32

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

		{"lance_dataset_current_version", &b.datasetCurrentVersion},
		{"lance_version_id", &b.versionID},
		{"lance_version_timestamp_ns", &b.versionTimestampNS},
		{"lance_version_metadata_count", &b.versionMetadataCount},
		{"lance_version_metadata_key_at", &b.versionMetadataKeyAt},
		{"lance_version_metadata_value_at", &b.versionMetadataValueAt},
		{"lance_version_close", &b.versionClose},
		{"lance_dataset_versions", &b.datasetVersions},
		{"lance_versions_count", &b.versionsCount},
		{"lance_versions_at", &b.versionsAt},
		{"lance_versions_id_at", &b.versionsIDAt},
		{"lance_versions_timestamp_ms_at", &b.versionsTimestampMSAt},
		{"lance_versions_close", &b.versionsClose},
		{"lance_dataset_version_refs", &b.datasetVersionRefs},
		{"lance_version_refs_count", &b.versionRefsCount},
		{"lance_version_refs_id_at", &b.versionRefsIDAt},
		{"lance_version_refs_close", &b.versionRefsClose},
		{"lance_ref_version_number", &b.refVersionNumber},
		{"lance_ref_version", &b.refVersion},
		{"lance_ref_tag", &b.refTag},
		{"lance_ref_close", &b.refClose},
		{"lance_dataset_checkout_ref", &b.datasetCheckoutRef},
		{"lance_dataset_checkout_branch", &b.datasetCheckoutBranch},
		{"lance_dataset_restore", &b.datasetRestore},

		{"lance_dataset_tags", &b.datasetTags},
		{"lance_tags_count", &b.tagsCount},
		{"lance_tags_at", &b.tagsAt},
		{"lance_tags_close", &b.tagsClose},
		{"lance_dataset_tag_get", &b.datasetTagGet},
		{"lance_dataset_tag_create", &b.datasetTagCreate},
		{"lance_dataset_tag_update", &b.datasetTagUpdate},
		{"lance_dataset_tag_delete", &b.datasetTagDelete},
		{"lance_dataset_tag_replace_metadata", &b.datasetTagReplaceMetadata},
		{"lance_tag_name", &b.tagName},
		{"lance_tag_version", &b.tagVersion},
		{"lance_tag_branch", &b.tagBranch},
		{"lance_tag_created_at_ns", &b.tagCreatedAtNS},
		{"lance_tag_updated_at_ns", &b.tagUpdatedAtNS},
		{"lance_tag_manifest_size", &b.tagManifestSize},
		{"lance_tag_metadata_count", &b.tagMetadataCount},
		{"lance_tag_metadata_key_at", &b.tagMetadataKeyAt},
		{"lance_tag_metadata_value_at", &b.tagMetadataValueAt},
		{"lance_tag_close", &b.tagClose},

		{"lance_dataset_branches_json", &b.datasetBranchesJSON},
		{"lance_dataset_branch_get_json", &b.datasetBranchGetJSON},
		{"lance_dataset_branch_identifier_json", &b.datasetBranchIdentifierJSON},
		{"lance_dataset_branch_create", &b.datasetBranchCreate},
		{"lance_dataset_branch_delete", &b.datasetBranchDelete},
		{"lance_dataset_branch_force_delete", &b.datasetBranchForceDelete},
		{"lance_dataset_branch_replace_metadata", &b.datasetBranchReplaceMetadata},

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
