package ffi

import (
	"fmt"
	"runtime"
)

// Config returns a stable copy of the dataset configuration snapshot. The
// native snapshot is valid independently of the dataset handle, but this
// method still copies every key and value before closing it.
func (d *Dataset) Config() (map[string]string, error) {
	handleID := d.handle.Load()
	if handleID == 0 {
		return nil, ErrClosed
	}
	b := d.b
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	config := b.datasetConfig(handleID)
	if config == 0 {
		if err := b.takeError(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("ffi: lance_dataset_config returned a nil snapshot without setting an error")
	}
	defer b.datasetConfigClose(config)

	count := b.datasetConfigCount(config)
	if count == 0 {
		// Count zero means either an empty config or a NULL handle. The
		// handle is non-null here, so a pending error is the only failure.
		if err := b.takeError(); err != nil {
			return nil, err
		}
		return map[string]string{}, nil
	}
	entries := make(map[string]string, count)
	for i := uint64(0); i < count; i++ {
		key := b.datasetConfigKeyAt(config, i)
		if key == nil {
			if err := b.takeError(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("ffi: config snapshot returned a nil key without setting an error")
		}
		value := b.datasetConfigValueAt(config, i)
		if value == nil {
			if err := b.takeError(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("ffi: config snapshot returned a nil value without setting an error")
		}
		entries[goString(key)] = goString(value)
	}
	return entries, nil
}

// UpdateConfigJSON merges or replaces the complete configuration and returns
// the complete updated map. A nil value removes the key.
func (d *Dataset) UpdateConfigJSON(keys []string, values []*string, mode int32) (map[string]string, error) {
	raw, err := d.updateMetadataJSON(d.b.datasetUpdateConfigJSON, keys, values, mode)
	if err != nil {
		return nil, err
	}
	return DecodeStringMap(raw)
}
