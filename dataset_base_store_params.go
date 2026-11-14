package lance

import (
	"sort"

	"github.com/lance-format/lance-go/internal/ffi"
)

// nativeBaseStoreParams acquires every per-base accessor and converts the
// representable fields to the FFI layout. On failure, releases contains every
// accessor acquired before the failure; the caller must run them.
func nativeBaseStoreParams(op, uri string, params map[string]BaseStoreParams) (map[string]ffi.BaseStoreParams, []func(), error) {
	if params == nil {
		return nil, nil, nil
	}
	paths := make([]string, 0, len(params))
	for path := range params {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	converted := make(map[string]ffi.BaseStoreParams, len(paths))
	releases := make([]func(), 0, len(paths))
	for _, path := range paths {
		base := params[path]
		accessor, release, err := acquireStorageOptionsAccessor(op, uri, base.StorageOptionsAccessor)
		if err != nil {
			return nil, releases, err
		}
		releases = append(releases, release)
		converted[path] = ffi.BaseStoreParams{
			StorageOptionsAccessor:     accessor,
			BlockSize:                  base.BlockSize,
			S3CredentialsRefreshOffset: base.S3CredentialsRefreshOffset,
			UseConstantSizeUploadParts: base.UseConstantSizeUploadParts,
			ListIsLexicallyOrdered:     base.ListIsLexicallyOrdered,
		}
	}
	return converted, releases, nil
}
