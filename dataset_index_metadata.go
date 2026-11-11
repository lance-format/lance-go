package lance

import (
	"encoding/json"
	"time"

	"github.com/lance-format/lance-go/index"
	"github.com/lance-format/lance-go/internal/ffi"
)

// IndexSegmentMetadata is an owned description of one distributed index
// segment. It borrows nothing from the bytes it was parsed from, so those bytes
// (typically a worker's uploaded artifact) can be discarded as soon as parsing
// succeeds. Close the metadata when it is no longer needed.
type IndexSegmentMetadata struct {
	native *ffi.SegmentMetadata
}

// ParseIndexSegmentMetadata decodes a segment metadata artifact produced by
// SegmentBuilder.Execute. The input is borrowed only for the duration of the
// call.
func ParseIndexSegmentMetadata(data []byte) (*IndexSegmentMetadata, error) {
	const op = "ParseIndexSegmentMetadata"
	native, err := ffi.ParseSegmentMetadata(data)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return &IndexSegmentMetadata{native: native}, nil
}

func (m *IndexSegmentMetadata) call(op string, fn func(*ffi.SegmentMetadata) error) error {
	if m == nil || m.native == nil {
		return invalidError(op, "", "index segment metadata is nil")
	}
	if err := fn(m.native); err != nil {
		return wrapError(op, "", err)
	}
	return nil
}

// Close releases the metadata. It is safe to call more than once.
func (m *IndexSegmentMetadata) Close() error {
	if m == nil || m.native == nil {
		return nil
	}
	if err := m.native.Close(); err != nil {
		return wrapError("IndexSegmentMetadata.Close", "", err)
	}
	return nil
}

// UUID reports the index UUID the segment was built for.
func (m *IndexSegmentMetadata) UUID() (index.UUID, error) {
	const op = "IndexSegmentMetadata.UUID"
	var uuid index.UUID
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, err := native.UUID()
		uuid = index.UUID(value)
		return err
	})
	return uuid, err
}

// Name reports the index name the segment was built for.
func (m *IndexSegmentMetadata) Name() (string, error) {
	const op = "IndexSegmentMetadata.Name"
	var name string
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, err := native.Name()
		name = value
		return err
	})
	return name, err
}

// Type reports the segment's index family and concrete type. Scalar and vector
// type values come from disjoint enums, so one value identifies both.
func (m *IndexSegmentMetadata) Type() (index.SegmentIndexType, error) {
	const op = "IndexSegmentMetadata.Type"
	var indexType index.SegmentIndexType
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, err := native.IndexType()
		indexType = index.SegmentIndexType(value)
		return err
	})
	return indexType, err
}

// DatasetVersion reports the dataset version the segment was built from.
func (m *IndexSegmentMetadata) DatasetVersion() (uint64, error) {
	const op = "IndexSegmentMetadata.DatasetVersion"
	var version uint64
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, err := native.DatasetVersion()
		version = value
		return err
	})
	return version, err
}

// IndexVersion reports the index format version. It is not a dataset version.
func (m *IndexSegmentMetadata) IndexVersion() (int, error) {
	const op = "IndexSegmentMetadata.IndexVersion"
	var version int
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, err := native.IndexVersion()
		version = int(value)
		return err
	})
	return version, err
}

// DetailsTypeURL reports the encoded type URL of the index-specific details, if
// the segment carries details.
func (m *IndexSegmentMetadata) DetailsTypeURL() (string, bool, error) {
	const op = "IndexSegmentMetadata.DetailsTypeURL"
	var url string
	var present bool
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, ok, err := native.DetailsTypeURL()
		url, present = value, ok
		return err
	})
	return url, present, err
}

// Details returns the index-specific details payload. A present payload can be
// empty, which is distinct from an absent payload.
func (m *IndexSegmentMetadata) Details() (json.RawMessage, bool, error) {
	const op = "IndexSegmentMetadata.Details"
	var details json.RawMessage
	var present bool
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, ok, err := native.Details()
		if ok {
			details = json.RawMessage(value)
		}
		present = ok
		return err
	})
	return details, present, err
}

// FieldIDs reports the fields the index is keyed on.
func (m *IndexSegmentMetadata) FieldIDs() ([]int32, error) {
	const op = "IndexSegmentMetadata.FieldIDs"
	var ids []int32
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, err := native.FieldIDs()
		ids = value
		return err
	})
	return ids, err
}

// CoveringFieldIDs reports the fields this segment's files can answer.
func (m *IndexSegmentMetadata) CoveringFieldIDs() ([]int32, error) {
	const op = "IndexSegmentMetadata.CoveringFieldIDs"
	var ids []int32
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, err := native.CoveringFieldIDs()
		ids = value
		return err
	})
	return ids, err
}

// FragmentIDs reports the fragments the segment covers, in ascending order. An
// empty result means the segment covers no fragments explicitly.
func (m *IndexSegmentMetadata) FragmentIDs() ([]uint32, error) {
	const op = "IndexSegmentMetadata.FragmentIDs"
	var ids []uint32
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, err := native.FragmentIDs()
		ids = value
		return err
	})
	return ids, err
}

// Files reports the physical files the segment references.
func (m *IndexSegmentMetadata) Files() ([]index.MetadataFile, error) {
	const op = "IndexSegmentMetadata.Files"
	var files []index.MetadataFile
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		count, err := native.FileCount()
		if err != nil {
			return err
		}
		for i := uint64(0); i < count; i++ {
			path, err := native.FilePathAt(i)
			if err != nil {
				return err
			}
			size, err := native.FileSizeAt(i)
			if err != nil {
				return err
			}
			files = append(files, index.MetadataFile{Path: path, SizeBytes: size})
		}
		return nil
	})
	return files, err
}

// CreatedAt reports the segment creation time. The boolean is false when the
// segment carries no timestamp.
func (m *IndexSegmentMetadata) CreatedAt() (time.Time, bool, error) {
	const op = "IndexSegmentMetadata.CreatedAt"
	var created time.Time
	var present bool
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		seconds, nanos, ok, err := native.CreatedAt()
		if ok {
			created = time.Unix(seconds, int64(nanos)).UTC()
		}
		present = ok
		return err
	})
	return created, present, err
}

// BaseID reports the index base the segment extends. The boolean is false when
// no base is recorded.
func (m *IndexSegmentMetadata) BaseID() (uint32, bool, error) {
	const op = "IndexSegmentMetadata.BaseID"
	var baseID uint32
	var present bool
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, ok, err := native.BaseID()
		baseID, present = value, ok
		return err
	})
	return baseID, present, err
}

// Clone returns an independent copy of the metadata.
func (m *IndexSegmentMetadata) Clone() (*IndexSegmentMetadata, error) {
	const op = "IndexSegmentMetadata.Clone"
	if m == nil || m.native == nil {
		return nil, invalidError(op, "", "index segment metadata is nil")
	}
	native, err := m.native.Clone()
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return &IndexSegmentMetadata{native: native}, nil
}

// CloneWithFragmentIDs returns an independent copy whose coverage is replaced by
// fragmentIDs. A nil slice keeps the original coverage; a non-nil slice,
// including an empty one, sets it as given. The native side copies the in-memory
// value without re-validating coverage.
func (m *IndexSegmentMetadata) CloneWithFragmentIDs(fragmentIDs []uint32) (*IndexSegmentMetadata, error) {
	const op = "IndexSegmentMetadata.CloneWithFragmentIDs"
	if m == nil || m.native == nil {
		return nil, invalidError(op, "", "index segment metadata is nil")
	}
	native, err := m.native.CloneWithFragmentIDs(fragmentIDs)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return &IndexSegmentMetadata{native: native}, nil
}

// Encode serializes the metadata back to its transport form. Encoding
// normalizes protobuf defaults, so the result can differ byte-wise from the
// input while describing the same value.
func (m *IndexSegmentMetadata) Encode() ([]byte, error) {
	const op = "IndexSegmentMetadata.Encode"
	var data []byte
	err := m.call(op, func(native *ffi.SegmentMetadata) error {
		value, err := native.Encode()
		data = value
		return err
	})
	return data, err
}
