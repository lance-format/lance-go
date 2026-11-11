package ffi

import (
	"encoding/json"
	"fmt"
)

// decodeStringMap decodes a native JSON object containing string keys and
// string values. The native API returns complete maps, so an empty object is a
// valid empty map rather than a nil result.
func DecodeStringMap(raw string) (map[string]string, error) {
	var decoded map[string]string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("ffi: decode metadata/config JSON: %w", err)
	}
	if decoded == nil {
		decoded = make(map[string]string)
	}
	return decoded, nil
}
