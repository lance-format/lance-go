package ffi

import (
	"reflect"
	"runtime"
	"testing"
	"unsafe"

	"github.com/lance-format/lance-go/compaction"
)

func TestCompactionTransactionPropertyPointers(t *testing.T) {
	for _, properties := range []map[string]string{nil, {}, {"origin": "compaction-test", "tag": "run-1"}} {
		t.Run("properties", func(t *testing.T) {
			var got map[string]string
			calls, frees := 0, 0
			b := &bindings{
				compactionOptionsNew:  func() uintptr { return 1 },
				compactionOptionsFree: func(uintptr) { frees++ },
				compactionOptionsSetTransactionProperties: func(_ uintptr, input *transactionProperties) int32 {
					calls++
					runtime.GC()
					got = make(map[string]string)
					keys := unsafe.Slice(input.Keys, input.Count)
					values := unsafe.Slice(input.Values, input.Count)
					for i := range keys {
						got[goString(keys[i])] = goString(values[i])
					}
					return 0
				},
			}
			options, err := buildCompactionOptions(b, &compaction.Options{TransactionProperties: properties})
			if err != nil {
				t.Fatal(err)
			}
			options.Close()
			if frees != 1 {
				t.Fatalf("options releases = %d, want 1", frees)
			}
			if properties == nil {
				if calls != 0 {
					t.Fatalf("unset properties invoked setter %d times", calls)
				}
			} else if calls != 1 || !reflect.DeepEqual(got, properties) {
				t.Fatalf("native properties = %v (%d calls), want %v once", got, calls, properties)
			}
		})
	}
}
