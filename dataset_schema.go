package lance

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
)

// SQLColumn is one column produced by a SQL expression for AddColumnsSQL.
type SQLColumn struct {
	Name       string
	Expression string
}

// ColumnAlteration changes one existing column. Path is required. Rename nil
// keeps the current name. Nullable nil keeps the current nullability. DataType
// nil keeps the current type. At least one change must be set.
type ColumnAlteration struct {
	Path     string
	Rename   *string
	Nullable *bool
	DataType arrow.DataType
}

// AddColumnsSQL returns a new Dataset with columns computed by SQL
// expressions. An optional batchSize controls the backfill batch size; zero
// uses Lance's default.
func (d *Dataset) AddColumnsSQL(ctx context.Context, columns []SQLColumn, batchSize ...uint64) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, invalidError("add columns SQL", d.uri, "at least one column is required")
	}
	size, err := optionalBatchSize(batchSize)
	if err != nil {
		return nil, invalidError("add columns SQL", d.uri, "%v", err)
	}
	nativeColumns := make([]ffi.SQLColumn, len(columns))
	for i, column := range columns {
		if column.Name == "" {
			return nil, invalidError("add columns SQL", d.uri, "column %d has an empty name", i)
		}
		if column.Expression == "" {
			return nil, invalidError("add columns SQL", d.uri, "column %d has an empty expression", i)
		}
		if err := validateCString(column.Name); err != nil {
			return nil, invalidError("add columns SQL", d.uri, "column %d name: %v", i, err)
		}
		if err := validateCString(column.Expression); err != nil {
			return nil, invalidError("add columns SQL", d.uri, "column %d expression: %v", i, err)
		}
		nativeColumns[i] = ffi.SQLColumn{Name: column.Name, Expression: column.Expression}
	}
	next, err := d.mutate("add columns SQL", func(native *ffi.Dataset) error {
		return native.AddColumnsSQL(nativeColumns, size)
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

// AddColumnsNulls returns a new Dataset with the given nullable columns added
// and filled with nulls. The added schema fields must be top-level and every
// added field must be nullable.
func (d *Dataset) AddColumnsNulls(ctx context.Context, schema *arrow.Schema) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if schema == nil {
		return nil, invalidError("add columns nulls", d.uri, "schema must not be nil")
	}
	if len(schema.Fields()) == 0 {
		return nil, invalidError("add columns nulls", d.uri, "schema must contain at least one field")
	}
	for i, field := range schema.Fields() {
		if !field.Nullable {
			return nil, invalidError("add columns nulls", d.uri, "field %d (%q) is not nullable", i, field.Name)
		}
		if field.Name == "" {
			return nil, invalidError("add columns nulls", d.uri, "field %d has an empty name", i)
		}
		if err := validateCString(field.Name); err != nil {
			return nil, invalidError("add columns nulls", d.uri, "field %d name: %v", i, err)
		}
	}
	next, err := d.mutate("add columns nulls", func(native *ffi.Dataset) error {
		return native.AddColumnsNulls(schema)
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

// AddColumnsStream returns a new Dataset with columns appended from source.
// source must contain the same number of rows as the dataset and its column
// names must not collide with existing columns. source is borrowed but may be
// advanced while the native call runs; release it after this method returns.
// An optional batchSize controls the backfill batch size; zero uses Lance's
// default.
func (d *Dataset) AddColumnsStream(ctx context.Context, source array.RecordReader, batchSize ...uint64) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, invalidError("add columns stream", d.uri, "source record reader must not be nil")
	}
	size, err := optionalBatchSize(batchSize)
	if err != nil {
		return nil, invalidError("add columns stream", d.uri, "%v", err)
	}
	next, err := d.mutate("add columns stream", func(native *ffi.Dataset) error {
		return native.AddColumnsStream(source, size)
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

// AlterColumns returns a new Dataset with the requested column alterations.
func (d *Dataset) AlterColumns(ctx context.Context, alterations ...ColumnAlteration) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if len(alterations) == 0 {
		return nil, invalidError("alter columns", d.uri, "at least one alteration is required")
	}
	nativeAlterations := make([]ffi.ColumnAlteration, len(alterations))
	for i, alteration := range alterations {
		if alteration.Path == "" {
			return nil, invalidError("alter columns", d.uri, "alteration %d has an empty path", i)
		}
		if err := validateCString(alteration.Path); err != nil {
			return nil, invalidError("alter columns", d.uri, "alteration %d path: %v", i, err)
		}
		if alteration.Rename == nil && alteration.Nullable == nil && alteration.DataType == nil {
			return nil, invalidError("alter columns", d.uri, "alteration %d does not change anything", i)
		}
		var nullableMode int32
		if alteration.Nullable != nil {
			if *alteration.Nullable {
				nullableMode = 1
			} else {
				nullableMode = 2
			}
		}
		if alteration.Rename != nil {
			if *alteration.Rename == "" {
				return nil, invalidError("alter columns", d.uri, "alteration %d has an empty rename", i)
			}
			if err := validateCString(*alteration.Rename); err != nil {
				return nil, invalidError("alter columns", d.uri, "alteration %d rename: %v", i, err)
			}
		}
		nativeAlterations[i] = ffi.ColumnAlteration{
			Path:         alteration.Path,
			Rename:       alteration.Rename,
			NullableMode: nullableMode,
			DataType:     alteration.DataType,
		}
	}
	next, err := d.mutate("alter columns", func(native *ffi.Dataset) error {
		return native.AlterColumns(nativeAlterations)
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

// DropColumns returns a new Dataset with the named columns removed.
func (d *Dataset) DropColumns(ctx context.Context, columns ...string) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, invalidError("drop columns", d.uri, "at least one column is required")
	}
	for i, column := range columns {
		if column == "" {
			return nil, invalidError("drop columns", d.uri, "column %d is empty", i)
		}
		if err := validateCString(column); err != nil {
			return nil, invalidError("drop columns", d.uri, "column %d: %v", i, err)
		}
	}
	next, err := d.mutate("drop columns", func(native *ffi.Dataset) error {
		return native.DropColumns(columns)
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

func optionalBatchSize(values []uint64) (uint64, error) {
	switch len(values) {
	case 0:
		return 0, nil
	case 1:
		return values[0], nil
	default:
		return 0, &Error{Code: CodeInvalidArgument, Message: "batch size may be specified at most once"}
	}
}
