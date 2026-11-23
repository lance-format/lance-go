package lance

import (
	"context"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/records"
)

// BlobRecords iterates over a planned whole-blob read. It matches ReadBlobs:
// the schema is row_address: UInt64 (non-null) and data: LargeBinary
// (nullable), and null and empty blobs stay distinct.
//
// The read is planned when iteration starts, so creating the sequence holds no
// stream and the Dataset stays usable after iteration ends. Every exit,
// including an early break, a cancelled context and a planning or stream
// error, releases the stream. Each yielded batch owns its buffers but belongs
// to the stream until iteration advances; call Retain to keep one.
func (d *Dataset) BlobRecords(ctx context.Context, column string, selector BlobRowSelector, rows []uint64, options *BlobReadOptions) iter.Seq2[arrow.RecordBatch, error] {
	return records.Sequence(func() (array.RecordReader, error) {
		return d.ReadBlobs(ctx, column, selector, rows, options)
	})
}

// BlobRangeRecords iterates over planned per-row blob-range reads. It matches
// ReadBlobRanges with the stream lifecycle and borrowing rules of BlobRecords;
// request_index preserves the zero-based input position of every request.
func (d *Dataset) BlobRangeRecords(ctx context.Context, column string, selector BlobRowSelector, requests []BlobRangeRequest, options *BlobReadOptions) iter.Seq2[arrow.RecordBatch, error] {
	return records.Sequence(func() (array.RecordReader, error) {
		return d.ReadBlobRanges(ctx, column, selector, requests, options)
	})
}
