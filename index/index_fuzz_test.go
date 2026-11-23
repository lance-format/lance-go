package index

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

// FuzzParseUUIDRoundTrip builds the documented UUID string forms from the 128
// bits supplied by the fuzz parameters and checks both directions: ParseUUID
// must recover the original bytes and UUID.String must produce the canonical
// hyphenated lowercase form.
func FuzzParseUUIDRoundTrip(f *testing.F) {
	f.Add(uint64(0), uint64(0))
	f.Add(^uint64(0), ^uint64(0))
	// The example pinned by TestParseUUIDValidForms.
	f.Add(uint64(0x0123456789abcdef), uint64(0x0123456789abcdef))
	f.Add(uint64(0x123e4567e89b12d3), uint64(0xa456426614174000))

	f.Fuzz(func(t *testing.T, high, low uint64) {
		var want UUID
		binary.BigEndian.PutUint64(want[0:8], high)
		binary.BigEndian.PutUint64(want[8:16], low)

		compact := hex.EncodeToString(want[:])
		canonical := compact[0:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:32]
		if got := want.String(); got != canonical {
			t.Fatalf("UUID.String() = %q, want %q", got, canonical)
		}

		forms := []struct {
			name  string
			input string
		}{
			{name: "hyphenated", input: canonical},
			{name: "hyphenated upper", input: strings.ToUpper(canonical)},
			{name: "bare", input: compact},
			{name: "bare upper", input: strings.ToUpper(compact)},
		}
		for _, form := range forms {
			got, err := ParseUUID(form.input)
			if err != nil {
				t.Fatalf("ParseUUID(%q): %v", form.input, err)
			}
			if got != want {
				t.Fatalf("ParseUUID(%q) = %x, want %x", form.input, got, want)
			}
		}
	})
}
