package ffi

import (
	"strings"
	"testing"
)

func TestCStringListTerminatesAndPreservesOrder(t *testing.T) {
	list, err := newCStringList([]string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("newCStringList: %v", err)
	}
	defer list.release()
	if got, want := len(list.ptrs), 3; got != want {
		t.Fatalf("pointer count = %d, want %d", got, want)
	}
	if list.ptrs[2] != nil {
		t.Fatalf("last pointer = %p, want NULL", list.ptrs[2])
	}
	if got, want := string(list.bufs[0]), "alpha\x00"; got != want {
		t.Fatalf("first buffer = %q, want %q", got, want)
	}
	if got, want := string(list.bufs[1]), "beta\x00"; got != want {
		t.Fatalf("second buffer = %q, want %q", got, want)
	}
	if got := list.first(); got != list.ptrs[0] {
		t.Fatalf("first() = %p, want %p", got, list.ptrs[0])
	}
	if got := list.data(); got == nil || *got != list.ptrs[0] {
		t.Fatalf("data() = %p, want address of the first pointer entry", got)
	}
}

func TestCStringListRejectsEmbeddedNUL(t *testing.T) {
	if _, err := newCStringList([]string{"ok", "bad\x00value"}); err == nil {
		t.Fatal("newCStringList accepted an embedded NUL")
	}
	if _, err := newNullableCStringList([]*string{nil, strPtr("bad\x00value")}); err == nil {
		t.Fatal("newNullableCStringList accepted an embedded NUL")
	}
}

func TestCStringListAbsentAndPresentEmpty(t *testing.T) {
	absent, err := newCStringList(nil)
	if err != nil {
		t.Fatalf("newCStringList(nil): %v", err)
	}
	defer absent.release()
	if absent.first() != nil || absent.data() != nil {
		t.Fatal("absent list must present NULL to C")
	}

	present, err := newPresentCStringList(nil)
	if err != nil {
		t.Fatalf("newPresentCStringList(nil): %v", err)
	}
	defer present.release()
	if present.data() == nil {
		t.Fatal("present empty list must pass a non-NULL pointer")
	}
	if present.first() != nil {
		t.Fatal("present empty list must start with a NULL entry")
	}
	if len(present.ptrs) != 1 {
		t.Fatalf("present empty list pointers = %#v, want one NULL entry", present.ptrs)
	}
}

func TestNullableCStringListKeepsNilEntries(t *testing.T) {
	empty := ""
	list, err := newNullableCStringList([]*string{strPtr("value"), nil, &empty})
	if err != nil {
		t.Fatalf("newNullableCStringList: %v", err)
	}
	defer list.release()
	if list.ptrs[0] == nil || goString(list.ptrs[0]) != "value" {
		t.Fatalf("first entry = %v, want %q", list.ptrs[0], "value")
	}
	if list.ptrs[1] != nil {
		t.Fatal("nil value must stay NULL for deletion semantics")
	}
	if list.ptrs[2] == nil || goString(list.ptrs[2]) != "" {
		t.Fatal("non-nil empty string must stay a real value")
	}
	if list.ptrs[3] != nil {
		t.Fatal("nullable list lost its trailing NULL sentinel")
	}
}

func TestCStringPairsAlternateAndTerminate(t *testing.T) {
	list, err := newCStringPairs([]string{"a", "b"}, []string{"1", "2"})
	if err != nil {
		t.Fatalf("newCStringPairs: %v", err)
	}
	defer list.release()
	want := []string{"a", "1", "b", "2"}
	for i, value := range want {
		if list.ptrs[i] == nil || goString(list.ptrs[i]) != value {
			t.Fatalf("pointer %d = %q, want %q", i, goString(list.ptrs[i]), value)
		}
	}
	if list.ptrs[len(want)] != nil {
		t.Fatal("pair list lost its trailing NULL sentinel")
	}

	if _, err := newCStringPairs([]string{"a"}, []string{"1", "2"}); err == nil {
		t.Fatal("newCStringPairs accepted mismatched key/value lengths")
	}
}

func TestCStringListReleaseIsIdempotent(t *testing.T) {
	baseline := listBaseline()
	list, err := newCStringList([]string{"alpha"})
	if err != nil {
		t.Fatalf("newCStringList: %v", err)
	}
	list.release()
	list.release()
	var nilList *cStringList
	nilList.release()
	assertNoLiveLists(t, baseline)
}

func TestStorageOptionListSortsKeys(t *testing.T) {
	list, err := storageOptionList(map[string]string{"b": "2", "a": "1"})
	if err != nil {
		t.Fatalf("storageOptionList: %v", err)
	}
	defer list.release()
	got := make([]string, 0, len(list.bufs))
	for _, buf := range list.bufs {
		got = append(got, strings.TrimSuffix(string(buf), "\x00"))
	}
	want := []string{"a", "1", "b", "2"}
	if len(got) != len(want) {
		t.Fatalf("options = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("options = %v, want %v", got, want)
		}
	}
}

func TestBuildSessionCacheSpec(t *testing.T) {
	raw, keep, err := buildSessionCacheSpec(CacheSpec{Kind: CacheSpecURI, URI: "memory://index"})
	if err != nil {
		t.Fatalf("URI cache spec: %v", err)
	}
	if raw.Kind != int32(CacheSpecURI) || raw.BackendURI == nil {
		t.Fatalf("URI cache spec = %+v", raw)
	}
	keep.release()

	raw, keep, err = buildSessionCacheSpec(CacheSpec{
		Kind:        CacheSpecConfig,
		BackendKind: "moka",
		Options:     map[string]string{"capacity": "8", "ttl": "60"},
	})
	if err != nil {
		t.Fatalf("config cache spec: %v", err)
	}
	if raw.BackendKind == nil || raw.OptionKeys == nil || raw.OptionValues == nil || raw.OptionCount != 2 {
		t.Fatalf("config cache spec = %+v", raw)
	}
	keep.release()

	// Failed constructions are covered by
	// TestBuildSessionCacheSpecReleasesPartialOwnership.
}

func TestOpenDatasetWithOptionsRejectsNULBeforeNative(t *testing.T) {
	b := &bindings{}
	if _, err := openDatasetWithOptions(b, "file:///bad\x00uri", OpenOptions{}); err == nil {
		t.Fatal("openDatasetWithOptions accepted an embedded NUL")
	}
}

func TestDatasetVersionReportsNativeError(t *testing.T) {
	message := []byte("missing\x00")
	b := &bindings{
		datasetVersion: func(uintptr) uint64 { return 0 },
		lastErrorCode:  func() int32 { return int32(CodeNotFound) },
		lastErrorMessage: func() *byte {
			return &message[0]
		},
		freeString: func(*byte) {},
	}
	dataset := &Dataset{b: b}
	dataset.handle.Store(1)
	defer dataset.handle.Store(0)

	if _, err := dataset.Version(); err == nil {
		t.Fatal("Version returned nil error for a zero version with a native error")
	} else if nativeErr, ok := err.(*Error); !ok || nativeErr.Code != CodeNotFound {
		t.Fatalf("Version error = %v, want native CodeNotFound", err)
	}
}

func strPtr(value string) *string {
	return &value
}
