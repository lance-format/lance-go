package lance_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	lance "github.com/lance-format/lance-go"
)

func TestNativeWriteOptionsCommitPaths(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()

	create, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1, 2, 3}, []string{"a", "b", "c"}, []bool{true, true, true})),
		lance.WithWriteParams(lance.WriteParams{
			EnableV2ManifestPaths: ptr(false),
			FileWriterOptions: &lance.DatasetFileWriterOptions{
				DataCacheBytes:    ptr(uint64(1 << 20)),
				MaxPageBytes:      ptr(uint64(8 << 20)),
				KeepOriginalArray: ptr(false),
			},
		}),
		lance.WithWriteTransactionProperties(map[string]string{"fixture": "native-create"}),
	)
	if err != nil {
		t.Fatalf("Create with native options: %v", err)
	}
	if got := mutationVersion(t, create); got != 1 {
		t.Fatalf("create version = %d, want 1", got)
	}
	requireMutationRows(t, create, []int64{1, 2, 3}, []string{"a", "b", "c"})
	tx, err := create.ReadTransactionByVersion(1)
	if err != nil {
		t.Fatalf("ReadTransactionByVersion: %v", err)
	}
	properties, present, err := tx.Properties()
	if err != nil {
		t.Fatalf("Properties: %v", err)
	}
	if !present || properties["fixture"] != "native-create" {
		t.Fatalf("properties = %v present %v", properties, present)
	}
	tx.Close()
	if err := create.Close(); err != nil {
		t.Fatalf("close create: %v", err)
	}
	snapshot, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open pre-append snapshot: %v", err)
	}
	defer snapshot.Close()

	appendDataset, err := lance.Append(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{4}, []string{"d"}, []bool{true})),
		lance.WithWriteParams(lance.WriteParams{
			FileWriterOptions: &lance.DatasetFileWriterOptions{DataCacheBytes: ptr(uint64(0))},
		}),
	)
	if err != nil {
		t.Fatalf("Append with native options: %v", err)
	}
	if got := mutationVersion(t, appendDataset); got != 2 {
		t.Fatalf("append version = %d, want 2", got)
	}
	requireMutationRows(t, appendDataset, []int64{1, 2, 3, 4}, []string{"a", "b", "c", "d"})
	requireMutationRows(t, snapshot, []int64{1, 2, 3}, []string{"a", "b", "c"})
	appendDataset.Close()

	overwrite, err := lance.Overwrite(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{5, 6}, []string{"e", "f"}, []bool{true, true})),
		lance.WithWriteParams(lance.WriteParams{BlobPackFileSizeThreshold: ptr(uint64(0))}),
	)
	if err != nil {
		t.Fatalf("Overwrite with native options: %v", err)
	}
	if got := mutationVersion(t, overwrite); got != 3 {
		t.Fatalf("overwrite version = %d, want 3", got)
	}
	requireMutationRows(t, overwrite, []int64{5, 6}, []string{"e", "f"})
	overwrite.Close()

	staged, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{7, 8}, []string{"g", "h"}, []bool{true, true})),
		lance.WithWriteMode(lance.WriteModeAppend),
		lance.WithWriteParams(lance.WriteParams{FileWriterOptions: &lance.DatasetFileWriterOptions{MaxPageBytes: ptr(uint64(4 << 20))}}),
	)
	if err != nil {
		t.Fatalf("WriteUncommitted with native options: %v", err)
	}
	committed, err := staged.Commit(ctx, uri)
	if err != nil {
		staged.Close()
		t.Fatalf("Commit staged native write: %v", err)
	}
	staged.Close()
	if got := mutationVersion(t, committed); got != 4 {
		t.Fatalf("staged commit version = %d, want 4", got)
	}
	committed.Close()

	reopened, err := lance.Open(ctx, uri)
	if err != nil {
		t.Fatalf("Open after staged commit: %v", err)
	}
	defer reopened.Close()
	requireMutationRows(t, reopened, []int64{5, 6, 7, 8}, []string{"e", "f", "g", "h"})
}

func TestNativeWriteOptionsBasePlacement(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	root := t.TempDir()
	uri := filepath.Join(root, "data.lance")
	base1 := filepath.Join(root, "base-one")
	base2 := filepath.Join(root, "base-two")
	schema := testSchema()
	params := lance.WriteParams{
		EnableV2ManifestPaths: ptr(false),
		InitialBases: []lance.BasePath{
			{ID: 1, Path: base1, IsDatasetRoot: true},
			{ID: 2, Path: base2, Name: ptr("base-two")},
		},
		TargetBases:    []uint32{1},
		MaxRowsPerFile: 1,
	}
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})), lance.WithWriteParams(params))
	if err != nil {
		t.Fatalf("Create with initial bases: %v", err)
	}
	requireMutationRows(t, dataset, []int64{1, 2}, []string{"a", "b"})
	bases, err := dataset.BasePaths()
	if err != nil {
		t.Fatalf("BasePaths: %v", err)
	}
	if len(bases) != 2 || bases[1].Path != base1 || bases[2].Path != base2 {
		t.Fatalf("base paths = %+v", bases)
	}
	if !datasetHasDataFileBaseID(t, dataset, 1) {
		t.Fatalf("no data file assigned to primary target base 1")
	}

	appended, err := lance.Append(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{3}, []string{"c"}, []bool{true})),
		lance.WithWriteParams(lance.WriteParams{
			TargetBaseNamesOrPaths: []string{"base-two"},
			MaxRowsPerFile:         1,
		}),
	)
	if err != nil {
		t.Fatalf("Append targeting base name: %v", err)
	}
	if !datasetHasDataFileBaseID(t, appended, 2) {
		t.Fatalf("no data file assigned to target base 2")
	}
	appended.Close()

	_, err = lance.Append(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{4}, []string{"d"}, []bool{true})),
		lance.WithWriteParams(lance.WriteParams{TargetBases: []uint32{}}),
	)
	if err == nil || !strings.Contains(err.Error(), "target_bases cannot be empty") {
		t.Fatalf("explicit empty target bases error = %v", err)
	}
	_, err = lance.Append(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{5}, []string{"e"}, []bool{true})),
		lance.WithWriteParams(lance.WriteParams{InitialBases: []lance.BasePath{}}),
	)
	if err == nil || !strings.Contains(err.Error(), "Cannot register new bases") {
		t.Fatalf("explicit empty initial bases error = %v", err)
	}
	dataset.Close()
}

func TestNativeWriteOptionsTargetAllBases(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	root := t.TempDir()
	uri := filepath.Join(root, "data.lance")
	base := filepath.Join(root, "base")
	schema := testSchema()
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})),
		lance.WithWriteParams(lance.WriteParams{
			InitialBases:   []lance.BasePath{{ID: 7, Path: base, IsDatasetRoot: true}},
			TargetAllBases: ptr(true),
			MaxRowsPerFile: 1,
		}),
	)
	if err != nil {
		t.Fatalf("Create targeting all bases: %v", err)
	}
	defer dataset.Close()
	if !datasetHasDataFileBaseID(t, dataset, 7) {
		t.Fatalf("no data file assigned to the only registered base")
	}
}

func TestNativeWriteOptionsBaseStoreParams(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	root := t.TempDir()
	uri := filepath.Join(root, "data.lance")
	base := filepath.Join(root, "base")
	schema := testSchema()
	accessor, err := lance.NewStorageOptionsAccessor(map[string]string{}, nil)
	if err != nil {
		t.Fatalf("NewStorageOptionsAccessor: %v", err)
	}
	params := lance.WriteParams{
		InitialBases: []lance.BasePath{{ID: 1, Path: base, IsDatasetRoot: true}},
		TargetBases:  []uint32{1},
		BaseStoreParams: map[string]lance.BaseStoreParams{
			base: {
				StorageOptionsAccessor:     accessor,
				BlockSize:                  ptr(uint64(4096)),
				S3CredentialsRefreshOffset: ptr(90 * time.Second),
				UseConstantSizeUploadParts: ptr(false),
				ListIsLexicallyOrdered:     ptr(false),
			},
		},
	}
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})), lance.WithWriteParams(params))
	if err != nil {
		accessor.Close()
		t.Fatalf("Create with base store params: %v", err)
	}
	accessor.Close()
	requireMutationRows(t, dataset, []int64{1}, []string{"a"})
	dataset.Close()

	_, err = lance.Append(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{2}, []string{"b"}, []bool{true})),
		lance.WithWriteParams(lance.WriteParams{
			BaseStoreParams: map[string]lance.BaseStoreParams{
				base: {S3CredentialsRefreshOffset: ptr(-time.Second)},
			},
		}),
	)
	if err == nil || !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("negative refresh offset error = %v", err)
	}
}

func TestNativeWriteOptionsExternalBlobModes(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	root := t.TempDir()
	uri := filepath.Join(root, "blob.lance")
	base := filepath.Join(root, "base")
	externalPath := filepath.Join(root, "external.bin")
	if err := os.WriteFile(externalPath, []byte("blob"), 0600); err != nil {
		t.Fatal(err)
	}
	field, err := lance.BlobField("blob", true)
	if err != nil {
		t.Fatalf("BlobField: %v", err)
	}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
		field,
	}, nil)
	// Prepared descriptors already contain resolved locations and bypass URI
	// policy validation. Use a raw URI so the native writer resolves it.
	builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).Append(1)
	blobs := builder.Field(1).(*array.StructBuilder)
	blobs.Append(true)
	blobs.FieldBuilder(0).(*array.BinaryBuilder).AppendNull()
	blobs.FieldBuilder(1).(*array.StringBuilder).Append("file://" + externalPath)
	record := builder.NewRecordBatch()
	defer record.Release()
	checkPayload := func(uri string) {
		t.Helper()
		dataset, err := lance.Open(ctx, uri)
		if err != nil {
			t.Fatalf("reopen blob dataset: %v", err)
		}
		defer dataset.Close()
		files, err := dataset.TakeBlobsByIndices(ctx, []uint64{0}, "blob")
		if err != nil {
			t.Fatalf("TakeBlobsByIndices: %v", err)
		}
		for _, file := range files {
			if file != nil {
				defer file.Close()
			}
		}
		if len(files) != 1 || files[0] == nil {
			t.Fatalf("blob files = %v, want one non-null file", files)
		}
		payload, err := io.ReadAll(files[0])
		if err != nil || string(payload) != "blob" {
			t.Fatalf("blob payload = %q, %v; want blob", payload, err)
		}
	}

	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, record),
		lance.WithWriteParams(lance.WriteParams{
			InitialBases:                  []lance.BasePath{{ID: 1, Path: base, IsDatasetRoot: true}},
			TargetBases:                   []uint32{1},
			AllowExternalBlobOutsideBases: true,
			ExternalBlobMode:              lance.ExternalBlobModeReference,
		}),
	)
	if err != nil {
		t.Fatalf("Create external blob reference: %v", err)
	}
	if rows, err := dataset.CountRows(ctx); err != nil || rows != 1 {
		dataset.Close()
		t.Fatalf("CountRows = %d, %v; want 1", rows, err)
	}
	dataset.Close()
	checkPayload(uri)

	_, err = lance.Create(ctx, filepath.Join(root, "no-allow.lance"), makeReader(t, schema, record),
		lance.WithWriteParams(lance.WriteParams{
			InitialBases:     []lance.BasePath{{ID: 1, Path: base, IsDatasetRoot: true}},
			TargetBases:      []uint32{1},
			ExternalBlobMode: lance.ExternalBlobModeReference,
		}),
	)
	if !errors.Is(err, lance.ErrInvalidArgument) || !strings.Contains(err.Error(), "outside registered external bases") {
		t.Fatalf("external reference without allow-outside = %v, want outside-base InvalidArgument", err)
	}

	_, err = lance.Create(ctx, filepath.Join(root, "ingest.lance"), makeReader(t, schema, record),
		lance.WithWriteParams(lance.WriteParams{
			AllowExternalBlobOutsideBases: true,
			ExternalBlobMode:              lance.ExternalBlobModeIngest,
		}),
	)
	if !errors.Is(err, lance.ErrInvalidArgument) || !strings.Contains(err.Error(), "only applies") {
		t.Fatalf("ingest with allow-outside = %v, want incompatible-policy InvalidArgument", err)
	}
	ingestURI := filepath.Join(root, "ingested.lance")
	ingested, err := lance.Create(ctx, ingestURI, makeReader(t, schema, record),
		lance.WithWriteParams(lance.WriteParams{ExternalBlobMode: lance.ExternalBlobModeIngest}))
	if err != nil {
		t.Fatalf("Create ingested blob: %v", err)
	}
	ingested.Close()
	if err := os.Remove(externalPath); err != nil {
		t.Fatal(err)
	}
	checkPayload(ingestURI)
}

func TestNativeWriteOptionsProgressAndStats(t *testing.T) {
	requireNative(t)
	var begins, completes, stats atomic.Int64
	var rows atomic.Uint64
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	progress := &lance.WriteProgress{
		BeginFragment: func(event lance.WriteFragmentProgress) error {
			begins.Add(1)
			return nil
		},
		CompleteFragment: func(event lance.WriteFragmentProgress) error {
			if event.PhysicalRows != nil {
				completes.Add(1)
			}
			return nil
		},
		WriteStats: func(event lance.WriteStats) {
			stats.Add(1)
			rows.Store(event.RowsWritten)
		},
	}
	dataset, err := lance.Create(context.Background(), uri, makeReader(t, schema, makeRecord(t, schema,
		[]int64{1, 2, 3, 4, 5}, []string{"a", "b", "c", "d", "e"}, []bool{true, true, true, true, true})),
		lance.WithWriteParams(lance.WriteParams{MaxRowsPerFile: 1, Progress: progress}),
	)
	if err != nil {
		t.Fatalf("Create with progress: %v", err)
	}
	if begins.Load() < 5 || completes.Load() < 5 {
		t.Fatalf("progress calls = begin %d complete %d, want at least 5 each", begins.Load(), completes.Load())
	}
	if stats.Load() < 1 || rows.Load() != 5 {
		t.Fatalf("stats calls = %d rows = %d, want at least one and 5 rows", stats.Load(), rows.Load())
	}
	dataset.Close()
}

func TestNativeWriteOptionsProgressErrorStopsStagedWrite(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	progress := &lance.WriteProgress{
		BeginFragment: func(lance.WriteFragmentProgress) error {
			return errors.New("refused fragment")
		},
	}
	_, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})),
		lance.WithWriteParams(lance.WriteParams{Progress: progress}),
	)
	if err == nil || !strings.Contains(err.Error(), "refused fragment") {
		t.Fatalf("staged progress error = %v", err)
	}
	if _, err := lance.Open(ctx, uri); err == nil {
		t.Fatal("failed staged write published a dataset")
	}
}

func TestNativeWriteOptionsProgressPanicStopsStagedWrite(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	progress := &lance.WriteProgress{
		BeginFragment: func(lance.WriteFragmentProgress) error {
			panic("begin panic")
		},
	}
	_, err := lance.WriteUncommitted(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})),
		lance.WithWriteParams(lance.WriteParams{Progress: progress}),
	)
	if err == nil {
		t.Fatal("staged write accepted a panicking begin callback")
	}
	if _, err := lance.Open(ctx, uri); err == nil {
		t.Fatal("failed staged write published a dataset")
	}
}

func TestNativeWriteOptionsStatsPanicIsContained(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true})),
		lance.WithWriteParams(lance.WriteParams{Progress: &lance.WriteProgress{
			WriteStats: func(lance.WriteStats) { panic("stats panic") },
		}}),
	)
	if err != nil {
		t.Fatalf("Create with panicking stats callback: %v", err)
	}
	defer dataset.Close()
	if rows, err := dataset.CountRows(ctx); err != nil || rows != 1 {
		t.Fatalf("CountRows = %d, %v; want 1", rows, err)
	}
}

func datasetHasDataFileBaseID(t *testing.T, dataset *lance.Dataset, want uint32) bool {
	t.Helper()
	fragments, err := dataset.Fragments()
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	defer func() {
		for _, fragment := range fragments {
			fragment.Close()
		}
	}()
	for _, fragment := range fragments {
		count, err := fragment.DataFileCount()
		if err != nil {
			t.Fatalf("DataFileCount: %v", err)
		}
		for i := uint64(0); i < count; i++ {
			dataFile, err := fragment.DataFileAt(i)
			if err != nil {
				t.Fatalf("DataFileAt(%d): %v", i, err)
			}
			baseID, present, baseErr := dataFile.BaseID()
			dataFile.Close()
			if baseErr != nil {
				t.Fatalf("data file base id: %v", baseErr)
			}
			if present && baseID == want {
				return true
			}
		}
	}
	return false
}

func TestNativeWriteOptionsRetainedSessionAndAccessor(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	uri := filepath.Join(t.TempDir(), "data.lance")
	schema := testSchema()
	session, err := lance.NewSession(1<<20, 1<<20)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	accessor, err := lance.NewStorageOptionsAccessor(map[string]string{}, nil)
	if err != nil {
		session.Close()
		t.Fatalf("NewStorageOptionsAccessor: %v", err)
	}
	dataset, err := lance.Create(ctx, uri, makeReader(t, schema, makeRecord(t, schema, []int64{1, 2}, []string{"a", "b"}, []bool{true, true})),
		lance.WithWriteSession(session),
		lance.WithWriteStorageOptionsAccessor(accessor),
	)
	if err != nil {
		accessor.Close()
		session.Close()
		t.Fatalf("Create with retained session and accessor: %v", err)
	}
	session.Close()
	accessor.Close()
	defer dataset.Close()
	requireMutationRows(t, dataset, []int64{1, 2}, []string{"a", "b"})
}
