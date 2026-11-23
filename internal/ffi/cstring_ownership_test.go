package ffi

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/cdata"
)

// listBaseline returns the number of string lists that currently hold pins.
// Each test compares it with a later reading so a missed release fails with the
// list ownership count instead of only surfacing as a fatal runtime.Pinner
// panic in a finalizer.
func listBaseline() int64 {
	return acquiredLists.Load()
}

func assertNoLiveLists(t *testing.T, baseline int64) {
	t.Helper()
	if got := acquiredLists.Load(); got != baseline {
		t.Fatalf("live pinned string lists = %d, want %d: a list was not released", got, baseline)
	}
}

func TestCStringListPinAccounting(t *testing.T) {
	baseline := listBaseline()
	list, err := newCStringList([]string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("newCStringList: %v", err)
	}
	if !list.acquired {
		t.Fatal("constructed list does not hold pins")
	}
	if got, want := listBaseline(), baseline+1; got != want {
		t.Fatalf("live pinned string lists = %d, want %d", got, want)
	}
	list.release()
	if list.acquired {
		t.Fatal("released list still holds pins")
	}
	assertNoLiveLists(t, baseline)
	// A second release must not change the accounting.
	list.release()
	assertNoLiveLists(t, baseline)

	present, err := newPresentCStringList(nil)
	if err != nil {
		t.Fatalf("newPresentCStringList: %v", err)
	}
	if !present.acquired {
		t.Fatal("present empty list does not hold its pointer array")
	}
	if got, want := listBaseline(), baseline+1; got != want {
		t.Fatalf("live pinned string lists = %d, want %d", got, want)
	}
	present.release()
	assertNoLiveLists(t, baseline)
}

func TestBuildFileReaderOptionsReleaseAccounting(t *testing.T) {
	baseline := listBaseline()
	raw, keep, err := buildFileReaderOptions(&FileReaderOptions{Columns: []string{"id", "name"}})
	if err != nil {
		t.Fatalf("buildFileReaderOptions: %v", err)
	}
	if raw.Columns == nil {
		t.Fatal("column pointer array was not forwarded")
	}
	if got, want := listBaseline(), baseline+1; got != want {
		t.Fatalf("live pinned string lists = %d, want %d", got, want)
	}
	keep.release()
	assertNoLiveLists(t, baseline)

	if _, _, err := buildFileReaderOptions(&FileReaderOptions{Columns: []string{"bad\x00name"}}); err == nil {
		t.Fatal("buildFileReaderOptions accepted an embedded NUL")
	}
	assertNoLiveLists(t, baseline)
}

func TestBuildFileWriterOptionsReleaseAccounting(t *testing.T) {
	baseline := listBaseline()
	raw, keep, err := buildFileWriterOptions(&FileWriterOptions{Version: "v1"})
	if err != nil {
		t.Fatalf("buildFileWriterOptions: %v", err)
	}
	if raw.Version == nil {
		t.Fatal("version pointer was not forwarded")
	}
	if got, want := listBaseline(), baseline+1; got != want {
		t.Fatalf("live pinned string lists = %d, want %d", got, want)
	}
	keep.release()
	assertNoLiveLists(t, baseline)
}

func TestOpenFileReaderReleasesColumnsOnSchedulerError(t *testing.T) {
	baseline := listBaseline()
	b := &bindings{}
	_, err := openFileReader(b, "file:///tmp/reader.lance", nil, &FileReaderOptions{Columns: []string{"id"}}, nil, &FileSchedulerConfig{LiteMode: 99})
	if err == nil || !strings.Contains(err.Error(), "scheduler") {
		t.Fatalf("openFileReader error = %v, want invalid scheduler mode", err)
	}
	assertNoLiveLists(t, baseline)
}

func TestOpenFileWriterReleasesVersionOnSchemaError(t *testing.T) {
	baseline := listBaseline()
	b := &bindings{}
	session := &FileSession{b: b, handle: 1}
	// A dictionary with a non-integer index cannot be exported, so the version
	// list is built before this failure and must be released.
	schema := arrow.NewSchema([]arrow.Field{{
		Name: "words",
		Type: &arrow.DictionaryType{IndexType: arrow.BinaryTypes.String, ValueType: arrow.BinaryTypes.String},
	}}, nil)
	_, err := session.OpenWriter("words.lance", schema, &FileWriterOptions{Version: "v1"})
	if err == nil || !strings.Contains(err.Error(), "export schema") {
		t.Fatalf("OpenWriter error = %v, want schema export failure", err)
	}
	assertNoLiveLists(t, baseline)
}

func TestBuildSessionCacheSpecReleasesPartialOwnership(t *testing.T) {
	baseline := listBaseline()
	// The keys build first; the value list then fails, so the already pinned
	// key list and backend kind must be released by the failed build.
	_, _, err := buildSessionCacheSpec(CacheSpec{
		Kind:        CacheSpecConfig,
		BackendKind: "moka",
		Options:     map[string]string{"capacity": "bad\x00value"},
	})
	if err == nil {
		t.Fatal("buildSessionCacheSpec accepted a NUL in an option value")
	}
	assertNoLiveLists(t, baseline)

	_, _, err = buildSessionCacheSpec(CacheSpec{
		Kind:        CacheSpecConfig,
		BackendKind: "moka",
		Options:     map[string]string{"": "value"},
	})
	if err == nil {
		t.Fatal("buildSessionCacheSpec accepted an empty option key")
	}
	assertNoLiveLists(t, baseline)

	if _, _, err := buildSessionCacheSpec(CacheSpec{Kind: CacheSpecKind(42)}); err == nil {
		t.Fatal("buildSessionCacheSpec accepted an unknown cache kind")
	}
	assertNoLiveLists(t, baseline)
}

func TestBuildCommitOptionsReleasesPartialVersion(t *testing.T) {
	baseline := listBaseline()
	version := "v1"
	_, _, _, err := buildCommitOptions(CommitOptions{
		DataStorageVersion: &version,
		StorageOptions:     map[string]string{"bad\x00key": "value"},
	})
	if err == nil {
		t.Fatal("buildCommitOptions accepted a NUL in a storage option key")
	}
	assertNoLiveLists(t, baseline)
}

func TestCommitReleasesOptionsOnInvalidURI(t *testing.T) {
	baseline := listBaseline()
	b := &bindings{}
	tx := &Transaction{b: b}
	tx.handle.Store(1)
	version := "v1"
	_, err := commit("bad\x00uri", tx, CommitOptions{
		DataStorageVersion: &version,
		StorageOptions:     map[string]string{"region": "west"},
	}, nil, nil)
	if err == nil {
		t.Fatal("commit accepted an embedded NUL in the URI")
	}
	assertNoLiveLists(t, baseline)
}

func TestCreateBranchReleasesStorageOnReferenceError(t *testing.T) {
	baseline := listBaseline()
	b := &bindings{
		refVersion: func(string, bool, uint64) uintptr { return 0 },
		lastErrorCode: func() int32 {
			return int32(CodeInvalidArgument)
		},
		lastErrorMessage: func() *byte { return nil },
	}
	d := &Dataset{b: b}
	d.handle.Store(1)
	_, err := d.CreateBranch("feature", Ref{Kind: RefBranchLatest, Branch: "main"}, &BranchOptions{
		StorageOptions: map[string]string{"region": "west"},
	})
	if err == nil {
		t.Fatal("CreateBranch accepted a failed native reference")
	}
	assertNoLiveLists(t, baseline)
}

// flakySchemaReader returns a schema on the first call and nil afterwards. The
// option builders validate the schema before the native call, and stream
// export reads it again; this models an export failure after the string lists
// were already built.
type flakySchemaReader struct {
	array.RecordReader
	calls int
}

func (r *flakySchemaReader) Schema() *arrow.Schema {
	r.calls++
	if r.calls > 1 {
		return nil
	}
	return r.RecordReader.Schema()
}

func TestCreateFragmentReleasesOptionsOnStreamExportError(t *testing.T) {
	baseline := listBaseline()
	b := &bindings{}
	reader := &flakySchemaReader{RecordReader: newTrackedReader(t)}
	_, err := createFragment(b, "file:///tmp/fragment.lance", 1, reader, FragmentCreateOptions{
		Mode:           WriteModeCreate,
		Write:          &FragmentWriteOptions{Params: WriteParams{DataStorageVersion: "v1"}},
		StorageOptions: map[string]string{"region": "west"},
	})
	if err == nil {
		t.Fatal("createFragment accepted a reader that lost its schema during export")
	}
	assertNoLiveLists(t, baseline)
}

func TestWriteFragmentsReleasesOptionsOnStreamExportError(t *testing.T) {
	baseline := listBaseline()
	b := &bindings{}
	reader := &flakySchemaReader{RecordReader: newTrackedReader(t)}
	_, err := writeFragments(b, "file:///tmp/fragments.lance", reader, FragmentCreateOptions{
		Mode:           WriteModeCreate,
		Write:          &FragmentWriteOptions{Params: WriteParams{DataStorageVersion: "v1"}},
		StorageOptions: map[string]string{"region": "west"},
	})
	if err == nil {
		t.Fatal("writeFragments accepted a reader that lost its schema during export")
	}
	assertNoLiveLists(t, baseline)
}

func TestCreateFragmentReleasesOptionsOnNativeError(t *testing.T) {
	baseline := listBaseline()
	b := &bindings{
		fragmentCreate: func(string, uint64, *fragmentCreateOptions, *cdata.ArrowArrayStream, *uintptr) int32 {
			return 1
		},
		lastErrorCode: func() int32 {
			return int32(CodeInvalidArgument)
		},
		lastErrorMessage: func() *byte { return nil },
	}
	_, err := createFragment(b, "file:///tmp/fragment.lance", 1, newTrackedReader(t), FragmentCreateOptions{
		Mode:           WriteModeCreate,
		Write:          &FragmentWriteOptions{Params: WriteParams{DataStorageVersion: "v1"}},
		StorageOptions: map[string]string{"region": "west"},
	})
	if err == nil {
		t.Fatal("createFragment ignored a native error")
	}
	assertNoLiveLists(t, baseline)
}

func TestStorageOptionsAccessorReleaseAccounting(t *testing.T) {
	baseline := listBaseline()
	b := &bindings{
		storageAccessorNew:   func(*storageOptionsInput, uintptr) uintptr { return 1 },
		storageAccessorClose: func(uintptr) {},
	}
	accessor, err := newStorageOptionsAccessorWithPresence(b, map[string]string{"region": "west"}, true, 0)
	if err != nil {
		t.Fatalf("newStorageOptionsAccessorWithPresence: %v", err)
	}
	accessor.Close()
	assertNoLiveLists(t, baseline)
}
