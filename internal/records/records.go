// Package records adapts a lazily opened Arrow record stream to a Go
// iterator.
//
// The adapter owns the stream it opens. A sequence opens its stream when
// iteration starts and releases it when iteration ends, so an unused sequence
// holds no native stream and an early break still releases one.
package records

import (
	"iter"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Sequence returns an iterator over the batches of the stream that open
// produces. open runs once per iteration, not when Sequence returns, and every
// iteration owns and releases its own stream.
//
// A stream that open reports is yielded before the first batch. A batch error
// reported by the stream is yielded after the last batch. Yielded batches
// belong to the stream until iteration advances, so a caller keeps one by
// retaining it.
func Sequence(open func() (array.RecordReader, error)) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		reader, err := open()
		if err != nil {
			yield(nil, err)
			return
		}
		defer reader.Release()

		for reader.Next() {
			if !yield(reader.RecordBatch(), nil) {
				return
			}
		}
		if err := reader.Err(); err != nil {
			yield(nil, err)
		}
	}
}
