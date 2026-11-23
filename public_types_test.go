package lance_test

import (
	"reflect"
	"testing"

	lance "github.com/lance-format/lance-go"
)

// These compile-time assertions exercise the option contracts from outside the
// package, as an ordinary consumer sees them. Shared constructors must return
// values their supported operations accept, and a constructor stored as a
// function value must name its return type.
var (
	_ lance.OpenOption        = lance.WithSession(nil)
	_ lance.WriteOption       = lance.WithSession(nil)
	_ lance.CommitOption      = lance.WithSession(nil)
	_ lance.OpenOption        = lance.WithStorageOptions(nil)
	_ lance.WriteOption       = lance.WithStorageOptions(nil)
	_ lance.CommitOption      = lance.WithStorageOptions(nil)
	_ lance.BranchOption      = lance.WithStorageOptions(nil)
	_ lance.OpenOption        = lance.WithStorageOptionsAccessor(nil)
	_ lance.WriteOption       = lance.WithStorageOptionsAccessor(nil)
	_ lance.CommitOption      = lance.WithStorageOptionsAccessor(nil)
	_ lance.OpenOption        = lance.WithCommitLock(nil)
	_ lance.WriteOption       = lance.WithCommitLock(nil)
	_ lance.CommitOption      = lance.WithCommitLock(nil)
	_ lance.WriteOption       = lance.WithSkipAutoCleanup(true)
	_ lance.CommitOption      = lance.WithSkipAutoCleanup(true)
	_ lance.WriteOption       = lance.WithTransactionProperties(nil)
	_ lance.TransactionOption = lance.WithTransactionProperties(nil)
	_ lance.AddColumnsOption  = lance.WithAddColumnsBatchSize(0)

	_ func(*lance.Session) *lance.SessionOption                               = lance.WithSession
	_ func(map[string]string) *lance.StorageOptionsOption                     = lance.WithStorageOptions
	_ func(*lance.StorageOptionsAccessor) *lance.StorageOptionsAccessorOption = lance.WithStorageOptionsAccessor
	_ func(*lance.CommitLock) *lance.CommitLockOption                         = lance.WithCommitLock
	_ func(bool) *lance.SkipAutoCleanupOption                                 = lance.WithSkipAutoCleanup
	_ func(map[string]string) *lance.TransactionPropertiesOption              = lance.WithTransactionProperties
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
		{"OpenOption", reflect.TypeOf((*lance.OpenOption)(nil)).Elem()},
		{"WriteOption", reflect.TypeOf((*lance.WriteOption)(nil)).Elem()},
		{"CommitOption", reflect.TypeOf((*lance.CommitOption)(nil)).Elem()},
		{"BranchOption", reflect.TypeOf((*lance.BranchOption)(nil)).Elem()},
		{"TransactionOption", reflect.TypeOf((*lance.TransactionOption)(nil)).Elem()},
		{"AddColumnsOption", reflect.TypeOf((*lance.AddColumnsOption)(nil)).Elem()},
		{"SessionOption", reflect.TypeOf((*lance.SessionOption)(nil)).Elem()},
		{"StorageOptionsOption", reflect.TypeOf((*lance.StorageOptionsOption)(nil)).Elem()},
		{"StorageOptionsAccessorOption", reflect.TypeOf((*lance.StorageOptionsAccessorOption)(nil)).Elem()},
		{"CommitLockOption", reflect.TypeOf((*lance.CommitLockOption)(nil)).Elem()},
		{"SkipAutoCleanupOption", reflect.TypeOf((*lance.SkipAutoCleanupOption)(nil)).Elem()},
		{"TransactionPropertiesOption", reflect.TypeOf((*lance.TransactionPropertiesOption)(nil)).Elem()},
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
