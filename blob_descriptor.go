package lance

import (
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
)

// BlobDescriptorBuilder builds one native prepared blob descriptor column.
// The builder is not safe for concurrent use.
//
// Finish consumes the native builder. Later append calls report that it is
// finished; Field keeps returning the prepared field until Close.
type BlobDescriptorBuilder struct {
	native *ffi.BlobDescriptorBuilder
	mu     sync.Mutex
	closed bool
}

// NewBlobDescriptorBuilder creates a descriptor builder for column. The
// prepared output can be written as a blob v2 column.
func NewBlobDescriptorBuilder(column string) (*BlobDescriptorBuilder, error) {
	if err := validateCString(column); err != nil {
		return nil, invalidError("blob descriptor builder", "", "%v", err)
	}
	native, err := ffi.NewBlobDescriptorBuilder(column)
	if err != nil {
		return nil, wrapError("blob descriptor builder", "", err)
	}
	return &BlobDescriptorBuilder{native: native}, nil
}

// Field exports the prepared field. It remains available after Finish.
func (b *BlobDescriptorBuilder) Field() (arrow.Field, error) {
	if b == nil {
		return arrow.Field{}, closedError("blob descriptor field", "")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return arrow.Field{}, closedError("blob descriptor field", "")
	}
	field, err := b.native.Field()
	if err != nil {
		return arrow.Field{}, wrapError("blob descriptor field", "", err)
	}
	return field, nil
}

// AppendInline appends an owned inline blob value.
func (b *BlobDescriptorBuilder) AppendInline(data []byte) error {
	if b == nil {
		return closedError("blob descriptor append inline", "")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return closedError("blob descriptor append inline", "")
	}
	if err := b.native.AppendInline(data); err != nil {
		return wrapError("blob descriptor append inline", "", err)
	}
	return nil
}

// AppendNull appends a null blob value.
func (b *BlobDescriptorBuilder) AppendNull() error {
	if b == nil {
		return closedError("blob descriptor append null", "")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return closedError("blob descriptor append null", "")
	}
	if err := b.native.AppendNull(); err != nil {
		return wrapError("blob descriptor append null", "", err)
	}
	return nil
}

// AppendPacked appends one packed descriptor.
func (b *BlobDescriptorBuilder) AppendPacked(blobID uint32, offset, length uint64) error {
	if b == nil {
		return closedError("blob descriptor append packed", "")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return closedError("blob descriptor append packed", "")
	}
	if err := b.native.AppendPacked(blobID, offset, length); err != nil {
		return wrapError("blob descriptor append packed", "", err)
	}
	return nil
}

// ExtendPacked appends packed descriptors in order. If a later range is
// invalid, native keeps the already appended prefix and returns an error.
func (b *BlobDescriptorBuilder) ExtendPacked(blobID uint32, ranges []BlobRange) error {
	if b == nil {
		return closedError("blob descriptor extend packed", "")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return closedError("blob descriptor extend packed", "")
	}
	native := make([]ffi.BlobRange, len(ranges))
	for i, r := range ranges {
		native[i] = ffi.BlobRange{Offset: r.Offset, Length: r.Length}
	}
	if err := b.native.ExtendPacked(blobID, native); err != nil {
		return wrapError("blob descriptor extend packed", "", err)
	}
	return nil
}

// AppendDedicated appends one dedicated descriptor.
func (b *BlobDescriptorBuilder) AppendDedicated(blobID uint32, length uint64) error {
	if b == nil {
		return closedError("blob descriptor append dedicated", "")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return closedError("blob descriptor append dedicated", "")
	}
	if err := b.native.AppendDedicated(blobID, length); err != nil {
		return wrapError("blob descriptor append dedicated", "", err)
	}
	return nil
}

// AppendManaged appends one managed descriptor. A nil baseID selects native
// None; a non-nil baseID selects Some, including Some(0). Managed registration
// and schema opt-in are validated by the dataset write, not by the builder.
func (b *BlobDescriptorBuilder) AppendManaged(uri string, offset, length uint64, baseID *uint32) error {
	if b == nil {
		return closedError("blob descriptor append managed", "")
	}
	if err := validateCString(uri); err != nil {
		return invalidError("blob descriptor append managed", "", "%v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return closedError("blob descriptor append managed", "")
	}
	if err := b.native.AppendManaged(uri, offset, length, baseID); err != nil {
		return wrapError("blob descriptor append managed", "", err)
	}
	return nil
}

// AppendExternal appends one external descriptor with an explicit base ID.
func (b *BlobDescriptorBuilder) AppendExternal(baseID uint32, uri string, offset, length uint64) error {
	if b == nil {
		return closedError("blob descriptor append external", "")
	}
	if err := validateCString(uri); err != nil {
		return invalidError("blob descriptor append external", "", "%v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return closedError("blob descriptor append external", "")
	}
	if err := b.native.AppendExternal(baseID, uri, offset, length); err != nil {
		return wrapError("blob descriptor append external", "", err)
	}
	return nil
}

// PushExternal appends an external descriptor through native push_external.
// A false hasRange stores offset 0 and length 0; true forwards the range.
func (b *BlobDescriptorBuilder) PushExternal(uri string, hasRange bool, offset, length uint64) error {
	if b == nil {
		return closedError("blob descriptor push external", "")
	}
	if err := validateCString(uri); err != nil {
		return invalidError("blob descriptor push external", "", "%v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return closedError("blob descriptor push external", "")
	}
	if err := b.native.PushExternal(uri, hasRange, offset, length); err != nil {
		return wrapError("blob descriptor push external", "", err)
	}
	return nil
}

// Finish consumes the native builder and returns the owned prepared array and
// field. The caller owns the array and must release it.
func (b *BlobDescriptorBuilder) Finish() (arrow.Array, arrow.Field, error) {
	if b == nil {
		return nil, arrow.Field{}, closedError("blob descriptor finish", "")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, arrow.Field{}, closedError("blob descriptor finish", "")
	}
	array, field, err := b.native.Finish()
	if err != nil {
		return nil, arrow.Field{}, wrapError("blob descriptor finish", "", err)
	}
	return array, field, nil
}

// Close frees the native builder. It is idempotent.
func (b *BlobDescriptorBuilder) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	b.native.Close()
	return nil
}
