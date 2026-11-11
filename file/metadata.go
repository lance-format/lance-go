package file

import (
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
)

// FormatVersion identifies the raw file format revision.
type FormatVersion int32

const (
	FormatV1   FormatVersion = 0
	FormatV2_0 FormatVersion = 1
	FormatV2_1 FormatVersion = 2
	FormatV2_2 FormatVersion = 3
	FormatV2_3 FormatVersion = 4
)

// MetadataInfo is an owned scalar metadata snapshot.
type MetadataInfo struct {
	FormatVersion          FormatVersion
	MajorVersion           uint16
	MinorVersion           uint16
	NumRows                uint64
	NumDataBytes           uint64
	NumColumnMetadataBytes uint64
	NumGlobalBufferBytes   uint64
	NumFooterBytes         uint64
	FileSizeBytes          uint64
	NumColumns             uint64
	NumPhysicalColumns     uint64
	NumGlobalBuffers       uint64
}

// BufferDescriptor locates a byte range in the file.
type BufferDescriptor struct {
	Position uint64
	Size     uint64
}

// EncodingKind identifies how a page or column is encoded.
type EncodingKind int32

const (
	EncodingAbsent   EncodingKind = 0
	EncodingNone     EncodingKind = 1
	EncodingDirect   EncodingKind = 2
	EncodingIndirect EncodingKind = 3
)

// EncodingDescriptor describes one native encoding.
type EncodingDescriptor struct {
	Kind           EncodingKind
	BufferLocation uint64
	BufferLength   uint64
	DirectBytes    []byte
}

// PageMetadata is an owned page descriptor.
type PageMetadata struct {
	Buffers             []BufferDescriptor
	NumRows             uint64
	Priority            uint64
	Encoding            EncodingDescriptor
	EncodingDescription string
}

// ColumnMetadata is an owned column descriptor.
type ColumnMetadata struct {
	Buffers  []BufferDescriptor
	Pages    []PageMetadata
	Encoding EncodingDescriptor
}

// Metadata is an independent native metadata snapshot. It stays usable after
// the reader and session that produced it are closed.
type Metadata struct {
	mu     sync.Mutex
	native *ffi.FileMetadata
	uri    string
}

// Close releases the native snapshot. It is idempotent and waits for in-flight
// snapshot calls.
func (m *Metadata) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	native := m.native
	m.native = nil
	m.mu.Unlock()
	if native != nil {
		native.Close()
	}
	return nil
}

func (m *Metadata) acquire() (*ffi.FileMetadata, func(), error) {
	m.mu.Lock()
	if m.native == nil {
		m.mu.Unlock()
		return nil, nil, closedError("metadata", m.uri)
	}
	return m.native, m.mu.Unlock, nil
}

// Info copies scalar metadata.
func (m *Metadata) Info() (MetadataInfo, error) {
	native, unlock, err := m.acquire()
	if err != nil {
		return MetadataInfo{}, err
	}
	defer unlock()

	info, err := native.Info()
	if err != nil {
		return MetadataInfo{}, wrapError("metadata info", m.uri, err)
	}
	return MetadataInfo{
		FormatVersion:          FormatVersion(info.FormatVersion),
		MajorVersion:           info.MajorVersion,
		MinorVersion:           info.MinorVersion,
		NumRows:                info.NumRows,
		NumDataBytes:           info.NumDataBytes,
		NumColumnMetadataBytes: info.NumColumnMetadataBytes,
		NumGlobalBufferBytes:   info.NumGlobalBufferBytes,
		NumFooterBytes:         info.NumFooterBytes,
		FileSizeBytes:          info.FileSizeBytes,
		NumColumns:             info.NumColumns,
		NumPhysicalColumns:     info.NumPhysicalColumns,
		NumGlobalBuffers:       info.NumGlobalBuffers,
	}, nil
}

// Schema returns the complete file schema, including field metadata.
func (m *Metadata) Schema() (*arrow.Schema, error) {
	native, unlock, err := m.acquire()
	if err != nil {
		return nil, err
	}
	defer unlock()

	schema, err := native.Schema()
	if err != nil {
		return nil, wrapError("metadata schema", m.uri, err)
	}
	return schema, nil
}

// GlobalBuffers copies global buffer descriptors. Use Reader.ReadGlobalBuffer
// to fetch contents.
func (m *Metadata) GlobalBuffers() ([]BufferDescriptor, error) {
	native, unlock, err := m.acquire()
	if err != nil {
		return nil, err
	}
	defer unlock()

	buffers, err := native.GlobalBuffers()
	if err != nil {
		return nil, wrapError("metadata global buffers", m.uri, err)
	}
	result := make([]BufferDescriptor, len(buffers))
	for i, buffer := range buffers {
		result[i] = BufferDescriptor{Position: buffer.Position, Size: buffer.Size}
	}
	return result, nil
}

// Columns copies per-column and per-page descriptors.
func (m *Metadata) Columns() ([]ColumnMetadata, error) {
	native, unlock, err := m.acquire()
	if err != nil {
		return nil, err
	}
	defer unlock()

	columns, err := native.Columns()
	if err != nil {
		return nil, wrapError("metadata columns", m.uri, err)
	}
	result := make([]ColumnMetadata, len(columns))
	for i, column := range columns {
		result[i] = ColumnMetadata{
			Buffers:  copyBuffers(column.Buffers),
			Pages:    copyPages(column.Pages),
			Encoding: copyEncoding(column.Encoding),
		}
	}
	return result, nil
}

func copyBuffers(buffers []ffi.FileBufferDescriptor) []BufferDescriptor {
	result := make([]BufferDescriptor, len(buffers))
	for i, buffer := range buffers {
		result[i] = BufferDescriptor{Position: buffer.Position, Size: buffer.Size}
	}
	return result
}

func copyPages(pages []ffi.FilePageMetadata) []PageMetadata {
	result := make([]PageMetadata, len(pages))
	for i, page := range pages {
		result[i] = PageMetadata{
			Buffers:             copyBuffers(page.Buffers),
			NumRows:             page.NumRows,
			Priority:            page.Priority,
			Encoding:            copyEncoding(page.Encoding),
			EncodingDescription: page.EncodingDescription,
		}
	}
	return result
}

func copyEncoding(encoding ffi.FileEncodingDescriptor) EncodingDescriptor {
	return EncodingDescriptor{
		Kind:           EncodingKind(encoding.Kind),
		BufferLocation: encoding.BufferLocation,
		BufferLength:   encoding.BufferLength,
		DirectBytes:    append([]byte(nil), encoding.DirectBytes...),
	}
}

// ColumnStatistics is an owned per-column statistics snapshot.
type ColumnStatistics struct {
	NumPages  uint64
	SizeBytes uint64
}

// Statistics is an independent native statistics snapshot.
type Statistics struct {
	mu     sync.Mutex
	native *ffi.FileStatistics
	uri    string
}

// Close releases the native snapshot. It is idempotent and waits for in-flight
// snapshot calls.
func (s *Statistics) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	native := s.native
	s.native = nil
	s.mu.Unlock()
	if native != nil {
		native.Close()
	}
	return nil
}

// Columns copies per-column statistics.
func (s *Statistics) Columns() ([]ColumnStatistics, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.native == nil {
		return nil, closedError("statistics", s.uri)
	}

	columns, err := s.native.Columns()
	if err != nil {
		return nil, wrapError("statistics columns", s.uri, err)
	}
	result := make([]ColumnStatistics, len(columns))
	for i, column := range columns {
		result[i] = ColumnStatistics{NumPages: column.NumPages, SizeBytes: column.SizeBytes}
	}
	return result, nil
}
