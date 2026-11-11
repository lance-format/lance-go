package lance

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
)

// BlobFieldOptions selects native blob v2 storage thresholds. A nil threshold
// keeps the native default. A present zero inline threshold is meaningful; a
// present zero dedicated threshold is rejected natively.
type BlobFieldOptions struct {
	InlineSizeThreshold    *uint64
	DedicatedSizeThreshold *uint64
}

// BlobField returns the native blob v2 field with default storage thresholds.
// The field is Struct<data: LargeBinary?, uri: Utf8?> with the
// "lance.blob.v2" extension metadata and belongs in the schema passed to a
// dataset write.
func BlobField(name string, nullable bool) (arrow.Field, error) {
	if err := validateCString(name); err != nil {
		return arrow.Field{}, invalidError("blob field", "", "%v", err)
	}
	field, err := ffi.BlobField(name, nullable)
	if err != nil {
		return arrow.Field{}, wrapError("blob field", "", err)
	}
	return field, nil
}

// BlobFieldWithOptions returns the native blob v2 field with explicit
// thresholds.
func BlobFieldWithOptions(name string, nullable bool, options BlobFieldOptions) (arrow.Field, error) {
	if err := validateCString(name); err != nil {
		return arrow.Field{}, invalidError("blob field", "", "%v", err)
	}
	raw := ffi.BlobFieldOptions{}
	if options.InlineSizeThreshold != nil {
		raw.HasInlineSizeThreshold = true
		raw.InlineSizeThreshold = *options.InlineSizeThreshold
	}
	if options.DedicatedSizeThreshold != nil {
		raw.HasDedicatedSizeThreshold = true
		raw.DedicatedSizeThreshold = *options.DedicatedSizeThreshold
	}
	field, err := ffi.BlobFieldWithOptions(name, nullable, raw)
	if err != nil {
		return arrow.Field{}, wrapError("blob field", "", err)
	}
	return field, nil
}
