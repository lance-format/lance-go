package lance_test

import (
	"testing"

	lance "github.com/lance-format/lance-go"
)

func TestRuntimeInfoAfterNativeBind(t *testing.T) {
	requireNative(t)

	session, err := lance.NewSession(1<<20, 1<<20)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Close()

	info := lance.RuntimeInfo()
	if !info.Loaded {
		t.Fatalf("RuntimeInfo did not report the loaded library: %+v", info)
	}
	if !info.FFIReady {
		t.Fatalf("RuntimeInfo did not report FFI readiness after bind: %+v", info)
	}
	if info.Library.Source == lance.RuntimeSourceUnknown {
		t.Fatalf("loaded runtime has unknown source: %+v", info)
	}
	if info.Library.Path == "" {
		t.Fatalf("loaded runtime has no path: %+v", info)
	}
	if info.Library.Source == lance.RuntimeSourceBundle {
		if info.Library.Bundle == nil {
			t.Fatalf("bundled runtime has no packaging metadata: %+v", info)
		}
	} else if info.Library.Bundle != nil {
		t.Fatalf("%s runtime reported bundle metadata: %+v", info.Library.Source, info.Library.Bundle)
	}
}
