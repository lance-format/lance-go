package lance_test

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"

	lance "github.com/lance-format/lance-go"
)

const (
	descriptorInline = iota
	descriptorNull
	descriptorPacked
)

// FuzzBlobDescriptorBuilderPreparedArray builds a bounded sequence of valid
// blob descriptors from logical values, then checks the prepared Arrow array
// through its typed accessors. It covers the public inline, null and packed
// append paths in row order; the wider descriptor matrix stays in
// TestBlobDescriptorBuilderPreparedArray.
func FuzzBlobDescriptorBuilderPreparedArray(f *testing.F) {
	f.Add(uint8(0), uint8(0), "alpha", uint32(7), uint64(10), uint64(4))
	f.Add(uint8(2), uint8(0), "", uint32(1), uint64(0), uint64(0))
	f.Add(uint8(7), uint8(1), "payload", uint32(1024), uint64(63), uint64(64))
	f.Add(uint8(4), uint8(2), "x", uint32(3), uint64(1), uint64(2))

	f.Fuzz(func(t *testing.T, count, variant uint8, payloadSeed string, blobID uint32, offset, length uint64) {
		requireNative(t)

		rows := int(count)%8 + 1
		payload := boundedBlobPayload(payloadSeed)
		packedID := blobID%1024 + 1
		packedOffset := offset % 64
		packedLength := length % 64

		type descriptor struct {
			kind   int
			inline []byte
			blobID uint32
			offset uint64
			length uint64
		}
		want := make([]descriptor, rows)
		builder, err := lance.NewBlobDescriptorBuilder("blob")
		if err != nil {
			t.Fatalf("NewBlobDescriptorBuilder: %v", err)
		}
		defer builder.Close()
		for i := 0; i < rows; i++ {
			switch (int(variant) + i) % 3 {
			case 0:
				if err := builder.AppendInline(payload); err != nil {
					t.Fatalf("AppendInline row %d: %v", i, err)
				}
				want[i] = descriptor{kind: descriptorInline, inline: payload}
			case 1:
				if err := builder.AppendNull(); err != nil {
					t.Fatalf("AppendNull row %d: %v", i, err)
				}
				want[i] = descriptor{kind: descriptorNull}
			case 2:
				if err := builder.AppendPacked(packedID, packedOffset, packedLength); err != nil {
					t.Fatalf("AppendPacked row %d: %v", i, err)
				}
				want[i] = descriptor{kind: descriptorPacked, blobID: packedID, offset: packedOffset, length: packedLength}
			}
		}

		prepared, _, err := builder.Finish()
		if err != nil {
			t.Fatalf("Finish: %v", err)
		}
		defer prepared.Release()

		structArray, ok := prepared.(*array.Struct)
		if !ok {
			t.Fatalf("prepared array = %T, want *array.Struct", prepared)
		}
		if structArray.Len() != rows {
			t.Fatalf("prepared rows = %d, want %d", structArray.Len(), rows)
		}
		kind := structArray.Field(0).(*array.Uint8)
		data := structArray.Field(1).(*array.LargeBinary)
		uri := structArray.Field(2).(*array.String)
		blobIDColumn := structArray.Field(3).(*array.Uint32)
		blobSize := structArray.Field(4).(*array.Uint64)
		position := structArray.Field(5).(*array.Uint64)

		for i, expected := range want {
			switch expected.kind {
			case descriptorInline:
				if structArray.IsNull(i) {
					t.Fatalf("inline row %d is null", i)
				}
				if got := kind.Value(i); got != 0 {
					t.Fatalf("row %d kind = %d, want inline", i, got)
				}
				if data.IsNull(i) || !bytes.Equal(data.Value(i), expected.inline) {
					t.Fatalf("row %d inline data = %q, want %q", i, data.Value(i), expected.inline)
				}
				if !uri.IsNull(i) || !blobIDColumn.IsNull(i) || !blobSize.IsNull(i) || !position.IsNull(i) {
					t.Fatalf("inline row %d has unexpected uri/blob fields", i)
				}
			case descriptorNull:
				if !structArray.IsNull(i) {
					t.Fatalf("null row %d is not null", i)
				}
				for column := 0; column < 6; column++ {
					if !structArray.Field(column).IsNull(i) {
						t.Fatalf("null row %d child %d is not null", i, column)
					}
				}
			case descriptorPacked:
				if structArray.IsNull(i) {
					t.Fatalf("packed row %d is null", i)
				}
				if got := kind.Value(i); got != 1 {
					t.Fatalf("row %d kind = %d, want packed", i, got)
				}
				if !data.IsNull(i) || !uri.IsNull(i) {
					t.Fatalf("packed row %d has unexpected data or uri", i)
				}
				if got := blobIDColumn.Value(i); got != expected.blobID {
					t.Fatalf("row %d blob_id = %d, want %d", i, got, expected.blobID)
				}
				if got := blobSize.Value(i); got != expected.length {
					t.Fatalf("row %d blob_size = %d, want %d", i, got, expected.length)
				}
				if got := position.Value(i); got != expected.offset {
					t.Fatalf("row %d position = %d, want %d", i, got, expected.offset)
				}
			}
		}
	})
}

// boundedBlobPayload copies fuzz text to a small binary payload. Empty
// payloads are valid and stay distinct from null rows.
func boundedBlobPayload(seed string) []byte {
	payload := []byte(seed)
	if len(payload) > 16 {
		payload = payload[:16]
	}
	return append([]byte(nil), payload...)
}
