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
	if got, want := len(list.ptrs), 3; got != want {
		t.Fatalf("pointer count = %d, want %d", got, want)
	}
	if list.ptrs[2] != 0 {
		t.Fatalf("last pointer = %#x, want NULL", list.ptrs[2])
	}
	if got, want := string(list.bufs[0]), "alpha\x00"; got != want {
		t.Fatalf("first buffer = %q, want %q", got, want)
	}
	if got, want := string(list.bufs[1]), "beta\x00"; got != want {
		t.Fatalf("second buffer = %q, want %q", got, want)
	}
	list.keepAlive()
}

func TestCStringListRejectsEmbeddedNUL(t *testing.T) {
	if _, err := newCStringList([]string{"ok", "bad\x00value"}); err == nil {
		t.Fatal("newCStringList accepted an embedded NUL")
	}
}

func TestStorageOptionListSortsKeys(t *testing.T) {
	list, err := storageOptionList(map[string]string{"b": "2", "a": "1"})
	if err != nil {
		t.Fatalf("storageOptionList: %v", err)
	}
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
	if raw.Kind != int32(CacheSpecURI) || raw.BackendURI == 0 {
		t.Fatalf("URI cache spec = %+v", raw)
	}
	keep.keepAlive()

	raw, keep, err = buildSessionCacheSpec(CacheSpec{
		Kind:        CacheSpecConfig,
		BackendKind: "moka",
		Options:     map[string]string{"capacity": "8", "ttl": "60"},
	})
	if err != nil {
		t.Fatalf("config cache spec: %v", err)
	}
	if raw.BackendKind == 0 || raw.OptionKeys == 0 || raw.OptionValues == 0 || raw.OptionCount != 2 {
		t.Fatalf("config cache spec = %+v", raw)
	}
	keep.keepAlive()

	if _, _, err := buildSessionCacheSpec(CacheSpec{Kind: CacheSpecKind(42)}); err == nil {
		t.Fatal("unknown cache spec kind was accepted")
	}
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
