package file

import (
	"context"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/records"
)

// Records iterates over all rows as Arrow batches. It matches ReadAll, but
// opens the stream when iteration starts and releases it when iteration ends,
// including an early break or an error, so the caller releases no stream.
//
// Each yielded batch is borrowed until iteration advances; call Retain to keep
// one. An open or iteration error is yielded as the second value. The Reader
// stays usable after iteration ends.
func (r *Reader) Records(ctx context.Context, opts ReadOptions) iter.Seq2[arrow.RecordBatch, error] {
	return records.Sequence(func() (array.RecordReader, error) {
		return r.ReadAll(ctx, opts)
	})
}

// RecordsRange iterates over numRows rows starting at offset. It matches
// ReadRange with the stream lifecycle and borrowing rules of Records.
func (r *Reader) RecordsRange(ctx context.Context, offset, numRows uint64, opts ReadOptions) iter.Seq2[arrow.RecordBatch, error] {
	return records.Sequence(func() (array.RecordReader, error) {
		return r.ReadRange(ctx, offset, numRows, opts)
	})
}

// RecordsIndices iterates over rows at strictly increasing, unique row
// indices. It matches ReadIndices with the stream lifecycle and borrowing
// rules of Records; an ordering the native reader rejects is yielded as an
// iteration error.
//
// Each traversal reads indices when it opens its stream, so a change made
// between traversals affects the next result; changing the slice while a
// traversal opens its stream races with that read.
func (r *Reader) RecordsIndices(ctx context.Context, indices []uint32, opts ReadOptions) iter.Seq2[arrow.RecordBatch, error] {
	return records.Sequence(func() (array.RecordReader, error) {
		return r.ReadIndices(ctx, indices, opts)
	})
}
