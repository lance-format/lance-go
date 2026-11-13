package lance

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/cdata"
	"github.com/lance-format/lance-go/internal/ffi"
)

// BatchInfo identifies one batch of one fragment during a batch UDF.
type BatchInfo struct {
	FragmentID uint32
	BatchIndex uint64
}

// BatchUDFCheckpointStore persists mapper inputs, outputs and fragment
// checkpoints during a batch UDF. A non-nil store must implement all methods.
// A nil Store disables native checkpointing.
//
// InsertBatch receives a borrowed batch whose schema includes opaque native
// schema metadata. Keep the batch and its schema together; call Retain
// before keeping it beyond the method. GetBatch must return that retained batch
// with its original schema, including the schema metadata received by
// InsertBatch. Do not rebuild a cached batch from the declared OutputSchema or
// synthesize native checkpoint metadata, because native validates the
// preserved schema before accepting a hit. The adapter exports the returned
// batch with its own schema and then releases the reference, so call Retain
// before returning a cached batch that must stay alive. GetFragment returns
// found=false when a fragment has no stored checkpoint. Strings passed to
// InsertFragment are copied and may be retained.
type BatchUDFCheckpointStore interface {
	GetBatch(info BatchInfo) (arrow.RecordBatch, bool, error)
	InsertBatch(info BatchInfo, batch arrow.RecordBatch) error
	GetFragment(fragmentID uint32) (string, bool, error)
	InsertFragment(fragmentID uint64, fragmentJSON string) error
}

// BatchUDF describes one mapper used by AddColumnsBatchUDF.
//
// Mapper borrows a Go-owned copy of the native input and returns an owned
// output reference, which the adapter releases after exporting it. Call Retain
// before keeping the input or returning an output reference owned elsewhere.
// Exporting the output retains its buffers without copying them. ReadColumns
// nil means all columns; a non-nil empty slice means no columns. BatchSize nil
// leaves the native default; a non-nil value, including zero, is forwarded.
type BatchUDF struct {
	Mapper       func(arrow.RecordBatch) (arrow.RecordBatch, error)
	OutputSchema *arrow.Schema
	ReadColumns  []string
	BatchSize    *uint32
	Store        BatchUDFCheckpointStore
}

// AddColumnsBatchUDF returns a new Dataset with columns produced by mapper.
// The operation runs synchronously. Mapper and store callbacks execute while
// the native dataset lock is held; they must not call back into this Dataset.
func (d *Dataset) AddColumnsBatchUDF(ctx context.Context, udf BatchUDF) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if udf.Mapper == nil {
		return nil, invalidError("add columns batch UDF", d.uri, "mapper must not be nil")
	}
	if udf.OutputSchema == nil {
		return nil, invalidError("add columns batch UDF", d.uri, "output schema must not be nil")
	}
	for i, column := range udf.ReadColumns {
		if column == "" {
			return nil, invalidError("add columns batch UDF", d.uri, "read column %d is empty", i)
		}
		if err := validateCString(column); err != nil {
			return nil, invalidError("add columns batch UDF", d.uri, "read column %d: %v", i, err)
		}
	}

	var exported cdata.ArrowSchema
	if err := cdata.ExportSchema(udf.OutputSchema, &exported); err != nil {
		return nil, invalidError("add columns batch UDF", d.uri, "output schema: %v", err)
	}
	defer cdata.ReleaseSchema(&exported)

	var store ffi.BatchUDFStore
	if udf.Store != nil {
		store = batchUDFStoreAdapter{store: udf.Store}
	}
	next, err := d.mutate("add columns batch UDF", func(native *ffi.Dataset) error {
		return native.AddColumnsBatchUDF(ffi.BatchUDF{
			Mapper:       udf.Mapper,
			OutputSchema: &exported,
			Store:        store,
		}, udf.ReadColumns, udf.ReadColumns != nil, udf.BatchSize)
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

type batchUDFStoreAdapter struct {
	store BatchUDFCheckpointStore
}

func (a batchUDFStoreAdapter) GetBatch(info ffi.BatchInfo) (arrow.RecordBatch, bool, error) {
	return a.store.GetBatch(BatchInfo{FragmentID: info.FragmentID, BatchIndex: info.BatchIndex})
}

func (a batchUDFStoreAdapter) InsertBatch(info ffi.BatchInfo, batch arrow.RecordBatch) error {
	return a.store.InsertBatch(BatchInfo{FragmentID: info.FragmentID, BatchIndex: info.BatchIndex}, batch)
}

func (a batchUDFStoreAdapter) GetFragment(fragmentID uint32) (string, bool, error) {
	return a.store.GetFragment(fragmentID)
}

func (a batchUDFStoreAdapter) InsertFragment(fragmentID uint64, fragmentJSON string) error {
	return a.store.InsertFragment(fragmentID, fragmentJSON)
}
