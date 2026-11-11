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

	datasetMetadataJSON             func(dataset uintptr) *byte
	datasetUpdateMetadataJSON       func(dataset uintptr, keys *uintptr, values *uintptr, count uint64, mode int32) *byte
	datasetUpdateSchemaMetadataJSON func(dataset uintptr, keys *uintptr, values *uintptr, count uint64, mode int32) *byte
	datasetUpdateFieldMetadata      func(dataset uintptr, updates *fieldMetadataUpdate, count uint64) int32
	datasetResolveFieldID           func(dataset uintptr, ref *fieldReference, out *int32) int32
	datasetFieldIDCount             func(dataset uintptr, out *uint64) int32
	datasetFieldIDAt                func(dataset uintptr, index uint64, out *int32) int32
	datasetDropColumns              func(dataset uintptr, columns *uintptr, count uint64) int32
	datasetAlterColumns             func(dataset uintptr, alterations *columnAlteration, count uint64) int32
	datasetAddColumnsSQL            func(dataset uintptr, columns *sqlColumn, count uint64, batchSize uint64) int32
	datasetAddColumnsNulls          func(dataset uintptr, schema *cdata.ArrowSchema) int32
	datasetAddColumnsStream         func(dataset uintptr, stream *cdata.ArrowArrayStream, batchSize uint64) int32
	datasetAddColumnsBatchUDF       func(dataset uintptr, udf *batchUDF, readColumns *uintptr, batchSize *uint32) int32
	datasetConfig                   func(dataset uintptr) uintptr
	datasetConfigCount              func(config uintptr) uint64
	datasetConfigKeyAt              func(config uintptr, index uint64) *byte
	datasetConfigValueAt            func(config uintptr, index uint64) *byte
	datasetConfigClose              func(config uintptr)
	datasetUpdateConfigJSON         func(dataset uintptr, keys *uintptr, values *uintptr, count uint64, mode int32) *byte

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

	datasetBasePathsJSON func(dataset uintptr) *byte
	datasetAddBases      func(dataset uintptr, bases []basePath, baseCount uint64, properties *transactionProperties) int32

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
	scannerNearest               func(scanner uintptr, column string, queryData unsafe.Pointer, queryLen uint64, elementType int32, k uint32) int32
	scannerNearestBatch          func(scanner uintptr, column string, queries unsafe.Pointer, dimension uint64, numQueries uint64, elementType int32, k uint32) int32
	scannerNearestMultiVector    func(scanner uintptr, column string, subvectors unsafe.Pointer, dimension uint64, numVectors uint64, elementType int32, k uint32) int32
	scannerSetDistanceRange      func(scanner uintptr, lowerDistance *float32, upperDistance *float32) int32
	scannerSetMetric             func(scanner uintptr, metric int32) int32
	scannerSetNProbes            func(scanner uintptr, nprobes uint32) int32
	scannerSetMinimumNProbes     func(scanner uintptr, nprobes uint32) int32
	scannerSetMaximumNProbes     func(scanner uintptr, nprobes uint32) int32
	scannerSetSearchEffort       func(scanner uintptr, effort float64) int32
	scannerSetApproxMode         func(scanner uintptr, mode int32) int32
	scannerSetQueryParallelism   func(scanner uintptr, parallelism int32) int32
	scannerSetRefineFactor       func(scanner uintptr, factor uint32) int32
	scannerSetEF                 func(scanner uintptr, ef uint32) int32
	scannerSetUseIndex           func(scanner uintptr, useIndex bool) int32
	scannerSetFastSearch         func(scanner uintptr, fastSearch bool) int32
	scannerSetPrefilter          func(scanner uintptr, prefilter bool) int32
	scannerSetRowAddrAllowlist   func(scanner uintptr, rowIDs []uint64, count uint64) int32
	scannerSetRowAddrBlocklist   func(scanner uintptr, rowIDs []uint64, count uint64) int32
	scannerSetStatisticsCallback func(scanner uintptr, callback uintptr, ctx uintptr) int32
	scannerSetFTSQuery           func(scanner uintptr, query uintptr) int32
	scannerSetFTSQueryContext    func(scanner uintptr, context uintptr) int32
	scannerSetMinHashQuery       func(scanner uintptr, column, text string) int32
	scannerSetFTSIndexSegments   func(scanner uintptr, uuids []byte, count uint64) int32
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

		{"lance_dataset_metadata_json", &b.datasetMetadataJSON},
		{"lance_dataset_update_metadata_json", &b.datasetUpdateMetadataJSON},
		{"lance_dataset_update_schema_metadata_json", &b.datasetUpdateSchemaMetadataJSON},
		{"lance_dataset_update_field_metadata", &b.datasetUpdateFieldMetadata},
		{"lance_dataset_resolve_field_id", &b.datasetResolveFieldID},
		{"lance_dataset_field_id_count", &b.datasetFieldIDCount},
		{"lance_dataset_field_id_at", &b.datasetFieldIDAt},
		{"lance_dataset_drop_columns", &b.datasetDropColumns},
		{"lance_dataset_alter_columns", &b.datasetAlterColumns},
		{"lance_dataset_add_columns_sql", &b.datasetAddColumnsSQL},
		{"lance_dataset_add_columns_nulls", &b.datasetAddColumnsNulls},
		{"lance_dataset_add_columns_stream", &b.datasetAddColumnsStream},
		{"lance_dataset_add_columns_batch_udf", &b.datasetAddColumnsBatchUDF},
		{"lance_dataset_config", &b.datasetConfig},
		{"lance_dataset_config_count", &b.datasetConfigCount},
		{"lance_dataset_config_key_at", &b.datasetConfigKeyAt},
		{"lance_dataset_config_value_at", &b.datasetConfigValueAt},
		{"lance_dataset_config_close", &b.datasetConfigClose},
		{"lance_dataset_update_config_json", &b.datasetUpdateConfigJSON},

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

		{"lance_dataset_base_paths_json", &b.datasetBasePathsJSON},
		{"lance_dataset_add_bases", &b.datasetAddBases},

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
		{"lance_scanner_nearest", &b.scannerNearest},
		{"lance_scanner_nearest_batch", &b.scannerNearestBatch},
		{"lance_scanner_nearest_multivector", &b.scannerNearestMultiVector},
		{"lance_scanner_set_distance_range", &b.scannerSetDistanceRange},
		{"lance_scanner_set_metric", &b.scannerSetMetric},
		{"lance_scanner_set_nprobes", &b.scannerSetNProbes},
		{"lance_scanner_set_minimum_nprobes", &b.scannerSetMinimumNProbes},
		{"lance_scanner_set_maximum_nprobes", &b.scannerSetMaximumNProbes},
		{"lance_scanner_set_search_effort", &b.scannerSetSearchEffort},
		{"lance_scanner_set_approx_mode", &b.scannerSetApproxMode},
		{"lance_scanner_set_query_parallelism", &b.scannerSetQueryParallelism},
		{"lance_scanner_set_refine_factor", &b.scannerSetRefineFactor},
		{"lance_scanner_set_ef", &b.scannerSetEF},
		{"lance_scanner_set_use_index", &b.scannerSetUseIndex},
		{"lance_scanner_set_fast_search", &b.scannerSetFastSearch},
		{"lance_scanner_set_prefilter", &b.scannerSetPrefilter},
		{"lance_scanner_set_row_addr_allowlist", &b.scannerSetRowAddrAllowlist},
		{"lance_scanner_set_row_addr_blocklist", &b.scannerSetRowAddrBlocklist},
		{"lance_scanner_set_statistics_callback", &b.scannerSetStatisticsCallback},
		{"lance_scanner_set_fts_query", &b.scannerSetFTSQuery},
		{"lance_scanner_set_fts_query_context", &b.scannerSetFTSQueryContext},
		{"lance_scanner_set_minhash_query", &b.scannerSetMinHashQuery},
		{"lance_scanner_set_fts_index_segments", &b.scannerSetFTSIndexSegments},
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
