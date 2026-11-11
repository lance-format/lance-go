package lance_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	lance "github.com/lance-format/lance-go"
)

type blobReadRow struct {
	rowAddress uint64
	data       []byte
	null       bool
}

func TestReadBlobsSelectorsAndSchema(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)

	reader, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelectorIDs,
		[]uint64{refs[3].RowID, refs[0].RowID, refs[1].RowID}, nil)
	if err != nil {
		t.Fatalf("ReadBlobs: %v", err)
	}
	schema := reader.Schema()
	if schema.NumFields() != 2 {
		t.Fatalf("schema has %d fields, want 2", schema.NumFields())
	}
	if f := schema.Field(0); f.Name != "row_address" || f.Type.ID() != arrow.UINT64 || f.Nullable {
		t.Fatalf("row_address field = %v", f)
	}
	if f := schema.Field(1); f.Name != "data" || f.Type.ID() != arrow.LARGE_BINARY || !f.Nullable {
		t.Fatalf("data field = %v", f)
	}

	var got []blobReadRow
	for reader.Next() {
		batch := reader.RecordBatch()
		addresses := batch.Column(0).(*array.Uint64)
		data := batch.Column(1).(*array.LargeBinary)
		for i := 0; i < int(batch.NumRows()); i++ {
			row := blobReadRow{rowAddress: addresses.Value(i), null: data.IsNull(i)}
			if !row.null {
				row.data = append([]byte(nil), data.Value(i)...)
			}
			got = append(got, row)
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("ReadBlobs stream: %v", err)
	}
	reader.Release()

	want := []blobReadRow{
		{rowAddress: refs[3].Addr, data: []byte{0, 1, 2, 3, 4}},
		{rowAddress: refs[0].Addr, data: []byte("alpha")},
		{rowAddress: refs[1].Addr, null: true},
	}
	if len(got) != len(want) {
		t.Fatalf("ReadBlobs returned %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].rowAddress != want[i].rowAddress || got[i].null != want[i].null || !bytes.Equal(got[i].data, want[i].data) {
			t.Fatalf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	checkSelector := func(selector lance.BlobRowSelector, rows []uint64, wantRows []blobReadRow) {
		t.Helper()
		reader, err := dataset.ReadBlobs(context.Background(), "blob", selector, rows, nil)
		if err != nil {
			t.Fatalf("ReadBlobs selector %d: %v", selector, err)
		}
		var collected []blobReadRow
		for reader.Next() {
			batch := reader.RecordBatch()
			addresses := batch.Column(0).(*array.Uint64)
			data := batch.Column(1).(*array.LargeBinary)
			for i := 0; i < int(batch.NumRows()); i++ {
				row := blobReadRow{rowAddress: addresses.Value(i), null: data.IsNull(i)}
				if !row.null {
					row.data = append([]byte(nil), data.Value(i)...)
				}
				collected = append(collected, row)
			}
		}
		if err := reader.Err(); err != nil {
			t.Fatalf("selector %d stream: %v", selector, err)
		}
		reader.Release()
		if len(collected) != len(wantRows) {
			t.Fatalf("selector %d returned %d rows, want %d", selector, len(collected), len(wantRows))
		}
		for i := range wantRows {
			if collected[i].rowAddress != wantRows[i].rowAddress || collected[i].null != wantRows[i].null || !bytes.Equal(collected[i].data, wantRows[i].data) {
				t.Fatalf("selector %d row %d = %+v, want %+v", selector, i, collected[i], wantRows[i])
			}
		}
	}

	checkSelector(lance.BlobRowSelectorAddresses,
		[]uint64{refs[4].Addr, refs[0].Addr},
		[]blobReadRow{{rowAddress: refs[4].Addr, data: []byte("omega")}, {rowAddress: refs[0].Addr, data: []byte("alpha")}})
	checkSelector(lance.BlobRowSelectorIndices,
		[]uint64{4, 2},
		[]blobReadRow{{rowAddress: refs[4].Addr, data: []byte("omega")}, {rowAddress: refs[2].Addr}})

	empty, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelectorIDs, nil, nil)
	if err != nil {
		t.Fatalf("empty ReadBlobs: %v", err)
	}
	if empty.Next() {
		t.Fatal("empty ReadBlobs returned a batch")
	}
	if err := empty.Err(); err != nil {
		t.Fatalf("empty ReadBlobs stream: %v", err)
	}
	empty.Release()
}

func TestReadBlobsNullAndEmpty(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)

	reader, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelectorIDs,
		[]uint64{refs[1].RowID, refs[2].RowID}, nil)
	if err != nil {
		t.Fatalf("ReadBlobs: %v", err)
	}
	var nulls, lengths []int
	var valid []bool
	for reader.Next() {
		batch := reader.RecordBatch()
		data := batch.Column(1).(*array.LargeBinary)
		for i := 0; i < int(batch.NumRows()); i++ {
			valid = append(valid, data.IsValid(i))
			lengths = append(lengths, len(data.Value(i)))
			nulls = append(nulls, boolToInt(data.IsNull(i)))
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("stream: %v", err)
	}
	reader.Release()
	if len(valid) != 2 {
		t.Fatalf("got %d rows, want 2", len(valid))
	}
	if valid[0] {
		t.Fatal("null blob row is valid")
	}
	if !valid[1] || lengths[1] != 0 {
		t.Fatalf("empty blob valid=%v len=%d, want valid empty", valid[1], lengths[1])
	}
	if nulls[1] != 0 {
		t.Fatal("empty blob is null")
	}
}

func TestReadBlobsStreamOwnership(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)

	reader, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelectorIDs, []uint64{refs[3].RowID}, nil)
	if err != nil {
		t.Fatalf("ReadBlobs: %v", err)
	}
	if !reader.Next() {
		t.Fatalf("no batch: %v", reader.Err())
	}
	batch := reader.RecordBatch()
	batch.Retain()
	reader.Release()
	if err := dataset.Close(); err != nil {
		t.Fatalf("Close dataset: %v", err)
	}
	if reader.Next() {
		t.Fatal("released reader returned a batch")
	}

	data := batch.Column(1).(*array.LargeBinary)
	if got := data.Value(0); !bytes.Equal(got, []byte{0, 1, 2, 3, 4}) {
		t.Fatalf("retained batch data = %v", got)
	}
	batch.Release()
}

func TestReadBlobsCancellationAndErrors(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)

	ctx, cancel := context.WithCancel(context.Background())
	reader, err := dataset.ReadBlobs(ctx, "blob", lance.BlobRowSelectorIDs, []uint64{refs[0].RowID}, nil)
	if err != nil {
		t.Fatalf("ReadBlobs: %v", err)
	}
	if !reader.Next() {
		t.Fatalf("no batch: %v", reader.Err())
	}
	cancel()
	if reader.Next() {
		t.Fatal("canceled reader returned another batch")
	}
	if !errors.Is(reader.Err(), context.Canceled) {
		t.Fatalf("canceled reader error = %v, want context.Canceled", reader.Err())
	}
	reader.Release()

	canceled, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, err := dataset.ReadBlobs(canceled, "blob", lance.BlobRowSelectorIDs, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled error = %v, want context.Canceled", err)
	}

	if _, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelectorNone, nil, nil); err == nil {
		t.Fatal("selector None succeeded")
	}
	if _, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelectorNone, []uint64{0}, nil); err == nil {
		t.Fatal("selector None with rows succeeded")
	}
	if _, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelector(99), []uint64{0}, nil); err == nil {
		t.Fatal("invalid selector succeeded")
	}
	if _, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelectorIDs, []uint64{0}, &lance.BlobReadOptions{IOBufferSize: ptr(uint64(0))}); err == nil {
		t.Fatal("explicit zero IOBufferSize succeeded")
	}
	if _, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelectorIDs, []uint64{0}, &lance.BlobReadOptions{Order: lance.BlobReadOrder(7)}); err == nil {
		t.Fatal("invalid order succeeded")
	}
	// The pinned native ABI maps FieldNotFound to Internal. Preserve its code
	// and verify the diagnostic identifies the requested missing column.
	if _, err := dataset.ReadBlobs(context.Background(), "missing", lance.BlobRowSelectorIDs, []uint64{0}, nil); !errors.Is(err, lance.ErrInternal) || !strings.Contains(err.Error(), "Field 'missing' not found") {
		t.Fatalf("missing column error = %v, want native Internal with field diagnostic", err)
	}

	if err := dataset.Close(); err != nil {
		t.Fatalf("Close dataset: %v", err)
	}
	if _, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelectorIDs, []uint64{0}, nil); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("closed dataset error = %v, want ErrClosed", err)
	}
}

func TestReadBlobRanges(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)

	requests := []lance.BlobRangeRequest{
		{Row: refs[0].Addr, Offset: 1, Length: 3},
		{Row: refs[0].Addr, Offset: 0, Length: 5},
		{Row: refs[1].Addr, Offset: 0, Length: 10},
		{Row: refs[2].Addr, Offset: 0, Length: 0},
		{Row: refs[3].Addr, Offset: 1, Length: 2},
	}
	reader, err := dataset.ReadBlobRanges(context.Background(), "blob", lance.BlobRowSelectorAddresses, requests, nil)
	if err != nil {
		t.Fatalf("ReadBlobRanges: %v", err)
	}
	schema := reader.Schema()
	wantFields := []struct {
		name     string
		id       arrow.Type
		nullable bool
	}{
		{"request_index", arrow.UINT64, false},
		{"row_address", arrow.UINT64, false},
		{"offset", arrow.UINT64, false},
		{"length", arrow.UINT64, false},
		{"data", arrow.LARGE_BINARY, true},
	}
	if schema.NumFields() != len(wantFields) {
		t.Fatalf("range schema has %d fields, want %d", schema.NumFields(), len(wantFields))
	}
	for i, want := range wantFields {
		field := schema.Field(i)
		if field.Name != want.name || field.Type.ID() != want.id || field.Nullable != want.nullable {
			t.Fatalf("range field %d = %v, want %+v", i, field, want)
		}
	}

	var indices, addresses, offsets, lengths []uint64
	var datas [][]byte
	var dataNulls []bool
	for reader.Next() {
		batch := reader.RecordBatch()
		for i := 0; i < int(batch.NumRows()); i++ {
			indices = append(indices, batch.Column(0).(*array.Uint64).Value(i))
			addresses = append(addresses, batch.Column(1).(*array.Uint64).Value(i))
			offsets = append(offsets, batch.Column(2).(*array.Uint64).Value(i))
			lengths = append(lengths, batch.Column(3).(*array.Uint64).Value(i))
			data := batch.Column(4).(*array.LargeBinary)
			dataNulls = append(dataNulls, data.IsNull(i))
			if data.IsNull(i) {
				datas = append(datas, nil)
			} else {
				datas = append(datas, append([]byte(nil), data.Value(i)...))
			}
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("range stream: %v", err)
	}
	reader.Release()

	want := []struct {
		address uint64
		offset  uint64
		length  uint64
		data    []byte
		null    bool
	}{
		{refs[0].Addr, 1, 3, []byte("lph"), false},
		{refs[0].Addr, 0, 5, []byte("alpha"), false},
		{refs[1].Addr, 0, 10, nil, true},
		{refs[2].Addr, 0, 0, []byte{}, false},
		{refs[3].Addr, 1, 2, []byte{1, 2}, false},
	}
	if len(indices) != len(want) {
		t.Fatalf("range rows = %d, want %d", len(indices), len(want))
	}
	for i, expected := range want {
		if indices[i] != uint64(i) || addresses[i] != expected.address || offsets[i] != expected.offset || lengths[i] != expected.length || dataNulls[i] != expected.null || !bytes.Equal(datas[i], expected.data) {
			t.Fatalf("range row %d = (%d,%d,%d,%d,%v,%v), want %+v", i, indices[i], addresses[i], offsets[i], lengths[i], dataNulls[i], datas[i], expected)
		}
	}

	empty, err := dataset.ReadBlobRanges(context.Background(), "blob", lance.BlobRowSelectorIndices, nil, nil)
	if err != nil {
		t.Fatalf("empty ReadBlobRanges: %v", err)
	}
	if empty.Next() {
		t.Fatal("empty ReadBlobRanges returned a batch")
	}
	empty.Release()

	if _, err := dataset.ReadBlobRanges(context.Background(), "blob", lance.BlobRowSelectorAddresses,
		[]lance.BlobRangeRequest{{Row: refs[0].Addr, Offset: 4, Length: 2}}, nil); err == nil {
		t.Fatal("out-of-bounds range succeeded")
	}
	if _, err := dataset.ReadBlobRanges(context.Background(), "blob", lance.BlobRowSelectorAddresses,
		[]lance.BlobRangeRequest{{Row: refs[0].Addr, Offset: ^uint64(0), Length: 2}}, nil); err == nil {
		t.Fatal("overflowing range succeeded")
	}
	if _, err := dataset.ReadBlobRanges(context.Background(), "blob", lance.BlobRowSelector(99), nil, nil); err == nil {
		t.Fatal("invalid range selector succeeded")
	}
	if _, err := dataset.ReadBlobRanges(context.Background(), "blob", lance.BlobRowSelectorNone, nil, nil); err == nil {
		t.Fatal("range selector None succeeded")
	}
	if _, err := dataset.ReadBlobRanges(context.Background(), "blob", lance.BlobRowSelectorIndices,
		[]lance.BlobRangeRequest{{Row: 0, Offset: 0, Length: 1}}, &lance.BlobReadOptions{IOBufferSize: ptr(uint64(0))}); err == nil {
		t.Fatal("range explicit zero IOBufferSize succeeded")
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestReadBlobsReportsPayloadFailureAfterPlanning(t *testing.T) {
	dataset, uri := newPackedBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)

	reader, err := dataset.ReadBlobs(context.Background(), "blob", lance.BlobRowSelectorIDs,
		[]uint64{refs[0].RowID, refs[3].RowID}, nil)
	if err != nil {
		t.Fatalf("ReadBlobs: %v", err)
	}
	defer reader.Release()

	if err := os.RemoveAll(filepath.Join(uri, "data")); err != nil {
		t.Fatalf("remove payload files: %v", err)
	}
	for reader.Next() {
	}
	if err := reader.Err(); err == nil {
		t.Fatal("planned read succeeded after payload files were removed")
	}
}
