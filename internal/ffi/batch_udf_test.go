package ffi

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go/internal/cdata"
	"github.com/lance-format/lance-go/internal/handle"
)

type batchUDFV2TestStore struct {
	batch arrow.RecordBatch
	found bool
	info  BatchInfo
}

func (s *batchUDFV2TestStore) GetBatch(info BatchInfo) (arrow.RecordBatch, bool, error) {
	s.info = info
	if !s.found {
		return nil, false, nil
	}
	s.batch.Retain()
	return s.batch, true, nil
}

func (s *batchUDFV2TestStore) InsertBatch(BatchInfo, arrow.RecordBatch) error {
	return errors.New("unexpected InsertBatch")
}

func (s *batchUDFV2TestStore) GetFragment(uint32) (string, bool, error) {
	return "", false, errors.New("unexpected GetFragment")
}

func (s *batchUDFV2TestStore) InsertFragment(uint64, string) error {
	return errors.New("unexpected InsertFragment")
}

func makeBatchUDFV2TestBatch(t *testing.T, schema *arrow.Schema) arrow.RecordBatch {
	t.Helper()
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	builder.AppendValues([]int64{11, 22}, nil)
	column := builder.NewArray()
	batch := array.NewRecordBatch(schema, []arrow.Array{column}, 2)
	column.Release()
	builder.Release()
	return batch
}

func TestBatchUDFGetBatchV2CallbackPreservesBatchSchema(t *testing.T) {
	metadata := arrow.NewMetadata(
		[]string{"lance:udf_checkpoint_context"},
		[]string{`{"fragment":7,"batch":3}`},
	)
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, &metadata)
	batch := makeBatchUDFV2TestBatch(t, schema)
	defer batch.Release()

	store := &batchUDFV2TestStore{batch: batch, found: true}
	adapter := &batchUDFAdapter{store: store}
	ctx := handle.New(adapter)
	defer handle.Remove(ctx)

	var output cdata.ArrowArray
	var outputSchema cdata.ArrowSchema
	defer cdata.ReleaseArray(&output)
	defer cdata.ReleaseSchema(&outputSchema)
	var callbackErr callbackError
	defer ownedStringReleaseCallback(&callbackErr.Message)

	info := batchInfo{FragmentID: 7, BatchIndex: 3}
	if got := batchUDFGetBatchV2Callback(uintptr(ctx), &info, &output, &outputSchema, &callbackErr); got != 1 {
		t.Fatalf("callback status = %d, want 1 (error %+v)", got, callbackErr)
	}
	if want := (BatchInfo{FragmentID: 7, BatchIndex: 3}); store.info != want {
		t.Fatalf("store info = %+v, want %+v", store.info, want)
	}
	if output.Release == 0 || outputSchema.Release == 0 {
		t.Fatalf("hit slots are not owned: array release=%d schema release=%d", output.Release, outputSchema.Release)
	}

	importedSchema, err := cdata.ImportSchema(&outputSchema)
	if err != nil {
		t.Fatalf("ImportSchema: %v", err)
	}
	value, ok := importedSchema.Metadata().GetValue("lance:udf_checkpoint_context")
	if !ok || value != `{"fragment":7,"batch":3}` {
		t.Fatalf("exported schema metadata = %q, present %v; want preserved checkpoint value", value, ok)
	}
	if got := importedSchema.Field(0).Name; got != "value" {
		t.Fatalf("exported schema field = %q, want value", got)
	}

	imported, err := cdata.ImportRecordBatch(&output, importedSchema)
	if err != nil {
		t.Fatalf("ImportRecordBatch: %v", err)
	}
	defer imported.Release()
	if imported.NumRows() != 2 || imported.NumCols() != 1 {
		t.Fatalf("imported batch shape = %d x %d, want 2 x 1", imported.NumRows(), imported.NumCols())
	}
	values := imported.Column(0).(*array.Int64)
	if values.Value(0) != 11 || values.Value(1) != 22 {
		t.Fatalf("imported values = [%d %d], want [11 22]", values.Value(0), values.Value(1))
	}
}

func TestBatchUDFGetBatchV2CallbackMissLeavesSlotsEmpty(t *testing.T) {
	store := &batchUDFV2TestStore{}
	adapter := &batchUDFAdapter{store: store}
	ctx := handle.New(adapter)
	defer handle.Remove(ctx)

	var output cdata.ArrowArray
	var outputSchema cdata.ArrowSchema
	var callbackErr callbackError
	defer ownedStringReleaseCallback(&callbackErr.Message)

	info := batchInfo{FragmentID: 9, BatchIndex: 2}
	if got := batchUDFGetBatchV2Callback(uintptr(ctx), &info, &output, &outputSchema, &callbackErr); got != 0 {
		t.Fatalf("callback status = %d, want 0 (error %+v)", got, callbackErr)
	}
	if output.Release != 0 || outputSchema.Release != 0 {
		t.Fatalf("miss left owned slots: array release=%d schema release=%d", output.Release, outputSchema.Release)
	}
	if callbackErr.Code != 0 || callbackErr.Message.Data != nil {
		t.Fatalf("miss set a callback error: %+v", callbackErr)
	}
}

type batchUDFSchemaFailureBatch struct {
	arrow.RecordBatch
	releases    *atomic.Int32
	panicSchema bool
}

func (b *batchUDFSchemaFailureBatch) Schema() *arrow.Schema {
	if b.panicSchema {
		panic("schema boom")
	}
	return nil
}

func (b *batchUDFSchemaFailureBatch) Retain() {
	b.RecordBatch.Retain()
}

func (b *batchUDFSchemaFailureBatch) Release() {
	b.releases.Add(1)
	b.RecordBatch.Release()
}

func TestBatchUDFGetBatchV2CallbackSchemaFailureReleasesArray(t *testing.T) {
	for _, tc := range []struct {
		name        string
		panicSchema bool
		wantMessage string
	}{
		{name: "nil schema", wantMessage: "nil schema"},
		{name: "panic", panicSchema: true, wantMessage: "schema boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := arrow.NewSchema([]arrow.Field{
				{Name: "value", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			}, nil)
			base := makeBatchUDFV2TestBatch(t, schema)
			defer base.Release()

			var releases atomic.Int32
			batch := &batchUDFSchemaFailureBatch{
				RecordBatch: base,
				releases:    &releases,
				panicSchema: tc.panicSchema,
			}
			store := &batchUDFV2TestStore{batch: batch, found: true}
			adapter := &batchUDFAdapter{store: store}
			ctx := handle.New(adapter)
			defer handle.Remove(ctx)

			var output cdata.ArrowArray
			var outputSchema cdata.ArrowSchema
			defer cdata.ReleaseArray(&output)
			defer cdata.ReleaseSchema(&outputSchema)
			var callbackErr callbackError
			defer ownedStringReleaseCallback(&callbackErr.Message)

			info := batchInfo{FragmentID: 1, BatchIndex: 0}
			if got := batchUDFGetBatchV2Callback(uintptr(ctx), &info, &output, &outputSchema, &callbackErr); got != ^uintptr(0) {
				t.Fatalf("callback status = %d, want -1", got)
			}
			if output.Release != 0 || outputSchema.Release != 0 {
				t.Fatalf("failure left owned slots: array release=%d schema release=%d", output.Release, outputSchema.Release)
			}
			if got := releases.Load(); got != 2 {
				t.Fatalf("exported array releases = %d, want 2 (store reference plus exported array)", got)
			}
			if callbackErr.Code == 0 || callbackErr.Message.Data == nil {
				t.Fatalf("callback error = %+v, want code and message", callbackErr)
			}
			if message := goString(callbackErr.Message.Data); !strings.Contains(message, tc.wantMessage) {
				t.Fatalf("callback message = %q, want substring %q", message, tc.wantMessage)
			}
		})
	}
}
