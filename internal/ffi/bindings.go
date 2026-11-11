package ffi

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

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
	datasetTakeBlobs         func(dataset uintptr, rowIDs []uint64, numRowIDs uint64, column string, out *uintptr) int32
	datasetTakeBlobsIndices  func(dataset uintptr, indices []uint64, numIndices uint64, column string, out *uintptr) int32
	datasetTakeBlobsAddrs    func(dataset uintptr, rowAddresses []uint64, numAddresses uint64, column string, out *uintptr) int32
	datasetReadBlobs         func(dataset uintptr, column string, selector int32, rows []uint64, numRows uint64, options *blobReadOptions, out *cdata.ArrowArrayStream) int32
	datasetReadBlobRanges    func(dataset uintptr, column string, selector int32, requests []blobRangeRequest, numRequests uint64, options *blobReadOptions, out *cdata.ArrowArrayStream) int32

	blobFileSize          func(blob uintptr) uint64
	blobFileRead          func(blob uintptr, dst *byte, dstLen uint64) int32
	blobFileReadUpTo      func(blob uintptr, dst *byte, dstLen uint64, bytesRead *uint64) int32
	blobFileReadRange     func(blob uintptr, offset uint64, dst *byte, dstLen uint64) int32
	blobFileReadRanges    func(blob uintptr, ranges []blobRange, numRanges uint64, destinations []unsafe.Pointer, destinationLens []uintptr) int32
	blobFileSeek          func(blob uintptr, pos uint64) int32
	blobFileTell          func(blob uintptr, pos *uint64) int32
	blobFileSetBufferSize func(blob uintptr, size uint64) int32
	blobFileClose         func(blob uintptr)

	blobField               func(name string, nullable bool, out *cdata.ArrowSchema) int32
	blobFieldWithOptions    func(name string, nullable bool, options *blobFieldOptions, out *cdata.ArrowSchema) int32
	blobDescriptorNew       func(column string) uintptr
	blobDescriptorField     func(builder uintptr, out *cdata.ArrowSchema) int32
	blobDescriptorInline    func(builder uintptr, data *byte, length uint64) int32
	blobDescriptorNull      func(builder uintptr) int32
	blobDescriptorPacked    func(builder uintptr, blobID uint32, offset uint64, length uint64) int32
	blobDescriptorExtend    func(builder uintptr, blobID uint32, ranges []blobRange, numRanges uint64) int32
	blobDescriptorDedicated func(builder uintptr, blobID uint32, length uint64) int32
	blobDescriptorManaged   func(builder uintptr, uri string, offset uint64, length uint64, hasBaseID bool, baseID uint32) int32
	blobDescriptorExtern    func(builder uintptr, baseID uint32, uri string, offset uint64, length uint64) int32
	blobDescriptorPush      func(builder uintptr, uri string, hasRange bool, offset uint64, length uint64) int32
	blobDescriptorFinish    func(builder uintptr, outArray *cdata.ArrowArray, outSchema *cdata.ArrowSchema) int32
	blobDescriptorClose     func(builder uintptr)

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
		{"lance_dataset_take_blobs", &b.datasetTakeBlobs},
		{"lance_dataset_take_blobs_by_indices", &b.datasetTakeBlobsIndices},
		{"lance_dataset_take_blobs_by_addresses", &b.datasetTakeBlobsAddrs},
		{"lance_dataset_read_blobs", &b.datasetReadBlobs},
		{"lance_dataset_read_blob_ranges", &b.datasetReadBlobRanges},
		{"lance_blob_file_size", &b.blobFileSize},
		{"lance_blob_file_read", &b.blobFileRead},
		{"lance_blob_file_read_up_to", &b.blobFileReadUpTo},
		{"lance_blob_file_read_range", &b.blobFileReadRange},
		{"lance_blob_file_read_ranges", &b.blobFileReadRanges},
		{"lance_blob_file_seek", &b.blobFileSeek},
		{"lance_blob_file_tell", &b.blobFileTell},
		{"lance_blob_file_set_buffer_size", &b.blobFileSetBufferSize},
		{"lance_blob_file_close", &b.blobFileClose},
		{"lance_blob_field", &b.blobField},
		{"lance_blob_field_with_options", &b.blobFieldWithOptions},
		{"lance_blob_descriptor_array_builder_new", &b.blobDescriptorNew},
		{"lance_blob_descriptor_array_builder_field", &b.blobDescriptorField},
		{"lance_blob_descriptor_array_builder_append_inline", &b.blobDescriptorInline},
		{"lance_blob_descriptor_array_builder_append_null", &b.blobDescriptorNull},
		{"lance_blob_descriptor_array_builder_append_packed", &b.blobDescriptorPacked},
		{"lance_blob_descriptor_array_builder_extend_packed", &b.blobDescriptorExtend},
		{"lance_blob_descriptor_array_builder_append_dedicated", &b.blobDescriptorDedicated},
		{"lance_blob_descriptor_array_builder_append_managed", &b.blobDescriptorManaged},
		{"lance_blob_descriptor_array_builder_append_external", &b.blobDescriptorExtern},
		{"lance_blob_descriptor_array_builder_push_external", &b.blobDescriptorPush},
		{"lance_blob_descriptor_array_builder_finish", &b.blobDescriptorFinish},
		{"lance_blob_descriptor_array_builder_close", &b.blobDescriptorClose},

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
