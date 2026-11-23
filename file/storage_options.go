package file

import (
	"github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/accessor"
	"github.com/lance-format/lance-go/internal/ffi"
)

// StorageOptions is an alias for the root lance.StorageOptionsAccessor. Raw
// file APIs accept it directly, so file readers, writers and sessions share
// the root accessor's single native lifecycle and the caller closes it once.
//
// A nil *StorageOptions selects native defaults. NewStorageOptions and
// NewStorageOptionsWithProvider create accessors with the same rules as the
// root constructors, except that NewStorageOptions treats a nil or empty map
// as an explicitly empty option set rather than native None.
//
// Every reader, writer and session pins the accessor while it opens and then
// retains its own native reference. Close is idempotent, prevents later opens,
// and waits only for open calls currently acquiring the accessor; it does not
// wait for native handles that already opened, and those handles remain usable
// afterwards. Callers own the provider value passed to
// NewStorageOptionsWithProvider; the SDK never calls a provider Close method
// and keeps the provider reachable until native releases the final owner,
// including after Close.
//
// NewStorageOptionsFromAccessor remains for source compatibility and returns
// the same accessor it receives. Pass the root accessor to ReaderOptions,
// WriterOptions or SessionOptions directly instead; closing the accessor
// stays with its creator.
type StorageOptions = lance.StorageOptionsAccessor

// NewStorageOptions creates static storage options. A nil or empty map
// produces an explicit empty option set, matching the historical file
// behavior and differing from a nil *StorageOptions, which selects native
// defaults.
func NewStorageOptions(values map[string]string) (*StorageOptions, error) {
	if values == nil {
		values = map[string]string{}
	}
	if err := validateStorageOptions(values); err != nil {
		return nil, err
	}
	owned, err := lance.NewStorageOptionsAccessor(values, nil)
	if err != nil {
		return nil, err
	}
	return owned, nil
}

// NewStorageOptionsWithProvider creates storage options with a dynamic
// provider and optional initial options. A nil initial map means native None
// until the provider resolves; a non-nil map, including an empty one, means an
// explicit initial set.
//
// The provider value remains caller-owned. The SDK does not call a provider
// Close method and keeps the provider reachable through the accessor's native
// callback adapter until native releases the final owner, so it may also be
// reused by another accessor.
func NewStorageOptionsWithProvider(initial map[string]string, provider lance.StorageOptionsProvider) (*StorageOptions, error) {
	if provider == nil {
		return nil, invalidError("storage options", "", "storage options provider must not be nil")
	}
	if err := validateStorageOptions(initial); err != nil {
		return nil, err
	}
	owned, err := lance.NewStorageOptionsAccessor(initial, provider)
	if err != nil {
		return nil, err
	}
	return owned, nil
}

// NewStorageOptionsFromAccessor returns owned unchanged. It is retained for
// source compatibility with the 0.2 adopting wrapper. Closing the returned
// accessor closes the same native accessor as closing owned; pass owned
// directly to file APIs instead and leave closing it to its creator. A nil
// owned value is rejected, while a non-nil zero value behaves as already
// closed.
//
// Deprecated: pass the accessor directly to ReaderOptions, WriterOptions or
// SessionOptions.
func NewStorageOptionsFromAccessor(owned *lance.StorageOptionsAccessor) (*StorageOptions, error) {
	if owned == nil {
		return nil, invalidError("storage options", "", "storage options accessor must not be nil")
	}
	return owned, nil
}

// validateStorageOptions keeps file constructor failures on their historical
// top-level identity: *lance.Error with Op "storage options" and the native
// invalid-argument classification.
func validateStorageOptions(values map[string]string) error {
	if err := ffi.ValidateStorageOptions(values); err != nil {
		return wrapError("storage options", "", err)
	}
	return nil
}

// lockStorage pins the shared accessor for one native open call. The returned
// release function must run after the native call returns.
func lockStorage(opts *StorageOptions) (func(), *ffi.StorageOptionsAccessor, error) {
	if opts == nil {
		return func() {}, nil, nil
	}
	core, ok := accessor.From(opts)
	if !ok || core == nil {
		return nil, nil, closedError("storage options", "")
	}
	native, release, ok := accessor.Acquire(core)
	if !ok {
		return nil, nil, closedError("storage options", "")
	}
	return release, native, nil
}
