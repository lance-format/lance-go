// Package exampleutil contains the small local fixtures shared by the runnable
// examples. It is not part of the public SDK.
package exampleutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go"
)

// TempURI returns a dataset path under a new temporary directory. The caller
// must call cleanup when it no longer needs the dataset.
func TempURI(prefix string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "lance-go-"+prefix+"-")
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(dir, "data.lance"), func() { _ = os.RemoveAll(dir) }, nil
}

// NewReader builds one Arrow batch with fill and returns a reader that owns its
// reference to the batch. The caller releases the reader.
func NewReader(schema *arrow.Schema, fill func(*array.RecordBuilder)) (array.RecordReader, error) {
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	fill(builder)
	batch := builder.NewRecordBatch()
	defer batch.Release()
	return array.NewRecordReader(schema, []arrow.RecordBatch{batch})
}

// Create writes one Arrow batch built by fill to a new dataset. The helper owns
// the reader; the returned dataset is owned by the caller.
func Create(ctx context.Context, uri string, schema *arrow.Schema, fill func(*array.RecordBuilder)) (*lance.Dataset, error) {
	reader, err := NewReader(schema, fill)
	if err != nil {
		return nil, err
	}
	defer reader.Release()
	return lance.Create(ctx, uri, reader)
}

// Int64Column returns a copy of an int64 column from batch.
func Int64Column(batch arrow.RecordBatch, name string) ([]int64, error) {
	column, err := columnByName(batch, name)
	if err != nil {
		return nil, err
	}
	values, ok := column.(*array.Int64)
	if !ok {
		return nil, fmt.Errorf("column %q has type %T, want *array.Int64", name, column)
	}
	result := make([]int64, values.Len())
	for i := range result {
		result[i] = values.Value(i)
	}
	return result, nil
}

// Float32Column returns a copy of a float32 column from batch.
func Float32Column(batch arrow.RecordBatch, name string) ([]float32, error) {
	column, err := columnByName(batch, name)
	if err != nil {
		return nil, err
	}
	values, ok := column.(*array.Float32)
	if !ok {
		return nil, fmt.Errorf("column %q has type %T, want *array.Float32", name, column)
	}
	result := make([]float32, values.Len())
	for i := range result {
		result[i] = values.Value(i)
	}
	return result, nil
}

// ScanInt64 scans one int64 column and returns its values in scan order.
func ScanInt64(ctx context.Context, dataset *lance.Dataset, column string, opts ...lance.ScanOption) ([]int64, error) {
	scanner, err := dataset.Scan(append([]lance.ScanOption{lance.WithColumns(column)}, opts...)...)
	if err != nil {
		return nil, err
	}
	defer scanner.Close()

	var result []int64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			return nil, err
		}
		values, err := Int64Column(batch, column)
		if err != nil {
			return nil, err
		}
		result = append(result, values...)
	}
	return result, nil
}

func columnByName(batch arrow.RecordBatch, name string) (arrow.Array, error) {
	for i := 0; i < int(batch.NumCols()); i++ {
		if batch.Schema().Field(i).Name == name {
			return batch.Column(i), nil
		}
	}
	return nil, fmt.Errorf("batch has no column %q", name)
}
