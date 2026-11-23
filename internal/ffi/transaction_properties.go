package ffi

import (
	"runtime"
	"sort"
)

// transactionProperties mirrors LanceTransactionProperties.
//
// Native code reads keys and values as parallel arrays of NUL-terminated
// strings, with Count entries. A nil map is passed as a NULL pointer and means native None;
// a non-nil map, including an empty one, is passed as a present map.
type transactionProperties struct {
	Keys   **byte
	Values **byte
	Count  uint64
}

// newTransactionProperties builds the C input for transaction properties.
//
// A nil properties map returns a nil pointer, which native entry points read
// as None. A non-nil map returns a present struct with keys sorted for
// deterministic wire order; an empty map keeps NULL key and value arrays with
// Count zero. The returned release function must run after the native call:
// the struct holds pinned string pointers that must be unpinned only once the
// call returns.
func newTransactionProperties(properties map[string]string) (*transactionProperties, func(), error) {
	if properties == nil {
		return nil, func() {}, nil
	}

	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = properties[key]
	}
	keyList, err := newCStringList(keys)
	if err != nil {
		return nil, nil, err
	}
	valueList, err := newCStringList(values)
	if err != nil {
		keyList.release()
		return nil, nil, err
	}

	raw := &transactionProperties{Count: uint64(len(keys))}
	if len(keys) > 0 {
		raw.Keys = keyList.data()
		raw.Values = valueList.data()
	}
	return raw, func() {
		keyList.release()
		valueList.release()
		runtime.KeepAlive(raw)
	}, nil
}
