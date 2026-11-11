package lance

import (
	"context"

	"github.com/lance-format/lance-go/internal/ffi"
)

// BasePath is one base-path registration in a dataset's manifest.
//
// ID zero asks Lance to allocate the next unused native base ID. A nil Name
// passes native None; a non-nil Name, including a pointer to an empty string,
// passes that name explicitly. IsDatasetRoot marks the dataset root rather
// than a data-only base.
type BasePath struct {
	ID            uint32
	Path          string
	Name          *string
	IsDatasetRoot bool
}

// BasePaths returns the base-path registry of d's snapshot, keyed by native
// base ID. The returned map and values are independent copies: they stay
// unchanged when d or another handle commits a new version.
func (d *Dataset) BasePaths() (map[uint32]BasePath, error) {
	if err := d.acquire("base paths"); err != nil {
		return nil, err
	}
	defer d.res.release()

	nativeBases, err := d.native.BasePaths()
	if err != nil {
		return nil, wrapError("base paths", d.uri, err)
	}
	bases := make(map[uint32]BasePath, len(nativeBases))
	for id, native := range nativeBases {
		bases[id] = BasePath{
			ID:            native.ID,
			Path:          native.Path,
			Name:          native.Name,
			IsDatasetRoot: native.IsDatasetRoot,
		}
	}
	return bases, nil
}

// AddBases registers bases in a new version and returns an independent Dataset
// at that version. d is not changed. Lance validates conflicts and allocates
// IDs for bases with ID zero; an empty bases slice still commits a version
// with an unchanged registry.
//
// A nil transactionProperties map passes native None. A non-nil map, including
// an empty one, passes an explicitly present property map to native code.
// Native protobuf persistence normalizes empty maps to absent, so
// ReadTransactionByVersion does not preserve that input distinction.
//
// AddBases records and lists bases; it does not route new writes to a base.
// Write placement is a separate capability that the pinned C API does not
// expose.
func (d *Dataset) AddBases(ctx context.Context, bases []BasePath, transactionProperties map[string]string) (*Dataset, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	nativeBases, err := nativeBasePaths(d.uri, bases)
	if err != nil {
		return nil, err
	}
	if err := validateTransactionProperties(d.uri, transactionProperties); err != nil {
		return nil, err
	}

	return d.mutate("add bases", func(native *ffi.Dataset) error {
		return native.AddBases(nativeBases, transactionProperties)
	})
}

func nativeBasePaths(uri string, bases []BasePath) ([]ffi.BasePath, error) {
	nativeBases := make([]ffi.BasePath, len(bases))
	for i, base := range bases {
		if err := validateCString(base.Path); err != nil {
			return nil, invalidError("add bases", uri, "bases[%d].path: %v", i, err)
		}
		if base.Name != nil {
			if err := validateCString(*base.Name); err != nil {
				return nil, invalidError("add bases", uri, "bases[%d].name: %v", i, err)
			}
		}
		nativeBases[i] = ffi.BasePath{
			ID:            base.ID,
			Path:          base.Path,
			Name:          base.Name,
			IsDatasetRoot: base.IsDatasetRoot,
		}
	}
	return nativeBases, nil
}

func validateTransactionProperties(uri string, properties map[string]string) error {
	for key, value := range properties {
		if err := validateCString(key); err != nil {
			return invalidError("add bases", uri, "transaction property key: %v", err)
		}
		if err := validateCString(value); err != nil {
			return invalidError("add bases", uri, "transaction property %q: %v", key, err)
		}
	}
	return nil
}
