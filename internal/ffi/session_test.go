package ffi

import (
	"reflect"
	"runtime"
	"testing"
	"unsafe"
)

func TestSessionCacheOptionPointers(t *testing.T) {
	for _, options := range []map[string]string{nil, {}, {"capacity": "1048576", "scope": "index"}} {
		t.Run("options", func(t *testing.T) {
			var got map[string]string
			b := &bindings{
				sessionNewWithCacheSpecs: func(index, metadata *sessionCacheSpec) uintptr {
					runtime.GC()
					if index.Kind != int32(CacheSpecConfig) || metadata.Kind != int32(CacheSpecDefault) {
						t.Fatal("incorrect cache configuration kinds")
					}
					got = make(map[string]string)
					keys := unsafe.Slice(index.OptionKeys, index.OptionCount)
					values := unsafe.Slice(index.OptionValues, index.OptionCount)
					for i := range keys {
						got[goString(keys[i])] = goString(values[i])
					}
					return 1
				},
				sessionClose: func(uintptr) {},
			}
			session, err := newSessionWithCacheSpecs(b, CacheSpec{Kind: CacheSpecConfig, BackendKind: "moka", Options: options}, CacheSpec{Kind: CacheSpecDefault})
			if err != nil {
				t.Fatal(err)
			}
			session.Close()
			if options == nil {
				options = map[string]string{}
			}
			if !reflect.DeepEqual(got, options) {
				t.Fatalf("native cache options = %v, want %v", got, options)
			}
		})
	}
}
