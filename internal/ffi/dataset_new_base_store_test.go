package ffi

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestOpenDatasetWithBaseStoreParamsPreservesNilAndEmpty(t *testing.T) {
	var legacyCalls, baseCalls int
	var gotHeader *baseStoreParamsListHeader
	b := &bindings{
		datasetOpen: func(string, []uintptr, uint64) uintptr {
			legacyCalls++
			return 0x1111
		},
		datasetClose: func(uintptr) {},
	}
	b.baseStoreOpen.datasetOpenWithBaseStoreParams = func(_ string, _ *datasetOpenOptions, _ uintptr, header *baseStoreParamsListHeader) uintptr {
		baseCalls++
		gotHeader = header
		return 0x2222
	}

	legacy, err := openDatasetWithOptions(b, "file:///data", OpenOptions{})
	if err != nil {
		t.Fatalf("nil base store params: %v", err)
	}
	legacy.Close()
	if legacyCalls != 1 || baseCalls != 0 {
		t.Fatalf("nil base store params calls = legacy %d base %d, want 1/0", legacyCalls, baseCalls)
	}

	present, err := openDatasetWithOptions(b, "file:///data", OpenOptions{BaseStoreParams: map[string]BaseStoreParams{}})
	if err != nil {
		t.Fatalf("present empty base store params: %v", err)
	}
	present.Close()
	if legacyCalls != 1 || baseCalls != 1 {
		t.Fatalf("present empty calls = legacy %d base %d, want 1/1", legacyCalls, baseCalls)
	}
	if gotHeader == nil || gotHeader.Items != nil || gotHeader.Count != 0 {
		t.Fatalf("present empty header = %+v, want non-nil count 0 with no items", gotHeader)
	}
}

func TestOpenWithoutBaseStoreParamsDoesNotRequireOptionalSymbol(t *testing.T) {
	var gotURI string
	var gotVersion uint64
	b := &bindings{
		datasetOpen: func(uri string, storageOpts []uintptr, version uint64) uintptr {
			gotURI = uri
			gotVersion = version
			if len(storageOpts) == 0 {
				t.Error("legacy open lost its static storage options")
			}
			return 0x1234
		},
		datasetClose: func(uintptr) {},
	}

	dataset, err := openDatasetWithOptions(b, "file:///data", OpenOptions{
		StorageOptions: map[string]string{"key": "value"},
		Version:        3,
	})
	if err != nil {
		t.Fatalf("open without base store params: %v", err)
	}
	dataset.Close()
	if gotURI != "file:///data" || gotVersion != 3 {
		t.Fatalf("legacy open received uri=%q version=%d", gotURI, gotVersion)
	}
	if b.baseStoreOpen.datasetOpenWithBaseStoreParams != nil {
		t.Fatal("test binding unexpectedly resolved the optional symbol")
	}
}

func TestOpenDatasetWithBaseStoreParamsRequiresOptionalSymbol(t *testing.T) {
	b := &bindings{
		datasetOpen: func(string, []uintptr, uint64) uintptr {
			t.Fatal("legacy open was used for a non-nil base store params map")
			return 0
		},
	}

	_, err := openDatasetWithOptions(b, "file:///data", OpenOptions{
		BaseStoreParams: map[string]BaseStoreParams{},
	})
	var nativeErr *Error
	if !errors.As(err, &nativeErr) {
		t.Fatalf("missing optional symbol error = %v, want *ffi.Error", err)
	}
	if nativeErr.Code != CodeNotSupported {
		t.Fatalf("missing optional symbol code = %v, want %v", nativeErr.Code, CodeNotSupported)
	}
	if !strings.Contains(nativeErr.Message, "lance_dataset_open_with_base_store_params") {
		t.Fatalf("missing optional symbol message = %q", nativeErr.Message)
	}
}

func TestOpenDatasetWithBaseStoreParamsMarshalsExactKeysAndScalars(t *testing.T) {
	blockSize := uint64(4096)
	refresh := 90*time.Second + 500*time.Nanosecond
	useConstant := false
	lexical := false
	accessor := &StorageOptionsAccessor{handle: 0x1234}
	var got *baseStoreParamsListHeader
	b := &bindings{datasetClose: func(uintptr) {}}
	b.baseStoreOpen.datasetOpenWithBaseStoreParams = func(_ string, options *datasetOpenOptions, session uintptr, header *baseStoreParamsListHeader) uintptr {
		if options == nil || options.Version != 7 || options.StorageOptionsAccessor != 0 {
			t.Errorf("open options = %+v, want version 7 without a dataset accessor", options)
		}
		if session != 0 {
			t.Errorf("session = %#x, want 0", session)
		}
		got = header
		return 0x1
	}

	params := map[string]BaseStoreParams{
		"base-b/../exact": {
			StorageOptionsAccessor:     accessor,
			BlockSize:                  &blockSize,
			S3CredentialsRefreshOffset: &refresh,
			UseConstantSizeUploadParts: &useConstant,
			ListIsLexicallyOrdered:     &lexical,
		},
		"base-a": {},
	}
	dataset, err := openDatasetWithOptions(b, "memory://data", OpenOptions{
		Version:         7,
		BaseStoreParams: params,
	})
	if err != nil {
		t.Fatalf("open with base store params: %v", err)
	}
	dataset.Close()

	if got == nil || got.Items == nil || got.Count != 2 {
		t.Fatalf("base store params header = %+v, want two entries", got)
	}
	items := unsafe.Slice(got.Items, got.Count)
	if key := goString(items[0].BasePath); key != "base-a" {
		t.Fatalf("first key = %q, want base-a", key)
	}
	if items[0].StorageOptionsAccessor != 0 {
		t.Fatalf("absent accessor = %#x, want 0", items[0].StorageOptionsAccessor)
	}
	if items[0].HasBlockSize != 0 || items[0].HasS3CredentialsRefreshOffset != 0 ||
		items[0].HasUseConstantSizeUploadParts != 0 || items[0].HasListIsLexicallyOrdered != 0 {
		t.Fatalf("absent scalars set presence flags: %+v", items[0])
	}

	second := items[1]
	if key := goString(second.BasePath); key != "base-b/../exact" {
		t.Fatalf("second key = %q, want exact base-b/../exact", key)
	}
	if second.StorageOptionsAccessor != 0x1234 {
		t.Fatalf("accessor = %#x, want 0x1234", second.StorageOptionsAccessor)
	}
	if second.HasBlockSize != 1 || second.BlockSize != blockSize {
		t.Fatalf("block size = has %d value %d", second.HasBlockSize, second.BlockSize)
	}
	if second.HasS3CredentialsRefreshOffset != 1 || second.S3CredentialsRefreshSeconds != 90 || second.S3CredentialsRefreshNanos != 500 {
		t.Fatalf("refresh = has %d seconds %d nanos %d", second.HasS3CredentialsRefreshOffset, second.S3CredentialsRefreshSeconds, second.S3CredentialsRefreshNanos)
	}
	if second.HasUseConstantSizeUploadParts != 1 || second.UseConstantSizeUploadParts {
		t.Fatalf("constant upload parts = has %d value %v", second.HasUseConstantSizeUploadParts, second.UseConstantSizeUploadParts)
	}
	if second.HasListIsLexicallyOrdered != 1 || second.ListIsLexicallyOrdered {
		t.Fatalf("lexical ordering = has %d value %v", second.HasListIsLexicallyOrdered, second.ListIsLexicallyOrdered)
	}
}

func TestOpenDatasetWithBaseStoreParamsForwardsSessionAndAccessor(t *testing.T) {
	session := &Session{}
	session.handle.Store(0x5678)
	accessor := &StorageOptionsAccessor{handle: 0x9abc}
	var gotVersion uint64
	var gotAccessor, gotSession uintptr
	b := &bindings{datasetClose: func(uintptr) {}}
	b.baseStoreOpen.datasetOpenWithBaseStoreParams = func(_ string, options *datasetOpenOptions, session uintptr, _ *baseStoreParamsListHeader) uintptr {
		gotVersion = options.Version
		gotAccessor = options.StorageOptionsAccessor
		gotSession = session
		return 0x1
	}

	dataset, err := openDatasetWithOptions(b, "memory://data", OpenOptions{
		Version:                3,
		StorageOptionsAccessor: accessor,
		Session:                session,
		BaseStoreParams:        map[string]BaseStoreParams{},
	})
	if err != nil {
		t.Fatalf("open with base store params: %v", err)
	}
	dataset.Close()
	if gotVersion != 3 || gotAccessor != 0x9abc || gotSession != 0x5678 {
		t.Fatalf("forwarded version=%d accessor=%#x session=%#x", gotVersion, gotAccessor, gotSession)
	}
}

func TestOpenDatasetWithBaseStoreParamsCapturesNativeError(t *testing.T) {
	message := []byte("base params rejected\x00")
	b := &bindings{
		lastErrorCode: func() int32 { return int32(CodeInvalidArgument) },
		lastErrorMessage: func() *byte {
			return &message[0]
		},
		freeString: func(*byte) {},
	}
	b.baseStoreOpen.datasetOpenWithBaseStoreParams = func(string, *datasetOpenOptions, uintptr, *baseStoreParamsListHeader) uintptr {
		return 0
	}

	_, err := openDatasetWithOptions(b, "memory://data", OpenOptions{
		BaseStoreParams: map[string]BaseStoreParams{},
	})
	var nativeErr *Error
	if !errors.As(err, &nativeErr) {
		t.Fatalf("native failure = %v, want *ffi.Error", err)
	}
	if nativeErr.Code != CodeInvalidArgument || nativeErr.Message != "base params rejected" {
		t.Fatalf("native failure = code %v message %q", nativeErr.Code, nativeErr.Message)
	}
}

func TestOpenDatasetWithBaseStoreParamsRejectsUnsupportedRoutes(t *testing.T) {
	lock := &CommitLock{}
	lock.handle.Store(1)

	for _, tc := range []struct {
		name string
		opts OpenOptions
		want string
	}{
		{
			name: "commit lock",
			opts: OpenOptions{CommitLock: lock, BaseStoreParams: map[string]BaseStoreParams{}},
			want: "commit lock",
		},
		{
			name: "static storage options",
			opts: OpenOptions{StorageOptions: map[string]string{}, BaseStoreParams: map[string]BaseStoreParams{}},
			want: "static storage options",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &bindings{}
			b.baseStoreOpen.datasetOpenWithBaseStoreParams = func(string, *datasetOpenOptions, uintptr, *baseStoreParamsListHeader) uintptr {
				t.Fatal("optional per-base open was called for an unsupported combination")
				return 0
			}
			_, err := openDatasetWithOptions(b, "memory://data", tc.opts)
			var nativeErr *Error
			if !errors.As(err, &nativeErr) {
				t.Fatalf("error = %v, want *ffi.Error", err)
			}
			if nativeErr.Code != CodeInvalidArgument || !strings.Contains(nativeErr.Message, tc.want) {
				t.Fatalf("error = code %v message %q, want invalid argument containing %q", nativeErr.Code, nativeErr.Message, tc.want)
			}
		})
	}
}
