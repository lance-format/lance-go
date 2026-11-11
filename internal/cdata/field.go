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
