// Package index defines the public vector and scalar index options, enums and
// metadata values used by the lance dataset package.
//
// The types in this package describe native lance-c values. Optional tuning
// fields use pointers so that an explicit zero or false is forwarded instead of
// being replaced by a native default. A nil option means the native default.
package index

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
)

// VectorType selects a native vector index algorithm.
type VectorType int32

const (
	VectorTypeIVFFlat     VectorType = 101
	VectorTypeIVFSQ       VectorType = 102
	VectorTypeIVFPQ       VectorType = 103
	VectorTypeIVFHNSWSQ   VectorType = 104
	VectorTypeIVFHNSWPQ   VectorType = 105
	VectorTypeIVFHNSWFlat VectorType = 106
	VectorTypeIVFRQ       VectorType = 107
)

// ScalarType selects a native scalar index plugin.
type ScalarType int32

const (
	ScalarBTree       ScalarType = 1
	ScalarBitmap      ScalarType = 2
	ScalarLabelList   ScalarType = 3
	ScalarInverted    ScalarType = 4
	ScalarNgram       ScalarType = 5
	ScalarZoneMap     ScalarType = 6
	ScalarBloomFilter ScalarType = 7
	ScalarMinHashLSH  ScalarType = 8
)

// Metric selects a vector distance metric.
type Metric int32

const (
	MetricL2      Metric = 0
	MetricCosine  Metric = 1
	MetricDot     Metric = 2
	MetricHamming Metric = 3
)

// FileVersion selects the on-disk index file format.
type FileVersion int32

const (
	FileVersionV3     FileVersion = 0
	FileVersionLegacy FileVersion = 1
)

// VectorParams mirrors the legacy LanceVectorIndexParams structure. Zero values
// select native defaults, including native validation for required fields.
type VectorParams struct {
	IndexType          VectorType
	Metric             Metric
	NumPartitions      uint32
	NumSubVectors      uint32
	NumBits            uint32
	MaxIterations      uint32
	HNSWM              uint32
	HNSWEFConstruction uint32
	SampleRate         uint32
}

// Options expands legacy vector parameters into VectorOptions. Only non-zero
// legacy fields become explicit options; zero keeps the native default.
func (p VectorParams) Options() VectorOptions {
	return VectorOptions{
		IndexType:          p.IndexType,
		Metric:             p.Metric,
		NumPartitions:      optionalUint64(p.NumPartitions),
		NumSubVectors:      optionalUint64(p.NumSubVectors),
		NumBits:            optionalUint64(p.NumBits),
		MaxIters:           optionalUint64(p.MaxIterations),
		HNSWM:              optionalUint64(p.HNSWM),
		HNSWEFConstruction: optionalUint64(p.HNSWEFConstruction),
		SampleRate:         optionalUint64(p.SampleRate),
	}
}

// PrecomputedShuffleBuffers selects stored shuffle buffers for distributed
// indexing. A non-nil value with no buffers means an explicitly empty set;
// nil means no precomputed shuffle buffers.
type PrecomputedShuffleBuffers struct {
	Path    string
	Buffers []string
}

// VectorOptions controls vector index creation. It covers every field of the
// native LanceVectorIndexOptions structure.
type VectorOptions struct {
	IndexType VectorType
	Metric    Metric

	NumPartitions         *uint64
	TargetPartitionSize   *uint64
	MaxIters              *uint64
	SampleRate            *uint64
	StreamingSampleRate   *uint64
	StreamingCoresetRate  *uint64
	StreamingRefinePasses uint64

	PrecomputedPartitionsFile   string
	PrecomputedShuffleBuffers   *PrecomputedShuffleBuffers
	ShufflePartitionBatches     *uint64
	ShufflePartitionConcurrency *uint64
	StorageOptions              map[string]string

	NumSubVectors *uint64
	NumBits       *uint64

	// IVFCentroids is an optional Arrow FixedSizeList array of precomputed
	// centroids. PQCodebook is an optional Arrow FixedSizeList array of
	// precomputed product-quantization codebooks.
	IVFCentroids arrow.Array
	PQCodebook   arrow.Array

	RabitQModelJSON json.RawMessage

	HNSWMaxLevel       *uint64
	HNSWM              *uint64
	HNSWEFConstruction *uint64

	FileVersion   FileVersion
	SkipTranspose bool

	RuntimeHints map[string]string

	// Train defaults to true. Set a non-nil value to choose the native train
	// flag explicitly.
	Train *bool
}

// ScalarOptions controls scalar index creation. Params is forwarded verbatim as
// the native plugin JSON, so family-specific keys survive even though they are
// not represented here.
type ScalarOptions struct {
	Train *bool
}

// PrewarmOptions selects index segments to prewarm. WithPosition warms FTS
// token positions. A nil SegmentUUIDs selects every segment; a non-nil empty
// slice explicitly selects none.
type PrewarmOptions struct {
	WithPosition bool
	SegmentUUIDs []UUID
}

// UUID is a native index segment identifier.
type UUID [16]byte

// String returns the canonical hyphenated UUID form.
func (u UUID) String() string {
	var out [36]byte
	hex.Encode(out[0:8], u[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], u[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], u[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], u[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], u[10:16])
	return string(out[:])
}

// ParseUUID parses a hyphenated or bare 32-character hexadecimal UUID.
func ParseUUID(value string) (UUID, error) {
	var uuid UUID
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return uuid, fmt.Errorf("index: invalid UUID %q", value)
	}
	decoded, err := hex.DecodeString(compact)
	if err != nil {
		return uuid, fmt.Errorf("index: invalid UUID %q: %w", value, err)
	}
	copy(uuid[:], decoded)
	return uuid, nil
}

// Summary is one entry from the native user index list.
type Summary struct {
	Name           string   `json:"name"`
	UUID           string   `json:"uuid"`
	Columns        []string `json:"columns"`
	Type           string   `json:"type"`
	DatasetVersion uint64   `json:"dataset_version"`
}

// Description describes one native index, including system indexes.
type Description struct {
	Name           string          `json:"name"`
	TypeURL        string          `json:"type_url"`
	IndexType      string          `json:"index_type"`
	Fields         []int32         `json:"fields"`
	FieldNames     []string        `json:"field_names"`
	NumRowsIndexed uint64          `json:"num_rows_indexed"`
	Details        json.RawMessage `json:"details"`
	TotalSizeBytes *uint64         `json:"total_size_bytes"`
	Segments       []SegmentInfo   `json:"segments"`
}

// SegmentInfo describes one physical segment of a described index. A nil
// FragmentIDs means the recorded coverage is unknown; a non-nil empty slice
// means the segment is known to cover no fragments.
type SegmentInfo struct {
	UUID                       string   `json:"uuid"`
	DatasetVersionAtLastUpdate uint64   `json:"dataset_version_at_last_update"`
	FragmentIDs                []uint32 `json:"fragment_ids"`
	IndexVersion               uint64   `json:"index_version"`
	CreatedAt                  *string  `json:"created_at"`
	SizeBytes                  *uint64  `json:"size_bytes"`
	BaseID                     *uint64  `json:"base_id"`
	CoveringFields             []int32  `json:"covering_fields"`
}

// SegmentListing is one physical index segment from the native segment list.
// A nil FragmentIDs means unknown coverage; an empty slice means known-empty.
type SegmentListing struct {
	Name           string   `json:"name"`
	UUID           string   `json:"uuid"`
	Columns        []string `json:"columns"`
	Type           string   `json:"type"`
	DatasetVersion uint64   `json:"dataset_version"`
	IndexVersion   uint64   `json:"index_version"`
	FragmentIDs    []uint32 `json:"fragment_ids"`
}

// Statistics is the native index statistics JSON. Its fields are specific to
// the index family, so callers decode it with json.Unmarshal. Keeping the raw
// bytes preserves uint64 identifiers and unknown native fields exactly.
type Statistics = json.RawMessage

func optionalUint64(value uint32) *uint64 {
	if value == 0 {
		return nil
	}
	copied := uint64(value)
	return &copied
}
