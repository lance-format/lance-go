//go:build linux || darwin

package lance_test

import (
	"context"
	"strings"
	"testing"

	lance "github.com/lance-format/lance-go"
)

func TestNativeWriteParamsRejectedForNamespace(t *testing.T) {
	ctx := context.Background()
	ns := newCustomNamespace(t, &customClient{}, "native-options-reject")
	tableID := []string{"events"}
	schema := testSchema()
	cases := []struct {
		name   string
		field  string
		params lance.WriteParams
	}{
		{"v2", "EnableV2ManifestPaths", lance.WriteParams{EnableV2ManifestPaths: ptr(false)}},
		{"initial", "InitialBases", lance.WriteParams{InitialBases: []lance.BasePath{}}},
		{"target", "TargetBases", lance.WriteParams{TargetBases: []uint32{}}},
		{"names", "TargetBaseNamesOrPaths", lance.WriteParams{TargetBaseNamesOrPaths: []string{}}},
		{"all", "TargetAllBases", lance.WriteParams{TargetAllBases: ptr(false)}},
		{"stores", "BaseStoreParams", lance.WriteParams{BaseStoreParams: map[string]lance.BaseStoreParams{}}},
		{"outside", "AllowExternalBlobOutsideBases", lance.WriteParams{AllowExternalBlobOutsideBases: true}},
		{"mode", "ExternalBlobMode", lance.WriteParams{ExternalBlobMode: lance.ExternalBlobModeIngest}},
		{"pack", "BlobPackFileSizeThreshold", lance.WriteParams{BlobPackFileSizeThreshold: ptr(uint64(0))}},
		{"writer", "FileWriterOptions", lance.WriteParams{FileWriterOptions: &lance.DatasetFileWriterOptions{}}},
		{"progress", "Progress", lance.WriteParams{Progress: &lance.WriteProgress{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := lance.WriteToNamespace(ctx, ns, tableID, makeReader(t, schema,
				makeRecord(t, schema, []int64{1}, []string{"a"}, []bool{true}),
			), lance.WithWriteParams(tc.params))
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("WriteToNamespace error = %v, want rejection of %s", err, tc.field)
			}
		})
	}
}
