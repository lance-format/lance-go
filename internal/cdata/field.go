package cdata

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

// ImportField imports one field from a borrowed ArrowSchema.
//
// Unlike ImportSchema, src may describe any Arrow type rather than a record
// batch root. src remains owned by the caller and is not released.
func ImportField(src *ArrowSchema) (arrow.Field, error) {
	field, err := importField(src)
	if err != nil {
		return arrow.Field{}, fmt.Errorf("cdata: %w", err)
	}
	return field, nil
}

// ExportField exports one field, including its metadata, into dst.
// On success the caller owns dst and must release it exactly once.
// On error dst is zeroed and no resources are leaked.
func ExportField(field arrow.Field, dst *ArrowSchema) error {
	if dst == nil {
		return invalidf("nil destination ArrowSchema")
	}
	*dst = ArrowSchema{}
	exported, err := exportField(field)
	if err != nil {
		return fmt.Errorf("cdata: export field: %w", err)
	}
	*dst = exported.value
	return nil
}
