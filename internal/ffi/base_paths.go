package ffi

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"unsafe"
)

// BasePath is one native base-path registration.
//
// ID zero requests native ID allocation. A nil Name maps to native None; a
// non-nil Name, including a pointer to an empty string, is an explicit name.
type BasePath struct {
	ID            uint32
	Path          string
	Name          *string
	IsDatasetRoot bool
}

// basePath is the C layout of LanceBasePath.
type basePath struct {
	ID            uint32
	Path          uintptr
	Name          uintptr
	IsDatasetRoot bool
}

// basePathView is the C layout of LanceBasePathView.
type basePathView struct {
	ID            uint32
	Path          *byte
	Name          *byte
	IsDatasetRoot bool
}

// basePathList owns the C array and the NUL-terminated copies of every path
// and name passed to a base-path entry point.
type basePathList struct {
	raw  []basePath
	bufs [][]byte
}

func newBasePathList(bases []BasePath) (*basePathList, error) {
	list := &basePathList{
		raw:  make([]basePath, len(bases)),
		bufs: make([][]byte, 0, 2*len(bases)),
	}
	for i, base := range bases {
		if err := validateCString(base.Path); err != nil {
			return nil, fmt.Errorf("bases[%d].path: %w", i, err)
		}
		list.raw[i].ID = base.ID
		list.raw[i].IsDatasetRoot = base.IsDatasetRoot
		pathBuf := append([]byte(base.Path), 0)
		list.bufs = append(list.bufs, pathBuf)
		list.raw[i].Path = uintptr(unsafe.Pointer(&pathBuf[0]))
		if base.Name != nil {
			if err := validateCString(*base.Name); err != nil {
				return nil, fmt.Errorf("bases[%d].name: %w", i, err)
			}
			nameBuf := append([]byte(*base.Name), 0)
			list.bufs = append(list.bufs, nameBuf)
			list.raw[i].Name = uintptr(unsafe.Pointer(&nameBuf[0]))
		}
	}
	return list, nil
}

// ptr returns the first element for a C array parameter, or nil for an empty
// list.
func (l *basePathList) ptr() *basePath {
	if len(l.raw) == 0 {
		return nil
	}
	return &l.raw[0]
}

// keepAlive must run after the native call: raw holds integer addresses into
// bufs, which do not keep the backing buffers live on their own.
func (l *basePathList) keepAlive() {
	runtime.KeepAlive(l.raw)
	runtime.KeepAlive(l.bufs)
}

// basePathJSON is one entry of the native base-path registry JSON.
type basePathJSON struct {
	ID            uint32  `json:"id"`
	Name          *string `json:"name"`
	Path          string  `json:"path"`
	IsDatasetRoot bool    `json:"is_dataset_root"`
}

// BasePaths returns the current snapshot's base-path registry keyed by native
// base ID. The map and its values are independent copies of the snapshot.
func (d *Dataset) BasePaths() (map[uint32]BasePath, error) {
	raw, err := d.jsonBytes("lance_dataset_base_paths_json", d.b.datasetBasePathsJSON)
	if err != nil {
		return nil, err
	}
	return decodeBasePaths(raw)
}

func decodeBasePaths(raw []byte) (map[uint32]BasePath, error) {
	var wire map[string]basePathJSON
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("ffi: decode dataset base paths: %w", err)
	}
	bases := make(map[uint32]BasePath, len(wire))
	for key, entry := range wire {
		id, err := strconv.ParseUint(key, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("ffi: decode dataset base paths: key %q is not a uint32 base ID", key)
		}
		if uint32(id) != entry.ID {
			return nil, fmt.Errorf("ffi: decode dataset base paths: key %q disagrees with entry id %d", key, entry.ID)
		}
		bases[uint32(id)] = BasePath{
			ID:            entry.ID,
			Path:          entry.Path,
			Name:          entry.Name,
			IsDatasetRoot: entry.IsDatasetRoot,
		}
	}
	return bases, nil
}

// AddBases registers bases on this handle and commits one native version.
//
// The native transaction copies the inputs before committing; ID allocation
// and conflict validation stay in Lance core. A nil properties map passes
// native None, while a non-nil map, including an empty one, passes an
// explicitly present property map.
func (d *Dataset) AddBases(bases []BasePath, properties map[string]string) error {
	handle := d.handle.Load()
	if handle == 0 {
		return ErrClosed
	}

	list, err := newBasePathList(bases)
	if err != nil {
		return err
	}
	rawProperties, keepAlive, err := newTransactionProperties(properties)
	if err != nil {
		return err
	}

	b := d.b
	runtime.LockOSThread()
	rc := b.datasetAddBases(handle, list.raw, uint64(len(list.raw)), rawProperties)
	list.keepAlive()
	keepAlive()
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: lance_dataset_add_bases returned %d without setting an error", rc)
	}
	runtime.UnlockOSThread()
	return nil
}
