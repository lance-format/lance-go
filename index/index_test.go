package index

import (
	"strings"
	"testing"
)

// TestParseUUIDValidForms covers the documented UUID inputs: hyphenated and
// bare 32-character hexadecimal forms, in either letter case. Every form must
// normalize to the canonical lowercase hyphenated string.
func TestParseUUIDValidForms(t *testing.T) {
	const hyphenated = "01234567-89ab-cdef-0123-456789abcdef"
	const bare = "0123456789abcdef0123456789abcdef"

	cases := []struct {
		name  string
		input string
	}{
		{name: "hyphenated", input: hyphenated},
		{name: "hyphenated upper", input: strings.ToUpper(hyphenated)},
		{name: "bare", input: bare},
		{name: "bare upper", input: strings.ToUpper(bare)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uuid, err := ParseUUID(tc.input)
			if err != nil {
				t.Fatalf("ParseUUID(%q): %v", tc.input, err)
			}
			if got := uuid.String(); got != hyphenated {
				t.Fatalf("ParseUUID(%q).String() = %q, want %q", tc.input, got, hyphenated)
			}
		})
	}
}
