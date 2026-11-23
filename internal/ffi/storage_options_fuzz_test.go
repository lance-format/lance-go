package ffi

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// FuzzStorageOptionsMarshalling builds a bounded nonempty storage-options map
// from supported Go key/value inputs and checks the marshalled snapshot against
// an independently sorted expectation. It covers the ordinary mapping only:
// nil, empty and invalid inputs are pinned by the existing storage-option and
// C-string tests.
func FuzzStorageOptionsMarshalling(f *testing.F) {
	f.Add(uint8(0), "region", "west", false)
	f.Add(uint8(2), "key", "", true)
	f.Add(uint8(7), "key with spaces", "value-ünïcode", false)
	f.Add(uint8(1), "", "v", false)

	f.Fuzz(func(t *testing.T, count uint8, keySeed, valueSeed string, emptyValues bool) {
		n := int(count)%8 + 1
		options := make(map[string]string, n)
		for i := 0; i < n; i++ {
			options[marshallingKey(keySeed, i)] = marshallingValue(valueSeed, i, emptyValues)
		}

		snapshot, err := newFetchedStorageOptions(options)
		if err != nil {
			t.Fatalf("newFetchedStorageOptions(%v): %v", options, err)
		}
		defer snapshot.releaseContext()

		keys := make([]string, 0, len(options))
		for key := range options {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		if len(snapshot.keyBufs) != len(keys) || len(snapshot.valueBufs) != len(keys) {
			t.Fatalf("marshalled %d/%d key/value buffers, want %d", len(snapshot.keyBufs), len(snapshot.valueBufs), len(keys))
		}
		if len(snapshot.keyPtrs) != len(keys) || len(snapshot.valuePtrs) != len(keys) {
			t.Fatalf("marshalled %d/%d key/value pointers, want %d", len(snapshot.keyPtrs), len(snapshot.valuePtrs), len(keys))
		}
		if snapshot.keysPtr() != &snapshot.keyPtrs[0] {
			t.Fatal("keysPtr does not address the key pointer slice")
		}
		if snapshot.valuesPtr() != &snapshot.valuePtrs[0] {
			t.Fatal("valuesPtr does not address the value pointer slice")
		}
		for i, key := range keys {
			wantKey := append([]byte(key), 0)
			if !bytes.Equal(snapshot.keyBufs[i], wantKey) {
				t.Fatalf("key buffer %d = %q, want %q", i, snapshot.keyBufs[i], wantKey)
			}
			wantValue := append([]byte(options[key]), 0)
			if !bytes.Equal(snapshot.valueBufs[i], wantValue) {
				t.Fatalf("value buffer for %q = %q, want %q", key, snapshot.valueBufs[i], wantValue)
			}
			if snapshot.keyPtrs[i] != &snapshot.keyBufs[i][0] {
				t.Fatalf("key pointer %d does not address its owned buffer", i)
			}
			if snapshot.valuePtrs[i] != &snapshot.valueBufs[i][0] {
				t.Fatalf("value pointer %d does not address its owned buffer", i)
			}
		}
	})
}

// marshallingKey derives a unique, nonempty key that the storage-option
// validator accepts. Empty keys and embedded NUL bytes are pinned by the
// existing invalid-input tests.
func marshallingKey(seed string, index int) string {
	seed = boundedMarshallingText(seed, 24)
	return fmt.Sprintf("%s#%d", seed, index)
}

// marshallingValue derives a supported value. Empty values are valid and are
// covered when emptyValues is set.
func marshallingValue(seed string, index int, empty bool) string {
	if empty {
		return ""
	}
	seed = boundedMarshallingText(seed, 32)
	return fmt.Sprintf("%s#%d", seed, index)
}

func boundedMarshallingText(text string, limit int) string {
	text = strings.ReplaceAll(text, "\x00", "")
	if len(text) > limit {
		text = text[:limit]
	}
	return text
}
