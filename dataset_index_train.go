package lance

import (
	"context"
	"encoding/json"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/index"
	"github.com/lance-format/lance-go/internal/ffi"
)

// Model is a trained index model array paired with the Arrow field it was
// trained with. Native index builders validate model provenance from that
// field's metadata, so reuse trained centroids or codebooks as a Model instead
// of the bare array. Model implements arrow.Array; Retain and Release operate on
// the embedded array.
type Model struct {
	arrow.Array
	field arrow.Field
}

// ModelField returns the Arrow field, including provenance metadata, that the
// model was trained with.
func (m *Model) ModelField() arrow.Field {
	return m.field
}

// TrainIVFModel trains IVF centroids on the current dataset snapshot and
// returns them as a Model wrapping a FixedSizeList<Float32>[dimension] array.
//
// A nil fragmentIDs trains on the whole dataset. A non-nil slice restricts
// training to those fragments and must not be empty; native validation rejects
// unknown or duplicate fragment IDs and owns the remaining parameter checks,
// including metric compatibility and a positive partition count.
func (d *Dataset) TrainIVFModel(ctx context.Context, column string, numPartitions uint32, metric index.Metric, fragmentIDs []uint32, options index.TrainerOptions) (*Model, error) {
	const op = "Dataset.TrainIVFModel"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()
	if err := validateCString(column); err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	native, field, err := d.native.TrainIVFModel(column, numPartitions, int32(metric), fragmentIDs, ffiTrainerOptions(options))
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	return &Model{Array: native, field: field}, nil
}

// TrainPQModel trains a product-quantization codebook conditioned on IVF
// centroids. The returned Model wraps a FixedSizeList<Float32> array holding
// numSubVectors*2^numBits rows of dimension/numSubVectors values, matching the
// native trainer.
//
// The centroids array is borrowed only for this call and remains owned by the
// caller. Native training reads the model provenance stored in the centroids
// field metadata, so centroids must carry it; the Model returned by
// TrainIVFModel does. Centroids without provenance, or trained with a different
// metric or dimension, are rejected. fragmentIDs follows the same contract as
// TrainIVFModel.
func (d *Dataset) TrainPQModel(ctx context.Context, column string, numSubVectors, numBits uint32, metric index.Metric, ivfCentroids arrow.Array, fragmentIDs []uint32, options index.TrainerOptions) (*Model, error) {
	const op = "Dataset.TrainPQModel"
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := d.acquire(op); err != nil {
		return nil, err
	}
	defer d.res.release()
	if err := validateCString(column); err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	if ivfCentroids == nil {
		return nil, invalidError(op, d.uri, "PQ training requires IVF centroid models")
	}
	native, field, err := d.native.TrainPQModel(column, numSubVectors, numBits, int32(metric), ivfCentroids, fragmentIDs, ffiTrainerOptions(options))
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	return &Model{Array: native, field: field}, nil
}

// BuildRQModel returns the portable IVF-RQ rotation artifact for a dimension.
// The artifact is native JSON and can be passed back through
// VectorOptions.RabitQModelJSON. A numBits of zero selects the native default;
// the dimension must be positive and divisible by eight.
func BuildRQModel(dimension, numBits uint32) (json.RawMessage, error) {
	const op = "BuildRQModel"
	model, err := ffi.BuildRQModel(dimension, numBits)
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	return json.RawMessage(model), nil
}

func ffiTrainerOptions(options index.TrainerOptions) *ffi.TrainerOptions {
	if options.SampleRate == nil && options.MaxIters == nil {
		return nil
	}
	return &ffi.TrainerOptions{
		SampleRate: options.SampleRate,
		MaxIters:   options.MaxIters,
	}
}
