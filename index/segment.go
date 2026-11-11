package index

import "strconv"

// TrainerOptions controls standalone IVF and PQ model training. A nil field
// leaves the native trainer default in place. A non-nil field is forwarded
// verbatim, including an explicit zero.
type TrainerOptions struct {
	SampleRate *uint64
	MaxIters   *uint64
}

// SegmentScope selects the fragment coverage and segment identity of a new
// index segment. The zero value covers the whole dataset and lets the native
// builder generate a segment UUID.
//
// FragmentIDs follows the same convention as SegmentInfo.FragmentIDs: nil
// covers the whole dataset, while a non-nil empty slice is an explicit empty
// coverage. UUID pins the segment identity so independently built shards can
// be recognized as parts of one planned index.
type SegmentScope struct {
	FragmentIDs []uint32
	UUID        *UUID
}

// ProgressEvent identifies the lifecycle point of one progress observation.
// Observations for different stages may interleave and arrive from multiple
// threads.
type ProgressEvent int32

const (
	// ProgressStart begins a stage. Stage names the native phase, and Total and
	// Unit describe its extent when the native builder knows them. Total may be
	// zero when the extent is unknown.
	ProgressStart ProgressEvent = 0
	// ProgressAdvance reports Completed work items of the running stage. The
	// native callback leaves Total and Unit empty for these events.
	ProgressAdvance ProgressEvent = 1
	// ProgressComplete ends a stage. Every other field is zero or empty.
	ProgressComplete ProgressEvent = 2
)

// Progress is one advisory observation from a segment build or metadata merge.
// Stage names are diagnostic native identifiers and may change between native
// releases; do not branch on them. Observations may be concurrent and
// unordered, so only use them for reporting.
type Progress struct {
	Event     ProgressEvent
	Stage     string
	Total     uint64
	Unit      string
	Completed uint64
}

// ProgressFunc observes index build progress. The native producer calls it
// from worker threads, possibly concurrently and for overlapping stages, so
// the function must be fast, thread-safe, and must not call back into any
// Lance API. A panic is recovered by the bridge and does not stop the build.
type ProgressFunc func(Progress)

// SegmentIndexType is the native index-type discriminant recorded in segment
// metadata. Scalar and vector discriminants live in disjoint native ranges, so
// at most one of Vector and Scalar reports true.
type SegmentIndexType int32

// Vector returns the vector index type named by t and whether t is one.
func (t SegmentIndexType) Vector() (VectorType, bool) {
	if t >= SegmentIndexType(VectorTypeIVFFlat) && t <= SegmentIndexType(VectorTypeIVFRQ) {
		return VectorType(t), true
	}
	return 0, false
}

// Scalar returns the scalar index type named by t and whether t is one.
func (t SegmentIndexType) Scalar() (ScalarType, bool) {
	if t >= SegmentIndexType(ScalarBTree) && t <= SegmentIndexType(ScalarMinHashLSH) {
		return ScalarType(t), true
	}
	return 0, false
}

// String returns the native name of t, or its decimal value for a
// discriminant this package does not know.
func (t SegmentIndexType) String() string {
	switch t {
	case SegmentIndexType(VectorTypeIVFFlat):
		return "IVF_FLAT"
	case SegmentIndexType(VectorTypeIVFSQ):
		return "IVF_SQ"
	case SegmentIndexType(VectorTypeIVFPQ):
		return "IVF_PQ"
	case SegmentIndexType(VectorTypeIVFHNSWSQ):
		return "IVF_HNSW_SQ"
	case SegmentIndexType(VectorTypeIVFHNSWPQ):
		return "IVF_HNSW_PQ"
	case SegmentIndexType(VectorTypeIVFHNSWFlat):
		return "IVF_HNSW_FLAT"
	case SegmentIndexType(VectorTypeIVFRQ):
		return "IVF_RQ"
	case SegmentIndexType(ScalarBTree):
		return "BTREE"
	case SegmentIndexType(ScalarBitmap):
		return "BITMAP"
	case SegmentIndexType(ScalarLabelList):
		return "LABEL_LIST"
	case SegmentIndexType(ScalarInverted):
		return "INVERTED"
	case SegmentIndexType(ScalarNgram):
		return "NGRAM"
	case SegmentIndexType(ScalarZoneMap):
		return "ZONE_MAP"
	case SegmentIndexType(ScalarBloomFilter):
		return "BLOOM_FILTER"
	case SegmentIndexType(ScalarMinHashLSH):
		return "MINHASH_LSH"
	}
	return strconv.Itoa(int(t))
}

// MergeMetadataOptions controls a distributed index metadata merge.
type MergeMetadataOptions struct {
	// BatchReadhead is forwarded as an explicit native batch readhead when
	// non-nil, including zero. The pinned native core currently ignores it.
	BatchReadhead *uint64
	// Progress observes merge progress and follows the ProgressFunc contract.
	Progress ProgressFunc
}

// MetadataFile is one index artifact recorded in segment metadata. SizeBytes
// is the recorded file size at segment creation time.
type MetadataFile struct {
	Path      string
	SizeBytes uint64
}
