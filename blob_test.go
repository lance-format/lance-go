package lance_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	lance "github.com/lance-format/lance-go"
)

func blobTestValues() []blobTestValue {
	return []blobTestValue{
		{Data: []byte("alpha")},
		{Null: true},
		{Data: []byte{}},
		{Data: []byte{0, 1, 2, 3, 4}},
		{Data: []byte("omega")},
	}
}

func closeBlobFilesForTest(t *testing.T, files []*lance.BlobFile) {
	t.Helper()
	t.Cleanup(func() {
		for _, file := range files {
			_ = file.Close()
		}
	})
}

func readBlobAll(file *lance.BlobFile) ([]byte, error) {
	return io.ReadAll(file)
}

func TestBlobTakeAndRead(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)

	files, err := dataset.TakeBlobs(context.Background(), []uint64{
		refs[3].RowID, refs[0].RowID, refs[2].RowID, refs[1].RowID, refs[3].RowID,
	}, "blob")
	if err != nil {
		t.Fatalf("TakeBlobs: %v", err)
	}
	closeBlobFilesForTest(t, files)
	if len(files) != 5 {
		t.Fatalf("TakeBlobs returned %d files, want 5", len(files))
	}
	if files[3] != nil {
		t.Fatal("null blob slot is not nil")
	}
	for i, want := range []uint64{5, 5, 0, 0, 5} {
		if i == 3 {
			if files[i] != nil {
				t.Fatalf("file %d is not nil for a null blob", i)
			}
			continue
		}
		if files[i] == nil {
			t.Fatalf("file %d is nil", i)
		}
		size, err := files[i].Size()
		if err != nil {
			t.Fatalf("file %d Size: %v", i, err)
		}
		if size != want {
			t.Fatalf("file %d size = %d, want %d", i, size, want)
		}
	}

	alpha := files[1]
	got, err := readBlobAll(alpha)
	if err != nil {
		t.Fatalf("read alpha: %v", err)
	}
	if string(got) != "alpha" {
		t.Fatalf("alpha = %q, want alpha", got)
	}

	binary := files[0]
	got, err = readBlobAll(binary)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if !bytes.Equal(got, []byte{0, 1, 2, 3, 4}) {
		t.Fatalf("binary = %v, want [0 1 2 3 4]", got)
	}
	duplicate := files[4]
	got, err = readBlobAll(duplicate)
	if err != nil {
		t.Fatalf("read duplicate binary: %v", err)
	}
	if !bytes.Equal(got, []byte{0, 1, 2, 3, 4}) {
		t.Fatalf("duplicate binary = %v, want [0 1 2 3 4]", got)
	}

	empty := files[2]
	got, err = readBlobAll(empty)
	if err != nil {
		t.Fatalf("read empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty blob returned %d bytes", len(got))
	}
	if _, err := empty.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("empty Read error = %v, want io.EOF", err)
	}
}

func TestBlobFileCursorAndRanges(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)
	files, err := dataset.TakeBlobsByIndices(context.Background(), []uint64{0}, "blob")
	if err != nil {
		t.Fatalf("TakeBlobsByIndices: %v", err)
	}
	closeBlobFilesForTest(t, files)
	file := files[0]

	readAt := make([]byte, 3)
	if n, err := file.ReadAt(readAt, 1); err != nil || n != 3 {
		t.Fatalf("ReadAt = %d, %v; want 3, nil", n, err)
	}
	if string(readAt) != "lph" {
		t.Fatalf("ReadAt = %q, want lph", readAt)
	}
	buffer := bytes.Repeat([]byte{0xAA}, 2)
	if n, err := file.ReadAt(buffer, 4); n != 1 || !errors.Is(err, io.EOF) {
		t.Fatalf("short ReadAt = %d, %v; want 1, io.EOF", n, err)
	}
	if buffer[0] != 'a' || buffer[1] != 0xAA {
		t.Fatalf("short ReadAt destination = %v, want [a 0xAA]", buffer)
	}
	if n, err := file.ReadAt(buffer, 5); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt at end = %d, %v; want 0, io.EOF", n, err)
	}
	if n, err := file.ReadAt(nil, 1000); err != nil || n != 0 {
		t.Fatalf("zero-length ReadAt = %d, %v; want 0, nil", n, err)
	}
	if _, err := file.ReadAt(make([]byte, 1), -1); err == nil {
		t.Fatal("negative ReadAt offset succeeded")
	}

	if pos, err := file.Tell(); err != nil || pos != 0 {
		t.Fatalf("initial Tell = %d, %v; want 0, nil", pos, err)
	}
	if pos, err := file.Seek(1, io.SeekStart); err != nil || pos != 1 {
		t.Fatalf("Seek start = %d, %v; want 1, nil", pos, err)
	}
	if n, err := file.ReadAt(readAt, 0); err != nil || n != 3 {
		t.Fatalf("ReadAt after Seek = %d, %v", n, err)
	}
	if pos, err := file.Tell(); err != nil || pos != 1 {
		t.Fatalf("Tell after ReadAt = %d, %v; want 1, nil", pos, err)
	}
	if got, err := io.ReadAll(file); err != nil || string(got) != "lpha" {
		t.Fatalf("Read after Seek = %q, %v; want lpha", got, err)
	}
	if _, err := file.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("read at EOF error = %v, want io.EOF", err)
	}
	if pos, err := file.Seek(3, io.SeekEnd); err != nil || pos != 8 {
		t.Fatalf("Seek past end = %d, %v; want 8, nil", pos, err)
	}
	if _, err := file.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("read past end error = %v, want io.EOF", err)
	}
	if pos, err := file.Seek(-1, io.SeekEnd); err != nil || pos != 4 {
		t.Fatalf("Seek from end = %d, %v; want 4, nil", pos, err)
	}
	if _, err := file.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("negative absolute Seek succeeded")
	}
	if _, err := file.Seek(-100, io.SeekCurrent); err == nil {
		t.Fatal("Seek before start succeeded")
	}
	if _, err := file.Seek(0, 99); err == nil {
		t.Fatal("invalid whence succeeded")
	}

	ranges := []lance.BlobRange{{Offset: 2, Length: 3}, {Offset: 0, Length: 2}, {Offset: 4, Length: 0}}
	destinations := [][]byte{make([]byte, 3), make([]byte, 2), make([]byte, 0)}
	if err := file.ReadRanges(ranges, destinations); err != nil {
		t.Fatalf("ReadRanges: %v", err)
	}
	if string(destinations[0]) != "pha" || string(destinations[1]) != "al" {
		t.Fatalf("ReadRanges = %q,%q; want pha,al", destinations[0], destinations[1])
	}
	if err := file.ReadRanges([]lance.BlobRange{{Offset: 0, Length: 6}}, [][]byte{make([]byte, 5)}); err == nil {
		t.Fatal("ReadRanges with small destination succeeded")
	}
	if err := file.ReadRanges([]lance.BlobRange{{Offset: 4, Length: 2}}, [][]byte{make([]byte, 2)}); err == nil {
		t.Fatal("out-of-bounds ReadRanges succeeded")
	}
	if err := file.ReadRanges([]lance.BlobRange{{Offset: 0, Length: 1}}, nil); err == nil {
		t.Fatal("mismatched ReadRanges lengths succeeded")
	}
	partial := [][]byte{bytes.Repeat([]byte{0xBB}, 2), bytes.Repeat([]byte{0xCC}, 2)}
	if err := file.ReadRanges([]lance.BlobRange{{Offset: 0, Length: 2}, {Offset: 4, Length: 2}}, partial); err == nil {
		t.Fatal("ReadRanges with a later out-of-bounds range succeeded")
	}
	if !bytes.Equal(partial[0], []byte{0xBB, 0xBB}) || !bytes.Equal(partial[1], []byte{0xCC, 0xCC}) {
		t.Fatalf("failed ReadRanges modified destinations: %v %v", partial[0], partial[1])
	}
	if err := file.ReadRanges(nil, nil); err != nil {
		t.Fatalf("empty ReadRanges: %v", err)
	}

	if err := file.SetReadAhead(2); err != nil {
		t.Fatalf("SetReadAhead: %v", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek after SetReadAhead: %v", err)
	}
	chunk := make([]byte, 2)
	if n, err := file.Read(chunk); err != nil || n != 2 || string(chunk) != "al" {
		t.Fatalf("chunked Read = %q, %d, %v", chunk, n, err)
	}
	if err := file.SetReadAhead(0); err != nil {
		t.Fatalf("disable SetReadAhead: %v", err)
	}
	if err := file.SetReadAhead(-1); err == nil {
		t.Fatal("negative read-ahead succeeded")
	}
}

func TestBlobFileCloseAndOwnership(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	files, err := dataset.TakeBlobsByIndices(context.Background(), []uint64{0}, "blob")
	if err != nil {
		t.Fatalf("TakeBlobsByIndices: %v", err)
	}
	file := files[0]
	if err := dataset.Close(); err != nil {
		t.Fatalf("close dataset: %v", err)
	}

	got, err := readBlobAll(file)
	if err != nil {
		t.Fatalf("read after dataset close: %v", err)
	}
	if string(got) != "alpha" {
		t.Fatalf("read after dataset close = %q, want alpha", got)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("second close file: %v", err)
	}
	if _, err := file.Size(); err == nil {
		t.Fatal("Size after Close succeeded")
	}
	if _, err := file.Read(make([]byte, 1)); err == nil {
		t.Fatal("Read after Close succeeded")
	}
	if _, err := file.ReadAt(make([]byte, 1), 0); err == nil {
		t.Fatal("ReadAt after Close succeeded")
	}
	if err := file.ReadRanges([]lance.BlobRange{{Offset: 0, Length: 1}}, [][]byte{make([]byte, 1)}); err == nil {
		t.Fatal("ReadRanges after Close succeeded")
	}
	if _, err := file.Tell(); err == nil {
		t.Fatal("Tell after Close succeeded")
	}
	if _, err := file.Seek(0, io.SeekStart); err == nil {
		t.Fatal("Seek after Close succeeded")
	}
	if err := file.SetReadAhead(1); err == nil {
		t.Fatal("SetReadAhead after Close succeeded")
	}

	var nilFile *lance.BlobFile
	if err := nilFile.Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}
	if _, err := nilFile.Size(); err == nil {
		t.Fatal("nil Size succeeded")
	}

}

func TestBlobTakeSelectorsAndErrors(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	refs := blobRefs(t, dataset)
	if refs[4].RowID == refs[4].Addr {
		t.Fatalf("row ID %d equals row address %d; selectors would not be distinguished", refs[4].RowID, refs[4].Addr)
	}

	byIndices, err := dataset.TakeBlobsByIndices(context.Background(), []uint64{4, 0, 2}, "blob")
	if err != nil {
		t.Fatalf("TakeBlobsByIndices: %v", err)
	}
	closeBlobFilesForTest(t, byIndices)
	if got, _ := readBlobAll(byIndices[0]); string(got) != "omega" {
		t.Fatalf("index 4 = %q, want omega", got)
	}
	if byIndices[2] == nil {
		t.Fatal("empty blob by index is nil")
	}

	byAddress, err := dataset.TakeBlobsByAddresses(context.Background(), []uint64{refs[3].Addr, refs[1].Addr}, "blob")
	if err != nil {
		t.Fatalf("TakeBlobsByAddresses: %v", err)
	}
	closeBlobFilesForTest(t, byAddress)
	if got, _ := readBlobAll(byAddress[0]); !bytes.Equal(got, []byte{0, 1, 2, 3, 4}) {
		t.Fatalf("address row 3 = %v", got)
	}
	if byAddress[1] != nil {
		t.Fatal("null blob by address is not nil")
	}

	emptyIDs, err := dataset.TakeBlobs(context.Background(), nil, "blob")
	if err != nil {
		t.Fatalf("empty TakeBlobs: %v", err)
	}
	if len(emptyIDs) != 0 {
		t.Fatalf("empty TakeBlobs returned %d files", len(emptyIDs))
	}
	emptyAddresses, err := dataset.TakeBlobsByAddresses(context.Background(), nil, "blob")
	if err != nil {
		t.Fatalf("empty TakeBlobsByAddresses: %v", err)
	}
	if len(emptyAddresses) != 0 {
		t.Fatalf("empty TakeBlobsByAddresses returned %d files", len(emptyAddresses))
	}

	if _, err := dataset.TakeBlobs(context.Background(), []uint64{refs[0].RowID}, "missing"); !errors.Is(err, lance.ErrInvalidArgument) {
		t.Fatalf("missing column error = %v, want ErrInvalidArgument", err)
	}

	plain, _, _ := newMutationDataset(t)
	if _, err := plain.TakeBlobs(context.Background(), nil, "missing"); err != nil {
		t.Fatalf("empty TakeBlobs on missing column = %v, want native empty-selection success", err)
	}
	if _, err := plain.TakeBlobsByAddresses(context.Background(), nil, "missing"); err == nil {
		t.Fatal("empty TakeBlobsByAddresses on missing column succeeded")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dataset.TakeBlobs(ctx, []uint64{refs[0].RowID}, "blob"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v, want context.Canceled", err)
	}

	if err := dataset.Close(); err != nil {
		t.Fatalf("Close dataset: %v", err)
	}
	if _, err := dataset.TakeBlobs(context.Background(), []uint64{refs[0].RowID}, "blob"); !errors.Is(err, lance.ErrClosed) {
		t.Fatalf("closed dataset error = %v, want ErrClosed", err)
	}
}

func TestBlobFileConcurrentReadAt(t *testing.T) {
	dataset, _ := newBlobDataset(t, blobTestValues())
	files, err := dataset.TakeBlobsByIndices(context.Background(), []uint64{3}, "blob")
	if err != nil {
		t.Fatalf("TakeBlobsByIndices: %v", err)
	}
	closeBlobFilesForTest(t, files)
	file := files[0]

	const readers = 8
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got := make([]byte, 5)
			if _, err := file.ReadAt(got, 0); err != nil {
				errs <- err
				return
			}
			if !bytes.Equal(got, []byte{0, 1, 2, 3, 4}) {
				errs <- errors.New("concurrent ReadAt returned wrong bytes")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
