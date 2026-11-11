package ffi

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// optionalTimestamp mirrors LanceOptionalTimestamp.
type optionalTimestamp struct {
	isSet       bool
	unixSeconds int64
	nanos       uint32
}

// SegmentMetadata owns one parsed native IndexMetadata protobuf. A parsed value
// is independent of the dataset and of the bytes it was parsed from.
type SegmentMetadata struct {
	b      *bindings
	handle atomic.Uintptr
}

// ParseSegmentMetadata decodes one encoded IndexMetadata protobuf.
func ParseSegmentMetadata(encoded []byte) (*SegmentMetadata, error) {
	if len(encoded) == 0 {
		return nil, invalidf("ffi: segment metadata must not be empty")
	}
	b, err := load()
	if err != nil {
		return nil, err
	}
	var parsed uintptr
	runtime.LockOSThread()
	rc := b.metadataParse(&encoded[0], uint64(len(encoded)), &parsed)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_index_segment_metadata_parse failed without setting an error")
	}
	if parsed == 0 {
		return nil, fmt.Errorf("ffi: lance_index_segment_metadata_parse returned NULL metadata")
	}
	metadata := &SegmentMetadata{b: b}
	metadata.handle.Store(parsed)
	return metadata, nil
}

// Close releases the metadata. It is idempotent.
func (m *SegmentMetadata) Close() error {
	if m == nil {
		return nil
	}
	if parsed := m.handle.Swap(0); parsed != 0 {
		m.b.metadataFree(parsed)
	}
	return nil
}

func (m *SegmentMetadata) nativeHandle() (uintptr, error) {
	if m == nil {
		return 0, invalidf("ffi: segment metadata is nil")
	}
	parsed := m.handle.Load()
	if parsed == 0 {
		return 0, ErrClosed
	}
	return parsed, nil
}

// invoke runs one fallible metadata operation on the locked OS thread and
// converts the recorded native error.
func (m *SegmentMetadata) invoke(operation string, call func(metadata uintptr) int32) error {
	metadata, err := m.nativeHandle()
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	rc := call(metadata)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = m.b.takeError()
	}
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: %s failed without setting an error", operation)
	}
	return nil
}

// readValue runs one value-returning accessor that signals failure only through
// the last-error slot. A zero value is valid, so the slot decides.
func (m *SegmentMetadata) readValue(operation string, call func(metadata uintptr) uint64) (uint64, error) {
	metadata, err := m.nativeHandle()
	if err != nil {
		return 0, err
	}
	runtime.LockOSThread()
	value := call(metadata)
	failed := m.b.lastErrorCode() != int32(CodeOK)
	var nativeErr *Error
	if failed {
		nativeErr = m.b.takeError()
	}
	runtime.UnlockOSThread()
	if failed {
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: %s failed without setting an error", operation)
	}
	return value, nil
}

// readInt32 runs one accessor that reports failure by returning a negative
// value and records the cause in the last-error slot.
func (m *SegmentMetadata) readInt32(operation string, call func(metadata uintptr) int32) (int32, error) {
	metadata, err := m.nativeHandle()
	if err != nil {
		return 0, err
	}
	runtime.LockOSThread()
	value := call(metadata)
	var nativeErr *Error
	if value < 0 {
		nativeErr = m.b.takeError()
	}
	runtime.UnlockOSThread()
	if value < 0 {
		if nativeErr != nil {
			return 0, nativeErr
		}
		return 0, fmt.Errorf("ffi: %s failed without setting an error", operation)
	}
	return value, nil
}

// UUID returns the segment UUID.
func (m *SegmentMetadata) UUID() ([16]byte, error) {
	var uuid [16]byte
	err := m.invoke("lance_index_segment_metadata_uuid", func(metadata uintptr) int32 {
		return m.b.metadataUUID(metadata, &uuid[0])
	})
	return uuid, err
}

// Name returns the segment name.
func (m *SegmentMetadata) Name() (string, error) {
	metadata, err := m.nativeHandle()
	if err != nil {
		return "", err
	}
	runtime.LockOSThread()
	value := m.b.metadataName(metadata)
	var nativeErr *Error
	if value == nil {
		nativeErr = m.b.takeError()
	}
	runtime.UnlockOSThread()
	if value == nil {
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_index_segment_metadata_name returned NULL")
	}
	return goString(value), nil
}

// DatasetVersion returns the dataset version the segment was built from.
func (m *SegmentMetadata) DatasetVersion() (uint64, error) {
	return m.readValue("lance_index_segment_metadata_dataset_version", m.b.metadataDatasetVersion)
}

// IndexVersion returns the native index format version of the segment.
func (m *SegmentMetadata) IndexVersion() (int32, error) {
	return m.readInt32("lance_index_segment_metadata_index_version", m.b.metadataIndexVersion)
}

// IndexType returns the native index-type discriminant.
func (m *SegmentMetadata) IndexType() (int32, error) {
	return m.readInt32("lance_index_segment_metadata_index_type", m.b.metadataIndexType)
}

// DetailsTypeURL returns the index-details type URL when the metadata records
// an index_details value.
func (m *SegmentMetadata) DetailsTypeURL() (string, bool, error) {
	metadata, err := m.nativeHandle()
	if err != nil {
		return "", false, err
	}
	runtime.LockOSThread()
	value := m.b.metadataDetailsTypeURL(metadata)
	var nativeErr *Error
	if value == nil {
		nativeErr = m.b.takeError()
	}
	runtime.UnlockOSThread()
	if value == nil {
		if nativeErr != nil {
			// Native reports a segment without index details as NOT_FOUND; that
			// is an absence, not a failure.
			if nativeErr.Code == CodeNotFound {
				return "", false, nil
			}
			return "", false, nativeErr
		}
		return "", false, nil
	}
	return goString(value), true, nil
}

// FieldIDs returns the field IDs the segment indexes.
func (m *SegmentMetadata) FieldIDs() ([]int32, error) {
	count, err := m.readValue("lance_index_segment_metadata_field_count", m.b.metadataFieldCount)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return []int32{}, nil
	}
	if count > uint64(maxInt) {
		return nil, fmt.Errorf("ffi: index segment field count %d exceeds the addressable size", count)
	}
	ids := make([]int32, count)
	var written uint64
	err = m.invoke("lance_index_segment_metadata_field_ids", func(metadata uintptr) int32 {
		return m.b.metadataFieldIDs(metadata, ids, count, &written)
	})
	if err != nil {
		return nil, err
	}
	if written != count {
		return nil, fmt.Errorf("ffi: index segment records %d field IDs, expected %d", written, count)
	}
	return ids, nil
}

// CoveringFieldIDs returns the fields whose data the segment covers.
func (m *SegmentMetadata) CoveringFieldIDs() ([]int32, error) {
	count, err := m.readValue("lance_index_segment_metadata_covering_field_count", m.b.metadataCoveringFieldCount)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return []int32{}, nil
	}
	if count > uint64(maxInt) {
		return nil, fmt.Errorf("ffi: index segment field count %d exceeds the addressable size", count)
	}
	ids := make([]int32, count)
	for i := range ids {
		index := uint64(i)
		err := m.invoke("lance_index_segment_metadata_covering_field_id_at", func(metadata uintptr) int32 {
			return m.b.metadataCoveringFieldIDAt(metadata, index, &ids[i])
		})
		if err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// FragmentIDs returns the fragment coverage recorded in the segment.
func (m *SegmentMetadata) FragmentIDs() ([]uint32, error) {
	count, err := m.readValue("lance_index_segment_metadata_fragment_count", m.b.metadataFragmentCount)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return []uint32{}, nil
	}
	if count > uint64(maxInt) {
		return nil, fmt.Errorf("ffi: index segment fragment count %d exceeds the addressable size", count)
	}
	ids := make([]uint32, count)
	var written uint64
	err = m.invoke("lance_index_segment_metadata_fragment_ids", func(metadata uintptr) int32 {
		return m.b.metadataFragmentIDs(metadata, ids, count, &written)
	})
	if err != nil {
		return nil, err
	}
	if written != count {
		return nil, fmt.Errorf("ffi: index segment records %d fragment IDs, expected %d", written, count)
	}
	return ids, nil
}

// Clone returns an independent copy of the metadata.
func (m *SegmentMetadata) Clone() (*SegmentMetadata, error) {
	metadata, err := m.nativeHandle()
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	cloned := m.b.metadataClone(metadata)
	var nativeErr *Error
	if cloned == 0 {
		nativeErr = m.b.takeError()
	}
	runtime.UnlockOSThread()
	if cloned == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_index_segment_metadata_clone returned NULL metadata")
	}
	copied := &SegmentMetadata{b: m.b}
	copied.handle.Store(cloned)
	return copied, nil
}

// CloneWithFragmentIDs returns an independent copy with replaced coverage. A
// nil slice keeps the recorded coverage; a non-nil slice, including an empty
// one, replaces it.
func (m *SegmentMetadata) CloneWithFragmentIDs(fragmentIDs []uint32) (*SegmentMetadata, error) {
	metadata, err := m.nativeHandle()
	if err != nil {
		return nil, err
	}
	hasIDs := fragmentIDs != nil
	var ids *uint32
	if len(fragmentIDs) > 0 {
		ids = &fragmentIDs[0]
	}
	runtime.LockOSThread()
	cloned := m.b.metadataCloneWithFragmentIDs(metadata, hasIDs, ids, uint64(len(fragmentIDs)))
	var nativeErr *Error
	if cloned == 0 {
		nativeErr = m.b.takeError()
	}
	runtime.UnlockOSThread()
	runtime.KeepAlive(fragmentIDs)
	if cloned == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_index_segment_metadata_clone_with_fragment_ids returned NULL metadata")
	}
	copied := &SegmentMetadata{b: m.b}
	copied.handle.Store(cloned)
	return copied, nil
}

// Encode serializes the metadata back into its protobuf transport form.
func (m *SegmentMetadata) Encode() ([]byte, error) {
	metadata, err := m.nativeHandle()
	if err != nil {
		return nil, err
	}
	var data *byte
	var length uint64
	runtime.LockOSThread()
	rc := m.b.metadataEncode(metadata, &data, &length)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = m.b.takeError()
	}
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_index_segment_metadata_encode failed without setting an error")
	}
	if data == nil || length == 0 {
		if data != nil {
			m.b.freeBytes(data)
		}
		return nil, fmt.Errorf("ffi: lance_index_segment_metadata_encode returned empty metadata")
	}
	if length > uint64(maxInt) {
		m.b.freeBytes(data)
		return nil, fmt.Errorf("ffi: encoded index metadata length %d exceeds the addressable size", length)
	}
	encoded := append([]byte(nil), unsafe.Slice(data, int(length))...)
	m.b.freeBytes(data)
	return encoded, nil
}

// Details returns the encoded index-details value. The bool reports whether the
// metadata carries one; a present but empty value returns a non-nil empty
// slice.
func (m *SegmentMetadata) Details() ([]byte, bool, error) {
	metadata, err := m.nativeHandle()
	if err != nil {
		return nil, false, err
	}
	var length uint64
	runtime.LockOSThread()
	data := m.b.metadataDetailsValue(metadata, &length)
	var nativeErr *Error
	if data == nil {
		nativeErr = m.b.takeError()
	}
	runtime.UnlockOSThread()
	if data == nil {
		if nativeErr != nil {
			if nativeErr.Code == CodeNotFound {
				return nil, false, nil
			}
			return nil, false, nativeErr
		}
		return nil, false, nil
	}
	if length > uint64(maxInt) {
		return nil, false, fmt.Errorf("ffi: index details length %d exceeds the addressable size", length)
	}
	details := make([]byte, int(length))
	copy(details, unsafe.Slice(data, int(length)))
	return details, true, nil
}

// FileCount returns the number of recorded index artifacts.
func (m *SegmentMetadata) FileCount() (uint64, error) {
	return m.readValue("lance_index_segment_metadata_file_count", m.b.metadataFileCount)
}

// FilePathAt returns the path of one recorded index artifact.
func (m *SegmentMetadata) FilePathAt(index uint64) (string, error) {
	metadata, err := m.nativeHandle()
	if err != nil {
		return "", err
	}
	runtime.LockOSThread()
	value := m.b.metadataFilePathAt(metadata, index)
	var nativeErr *Error
	if value == nil {
		nativeErr = m.b.takeError()
	}
	runtime.UnlockOSThread()
	if value == nil {
		if nativeErr != nil {
			return "", nativeErr
		}
		return "", fmt.Errorf("ffi: lance_index_segment_metadata_file_path_at returned NULL")
	}
	return goString(value), nil
}

// FileSizeAt returns the recorded size of one index artifact.
func (m *SegmentMetadata) FileSizeAt(index uint64) (uint64, error) {
	var size uint64
	err := m.invoke("lance_index_segment_metadata_file_size_at", func(metadata uintptr) int32 {
		return m.b.metadataFileSizeAt(metadata, index, &size)
	})
	return size, err
}

// CreatedAt returns the recorded creation timestamp. The bool reports whether
// the optional timestamp is present.
func (m *SegmentMetadata) CreatedAt() (int64, uint32, bool, error) {
	var created optionalTimestamp
	err := m.invoke("lance_index_segment_metadata_created_at", func(metadata uintptr) int32 {
		return m.b.metadataCreatedAt(metadata, &created)
	})
	return created.unixSeconds, created.nanos, created.isSet, err
}

// BaseID returns the optional base fragment ID recorded in the metadata.
func (m *SegmentMetadata) BaseID() (uint32, bool, error) {
	var baseID uint32
	var isSet bool
	err := m.invoke("lance_index_segment_metadata_base_id", func(metadata uintptr) int32 {
		return m.b.metadataBaseID(metadata, &isSet, &baseID)
	})
	return baseID, isSet, err
}
