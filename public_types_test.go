package lance_test

import (
	"reflect"
	"testing"

	lance "github.com/lance-format/lance-go"
)

// TestPublicTypesDeclaredInRootPackage protects the declaration location that
// go doc and pkg.go.dev index. Aliases to internal packages would change these
// paths and hide their methods and fields from the root package documentation.
func TestPublicTypesDeclaredInRootPackage(t *testing.T) {
	root := reflect.TypeOf(lance.CacheSpec{}).PkgPath()
	types := []struct {
		name string
		typ  reflect.Type
	}{
		{"Session", reflect.TypeOf((*lance.Session)(nil)).Elem()},
		{"CacheStats", reflect.TypeOf(lance.CacheStats{})},
		{"StorageOptionsProvider", reflect.TypeOf((*lance.StorageOptionsProvider)(nil)).Elem()},
		{"ForceStorageOptionsProvider", reflect.TypeOf((*lance.ForceStorageOptionsProvider)(nil)).Elem()},
		{"StorageOptionsAccessor", reflect.TypeOf((*lance.StorageOptionsAccessor)(nil)).Elem()},
	}
	for _, tc := range types {
		if got := tc.typ.PkgPath(); got != root {
			t.Errorf("%s package path = %q, want %q", tc.name, got, root)
		}
		if got := tc.typ.Name(); got != tc.name {
			t.Errorf("type name = %q, want %q", got, tc.name)
		}
	}
}
