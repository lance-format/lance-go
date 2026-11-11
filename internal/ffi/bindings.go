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
	freeBytes        func(*byte)

	datasetOpen              func(uri string, storageOpts []uintptr, version uint64) uintptr
	datasetOpenWithSession   func(uri string, storageOpts []uintptr, version uint64, session uintptr) uintptr
	datasetOpenWithOptions   func(uri string, options *datasetOpenOptions, session uintptr) uintptr
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

	segmentBuilderNewScalar            func(dataset uintptr, column string, indexName uintptr, indexType int32, paramsJSON uintptr, options *segmentBuildOptions) uintptr
	segmentBuilderNewScalarWithOptions func(dataset uintptr, column string, indexName uintptr, indexType int32, paramsJSON uintptr, scalarOptions *scalarIndexOptions, options *segmentBuildOptions) uintptr
	segmentBuilderNewVectorWithOptions func(dataset uintptr, column string, indexName uintptr, options *vectorIndexOptions, scope *segmentScope) uintptr
	segmentBuilderExecuteUncommitted   func(builder uintptr, outBytes **byte, outLen *uint64) int32
	segmentBuilderSetProgressCallback  func(builder uintptr, callback uintptr, ctx uintptr) int32
	segmentBuilderFree                 func(builder uintptr)

	indexTrainIVFModel            func(dataset uintptr, column string, numPartitions uint32, metric int32, fragmentIDs *uint32, fragmentCount uint64, outArray *cdata.ArrowArray, outSchema *cdata.ArrowSchema) int32
	indexTrainIVFModelWithOptions func(dataset uintptr, column string, numPartitions uint32, metric int32, fragmentIDs *uint32, fragmentCount uint64, options *trainerOptions, outArray *cdata.ArrowArray, outSchema *cdata.ArrowSchema) int32
	indexTrainPQModel             func(dataset uintptr, column string, numSubVectors uint32, numBits uint32, metric int32, fragmentIDs *uint32, fragmentCount uint64, ivfCentroids *cdata.ArrowArray, ivfCentroidsSchema *cdata.ArrowSchema, outArray *cdata.ArrowArray, outSchema *cdata.ArrowSchema) int32
	indexTrainPQModelWithOptions  func(dataset uintptr, column string, numSubVectors uint32, numBits uint32, metric int32, fragmentIDs *uint32, fragmentCount uint64, ivfCentroids *cdata.ArrowArray, ivfCentroidsSchema *cdata.ArrowSchema, options *trainerOptions, outArray *cdata.ArrowArray, outSchema *cdata.ArrowSchema) int32
	indexBuildRQModel             func(dimension uint32, numBits uint32) *byte

	datasetCommitIndexSegments func(dataset uintptr, indexName, column string, segmentBytes []*byte, segmentLens []uint64, segmentCount uint64) int32
	datasetMergeIndexSegments  func(dataset uintptr, segmentBytes []*byte, segmentLens []uint64, segmentCount uint64, outBytes **byte, outLen *uint64) int32
	datasetMergeIndexMetadata  func(dataset uintptr, indexUUID *byte, indexType string, hasBatchReadhead bool, batchReadhead uint64, callback uintptr, ctx uintptr) int32

	metadataParse                func(bytes *byte, length uint64, outMetadata *uintptr) int32
	metadataUUID                 func(metadata uintptr, outUUID *byte) int32
	metadataName                 func(metadata uintptr) *byte
	metadataDatasetVersion       func(metadata uintptr) uint64
	metadataIndexVersion         func(metadata uintptr) int32
	metadataIndexType            func(metadata uintptr) int32
	metadataDetailsTypeURL       func(metadata uintptr) *byte
	metadataFieldCount           func(metadata uintptr) uint64
	metadataFieldIDs             func(metadata uintptr, outFieldIDs []int32, capacity uint64, outCount *uint64) int32
	metadataFragmentCount        func(metadata uintptr) uint64
	metadataFragmentIDs          func(metadata uintptr, outFragmentIDs []uint32, capacity uint64, outCount *uint64) int32
	metadataClone                func(metadata uintptr) uintptr
	metadataCloneWithFragmentIDs func(metadata uintptr, hasFragmentIDs bool, fragmentIDs *uint32, fragmentCount uint64) uintptr
	metadataEncode               func(metadata uintptr, outBytes **byte, outLen *uint64) int32
	metadataCoveringFieldCount   func(metadata uintptr) uint64
	metadataCoveringFieldIDAt    func(metadata uintptr, index uint64, outFieldID *int32) int32
	metadataDetailsValue         func(metadata uintptr, outLen *uint64) *byte
	metadataFileCount            func(metadata uintptr) uint64
	metadataFilePathAt           func(metadata uintptr, index uint64) *byte
	metadataFileSizeAt           func(metadata uintptr, index uint64, outSize *uint64) int32
	metadataCreatedAt            func(metadata uintptr, outTimestamp *optionalTimestamp) int32
	metadataBaseID               func(metadata uintptr, outIsSet *bool, outBaseID *uint32) int32
	metadataFree                 func(metadata uintptr)

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

	datasetWriteWithOptions            func(uri string, schema *cdata.ArrowSchema, stream *cdata.ArrowArrayStream, mode int32, options *writeOptions, storageOpts []uintptr, outDataset *uintptr) int32
	datasetWriteWithOptionsAndAccessor func(uri string, schema *cdata.ArrowSchema, stream *cdata.ArrowArrayStream, mode int32, options *writeOptions, accessor uintptr, outDataset *uintptr) int32

	scannerNew                          func(dataset uintptr, columns []uintptr, filter uintptr) uintptr
	scannerClose                        func(scanner uintptr)
	scannerSetLimit                     func(scanner uintptr, limit int64) int32
	scannerSetOffset                    func(scanner uintptr, offset int64) int32
	scannerSetBatchSize                 func(scanner uintptr, size int64) int32
	scannerSetBatchSizeBytes            func(scanner uintptr, size uint64) int32
	scannerSetIOBufferSize              func(scanner uintptr, size uint64) int32
	scannerSetBatchReadahead            func(scanner uintptr, batches uint64) int32
	scannerSetFragmentReadahead         func(scanner uintptr, fragments uint64) int32
	scannerSetTargetParallelism         func(scanner uintptr, tasks uint64) int32
	scannerSetScanInOrder               func(scanner uintptr, enabled bool) int32
	scannerSetUseScalarIndex            func(scanner uintptr, enabled bool) int32
	scannerSetStrictBatchSize           func(scanner uintptr, enabled bool) int32
	scannerSetUseStats                  func(scanner uintptr, enabled bool) int32
	scannerSetIncludeDeletedRows        func(scanner uintptr, enabled bool) int32
	scannerSetBlobHandling              func(scanner uintptr, handling int32) int32
	scannerSetMaterializationStyle      func(scanner uintptr, style int32, earlyExcept []uintptr, count uint64) int32
	scannerSetFragmentIDs               func(scanner uintptr, ids []uint64, count uint64) int32
	scannerDisableScoringAutoprojection func(scanner uintptr) int32
	scannerSetScalarIndexSegment        func(scanner uintptr, segment []byte) int32
	scannerSetProjection                func(scanner uintptr, columns []projectionColumn, count uint64) int32
	scannerSetOrderBy                   func(scanner uintptr, orderings []columnOrdering, count uint64) int32
	scannerWithRowID                    func(scanner uintptr, enabled bool) int32
	scannerWithRowAddress               func(scanner uintptr, enabled bool) int32
	scannerSetSubstraitFilter           func(scanner uintptr, data []byte, length uint64) int32
	scannerSetSubstraitAggregate        func(scanner uintptr, data []byte, length uint64) int32
	scannerAdditionalSQLFilter          func(scanner uintptr, filter string) int32
	scannerNearest                      func(scanner uintptr, column string, queryData unsafe.Pointer, queryLen uint64, elementType int32, k uint32) int32
	scannerNearestBatch                 func(scanner uintptr, column string, queries unsafe.Pointer, dimension uint64, numQueries uint64, elementType int32, k uint32) int32
	scannerNearestMultiVector           func(scanner uintptr, column string, subvectors unsafe.Pointer, dimension uint64, numVectors uint64, elementType int32, k uint32) int32
	scannerSetDistanceRange             func(scanner uintptr, lowerDistance *float32, upperDistance *float32) int32
	scannerSetMetric                    func(scanner uintptr, metric int32) int32
	scannerSetNProbes                   func(scanner uintptr, nprobes uint32) int32
	scannerSetMinimumNProbes            func(scanner uintptr, nprobes uint32) int32
	scannerSetMaximumNProbes            func(scanner uintptr, nprobes uint32) int32
	scannerSetSearchEffort              func(scanner uintptr, effort float64) int32
	scannerSetApproxMode                func(scanner uintptr, mode int32) int32
	scannerSetQueryParallelism          func(scanner uintptr, parallelism int32) int32
	scannerSetRefineFactor              func(scanner uintptr, factor uint32) int32
	scannerSetEF                        func(scanner uintptr, ef uint32) int32
	scannerSetUseIndex                  func(scanner uintptr, useIndex bool) int32
	scannerSetFastSearch                func(scanner uintptr, fastSearch bool) int32
	scannerSetPrefilter                 func(scanner uintptr, prefilter bool) int32
	scannerSetRowAddrAllowlist          func(scanner uintptr, rowIDs []uint64, count uint64) int32
	scannerSetRowAddrBlocklist          func(scanner uintptr, rowIDs []uint64, count uint64) int32
	scannerSetStatisticsCallback        func(scanner uintptr, callback uintptr, ctx uintptr) int32
	scannerSetFTSQuery                  func(scanner uintptr, query uintptr) int32
	scannerSetFTSQueryContext           func(scanner uintptr, context uintptr) int32
	scannerSetMinHashQuery              func(scanner uintptr, column, text string) int32
	scannerSetFTSIndexSegments          func(scanner uintptr, uuids []byte, count uint64) int32
	scannerSchema                       func(scanner uintptr, out *cdata.ArrowSchema) int32
	scannerPollNext                     func(scanner uintptr, waker uintptr, wakerCtx uintptr, out *uintptr) int32

	storageProviderNew        func(spec *storageOptionsProviderSpec) uintptr
	storageProviderClose      func(provider uintptr)
	storageProviderID         func(provider uintptr) *byte
	storageAccessorNew        func(initial *storageOptionsInput, provider uintptr) uintptr
	storageAccessorClose      func(accessor uintptr)
	storageAccessorSnapshot   func(accessor uintptr) uintptr
	storageSnapshotHasOptions func(snapshot uintptr) int32
	storageSnapshotCount      func(snapshot uintptr) uint64
	storageSnapshotKeyAt      func(snapshot uintptr, index uint64) *byte
	storageSnapshotValueAt    func(snapshot uintptr, index uint64) *byte
	storageSnapshotClose      func(snapshot uintptr)

	fileSessionNew                     func(baseURI string, storageOptions uintptr, session uintptr, scheduler *fileSchedulerConfig) uintptr
	fileSessionClose                   func(session uintptr)
	fileSessionBasePath                func(session uintptr) *byte
	fileSessionOpenReader              func(session uintptr, relativePath string, options *fileReaderOptions) uintptr
	fileSessionOpenWriter              func(session uintptr, relativePath string, schema *cdata.ArrowSchema, options *fileWriterOptions) uintptr
	fileSessionOpenPackedBlobWriter    func(session uintptr, dataFilePath string, blobID uint32) uintptr
	fileSessionOpenDedicatedBlobWriter func(session uintptr, dataFilePath string, blobID uint32) uintptr
	fileSessionContains                func(session uintptr, relativePath string, outExists *int32) int32
	fileSessionList                    func(session uintptr, relativePath *byte) uintptr
	fileSessionListWithDelimiter       func(session uintptr, relativePath *byte) uintptr
	fileSessionReadRange               func(session uintptr, relativePath string, offset, length uint64) uintptr
	fileSessionDeleteFile              func(session uintptr, relativePath string) int32
	fileSessionUploadFile              func(session uintptr, localPath, relativeRemotePath string) int32
	fileSessionDownloadFile            func(session uintptr, relativeRemotePath, localPath string) int32
	fileSessionGetSchedulerStats       func(session uintptr, out *fileScanStats) int32
	fileSessionGetRegistryStats        func(session uintptr, out *fileRegistryStats) int32
	fileSessionGetStoreInfo            func(session uintptr, out *fileStoreInfo) int32
	fileSessionGetStoreIOStats         func(session uintptr, out *fileStoreIOStats) int32
	pathListCount                      func(list uintptr) uint64
	pathListAt                         func(list uintptr, index uint64) *byte
	pathListClose                      func(list uintptr)
	delimitedListCommonPrefixCount     func(list uintptr) uint64
	delimitedListCommonPrefixAt        func(list uintptr, index uint64) *byte
	delimitedListObjectCount           func(list uintptr) uint64
	delimitedListObjectAt              func(list uintptr, index uint64) *byte
	delimitedListClose                 func(list uintptr)
	byteBufferData                     func(buffer uintptr) *byte
	byteBufferLen                      func(buffer uintptr) uint64
	byteBufferClose                    func(buffer uintptr)
	fileReaderOpen                     func(uri string, storageOptions uintptr, options *fileReaderOptions, session uintptr, scheduler *fileSchedulerConfig) uintptr
	fileReaderNumRows                  func(reader uintptr, outRows *uint64) int32
	fileReaderGetMetadata              func(reader uintptr, outMetadata *uintptr) int32
	fileReaderGetStatistics            func(reader uintptr, outStatistics *uintptr) int32
	fileReaderReadGlobalBuffer         func(reader uintptr, index uint32, outBytes *uintptr) int32
	fileReaderReadAll                  func(reader uintptr, batchSize, batchReadahead uint32, out *cdata.ArrowArrayStream) int32
	fileReaderReadRange                func(reader uintptr, offset, numRows uint64, batchSize, batchReadahead uint32, out *cdata.ArrowArrayStream) int32
	fileReaderReadIndices              func(reader uintptr, indices *uint32, count uint64, batchSize, batchReadahead uint32, out *cdata.ArrowArrayStream) int32
	fileReaderGetIOStats               func(reader uintptr, out *fileScanStats) int32
	fileReaderClose                    func(reader uintptr)
	fileMetadataGetInfo                func(metadata uintptr, out *fileMetadataInfo) int32
	fileMetadataGetSchema              func(metadata uintptr, out *cdata.ArrowSchema) int32
	fileMetadataGetGlobalBuffers       func(metadata uintptr, outBuffers **fileBufferDescriptor, outCount *uint64) int32
	fileMetadataGetColumns             func(metadata uintptr, outColumns **fileColumnMetadata, outCount *uint64) int32
	fileMetadataClose                  func(metadata uintptr)
	fileStatisticsGetColumns           func(statistics uintptr, outColumns **fileColumnStatistics, outCount *uint64) int32
	fileStatisticsClose                func(statistics uintptr)
	fileWriterOpen                     func(uri string, schema *cdata.ArrowSchema, options *fileWriterOptions, storageOptions uintptr, session uintptr) uintptr
	fileWriterWriteBatch               func(writer uintptr, batch uintptr) int32
	fileWriterAddSchemaMetadata        func(writer uintptr, key, value string) int32
	fileWriterAddGlobalBuffer          func(writer uintptr, data *byte, length uint64, outIndex *uint32) int32
	fileWriterFinish                   func(writer uintptr, outSummary *fileWriteSummary) int32
	fileWriterAbort                    func(writer uintptr) int32
	fileWriterClose                    func(writer uintptr)
	packedBlobWriterBlobID             func(writer uintptr) uint32
	packedBlobWriterPath               func(writer uintptr) *byte
	packedBlobWriterWriteBlob          func(writer uintptr, data *byte, length uint64) int32
	packedBlobWriterWriteBlobs         func(writer uintptr, payloads *cdata.ArrowArray, schema *cdata.ArrowSchema) int32
	packedBlobWriterFinishArray        func(writer uintptr, fieldName string, outArray *cdata.ArrowArray, outSchema *cdata.ArrowSchema) int32
	packedBlobWriterClose              func(writer uintptr)
	dedicatedBlobWriterBlobID          func(writer uintptr) uint32
	dedicatedBlobWriterPath            func(writer uintptr) *byte
	dedicatedBlobWriterWrite           func(writer uintptr, data *byte, length uint64) int32
	dedicatedBlobWriterFinishArray     func(writer uintptr, fieldName string, outArray *cdata.ArrowArray, outSchema *cdata.ArrowSchema) int32
	dedicatedBlobWriterClose           func(writer uintptr)

	batchFromArrow func(array *cdata.ArrowArray, schema *cdata.ArrowSchema, out *uintptr) int32

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
	datasetWriteUncommittedStreamWithAccessor    func(uri string, schema *cdata.ArrowSchema, stream *cdata.ArrowArrayStream, mode int32, options *uncommittedWriteOptions, accessor uintptr, session uintptr, out *uintptr) int32
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
	transactionCommitWithAccessor                func(uri string, transaction uintptr, options *commitOptions, accessor uintptr, session uintptr, out *uintptr) int32
	transactionCommitBatch                       func(uri string, transactions []uintptr, count uint64, options *commitOptions, storageOpts []uintptr, session uintptr, outDataset *uintptr, outTransaction *uintptr) int32
	transactionCommitBatchWithAccessor           func(uri string, transactions []uintptr, count uint64, options *commitOptions, accessor uintptr, session uintptr, outDataset *uintptr, outTransaction *uintptr) int32

	namespaceResultClose          func(result *lanceNamespaceResult)
	namespaceID                   func(namespace uintptr) *byte
	namespaceRetrieveOpsMetrics   func(namespace uintptr) *byte
	namespaceResetOpsMetrics      func(namespace uintptr) int32
	namespaceDirectoryNew         func(propertiesJSON string, session uintptr, spec *namespaceContextProviderSpec) uintptr
	namespaceRestNew              func(propertiesJSON string, spec *namespaceContextProviderSpec) uintptr
	namespaceConnectNew           func(implName, propertiesJSON string, session uintptr, spec *namespaceContextProviderSpec) uintptr
	namespaceCustomDispatchNew    func(spec *namespaceCustomDispatchSpec) uintptr
	namespaceClose                func(namespace uintptr)
	namespaceCall                 func(namespace uintptr, operation, requestJSON string, requestBytes []byte, requestBytesLen uint64, result *lanceNamespaceResult) int32
	datasetOpenNamespace          func(namespace uintptr, tableIDJSON, storageOptionsJSON string, version uint64, session uintptr) uintptr
	datasetWriteNamespace         func(namespace uintptr, tableIDJSON string, schema *cdata.ArrowSchema, stream *cdata.ArrowArrayStream, mode int32, params *writeParams, options *writeOptions, storageOptionsJSON string, session uintptr, branch *byte, outDataset *uintptr) int32
	transactionCommitNamespaceURI func(namespace uintptr, tableIDJSON, uri string, transaction uintptr, options *commitOptions, storageOpts []uintptr, session uintptr, out *uintptr) int32
	namespaceStorageProviderNew   func(namespace uintptr, tableIDJSON string) uintptr
	restAdapterStart              func(namespace uintptr, host string, port uint16, out *uintptr) int32
	restAdapterPort               func(adapter uintptr) uint16
	restAdapterShutdown           func(adapter uintptr)
	restAdapterClose              func(adapter uintptr)

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

	optimizeIndicesOptionsNew                           func() uintptr
	optimizeIndicesOptionsFree                          func(uintptr)
	optimizeIndicesOptionsSetNumIndicesToMerge          func(uintptr, bool, uint64) int32
	optimizeIndicesOptionsSetEnablePartitionRebalancing func(uintptr, bool) int32
	optimizeIndicesOptionsSetIndexNames                 func(uintptr, bool, *uintptr, uint64) int32
	optimizeIndicesOptionsSetRetrain                    func(uintptr, bool) int32
	optimizeIndicesOptionsSetTransactionProperties      func(uintptr, *transactionProperties) int32
	optimizeIndicesOptionsSetMaxRowsPerTask             func(uintptr, bool, uint64) int32
	optimizeIndicesOptionsSetMaxRowsPerSegment          func(uintptr, bool, uint64) int32
	datasetOptimizeIndices                              func(uintptr, uintptr) int32
	datasetPlanOptimizeIndices                          func(uintptr, uintptr) uintptr
	datasetCommitOptimizeIndices                        func(uintptr, []uintptr, uint64, uintptr) int32
	optimizeIndicesPlanPlanID                           func(uintptr, *byte) int32
	optimizeIndicesPlanReadVersion                      func(uintptr) uint64
	optimizeIndicesPlanTaskCount                        func(uintptr) uint64
	optimizeIndicesPlanMergeCount                       func(uintptr) uint64
	optimizeIndicesPlanTaskAt                           func(uintptr, uint64) uintptr
	optimizeIndicesPlanMergeAt                          func(uintptr, uint64) uintptr
	optimizeIndicesPlanJSON                             func(uintptr) *byte
	optimizeIndicesPlanFromJSON                         func(string) uintptr
	optimizeIndicesPlanClose                            func(uintptr)
	optimizeIndexTaskPlanID                             func(uintptr, *byte) int32
	optimizeIndexTaskReadVersion                        func(uintptr) uint64
	optimizeIndexTaskGroup                              func(uintptr) uint32
	optimizeIndexTaskFragmentCount                      func(uintptr) uint64
	optimizeIndexTaskFragmentIDAt                       func(uintptr, uint64, *uint32) int32
	optimizeIndexTaskReference                          func(uintptr) uintptr
	optimizeIndexTaskOldSegmentCount                    func(uintptr) uint64
	optimizeIndexTaskOldSegmentAt                       func(uintptr, uint64) uintptr
	optimizeIndexTaskNumIndicesToMerge                  func(uintptr, *bool, *uint64) int32
	optimizeIndexTaskEnablePartitionRebalancing         func(uintptr, *bool) int32
	optimizeIndexTaskRetrain                            func(uintptr, *bool) int32
	optimizeIndexTaskExecute                            func(uintptr, uintptr, *uintptr) int32
	optimizeIndexTaskJSON                               func(uintptr) *byte
	optimizeIndexTaskFromJSON                           func(string) uintptr
	optimizeIndexTaskClose                              func(uintptr)
	optimizeIndexMergePlanID                            func(uintptr, *byte) int32
	optimizeIndexMergeReadVersion                       func(uintptr) uint64
	optimizeIndexMergeGroup                             func(uintptr) uint32
	optimizeIndexMergeOldSegmentCount                   func(uintptr) uint64
	optimizeIndexMergeOldSegmentAt                      func(uintptr, uint64) uintptr
	optimizeIndexMergeReplaceCount                      func(uintptr) uint64
	optimizeIndexMergeReplaceUUIDAt                     func(uintptr, uint64, *byte) int32
	optimizeIndexMergeExecute                           func(uintptr, uintptr, []uintptr, uint64, *uintptr) int32
	optimizeIndexMergeJSON                              func(uintptr) *byte
	optimizeIndexMergeFromJSON                          func(string) uintptr
	optimizeIndexMergeClose                             func(uintptr)
	indexSegmentResultPlanID                            func(uintptr, *byte) int32
	indexSegmentResultReadVersion                       func(uintptr) uint64
	indexSegmentResultGroup                             func(uintptr) uint32
	indexSegmentResultSegment                           func(uintptr) uintptr
	indexSegmentResultReplaceCount                      func(uintptr) uint64
	indexSegmentResultReplaceUUIDAt                     func(uintptr, uint64, *byte) int32
	indexSegmentResultJSON                              func(uintptr) *byte
	indexSegmentResultFromJSON                          func(string) uintptr
	indexSegmentResultClose                             func(uintptr)

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
		{"lance_free_bytes", &b.freeBytes},
		{"lance_dataset_open", &b.datasetOpen},
		{"lance_dataset_open_with_session", &b.datasetOpenWithSession},
		{"lance_dataset_open_with_options", &b.datasetOpenWithOptions},
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
		{"lance_index_segment_builder_new_scalar", &b.segmentBuilderNewScalar},
		{"lance_index_segment_builder_new_scalar_with_options", &b.segmentBuilderNewScalarWithOptions},
		{"lance_index_segment_builder_new_vector_with_options", &b.segmentBuilderNewVectorWithOptions},
		{"lance_index_segment_builder_execute_uncommitted", &b.segmentBuilderExecuteUncommitted},
		{"lance_index_segment_builder_set_progress_callback", &b.segmentBuilderSetProgressCallback},
		{"lance_index_segment_builder_free", &b.segmentBuilderFree},
		{"lance_index_train_ivf_model", &b.indexTrainIVFModel},
		{"lance_index_train_ivf_model_with_options", &b.indexTrainIVFModelWithOptions},
		{"lance_index_train_pq_model", &b.indexTrainPQModel},
		{"lance_index_train_pq_model_with_options", &b.indexTrainPQModelWithOptions},
		{"lance_index_build_rq_model", &b.indexBuildRQModel},
		{"lance_dataset_commit_index_segments", &b.datasetCommitIndexSegments},
		{"lance_dataset_merge_index_segments", &b.datasetMergeIndexSegments},
		{"lance_dataset_merge_index_metadata", &b.datasetMergeIndexMetadata},
		{"lance_index_segment_metadata_parse", &b.metadataParse},
		{"lance_index_segment_metadata_uuid", &b.metadataUUID},
		{"lance_index_segment_metadata_name", &b.metadataName},
		{"lance_index_segment_metadata_dataset_version", &b.metadataDatasetVersion},
		{"lance_index_segment_metadata_index_version", &b.metadataIndexVersion},
		{"lance_index_segment_metadata_index_type", &b.metadataIndexType},
		{"lance_index_segment_metadata_index_details_type_url", &b.metadataDetailsTypeURL},
		{"lance_index_segment_metadata_field_count", &b.metadataFieldCount},
		{"lance_index_segment_metadata_field_ids", &b.metadataFieldIDs},
		{"lance_index_segment_metadata_fragment_count", &b.metadataFragmentCount},
		{"lance_index_segment_metadata_fragment_ids", &b.metadataFragmentIDs},
		{"lance_index_segment_metadata_clone", &b.metadataClone},
		{"lance_index_segment_metadata_clone_with_fragment_ids", &b.metadataCloneWithFragmentIDs},
		{"lance_index_segment_metadata_encode", &b.metadataEncode},
		{"lance_index_segment_metadata_covering_field_count", &b.metadataCoveringFieldCount},
		{"lance_index_segment_metadata_covering_field_id_at", &b.metadataCoveringFieldIDAt},
		{"lance_index_segment_metadata_index_details_value", &b.metadataDetailsValue},
		{"lance_index_segment_metadata_file_count", &b.metadataFileCount},
		{"lance_index_segment_metadata_file_path_at", &b.metadataFilePathAt},
		{"lance_index_segment_metadata_file_size_at", &b.metadataFileSizeAt},
		{"lance_index_segment_metadata_created_at", &b.metadataCreatedAt},
		{"lance_index_segment_metadata_base_id", &b.metadataBaseID},
		{"lance_index_segment_metadata_free", &b.metadataFree},
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
		{"lance_dataset_write_with_options_and_accessor", &b.datasetWriteWithOptionsAndAccessor},

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
		{"lance_dataset_write_uncommitted_stream_with_accessor", &b.datasetWriteUncommittedStreamWithAccessor},
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
		{"lance_transaction_commit_with_accessor", &b.transactionCommitWithAccessor},
		{"lance_transaction_commit_batch", &b.transactionCommitBatch},
		{"lance_transaction_commit_batch_with_accessor", &b.transactionCommitBatchWithAccessor},

		{"lance_namespace_result_close", &b.namespaceResultClose},
		{"lance_namespace_id", &b.namespaceID},
		{"lance_namespace_retrieve_ops_metrics", &b.namespaceRetrieveOpsMetrics},
		{"lance_namespace_reset_ops_metrics", &b.namespaceResetOpsMetrics},
		{"lance_namespace_directory_new", &b.namespaceDirectoryNew},
		{"lance_namespace_rest_new", &b.namespaceRestNew},
		{"lance_namespace_connect_new", &b.namespaceConnectNew},
		{"lance_namespace_custom_dispatch_new", &b.namespaceCustomDispatchNew},
		{"lance_namespace_close", &b.namespaceClose},
		{"lance_namespace_call", &b.namespaceCall},
		{"lance_dataset_open_namespace", &b.datasetOpenNamespace},
		{"lance_dataset_write_namespace", &b.datasetWriteNamespace},
		{"lance_transaction_commit_namespace_uri", &b.transactionCommitNamespaceURI},
		{"lance_namespace_storage_options_provider_new", &b.namespaceStorageProviderNew},
		{"lance_rest_adapter_start", &b.restAdapterStart},
		{"lance_rest_adapter_port", &b.restAdapterPort},
		{"lance_rest_adapter_shutdown", &b.restAdapterShutdown},
		{"lance_rest_adapter_close", &b.restAdapterClose},

		{"lance_scanner_new", &b.scannerNew},
		{"lance_scanner_close", &b.scannerClose},
		{"lance_scanner_set_limit", &b.scannerSetLimit},
		{"lance_scanner_set_offset", &b.scannerSetOffset},
		{"lance_scanner_set_batch_size", &b.scannerSetBatchSize},
		{"lance_scanner_set_batch_size_bytes", &b.scannerSetBatchSizeBytes},
		{"lance_scanner_set_io_buffer_size", &b.scannerSetIOBufferSize},
		{"lance_scanner_set_batch_readahead", &b.scannerSetBatchReadahead},
		{"lance_scanner_set_fragment_readahead", &b.scannerSetFragmentReadahead},
		{"lance_scanner_set_target_parallelism", &b.scannerSetTargetParallelism},
		{"lance_scanner_set_scan_in_order", &b.scannerSetScanInOrder},
		{"lance_scanner_set_use_scalar_index", &b.scannerSetUseScalarIndex},
		{"lance_scanner_set_strict_batch_size", &b.scannerSetStrictBatchSize},
		{"lance_scanner_set_use_stats", &b.scannerSetUseStats},
		{"lance_scanner_set_include_deleted_rows", &b.scannerSetIncludeDeletedRows},
		{"lance_scanner_set_blob_handling", &b.scannerSetBlobHandling},
		{"lance_scanner_set_materialization_style", &b.scannerSetMaterializationStyle},
		{"lance_scanner_set_fragment_ids", &b.scannerSetFragmentIDs},
		{"lance_scanner_disable_scoring_autoprojection", &b.scannerDisableScoringAutoprojection},
		{"lance_scanner_set_scalar_index_segment", &b.scannerSetScalarIndexSegment},
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

		{"lance_storage_options_provider_new", &b.storageProviderNew},
		{"lance_storage_options_provider_close", &b.storageProviderClose},
		{"lance_storage_options_provider_id", &b.storageProviderID},
		{"lance_storage_options_accessor_new", &b.storageAccessorNew},
		{"lance_storage_options_accessor_close", &b.storageAccessorClose},
		{"lance_storage_options_accessor_snapshot", &b.storageAccessorSnapshot},
		{"lance_storage_options_snapshot_has_options", &b.storageSnapshotHasOptions},
		{"lance_storage_options_snapshot_count", &b.storageSnapshotCount},
		{"lance_storage_options_snapshot_key_at", &b.storageSnapshotKeyAt},
		{"lance_storage_options_snapshot_value_at", &b.storageSnapshotValueAt},
		{"lance_storage_options_snapshot_close", &b.storageSnapshotClose},

		{"lance_file_session_new", &b.fileSessionNew},
		{"lance_file_session_close", &b.fileSessionClose},
		{"lance_file_session_base_path", &b.fileSessionBasePath},
		{"lance_file_session_open_reader", &b.fileSessionOpenReader},
		{"lance_file_session_open_writer", &b.fileSessionOpenWriter},
		{"lance_file_session_open_packed_blob_writer", &b.fileSessionOpenPackedBlobWriter},
		{"lance_file_session_open_dedicated_blob_writer", &b.fileSessionOpenDedicatedBlobWriter},
		{"lance_file_session_contains", &b.fileSessionContains},
		{"lance_file_session_list", &b.fileSessionList},
		{"lance_file_session_list_with_delimiter", &b.fileSessionListWithDelimiter},
		{"lance_file_session_read_range", &b.fileSessionReadRange},
		{"lance_file_session_delete_file", &b.fileSessionDeleteFile},
		{"lance_file_session_upload_file", &b.fileSessionUploadFile},
		{"lance_file_session_download_file", &b.fileSessionDownloadFile},
		{"lance_file_session_get_scheduler_stats", &b.fileSessionGetSchedulerStats},
		{"lance_file_session_get_registry_stats", &b.fileSessionGetRegistryStats},
		{"lance_file_session_get_store_info", &b.fileSessionGetStoreInfo},
		{"lance_file_session_get_store_io_stats", &b.fileSessionGetStoreIOStats},
		{"lance_path_list_count", &b.pathListCount},
		{"lance_path_list_at", &b.pathListAt},
		{"lance_path_list_close", &b.pathListClose},
		{"lance_delimited_list_common_prefix_count", &b.delimitedListCommonPrefixCount},
		{"lance_delimited_list_common_prefix_at", &b.delimitedListCommonPrefixAt},
		{"lance_delimited_list_object_count", &b.delimitedListObjectCount},
		{"lance_delimited_list_object_at", &b.delimitedListObjectAt},
		{"lance_delimited_list_close", &b.delimitedListClose},
		{"lance_byte_buffer_data", &b.byteBufferData},
		{"lance_byte_buffer_len", &b.byteBufferLen},
		{"lance_byte_buffer_close", &b.byteBufferClose},
		{"lance_file_reader_open", &b.fileReaderOpen},
		{"lance_file_reader_num_rows", &b.fileReaderNumRows},
		{"lance_file_reader_get_metadata", &b.fileReaderGetMetadata},
		{"lance_file_reader_get_statistics", &b.fileReaderGetStatistics},
		{"lance_file_reader_read_global_buffer", &b.fileReaderReadGlobalBuffer},
		{"lance_file_reader_read_all", &b.fileReaderReadAll},
		{"lance_file_reader_read_range", &b.fileReaderReadRange},
		{"lance_file_reader_read_indices", &b.fileReaderReadIndices},
		{"lance_file_reader_get_io_stats", &b.fileReaderGetIOStats},
		{"lance_file_reader_close", &b.fileReaderClose},
		{"lance_file_metadata_get_info", &b.fileMetadataGetInfo},
		{"lance_file_metadata_get_schema", &b.fileMetadataGetSchema},
		{"lance_file_metadata_get_global_buffers", &b.fileMetadataGetGlobalBuffers},
		{"lance_file_metadata_get_columns", &b.fileMetadataGetColumns},
		{"lance_file_metadata_close", &b.fileMetadataClose},
		{"lance_file_statistics_get_columns", &b.fileStatisticsGetColumns},
		{"lance_file_statistics_close", &b.fileStatisticsClose},
		{"lance_file_writer_open", &b.fileWriterOpen},
		{"lance_file_writer_write_batch", &b.fileWriterWriteBatch},
		{"lance_file_writer_add_schema_metadata", &b.fileWriterAddSchemaMetadata},
		{"lance_file_writer_add_global_buffer", &b.fileWriterAddGlobalBuffer},
		{"lance_file_writer_finish", &b.fileWriterFinish},
		{"lance_file_writer_abort", &b.fileWriterAbort},
		{"lance_file_writer_close", &b.fileWriterClose},
		{"lance_packed_blob_writer_blob_id", &b.packedBlobWriterBlobID},
		{"lance_packed_blob_writer_path", &b.packedBlobWriterPath},
		{"lance_packed_blob_writer_write_blob", &b.packedBlobWriterWriteBlob},
		{"lance_packed_blob_writer_write_blobs", &b.packedBlobWriterWriteBlobs},
		{"lance_packed_blob_writer_finish_array", &b.packedBlobWriterFinishArray},
		{"lance_packed_blob_writer_close", &b.packedBlobWriterClose},
		{"lance_dedicated_blob_writer_blob_id", &b.dedicatedBlobWriterBlobID},
		{"lance_dedicated_blob_writer_path", &b.dedicatedBlobWriterPath},
		{"lance_dedicated_blob_writer_write", &b.dedicatedBlobWriterWrite},
		{"lance_dedicated_blob_writer_finish_array", &b.dedicatedBlobWriterFinishArray},
		{"lance_dedicated_blob_writer_close", &b.dedicatedBlobWriterClose},

		{"lance_batch_from_arrow", &b.batchFromArrow},

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
		{"lance_optimize_indices_options_new", &b.optimizeIndicesOptionsNew},
		{"lance_optimize_indices_options_free", &b.optimizeIndicesOptionsFree},
		{"lance_optimize_indices_options_set_num_indices_to_merge", &b.optimizeIndicesOptionsSetNumIndicesToMerge},
		{"lance_optimize_indices_options_set_enable_partition_rebalancing", &b.optimizeIndicesOptionsSetEnablePartitionRebalancing},
		{"lance_optimize_indices_options_set_index_names", &b.optimizeIndicesOptionsSetIndexNames},
		{"lance_optimize_indices_options_set_retrain", &b.optimizeIndicesOptionsSetRetrain},
		{"lance_optimize_indices_options_set_transaction_properties", &b.optimizeIndicesOptionsSetTransactionProperties},
		{"lance_optimize_indices_options_set_max_rows_per_task", &b.optimizeIndicesOptionsSetMaxRowsPerTask},
		{"lance_optimize_indices_options_set_max_rows_per_segment", &b.optimizeIndicesOptionsSetMaxRowsPerSegment},
		{"lance_dataset_optimize_indices", &b.datasetOptimizeIndices},
		{"lance_dataset_plan_optimize_indices", &b.datasetPlanOptimizeIndices},
		{"lance_dataset_commit_optimize_indices", &b.datasetCommitOptimizeIndices},
		{"lance_optimize_indices_plan_id", &b.optimizeIndicesPlanPlanID},
		{"lance_optimize_indices_plan_read_version", &b.optimizeIndicesPlanReadVersion},
		{"lance_optimize_indices_plan_task_count", &b.optimizeIndicesPlanTaskCount},
		{"lance_optimize_indices_plan_merge_count", &b.optimizeIndicesPlanMergeCount},
		{"lance_optimize_indices_plan_task_at", &b.optimizeIndicesPlanTaskAt},
		{"lance_optimize_indices_plan_merge_at", &b.optimizeIndicesPlanMergeAt},
		{"lance_optimize_indices_plan_json", &b.optimizeIndicesPlanJSON},
		{"lance_optimize_indices_plan_from_json", &b.optimizeIndicesPlanFromJSON},
		{"lance_optimize_indices_plan_close", &b.optimizeIndicesPlanClose},
		{"lance_optimize_index_task_plan_id", &b.optimizeIndexTaskPlanID},
		{"lance_optimize_index_task_read_version", &b.optimizeIndexTaskReadVersion},
		{"lance_optimize_index_task_group", &b.optimizeIndexTaskGroup},
		{"lance_optimize_index_task_fragment_count", &b.optimizeIndexTaskFragmentCount},
		{"lance_optimize_index_task_fragment_id_at", &b.optimizeIndexTaskFragmentIDAt},
		{"lance_optimize_index_task_reference", &b.optimizeIndexTaskReference},
		{"lance_optimize_index_task_old_segment_count", &b.optimizeIndexTaskOldSegmentCount},
		{"lance_optimize_index_task_old_segment_at", &b.optimizeIndexTaskOldSegmentAt},
		{"lance_optimize_index_task_num_indices_to_merge", &b.optimizeIndexTaskNumIndicesToMerge},
		{"lance_optimize_index_task_enable_partition_rebalancing", &b.optimizeIndexTaskEnablePartitionRebalancing},
		{"lance_optimize_index_task_retrain", &b.optimizeIndexTaskRetrain},
		{"lance_optimize_index_task_execute", &b.optimizeIndexTaskExecute},
		{"lance_optimize_index_task_json", &b.optimizeIndexTaskJSON},
		{"lance_optimize_index_task_from_json", &b.optimizeIndexTaskFromJSON},
		{"lance_optimize_index_task_close", &b.optimizeIndexTaskClose},
		{"lance_optimize_index_merge_plan_id", &b.optimizeIndexMergePlanID},
		{"lance_optimize_index_merge_read_version", &b.optimizeIndexMergeReadVersion},
		{"lance_optimize_index_merge_group", &b.optimizeIndexMergeGroup},
		{"lance_optimize_index_merge_old_segment_count", &b.optimizeIndexMergeOldSegmentCount},
		{"lance_optimize_index_merge_old_segment_at", &b.optimizeIndexMergeOldSegmentAt},
		{"lance_optimize_index_merge_replace_count", &b.optimizeIndexMergeReplaceCount},
		{"lance_optimize_index_merge_replace_uuid_at", &b.optimizeIndexMergeReplaceUUIDAt},
		{"lance_optimize_index_merge_execute", &b.optimizeIndexMergeExecute},
		{"lance_optimize_index_merge_json", &b.optimizeIndexMergeJSON},
		{"lance_optimize_index_merge_from_json", &b.optimizeIndexMergeFromJSON},
		{"lance_optimize_index_merge_close", &b.optimizeIndexMergeClose},
		{"lance_index_segment_result_plan_id", &b.indexSegmentResultPlanID},
		{"lance_index_segment_result_read_version", &b.indexSegmentResultReadVersion},
		{"lance_index_segment_result_group", &b.indexSegmentResultGroup},
		{"lance_index_segment_result_segment", &b.indexSegmentResultSegment},
		{"lance_index_segment_result_replace_count", &b.indexSegmentResultReplaceCount},
		{"lance_index_segment_result_replace_uuid_at", &b.indexSegmentResultReplaceUUIDAt},
		{"lance_index_segment_result_json", &b.indexSegmentResultJSON},
		{"lance_index_segment_result_from_json", &b.indexSegmentResultFromJSON},
		{"lance_index_segment_result_close", &b.indexSegmentResultClose},

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
