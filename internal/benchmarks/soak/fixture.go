package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
	lancefile "github.com/lance-format/lance-go/file"
)

const (
	// fixtureBatchRows bounds the memory used while creating and verifying the
	// fixture. The fixture itself stays bounded and is never appended to.
	fixtureBatchRows = 1024

	blobInlineThresholdBytes = 1 << 20
	blobNullEvery            = 4
)

type fixture struct {
	schema       *arrow.Schema
	mainURI      string
	blobURI      string
	rawURI       string
	writeDir     string
	rows         int
	payloadBytes int
	writeRows    int

	expectedHash uint64
	writeHash    uint64

	blobRows    int
	blobIndices []uint64
	blobValues  [][]byte

	writeBatch arrow.RecordBatch
}

type fixtureInfo struct {
	Dir                      string `json:"dir"`
	MainURI                  string `json:"main_dataset_uri"`
	BlobURI                  string `json:"blob_dataset_uri"`
	RawURI                   string `json:"raw_file_uri"`
	Rows                     int    `json:"rows"`
	PayloadBytes             int    `json:"payload_bytes"`
	ApproxWorkingSetBytes    int64  `json:"approx_working_set_bytes"`
	WriteRows                int    `json:"write_rows"`
	BlobRows                 int    `json:"blob_rows"`
	BatchSize                int    `json:"scan_batch_size"`
	BlobNullEvery            int    `json:"blob_null_every"`
	BlobInlineThresholdBytes int64  `json:"blob_inline_threshold_bytes"`
}

func fixturePlan(dir string, cfg config) fixtureInfo {
	writeRows := min(cfg.Rows, fixtureBatchRows)
	blobRows := min(cfg.Rows, 16)
	// Fixture creation holds all payload batches once before writing them. The
	// retained fixture afterwards is the write batch plus the small blob values.
	approx := int64(cfg.Rows) * int64(cfg.PayloadBytes)
	approx += int64(writeRows) * int64(24+cfg.PayloadBytes)
	approx += int64(blobRows) * int64(24+max(1, cfg.PayloadBytes))
	return fixtureInfo{
		Dir:                      dir,
		MainURI:                  filepath.Join(dir, "main.lance"),
		BlobURI:                  filepath.Join(dir, "blob.lance"),
		RawURI:                   filepath.Join(dir, "raw.bin"),
		Rows:                     cfg.Rows,
		PayloadBytes:             cfg.PayloadBytes,
		ApproxWorkingSetBytes:    approx,
		WriteRows:                writeRows,
		BlobRows:                 blobRows,
		BatchSize:                cfg.BatchSize,
		BlobNullEvery:            blobNullEvery,
		BlobInlineThresholdBytes: blobInlineThresholdBytes,
	}
}

func newFixture(ctx context.Context, dir string, cfg config) (*fixture, error) {
	info := fixturePlan(dir, cfg)
	fx := &fixture{
		schema: arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
			{Name: "name", Type: arrow.BinaryTypes.String, Nullable: false},
			{Name: "payload", Type: arrow.BinaryTypes.Binary, Nullable: false},
		}, nil),
		mainURI:      info.MainURI,
		blobURI:      info.BlobURI,
		rawURI:       info.RawURI,
		writeDir:     filepath.Join(dir, "write"),
		rows:         info.Rows,
		payloadBytes: info.PayloadBytes,
		writeRows:    info.WriteRows,
		blobRows:     info.BlobRows,
	}
	if err := os.MkdirAll(fx.writeDir, 0o755); err != nil {
		return nil, fmt.Errorf("create write workload directory: %w", err)
	}

	for i := 0; i < fx.rows; i++ {
		name := expectedName(int64(i))
		payload := makePayload(i, fx.payloadBytes)
		hash := addRowHash(0, int64(i), name, payload)
		fx.expectedHash += hash
		if i < fx.writeRows {
			fx.writeHash += hash
		}
	}

	if err := fx.writeMainDataset(ctx); err != nil {
		return nil, err
	}
	if err := fx.writeRawFile(ctx); err != nil {
		return nil, err
	}
	if err := fx.writeBlobDataset(ctx); err != nil {
		return nil, err
	}
	batch, err := fx.buildMainBatch(0, fx.writeRows)
	if err != nil {
		return nil, err
	}
	fx.writeBatch = batch
	return fx, nil
}

func (fx *fixture) cleanup() {
	if fx.writeBatch != nil {
		fx.writeBatch.Release()
		fx.writeBatch = nil
	}
}

func makePayload(i, size int) []byte {
	payload := make([]byte, size)
	fillPayload(payload, i)
	return payload
}

func fillPayload(payload []byte, i int) {
	for j := range payload {
		payload[j] = byte((i*31 + j*17 + 11) % 251)
	}
	if len(payload) >= 8 {
		binary.LittleEndian.PutUint64(payload[:8], uint64(i))
	}
}

func expectedName(id int64) string {
	return fmt.Sprintf("row-%06d", id)
}

// verifyName checks the generated name without allocating a comparison string.
func verifyName(name string, id int64) bool {
	if len(name) != 10 || name[0] != 'r' || name[1] != 'o' || name[2] != 'w' || name[3] != '-' {
		return false
	}
	value, err := strconv.ParseUint(name[4:], 10, 64)
	return err == nil && value == uint64(id)
}

const (
	fnvOffset64 = uint64(14695981039346656037)
	fnvPrime64  = uint64(1099511628211)
)

// addRowHash extends a FNV-1a sum with one row. The worker verifies each row
// against the generated values as well; the hash additionally catches a
// combination of duplicated and missing rows that preserves the row count.
func addRowHash(hash uint64, id int64, name string, payload []byte) uint64 {
	var idBytes [8]byte
	binary.LittleEndian.PutUint64(idBytes[:], uint64(id))
	hash = fnvBytes(hash, idBytes[:])
	hash = fnvString(hash, name)
	return fnvBytes(hash, payload)
}

func fnvBytes(hash uint64, data []byte) uint64 {
	for _, value := range data {
		hash ^= uint64(value)
		hash *= fnvPrime64
	}
	return hash
}

func fnvString(hash uint64, value string) uint64 {
	for i := 0; i < len(value); i++ {
		hash ^= uint64(value[i])
		hash *= fnvPrime64
	}
	return hash
}

func (fx *fixture) buildMainBatch(start, count int) (arrow.RecordBatch, error) {
	if start < 0 || count < 1 || start+count > fx.rows {
		return nil, fmt.Errorf("invalid fixture batch range [%d, %d) for %d rows", start, start+count, fx.rows)
	}
	builder := array.NewRecordBuilder(memory.DefaultAllocator, fx.schema)
	defer builder.Release()
	ids := builder.Field(0).(*array.Int64Builder)
	names := builder.Field(1).(*array.StringBuilder)
	payloads := builder.Field(2).(*array.BinaryBuilder)
	for i := start; i < start+count; i++ {
		ids.Append(int64(i))
		names.Append(expectedName(int64(i)))
		payloads.Append(makePayload(i, fx.payloadBytes))
	}
	return builder.NewRecordBatch(), nil
}

func (fx *fixture) writeMainDataset(ctx context.Context) error {
	var batches []arrow.RecordBatch
	defer func() {
		for _, batch := range batches {
			batch.Release()
		}
	}()
	for start := 0; start < fx.rows; start += fixtureBatchRows {
		count := min(fixtureBatchRows, fx.rows-start)
		batch, err := fx.buildMainBatch(start, count)
		if err != nil {
			return err
		}
		batches = append(batches, batch)
	}
	reader, err := array.NewRecordReader(fx.schema, batches)
	if err != nil {
		return fmt.Errorf("build main fixture reader: %w", err)
	}
	dataset, err := lance.Create(ctx, fx.mainURI, reader)
	reader.Release()
	if err != nil {
		return fmt.Errorf("create main fixture dataset: %w", err)
	}
	if err := dataset.Close(); err != nil {
		return fmt.Errorf("close main fixture dataset: %w", err)
	}
	return nil
}

func (fx *fixture) writeRawFile(ctx context.Context) (err error) {
	writer, err := lancefile.Create(ctx, fx.rawURI, fx.schema, nil)
	if err != nil {
		return fmt.Errorf("create raw fixture file: %w", err)
	}
	defer func() {
		if closeErr := writer.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close raw fixture writer: %w", closeErr)
		}
	}()
	for start := 0; start < fx.rows; start += fixtureBatchRows {
		count := min(fixtureBatchRows, fx.rows-start)
		batch, err := fx.buildMainBatch(start, count)
		if err != nil {
			return err
		}
		writeErr := writer.Write(ctx, batch)
		batch.Release()
		if writeErr != nil {
			return fmt.Errorf("write raw fixture batch: %w", writeErr)
		}
	}
	summary, err := writer.Finish(ctx)
	if err != nil {
		return fmt.Errorf("finish raw fixture file: %w", err)
	}
	if summary.NumRows != uint64(fx.rows) {
		return fmt.Errorf("raw fixture reports %d rows, want %d", summary.NumRows, fx.rows)
	}
	return nil
}

func (fx *fixture) writeBlobDataset(ctx context.Context) (err error) {
	field, err := lance.BlobFieldWithOptions("blob", true, lance.BlobFieldOptions{
		InlineSizeThreshold: ptrUint64(blobInlineThresholdBytes),
	})
	if err != nil {
		return fmt.Errorf("build blob field: %w", err)
	}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		field,
	}, nil)
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	ids := builder.Field(0).(*array.Int64Builder)
	blobs := builder.Field(1).(*array.StructBuilder)
	data := blobs.FieldBuilder(0).(*array.BinaryBuilder)
	uri := blobs.FieldBuilder(1).(*array.StringBuilder)
	blobPayloadBytes := max(1, fx.payloadBytes)
	for i := 0; i < fx.blobRows; i++ {
		ids.Append(int64(i))
		fx.blobIndices = append(fx.blobIndices, uint64(i))
		if i%blobNullEvery == blobNullEvery-1 {
			fx.blobValues = append(fx.blobValues, nil)
			blobs.AppendNull()
			continue
		}
		payload := makePayload(100000+i, blobPayloadBytes)
		fx.blobValues = append(fx.blobValues, payload)
		blobs.Append(true)
		data.Append(payload)
		uri.AppendNull()
	}
	record := builder.NewRecordBatch()
	defer record.Release()
	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{record})
	if err != nil {
		return fmt.Errorf("build blob fixture reader: %w", err)
	}
	dataset, err := lance.Create(ctx, fx.blobURI, reader, lance.WithWriteParams(lance.WriteParams{EnableStableRowIDs: true}))
	reader.Release()
	if err != nil {
		return fmt.Errorf("create blob fixture dataset: %w", err)
	}
	if err := dataset.Close(); err != nil {
		return fmt.Errorf("close blob fixture dataset: %w", err)
	}
	return nil
}

func ptrUint64(value uint64) *uint64 {
	return &value
}

func (fx *fixture) verifyBatch(batch arrow.RecordBatch) (int, uint64, error) {
	if batch == nil {
		return 0, 0, errors.New("nil record batch")
	}
	idColumn, err := columnByName(batch, "id")
	if err != nil {
		return 0, 0, err
	}
	ids, ok := idColumn.(*array.Int64)
	if !ok {
		return 0, 0, fmt.Errorf("id column has type %s, want int64", idColumn.DataType())
	}
	nameColumn, err := columnByName(batch, "name")
	if err != nil {
		return 0, 0, err
	}
	names, ok := nameColumn.(*array.String)
	if !ok {
		return 0, 0, fmt.Errorf("name column has type %s, want utf8", nameColumn.DataType())
	}
	payloadColumn, err := columnByName(batch, "payload")
	if err != nil {
		return 0, 0, err
	}
	payloads, ok := payloadColumn.(*array.Binary)
	if !ok {
		return 0, 0, fmt.Errorf("payload column has type %s, want binary", payloadColumn.DataType())
	}
	rows := int(batch.NumRows())
	if ids.Len() != rows || names.Len() != rows || payloads.Len() != rows {
		return 0, 0, fmt.Errorf("column lengths id=%d name=%d payload=%d do not match batch rows %d", ids.Len(), names.Len(), payloads.Len(), rows)
	}

	var hash uint64
	wantPayload := make([]byte, fx.payloadBytes)
	for i := 0; i < rows; i++ {
		if ids.IsNull(i) || names.IsNull(i) || payloads.IsNull(i) {
			return 0, 0, fmt.Errorf("unexpected null value in row %d", i)
		}
		id := ids.Value(i)
		if id < 0 || id >= int64(fx.rows) {
			return 0, 0, fmt.Errorf("row %d has out-of-range id %d", i, id)
		}
		if !verifyName(names.Value(i), id) {
			return 0, 0, fmt.Errorf("row %d id %d has unexpected name %q", i, id, names.Value(i))
		}
		fillPayload(wantPayload, int(id))
		if !bytes.Equal(payloads.Value(i), wantPayload) {
			return 0, 0, fmt.Errorf("row %d id %d payload mismatch: got %d bytes, want %d bytes", i, id, len(payloads.Value(i)), len(wantPayload))
		}
		hash += addRowHash(0, id, expectedName(id), wantPayload)
	}
	return rows, hash, nil
}

func (fx *fixture) verifyBlobBatch(batch arrow.RecordBatch, rowOffset int) error {
	if batch == nil {
		return errors.New("nil blob record batch")
	}
	column, err := columnByName(batch, "data")
	if err != nil {
		return err
	}
	data, ok := column.(*array.LargeBinary)
	if !ok {
		return fmt.Errorf("blob data column has type %s, want large binary", column.DataType())
	}
	rows := int(batch.NumRows())
	if data.Len() != rows {
		return fmt.Errorf("blob data length %d does not match batch rows %d", data.Len(), rows)
	}
	if rows > len(fx.blobIndices) {
		return fmt.Errorf("blob batch has %d rows, only %d were requested", rows, len(fx.blobIndices))
	}
	for i := 0; i < rows; i++ {
		position := rowOffset + i
		if position >= len(fx.blobIndices) {
			return fmt.Errorf("blob row %d maps outside the %d requested rows", position, len(fx.blobIndices))
		}
		index := fx.blobIndices[position]
		want := fx.blobValues[position]
		if data.IsNull(i) {
			if want == nil {
				continue
			}
			return fmt.Errorf("blob row %d (index %d) is null, want %d bytes", position, index, len(want))
		}
		if want == nil {
			return fmt.Errorf("blob row %d (index %d) is non-null, want null", position, index)
		}
		if !bytes.Equal(data.Value(i), want) {
			return fmt.Errorf("blob row %d (index %d) payload mismatch: got %d bytes, want %d bytes", position, index, len(data.Value(i)), len(want))
		}
	}
	return nil
}

func (fx *fixture) scanAndVerify(ctx context.Context, dataset *lance.Dataset, batchSize, wantRows int, wantHash uint64) error {
	scanner, err := dataset.Scan(
		lance.WithColumns("id", "name", "payload"),
		lance.WithBatchSize(int64(batchSize)),
	)
	if err != nil {
		return fmt.Errorf("create scan: %w", err)
	}
	rows := 0
	var hash uint64
	for batch, err := range scanner.Records(ctx) {
		if err != nil {
			return fmt.Errorf("scan records: %w", err)
		}
		batchRows, batchHash, err := fx.verifyBatch(batch)
		if err != nil {
			return fmt.Errorf("verify scan batch: %w", err)
		}
		rows += batchRows
		hash += batchHash
	}
	if closeErr := scanner.Close(); closeErr != nil {
		return fmt.Errorf("close scanner: %w", closeErr)
	}
	if rows != wantRows {
		return fmt.Errorf("scan returned %d rows, want %d", rows, wantRows)
	}
	if hash != wantHash {
		return fmt.Errorf("scan hash %016x, want %016x", hash, wantHash)
	}
	return nil
}

func columnByName(batch arrow.RecordBatch, name string) (arrow.Array, error) {
	fields := batch.Schema().Fields()
	for i := range fields {
		if fields[i].Name == name {
			return batch.Column(i), nil
		}
	}
	return nil, fmt.Errorf("batch schema has no column %q", name)
}
