package compaction_test

import (
	"testing"

	"github.com/lance-format/lance-go/compaction"
)

func TestOptionalStates(t *testing.T) {
	tests := []struct {
		name     string
		optional compaction.Optional[int]
		wantSet  bool
		wantNone bool
		want     int
		wantOK   bool
	}{
		{
			name: "zero value is unset",
		},
		{
			name:     "Unset",
			optional: compaction.Unset[int](),
		},
		{
			name:     "None",
			optional: compaction.None[int](),
			wantSet:  true,
			wantNone: true,
		},
		{
			name:     "Some zero",
			optional: compaction.Some(0),
			wantSet:  true,
			wantOK:   true,
		},
		{
			name:     "Some value",
			optional: compaction.Some(-3),
			wantSet:  true,
			want:     -3,
			wantOK:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.optional.IsSet(); got != test.wantSet {
				t.Errorf("IsSet() = %v, want %v", got, test.wantSet)
			}
			if got := test.optional.IsNone(); got != test.wantNone {
				t.Errorf("IsNone() = %v, want %v", got, test.wantNone)
			}
			got, ok := test.optional.Get()
			if got != test.want || ok != test.wantOK {
				t.Errorf("Get() = (%d, %v), want (%d, %v)", got, ok, test.want, test.wantOK)
			}
		})
	}
}

// TestOptionalSomeZeroIsNotUnset guards the contract that an explicit zero or
// empty value is forwarded to native instead of being treated as unset. The
// distinction matters for DataStorageVersion, where Some("") is an explicit
// invalid value rather than an absent option.
func TestOptionalSomeZeroIsNotUnset(t *testing.T) {
	version := compaction.Some("")
	if !version.IsSet() || version.IsNone() {
		t.Fatalf("Some(\"\") state = set %v, none %v; want set, not none", version.IsSet(), version.IsNone())
	}
	if value, ok := version.Get(); !ok || value != "" {
		t.Fatalf("Some(\"\").Get() = (%q, %v), want (\"\", true)", value, ok)
	}

	absent := compaction.Optional[string]{}
	if absent.IsSet() || absent.IsNone() {
		t.Fatalf("zero Optional state = set %v, none %v; want both false", absent.IsSet(), absent.IsNone())
	}
	if value, ok := absent.Get(); ok || value != "" {
		t.Fatalf("zero Optional.Get() = (%q, %v), want (\"\", false)", value, ok)
	}
}

func TestModeString(t *testing.T) {
	tests := []struct {
		mode compaction.Mode
		want string
	}{
		{mode: compaction.ModeReencode, want: "Reencode"},
		{mode: compaction.ModeTryBinaryCopy, want: "TryBinaryCopy"},
		{mode: compaction.ModeForceBinaryCopy, want: "ForceBinaryCopy"},
		{mode: compaction.Mode(7), want: "Mode(7)"},
	}
	for _, test := range tests {
		if got := test.mode.String(); got != test.want {
			t.Errorf("Mode(%d).String() = %q, want %q", int32(test.mode), got, test.want)
		}
	}
}

func TestIndexRemapModeString(t *testing.T) {
	tests := []struct {
		mode compaction.IndexRemapMode
		want string
	}{
		{mode: compaction.IndexRemapCompact, want: "Compact"},
		{mode: compaction.IndexRemapDirect, want: "Direct"},
		{mode: compaction.IndexRemapMode(7), want: "IndexRemapMode(7)"},
	}
	for _, test := range tests {
		if got := test.mode.String(); got != test.want {
			t.Errorf("IndexRemapMode(%d).String() = %q, want %q", int32(test.mode), got, test.want)
		}
	}
}
