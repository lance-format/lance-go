package file_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/file"
	"github.com/lance-format/lance-go/internal/loader"
)

func requireNative(t *testing.T) {
	t.Helper()
	if _, err := loader.Load(); err != nil {
		if errors.Is(err, loader.ErrNotFound) {
			t.Skipf("lance-c is not available: %v", err)
		}
		t.Fatalf("load lance-c: %v", err)
	}
}

func testSchema() *arrow.Schema {
	metadata := arrow.NewMetadata([]string{"owner"}, []string{"lance"})
	return arrow.NewSchema([]arrow.Field{
		{
			Name:     "id",
			Type:     arrow.PrimitiveTypes.Int64,
			Nullable: true,
			Metadata: arrow.NewMetadata([]string{"role"}, []string{"primary"}),
		},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "tags", Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: true},
		{
			Name: "info",
			Type: arrow.StructOf(
				arrow.Field{Name: "score", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
				arrow.Field{Name: "active", Type: arrow.FixedWidthTypes.Boolean, Nullable: true},
			),
			Nullable: true,
		},
	}, &metadata)
}

// testBatch builds four rows with nulls, a nested list, and a struct.
func testBatch(t *testing.T, schema *arrow.Schema) arrow.RecordBatch {
	t.Helper()
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()

	ids := builder.Field(0).(*array.Int64Builder)
	ids.AppendValues([]int64{1, 2, 3, 4}, []bool{true, false, true, true})

	names := builder.Field(1).(*array.StringBuilder)
	names.AppendValues([]string{"a", "", "ccc", "dddd"}, []bool{true, false, true, true})

	tags := builder.Field(2).(*array.ListBuilder)
	tagValues := tags.ValueBuilder().(*array.StringBuilder)
	tags.Append(true)
	tagValues.Append("x")
	tagValues.Append("y")
	tags.Append(false)
	tags.Append(true)
	tags.Append(true)
	tagValues.Append("z")

	info := builder.Field(3).(*array.StructBuilder)
	score := info.FieldBuilder(0).(*array.Float64Builder)
	active := info.FieldBuilder(1).(*array.BooleanBuilder)
	for i := 0; i < 4; i++ {
		info.Append(true)
		score.Append(1.5 + float64(i))
		active.Append(i%2 == 0)
	}

	return builder.NewRecordBatch()
}

func drain(t *testing.T, rr array.RecordReader, fn func(arrow.RecordBatch)) {
	t.Helper()
	defer rr.Release()
	for rr.Next() {
		fn(rr.RecordBatch())
	}
	if err := rr.Err(); err != nil {
		t.Fatalf("read batches: %v", err)
	}
}

func collectIDs(t *testing.T, rr array.RecordReader) []int64 {
	t.Helper()
	var ids []int64
	drain(t, rr, func(batch arrow.RecordBatch) {
		column := batch.Column(0).(*array.Int64)
		for i := 0; i < column.Len(); i++ {
			if column.IsNull(i) {
				ids = append(ids, -1)
			} else {
				ids = append(ids, column.Value(i))
			}
		}
	})
	return ids
}

func TestRoundTrip(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()

	writer, err := file.Create(ctx, path, schema, nil)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	batch := testBatch(t, schema)
	defer batch.Release()
	if err := writer.Write(ctx, batch); err != nil {
		t.Fatalf("write batch: %v", err)
	}
	if err := writer.AddSchemaMetadata(ctx, "stage", "roundtrip"); err != nil {
		t.Fatalf("add schema metadata: %v", err)
	}
	if index, err := writer.AddGlobalBuffer(ctx, []byte("hello")); err != nil || index != 1 {
		t.Fatalf("add global buffer = %d, %v; want 1, nil", index, err)
	}
	if index, err := writer.AddGlobalBuffer(ctx, []byte("world")); err != nil || index != 2 {
		t.Fatalf("add second global buffer = %d, %v; want 2, nil", index, err)
	}
	summary, err := writer.Finish(ctx)
	if err != nil {
		t.Fatalf("finish writer: %v", err)
	}
	if summary.NumRows != 4 || summary.SizeBytes == 0 {
		t.Fatalf("write summary = %+v, want 4 rows and non-zero size", summary)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("second close writer: %v", err)
	}

	reader, err := file.Open(ctx, path, nil)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	rows, err := reader.NumRows()
	if err != nil || rows != 4 {
		t.Fatalf("num rows = %d, %v; want 4, nil", rows, err)
	}

	metadata, err := reader.Metadata()
	if err != nil {
		t.Fatalf("reader metadata: %v", err)
	}
	defer metadata.Close()
	info, err := metadata.Info()
	if err != nil {
		t.Fatalf("metadata info: %v", err)
	}
	if info.NumRows != 4 || info.NumColumns != 4 || info.NumGlobalBuffers < 2 {
		t.Fatalf("metadata info = %+v, want 4 rows, 4 columns, at least 2 global buffers", info)
	}

	gotSchema, err := reader.Schema()
	if err != nil {
		t.Fatalf("reader schema: %v", err)
	}
	owner, ownerOK := gotSchema.Metadata().GetValue("owner")
	stage, stageOK := gotSchema.Metadata().GetValue("stage")
	if !ownerOK || !stageOK || owner != "lance" || stage != "roundtrip" {
		t.Fatalf("schema metadata owner = %q, stage = %q", owner, stage)
	}
	role, roleOK := gotSchema.Field(0).Metadata.GetValue("role")
	if !roleOK || role != "primary" {
		t.Fatalf("field metadata = %q", role)
	}

	schemaBuffer, err := reader.ReadGlobalBuffer(ctx, 0)
	if err != nil || len(schemaBuffer) == 0 {
		t.Fatalf("read serialized schema buffer = %d bytes, %v", len(schemaBuffer), err)
	}
	global, err := reader.ReadGlobalBuffer(ctx, 1)
	if err != nil || string(global) != "hello" {
		t.Fatalf("read global buffer = %q, %v; want hello", global, err)
	}
	if _, err := reader.ReadGlobalBuffer(ctx, 3); err == nil {
		t.Fatal("reading missing global buffer succeeded")
	}

	columns, err := metadata.Columns()
	if err != nil {
		t.Fatalf("metadata columns: %v", err)
	}
	if len(columns) != int(info.NumPhysicalColumns) || len(columns) == 0 || len(columns[0].Pages) == 0 {
		t.Fatalf("metadata columns = %d, physical = %d", len(columns), info.NumPhysicalColumns)
	}
	statistics, err := reader.Statistics()
	if err != nil {
		t.Fatalf("reader statistics: %v", err)
	}
	statColumns, err := statistics.Columns()
	if err != nil {
		t.Fatalf("statistics columns: %v", err)
	}
	if len(statColumns) == 0 {
		t.Fatal("statistics returned no columns")
	}
	if err := statistics.Close(); err != nil {
		t.Fatalf("close statistics: %v", err)
	}

	var batches int
	drain(t, mustReadAll(t, reader), func(batch arrow.RecordBatch) {
		batches++
		id := batch.Column(0).(*array.Int64)
		if id.Len() != 4 || id.Value(0) != 1 || !id.IsNull(1) || id.Value(3) != 4 {
			t.Fatalf("id column values are unexpected: %v", id)
		}
		tags := batch.Column(2).(*array.List)
		if !tags.IsNull(1) {
			t.Fatal("row 1 tags should be null")
		}
		values := tags.ListValues().(*array.String)
		offsets := tags.Offsets()
		if got := values.Value(int(offsets[0])); got != "x" {
			t.Fatalf("first tag = %q, want x", got)
		}
		info := batch.Column(3).(*array.Struct)
		if got := info.Field(0).(*array.Float64).Value(2); got != 3.5 {
			t.Fatalf("row 2 score = %v, want 3.5", got)
		}
	})
	if batches == 0 {
		t.Fatal("read all produced no batches")
	}

	if got := collectIDs(t, mustReadRange(t, reader, 1, 2)); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Fatalf("range ids = %v, want [2 3]", got)
	}
	if got := collectIDs(t, mustReadIndices(t, reader, []uint32{3, 0})); !reflect.DeepEqual(got, []int64{4, 1}) {
		t.Fatalf("index ids = %v, want [4 1]", got)
	}

	stats, err := reader.IOStats()
	if err != nil {
		t.Fatalf("reader io stats: %v", err)
	}
	if stats.BytesRead == 0 {
		t.Fatalf("reader io stats = %+v, want non-zero bytes", stats)
	}

	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("second close reader: %v", err)
	}
	if _, err := reader.NumRows(); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("num rows after close = %v, want ErrClosed", err)
	}
	// Metadata snapshots are independent of the reader handle.
	if _, err := metadata.Info(); err != nil {
		t.Fatalf("metadata info after reader close: %v", err)
	}
	if _, err := metadata.Schema(); err != nil {
		t.Fatalf("metadata schema after reader close: %v", err)
	}
}

func mustReadAll(t *testing.T, reader *file.Reader) array.RecordReader {
	t.Helper()
	rr, err := reader.ReadAll(context.Background(), file.ReadOptions{})
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	return rr
}

func mustReadRange(t *testing.T, reader *file.Reader, offset, rows uint64) array.RecordReader {
	t.Helper()
	rr, err := reader.ReadRange(context.Background(), offset, rows, file.ReadOptions{})
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	return rr
}

func mustReadIndices(t *testing.T, reader *file.Reader, indices []uint32) array.RecordReader {
	t.Helper()
	rr, err := reader.ReadIndices(context.Background(), indices, file.ReadOptions{})
	if err != nil {
		t.Fatalf("read indices: %v", err)
	}
	return rr
}

func TestSlicedBatch(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sliced.lance")
	schema := testSchema()

	base := testBatch(t, schema)
	sliced := base.NewSlice(1, 3)
	defer sliced.Release()
	defer base.Release()

	writer, err := file.Create(ctx, path, schema, nil)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	if err := writer.Write(ctx, sliced); err != nil {
		t.Fatalf("write sliced batch: %v", err)
	}
	if _, err := writer.Finish(ctx); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	reader, err := file.Open(ctx, path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()
	if got := collectIDs(t, mustReadAll(t, reader)); !reflect.DeepEqual(got, []int64{-1, 3, 4}) {
		t.Fatalf("sliced ids = %v, want [-1 3 4]", got)
	}
}

func TestStaticStorageOptions(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "storage.lance")
	schema := testSchema()

	storage, err := file.NewStorageOptions(nil)
	if err != nil {
		t.Fatalf("new storage options: %v", err)
	}
	keep := false
	validate := false
	writer, err := file.Create(ctx, path, schema, &file.WriterOptions{
		StorageOptions:    storage,
		KeepOriginalArray: &keep,
	})
	if err != nil {
		t.Fatalf("create with storage options: %v", err)
	}
	batch := testBatch(t, schema)
	defer batch.Release()
	if err := writer.Write(ctx, batch); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := writer.Finish(ctx); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	reader, err := file.Open(ctx, path, &file.ReaderOptions{
		StorageOptions:   storage,
		ValidateOnDecode: &validate,
	})
	if err != nil {
		t.Fatalf("open with storage options: %v", err)
	}
	// Native handles retain the resolved store, so the accessor may be closed.
	if err := storage.Close(); err != nil {
		t.Fatalf("close storage options: %v", err)
	}
	if got := collectIDs(t, mustReadAll(t, reader)); !reflect.DeepEqual(got, []int64{1, -1, 3, 4}) {
		t.Fatalf("ids with static storage options = %v", got)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
}

func TestEmptyFile(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "empty.lance")
	schema := testSchema()

	writer, err := file.Create(ctx, path, schema, nil)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	summary, err := writer.Finish(ctx)
	if err != nil {
		t.Fatalf("finish empty file: %v", err)
	}
	if summary.NumRows != 0 {
		t.Fatalf("empty summary rows = %d, want 0", summary.NumRows)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	reader, err := file.Open(ctx, path, nil)
	if err != nil {
		t.Fatalf("open empty file: %v", err)
	}
	defer reader.Close()
	if rows, err := reader.NumRows(); err != nil || rows != 0 {
		t.Fatalf("empty rows = %d, %v; want 0, nil", rows, err)
	}
	count := 0
	drain(t, mustReadAll(t, reader), func(batch arrow.RecordBatch) { count += int(batch.NumRows()) })
	if count != 0 {
		t.Fatalf("read empty file rows = %d, want 0", count)
	}
}

func TestAbortUnfinishedFile(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "aborted.lance")
	schema := testSchema()

	writer, err := file.Create(ctx, path, schema, nil)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	batch := testBatch(t, schema)
	defer batch.Release()
	if err := writer.Write(ctx, batch); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Abort(ctx); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	if reader, err := file.Open(ctx, path, nil); err == nil {
		reader.Close()
		t.Fatal("opened a file that was never finished")
	}
}

func TestMalformedIndicesAndClosedReader(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	path := writeTestFile(t, ctx, "malformed.lance")

	reader, err := file.Open(ctx, path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	rr, err := reader.ReadIndices(ctx, []uint32{99}, file.ReadOptions{})
	if err == nil {
		for rr.Next() {
		}
		err = rr.Err()
		rr.Release()
	}
	if err == nil {
		t.Fatal("reading an out-of-range index succeeded")
	}

	if err := reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := reader.ReadAll(ctx, file.ReadOptions{}); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("read all after close = %v, want ErrClosed", err)
	}
	if _, err := reader.Metadata(); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("metadata after close = %v, want ErrClosed", err)
	}
}

func writeTestFile(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	schema := testSchema()
	writer, err := file.Create(ctx, path, schema, nil)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	batch := testBatch(t, schema)
	defer batch.Release()
	if err := writer.Write(ctx, batch); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := writer.Finish(ctx); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return path
}

func TestSessionLocalIO(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	base := t.TempDir()
	schema := testSchema()

	session, err := file.OpenSession(ctx, base, nil)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer session.Close()

	writer, err := session.OpenWriter(ctx, "data.lance", schema, nil)
	if err != nil {
		t.Fatalf("session open writer: %v", err)
	}
	batch := testBatch(t, schema)
	defer batch.Release()
	if err := writer.Write(ctx, batch); err != nil {
		t.Fatalf("session write: %v", err)
	}
	if _, err := writer.Finish(ctx); err != nil {
		t.Fatalf("session finish: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("session close writer: %v", err)
	}

	exists, err := session.Contains(ctx, "data.lance")
	if err != nil || !exists {
		t.Fatalf("contains data.lance = %v, %v; want true", exists, err)
	}
	exists, err = session.Contains(ctx, "missing.lance")
	if err != nil || exists {
		t.Fatalf("contains missing.lance = %v, %v; want false", exists, err)
	}

	paths, err := session.List(ctx, "")
	if err != nil {
		t.Fatalf("session list: %v", err)
	}
	if !contains(paths, "data.lance") {
		t.Fatalf("session list = %v, want data.lance", paths)
	}
	delimited, err := session.ListWithDelimiter(ctx, "")
	if err != nil {
		t.Fatalf("session list with delimiter: %v", err)
	}
	if !contains(delimited.Objects, "data.lance") {
		t.Fatalf("session delimited objects = %v, want data.lance", delimited.Objects)
	}

	raw, err := os.ReadFile(filepath.Join(base, "data.lance"))
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	if len(raw) < 16 {
		t.Fatalf("local file is only %d bytes", len(raw))
	}
	prefix, err := session.ReadRange(ctx, "data.lance", 0, 16)
	if err != nil {
		t.Fatalf("session read range: %v", err)
	}
	if !bytes.Equal(prefix, raw[:16]) {
		t.Fatal("session read range does not match file prefix")
	}

	download := filepath.Join(t.TempDir(), "download.lance")
	if err := session.DownloadFile(ctx, "data.lance", download); err != nil {
		t.Fatalf("session download: %v", err)
	}
	downloaded, err := os.ReadFile(download)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if !bytes.Equal(downloaded, raw) {
		t.Fatal("downloaded file differs from source")
	}

	uploadSource := filepath.Join(t.TempDir(), "upload.lance")
	if err := os.WriteFile(uploadSource, []byte("uploaded payload"), 0o644); err != nil {
		t.Fatalf("write upload source: %v", err)
	}
	if err := session.UploadFile(ctx, uploadSource, "uploaded.lance"); err != nil {
		t.Fatalf("session upload: %v", err)
	}
	uploaded, err := session.ReadRange(ctx, "uploaded.lance", 0, 16)
	if err != nil || string(uploaded) != "uploaded payload" {
		t.Fatalf("uploaded contents = %q, %v", uploaded, err)
	}
	if err := session.DeleteFile(ctx, "uploaded.lance"); err != nil {
		t.Fatalf("session delete: %v", err)
	}
	if exists, err := session.Contains(ctx, "uploaded.lance"); err != nil || exists {
		t.Fatalf("contains deleted file = %v, %v; want false", exists, err)
	}
	if err := session.DeleteFile(ctx, "uploaded.lance"); err == nil {
		t.Fatal("second delete succeeded")
	}

	if _, err := session.SchedulerStats(); err != nil {
		t.Fatalf("scheduler stats: %v", err)
	}
	if _, err := session.RegistryStats(); err != nil {
		t.Fatalf("registry stats: %v", err)
	}
	if _, err := session.StoreInfo(); err != nil {
		t.Fatalf("store info: %v", err)
	}
	if _, err := session.StoreIOStats(); err != nil {
		t.Fatalf("store io stats: %v", err)
	}
	if got, err := session.BasePath(); err != nil || got == "" {
		t.Fatalf("session base path = %q, %v", got, err)
	}

	if err := session.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second close session: %v", err)
	}
	if _, err := session.Contains(ctx, "data.lance"); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("contains after close = %v, want ErrClosed", err)
	}
}

func TestRetainedStreamAfterSessionClose(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	base := t.TempDir()
	schema := testSchema()

	session, err := file.OpenSession(ctx, base, nil)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	writer, err := session.OpenWriter(ctx, "data.lance", schema, nil)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	batch := testBatch(t, schema)
	defer batch.Release()
	if err := writer.Write(ctx, batch); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := writer.Finish(ctx); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	reader, err := session.OpenReader(ctx, "data.lance", nil)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	rr, err := reader.ReadAll(ctx, file.ReadOptions{})
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}

	if got := collectIDs(t, rr); !reflect.DeepEqual(got, []int64{1, -1, 3, 4}) {
		t.Fatalf("retained stream ids = %v", got)
	}
}

func TestBlobWriters(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	session, err := file.OpenSession(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	defer session.Close()

	packed, err := session.OpenPackedBlobWriter(ctx, "packed.lance", 1)
	if err != nil {
		t.Fatalf("open packed blob writer: %v", err)
	}
	if packed.BlobID() != 1 {
		t.Fatalf("packed blob id = %d, want 1", packed.BlobID())
	}
	if path, err := packed.Path(); err != nil || path == "" {
		t.Fatalf("packed path = %q, %v", path, err)
	}
	for _, payload := range [][]byte{[]byte("alpha"), []byte("beta")} {
		if err := packed.Write(ctx, payload); err != nil {
			t.Fatalf("packed write: %v", err)
		}
	}
	field, arr, err := packed.FinishArray(ctx, "packed_blobs")
	if err != nil {
		t.Fatalf("packed finish array: %v", err)
	}
	if field.Name != "packed_blobs" || arr.Len() != 2 {
		t.Fatalf("packed descriptor field = %q, len = %d; want packed_blobs, 2", field.Name, arr.Len())
	}
	arr.Release()
	if err := packed.Close(); err != nil {
		t.Fatalf("close packed writer: %v", err)
	}
	if err := packed.Close(); err != nil {
		t.Fatalf("second close packed writer: %v", err)
	}

	dedicated, err := session.OpenDedicatedBlobWriter(ctx, "dedicated.lance", 2)
	if err != nil {
		t.Fatalf("open dedicated blob writer: %v", err)
	}
	if dedicated.BlobID() != 2 {
		t.Fatalf("dedicated blob id = %d, want 2", dedicated.BlobID())
	}
	for _, payload := range [][]byte{[]byte("hello"), []byte("world")} {
		if err := dedicated.Write(ctx, payload); err != nil {
			t.Fatalf("dedicated write: %v", err)
		}
	}
	field, arr, err = dedicated.FinishArray(ctx, "dedicated_blob")
	if err != nil {
		t.Fatalf("dedicated finish array: %v", err)
	}
	if field.Name != "dedicated_blob" || arr.Len() != 1 {
		t.Fatalf("dedicated descriptor field = %q, len = %d; want dedicated_blob, 1", field.Name, arr.Len())
	}
	arr.Release()
	if err := dedicated.Close(); err != nil {
		t.Fatalf("close dedicated writer: %v", err)
	}
}

func contains(values []string, want string) bool {
	values = append([]string(nil), values...)
	sort.Strings(values)
	index := sort.SearchStrings(values, want)
	return index < len(values) && values[index] == want
}
