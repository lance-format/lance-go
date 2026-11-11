package ffi

import (
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/lance-format/lance-go/internal/handle"
)

func TestNativeWriteOptionsSpecLayout(t *testing.T) {
	if got, want := unsafe.Sizeof(nativeWriteOptionsSpec{}), uintptr(128); got != want {
		t.Fatalf("nativeWriteOptionsSpec size = %d, want %d", got, want)
	}
	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"write_options", unsafe.Offsetof(nativeWriteOptionsSpec{}.WriteOptions), 0},
		{"transaction_properties", unsafe.Offsetof(nativeWriteOptionsSpec{}.TransactionProperties), 8},
		{"has_enable_v2_manifest_paths", unsafe.Offsetof(nativeWriteOptionsSpec{}.HasEnableV2ManifestPaths), 16},
		{"enable_v2_manifest_paths", unsafe.Offsetof(nativeWriteOptionsSpec{}.EnableV2ManifestPaths), 20},
		{"initial_bases", unsafe.Offsetof(nativeWriteOptionsSpec{}.InitialBases), 24},
		{"target_bases", unsafe.Offsetof(nativeWriteOptionsSpec{}.TargetBases), 32},
		{"target_base_names_or_paths", unsafe.Offsetof(nativeWriteOptionsSpec{}.TargetBaseNamesOrPaths), 40},
		{"has_target_all_bases", unsafe.Offsetof(nativeWriteOptionsSpec{}.HasTargetAllBases), 48},
		{"target_all_bases", unsafe.Offsetof(nativeWriteOptionsSpec{}.TargetAllBases), 52},
		{"base_store_params", unsafe.Offsetof(nativeWriteOptionsSpec{}.BaseStoreParams), 56},
		{"allow_external_blob_outside_bases", unsafe.Offsetof(nativeWriteOptionsSpec{}.AllowExternalBlobOutsideBases), 64},
		{"external_blob_mode", unsafe.Offsetof(nativeWriteOptionsSpec{}.ExternalBlobMode), 68},
		{"has_blob_pack_file_size_threshold", unsafe.Offsetof(nativeWriteOptionsSpec{}.HasBlobPackFileSizeThreshold), 72},
		{"blob_pack_file_size_threshold", unsafe.Offsetof(nativeWriteOptionsSpec{}.BlobPackFileSizeThreshold), 80},
		{"file_writer_options", unsafe.Offsetof(nativeWriteOptionsSpec{}.FileWriterOptions), 88},
		{"session", unsafe.Offsetof(nativeWriteOptionsSpec{}.Session), 96},
		{"storage_options_accessor", unsafe.Offsetof(nativeWriteOptionsSpec{}.StorageOptionsAccessor), 104},
		{"commit_lock", unsafe.Offsetof(nativeWriteOptionsSpec{}.CommitLock), 112},
		{"progress", unsafe.Offsetof(nativeWriteOptionsSpec{}.Progress), 120},
	}
	for _, offset := range offsets {
		if offset.got != offset.want {
			t.Fatalf("%s offset = %d, want %d", offset.name, offset.got, offset.want)
		}
	}

	if got, want := unsafe.Sizeof(nativeWriteProgress{}), uintptr(40); got != want {
		t.Fatalf("nativeWriteProgress size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(nativeWriteFragmentProgress{}), uintptr(32); got != want {
		t.Fatalf("nativeWriteFragmentProgress size = %d, want %d", got, want)
	}
	if got, want := unsafe.Offsetof(nativeWriteFragmentProgress{}.PhysicalRows), uintptr(16); got != want {
		t.Fatalf("physical_rows offset = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(nativeWriteStats{}), uintptr(24); got != want {
		t.Fatalf("nativeWriteStats size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(baseStoreParams{}), uintptr(72); got != want {
		t.Fatalf("baseStoreParams size = %d, want %d", got, want)
	}
	if got, want := unsafe.Offsetof(baseStoreParams{}.S3CredentialsRefreshSeconds), uintptr(40); got != want {
		t.Fatalf("s3 refresh seconds offset = %d, want %d", got, want)
	}
	if got, want := unsafe.Offsetof(baseStoreParams{}.ListIsLexicallyOrdered), uintptr(64); got != want {
		t.Fatalf("lexical ordering offset = %d, want %d", got, want)
	}
}

func TestNativeWriteOptionsPreservesExplicitEmptyLists(t *testing.T) {
	inputs, err := buildNativeWriteOptions(NativeWriteOptionsSpec{
		InitialBases:             []BasePath{},
		TargetBases:              []uint32{},
		TargetBaseNamesOrPaths:   []string{},
		BaseStoreParams:          map[string]BaseStoreParams{},
		HasTransactionProperties: true,
		TransactionProperties:    nil,
	})
	if err != nil {
		t.Fatalf("buildNativeWriteOptions: %v", err)
	}
	if inputs.raw.InitialBases == nil || inputs.raw.InitialBases.Count != 0 {
		t.Fatalf("initial bases = %+v, want present empty list", inputs.raw.InitialBases)
	}
	if inputs.raw.TargetBases == nil || inputs.raw.TargetBases.Count != 0 {
		t.Fatalf("target bases = %+v, want present empty list", inputs.raw.TargetBases)
	}
	if inputs.raw.TargetBaseNamesOrPaths == nil || inputs.raw.TargetBaseNamesOrPaths.Count != 0 {
		t.Fatalf("target names = %+v, want present empty list", inputs.raw.TargetBaseNamesOrPaths)
	}
	if inputs.raw.BaseStoreParams == nil || inputs.raw.BaseStoreParams.Count != 0 {
		t.Fatalf("base store params = %+v, want present empty list", inputs.raw.BaseStoreParams)
	}
	if inputs.raw.TransactionProperties == 0 {
		t.Fatal("nil transaction properties with HasTransactionProperties were omitted")
	}
}

func TestNativeWriteOptionsListCountsExcludeSentinels(t *testing.T) {
	inputs, err := buildNativeWriteOptions(NativeWriteOptionsSpec{
		TargetBaseNamesOrPaths: []string{"base-a", "base-b"},
		TargetBases:            []uint32{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("buildNativeWriteOptions: %v", err)
	}
	if inputs.raw.TargetBaseNamesOrPaths == nil || inputs.raw.TargetBaseNamesOrPaths.Count != 2 {
		t.Fatalf("target names = %+v, want count 2", inputs.raw.TargetBaseNamesOrPaths)
	}
	if inputs.raw.TargetBases == nil || inputs.raw.TargetBases.Count != 3 {
		t.Fatalf("target bases = %+v, want count 3", inputs.raw.TargetBases)
	}
	if inputs.targetNames.ptrs[inputs.raw.TargetBaseNamesOrPaths.Count] != 0 {
		t.Fatal("target name list lost its trailing NULL sentinel")
	}
}

func TestNativeWriteOptionsBaseListLayout(t *testing.T) {
	inputs, err := buildNativeWriteOptions(NativeWriteOptionsSpec{
		InitialBases: []BasePath{
			{ID: 1, Path: "/base-one", IsDatasetRoot: true},
			{ID: 2, Path: "/base-two", Name: ptrTo("two")},
		},
	})
	if err != nil {
		t.Fatalf("buildNativeWriteOptions: %v", err)
	}
	if inputs.raw.InitialBases == nil || inputs.raw.InitialBases.Count != 2 || inputs.raw.InitialBases.Items == nil {
		t.Fatalf("initial bases header = %+v", inputs.raw.InitialBases)
	}
	first := inputs.raw.InitialBases.Items
	if first.ID != 1 || goString(first.Path) != "/base-one" || first.Name != nil || !first.IsDatasetRoot {
		t.Fatalf("first base = %+v", first)
	}
	second := &inputs.initialBases.raw[1]
	if second.ID != 2 || goString(second.Path) != "/base-two" || second.Name == nil || goString(second.Name) != "two" || second.IsDatasetRoot {
		t.Fatalf("second base = %+v", second)
	}
}

func TestNativeWriteOptionsBaseStorePresence(t *testing.T) {
	offset := 90*time.Second + 500*time.Millisecond
	blockSize := uint64(4096)
	keepParts := false
	lexical := false
	inputs, err := buildNativeWriteOptions(NativeWriteOptionsSpec{
		BaseStoreParams: map[string]BaseStoreParams{
			"base-a": {
				BlockSize:                  &blockSize,
				S3CredentialsRefreshOffset: &offset,
				UseConstantSizeUploadParts: &keepParts,
				ListIsLexicallyOrdered:     &lexical,
			},
		},
	})
	if err != nil {
		t.Fatalf("buildNativeWriteOptions: %v", err)
	}
	if inputs.raw.BaseStoreParams == nil || inputs.raw.BaseStoreParams.Count != 1 {
		t.Fatalf("base store params = %+v, want one entry", inputs.raw.BaseStoreParams)
	}
	raw := inputs.baseStoreList.raw[0]
	if raw.HasBlockSize != 1 || raw.BlockSize != blockSize {
		t.Fatalf("block size presence = %d value %d", raw.HasBlockSize, raw.BlockSize)
	}
	if raw.HasS3CredentialsRefreshOffset != 1 || raw.S3CredentialsRefreshSeconds != 90 || raw.S3CredentialsRefreshNanos != 500000000 {
		t.Fatalf("refresh duration = has %d seconds %d nanos %d", raw.HasS3CredentialsRefreshOffset, raw.S3CredentialsRefreshSeconds, raw.S3CredentialsRefreshNanos)
	}
	if raw.HasUseConstantSizeUploadParts != 1 || raw.UseConstantSizeUploadParts {
		t.Fatalf("constant upload parts = has %d value %v", raw.HasUseConstantSizeUploadParts, raw.UseConstantSizeUploadParts)
	}
	if raw.HasListIsLexicallyOrdered != 1 || raw.ListIsLexicallyOrdered {
		t.Fatalf("lexical ordering = has %d value %v", raw.HasListIsLexicallyOrdered, raw.ListIsLexicallyOrdered)
	}
}

func TestNativeWriteProgressDispatchRouting(t *testing.T) {
	var begins, completes, stats atomic.Int32
	adapter := &nativeWriteProgressAdapter{progress: WriteProgress{
		BeginFragment: func(event WriteFragmentProgress) error {
			if event.FragmentID != 7 || event.PhysicalRows != nil || event.FileCount != 1 {
				t.Errorf("begin event = %+v", event)
			}
			begins.Add(1)
			return nil
		},
		CompleteFragment: func(event WriteFragmentProgress) error {
			if event.FragmentID != 7 || event.PhysicalRows == nil || *event.PhysicalRows != 11 || event.FileCount != 2 {
				t.Errorf("complete event = %+v", event)
			}
			completes.Add(1)
			return nil
		},
		WriteStats: func(event WriteStats) {
			if event.BytesWritten != 1024 || event.RowsWritten != 11 || event.FilesWritten != 3 {
				t.Errorf("stats event = %+v", event)
			}
			stats.Add(1)
		},
	}}
	ctxID := handle.New(adapter)
	defer handle.Take(ctxID)

	begin := nativeWriteFragmentProgress{FragmentID: 7, FileCount: 1}
	var callbackErr callbackError
	if rc := dispatchStatus3(uintptr(ctxID), false, unsafe.Pointer(&begin), &callbackErr); rc != 0 {
		t.Fatalf("begin rc = %d error = %s", rc, goString(callbackErr.Message.Data))
	}
	rows := uint64(11)
	complete := nativeWriteFragmentProgress{FragmentID: 7, HasPhysicalRows: 1, PhysicalRows: rows, FileCount: 2}
	clearCallbackError(&callbackErr)
	if rc := dispatchStatus3(uintptr(ctxID), true, unsafe.Pointer(&complete), &callbackErr); rc != 0 {
		t.Fatalf("complete rc = %d error = %s", rc, goString(callbackErr.Message.Data))
	}
	dispatchVoid2(uintptr(ctxID), unsafe.Pointer(&nativeWriteStats{BytesWritten: 1024, RowsWritten: 11, FilesWritten: 3}))
	if begins.Load() != 1 || completes.Load() != 1 || stats.Load() != 1 {
		t.Fatalf("callbacks = begins %d completes %d stats %d", begins.Load(), completes.Load(), stats.Load())
	}
}

func TestNativeWriteProgressInvalidContextAndPanic(t *testing.T) {
	var callbackErr callbackError
	if rc := dispatchStatus3(0, false, unsafe.Pointer(&nativeWriteFragmentProgress{}), &callbackErr); rc == 0 {
		t.Fatal("dispatchStatus3 accepted a NULL context")
	}
	clearCallbackError(&callbackErr)

	adapter := &nativeWriteProgressAdapter{progress: WriteProgress{
		BeginFragment: func(WriteFragmentProgress) error { panic("begin panic") },
	}}
	ctxID := handle.New(adapter)
	defer handle.Take(ctxID)
	if rc := dispatchStatus3(uintptr(ctxID), false, unsafe.Pointer(&nativeWriteFragmentProgress{}), &callbackErr); rc == 0 {
		t.Fatal("dispatchStatus3 did not contain a callback panic")
	}
	clearCallbackError(&callbackErr)

	// A stats panic has no error channel and must not escape the dispatch.
	statsAdapter := &nativeWriteProgressAdapter{progress: WriteProgress{
		WriteStats: func(WriteStats) { panic("stats panic") },
	}}
	statsCtx := handle.New(statsAdapter)
	defer handle.Take(statsCtx)
	dispatchVoid2(uintptr(statsCtx), unsafe.Pointer(&nativeWriteStats{}))
	dispatchVoid2(0, unsafe.Pointer(&nativeWriteStats{}))
}
