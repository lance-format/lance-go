package ffi

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/lance-format/lance-go/internal/loader"
)

type fakeSymbolSource struct {
	path   string
	lookup func(name string) (uintptr, error)
}

func (f fakeSymbolSource) Path() string {
	return f.path
}

func (f fakeSymbolSource) Lookup(name string) (uintptr, error) {
	return f.lookup(name)
}

func TestBindReportsActionableABIMismatch(t *testing.T) {
	const selectedPath = "/opt/lance/lib/liblance_c.so"
	required := (&bindings{}).symbols()
	source := fakeSymbolSource{
		path: selectedPath,
		lookup: func(string) (uintptr, error) {
			return 0, errors.New("symbol not found")
		},
	}

	b, err := bind(source)
	if err == nil {
		t.Fatal("bind succeeded with a library that exports no required symbols")
	}
	if b != nil {
		t.Fatal("bind returned bindings together with an error")
	}

	var mismatch *abiMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("bind error is %T, want *abiMismatchError", err)
	}
	if mismatch.Path != selectedPath {
		t.Fatalf("mismatch path = %q, want %q", mismatch.Path, selectedPath)
	}
	if mismatch.Required != len(required) {
		t.Fatalf("mismatch required = %d, want %d", mismatch.Required, len(required))
	}
	if len(mismatch.Missing) != len(required) {
		t.Fatalf("mismatch missing = %d, want %d", len(mismatch.Missing), len(required))
	}

	text := err.Error()
	wantLead := fmt.Sprintf(
		"ffi: selected native library %s has an ABI mismatch with this Go SDK; expected ABI from binding metadata: lance-c %s at source revision %s; %d of %d required symbols are missing\n",
		selectedPath, upstreamRelease, upstreamCommit, len(required), len(required),
	)
	if !strings.HasPrefix(text, wantLead) {
		t.Fatalf("diagnostic does not lead with the selected path, ABI mismatch, release and revision:\n%s", text)
	}

	for _, want := range []string{
		"bundled: unset " + loader.EnvLibPath,
		"default build uses the matching companion library",
		"external: build with CGO_ENABLED=0",
		runtime.GOOS + "/" + runtime.GOARCH,
		upstreamRelease,
		upstreamCommit,
		upstreamRepo + "/releases",
		"missing symbols (" + strconv.Itoa(len(required)) + "):",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostic does not contain %q:\n%s", want, text)
		}
	}

	for _, symbol := range required {
		if !strings.Contains(text, symbol.name) {
			t.Fatalf("diagnostic does not include missing symbol %q", symbol.name)
		}
	}

	for _, optional := range []string{
		"lance_dataset_add_columns_batch_udf_v2",
		"lance_fragment_add_columns_batch_udf_v2",
		"lance_dataset_open_with_base_store_params",
	} {
		for _, missing := range mismatch.Missing {
			if missing == optional {
				t.Fatalf("optional symbol %q was treated as required", optional)
			}
		}
	}
}

func TestBindKeepsOptionalSymbolsOptional(t *testing.T) {
	required := (&bindings{}).symbols()
	address := reflect.ValueOf(func() {}).Pointer()
	source := fakeSymbolSource{
		path: "/opt/lance/lib/liblance_c.so",
		lookup: func(name string) (uintptr, error) {
			for _, symbol := range required {
				if symbol.name == name {
					return address, nil
				}
			}
			return 0, errors.New("optional symbol not found")
		},
	}

	b, err := bind(source)
	if err != nil {
		t.Fatalf("bind rejected a library with all required symbols and no optional symbols: %v", err)
	}
	if b.batchUDFV2.datasetAddColumnsBatchUDFV2 != nil ||
		b.batchUDFV2.fragmentAddColumnsBatchUDFV2 != nil {
		t.Fatal("bind populated optional BatchUDF V2 symbols that the library does not export")
	}
	if b.baseStoreOpen.datasetOpenWithBaseStoreParams != nil {
		t.Fatal("bind populated the optional per-base open symbol that the library does not export")
	}
}
