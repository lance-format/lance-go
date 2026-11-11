package ffi

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/cdata"
)

// FragmentWriteOptions mirrors the native write-parameter portion of
// LanceFragmentCreateOptions. It is separate from WriteOptions because the
// fragment-create entry points carry the write mode in their own field.
type FragmentWriteOptions struct {
	Params          WriteParams
	AutoCleanup     *AutoCleanupParams
	SkipAutoCleanup bool
}

// FragmentCreateOptions mirrors LanceFragmentCreateOptions.
//
// A nil Schema leaves native schema inference in place. A non-nil Schema is
// borrowed for the call and cloned by native code before the asynchronous
// write starts. A nil Session or empty StorageOptions map selects native
// defaults.
type FragmentCreateOptions struct {
	Schema         *Schema
	Mode           WriteMode
	Write          *FragmentWriteOptions
	StorageOptions map[string]string
	Session        *Session
}

// fragmentCreateOptions is the C layout of LanceFragmentCreateOptions. Field
// order and padding must match the header.
type fragmentCreateOptions struct {
	Schema      uintptr
	Mode        int32
	Write       *writeOptions
	StorageOpts *uintptr
	Session     uintptr
}

// CreateFragment writes one uncommitted fragment and returns detached
// metadata. It does not publish a version. The stream is consumed by native
// code on every return path after export; reader is borrowed exactly as in
// Write.
func CreateFragment(uri string, fragmentID uint64, reader array.RecordReader, options FragmentCreateOptions) (*Fragment, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return createFragment(b, uri, fragmentID, reader, options)
}

func createFragment(b *bindings, uri string, fragmentID uint64, reader array.RecordReader, options FragmentCreateOptions) (*Fragment, error) {
	rawOptions, storageList, versionList, err := prepareFragmentCreateOptions(uri, reader, options)
	if err != nil {
		return nil, err
	}

	var rawStream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &rawStream); err != nil {
		return nil, fmt.Errorf("ffi: export fragment stream: %w", err)
	}
	streamConsumed := false
	defer func() {
		if !streamConsumed {
			cdata.ReleaseStream(&rawStream)
		}
	}()

	runtime.LockOSThread()
	var handle uintptr
	rc := b.fragmentCreate(uri, fragmentID, &rawOptions, &rawStream, &handle)
	streamConsumed = true
	versionList.keepAlive()
	storageList.keepAlive()
	runtime.KeepAlive(&rawOptions)
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_create failed without setting an error")
	}
	runtime.UnlockOSThread()

	if handle == 0 {
		return nil, fmt.Errorf("ffi: lance_fragment_create returned NULL without setting an error")
	}
	return wrapFragment(b, handle), nil
}

// WriteFragments writes zero or more uncommitted fragments. Native code owns
// max_rows_per_file splitting. Each returned fragment is independently owned.
// The stream is consumed by native code on every return path after export.
func WriteFragments(uri string, reader array.RecordReader, options FragmentCreateOptions) ([]*Fragment, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	return writeFragments(b, uri, reader, options)
}

func writeFragments(b *bindings, uri string, reader array.RecordReader, options FragmentCreateOptions) ([]*Fragment, error) {
	rawOptions, storageList, versionList, err := prepareFragmentCreateOptions(uri, reader, options)
	if err != nil {
		return nil, err
	}

	var rawStream cdata.ArrowArrayStream
	if err := cdata.ExportRecordReader(reader, &rawStream); err != nil {
		return nil, fmt.Errorf("ffi: export fragment stream: %w", err)
	}
	streamConsumed := false
	defer func() {
		if !streamConsumed {
			cdata.ReleaseStream(&rawStream)
		}
	}()

	runtime.LockOSThread()
	var outFragments *uintptr
	var count uint64
	rc := b.fragmentCreateMany(uri, &rawOptions, &rawStream, &outFragments, &count)
	streamConsumed = true
	versionList.keepAlive()
	storageList.keepAlive()
	runtime.KeepAlive(&rawOptions)
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_create_many failed without setting an error")
	}
	runtime.UnlockOSThread()

	if outFragments == nil {
		if count != 0 {
			return nil, fmt.Errorf("ffi: lance_fragment_create_many returned a NULL fragment array with count %d", count)
		}
		return nil, nil
	}
	if count > uint64(int(^uint(0)>>1)) {
		freeFragmentArray(b, outFragments, count)
		return nil, fmt.Errorf("ffi: fragment count %d exceeds addressable slice length", count)
	}

	handles := unsafe.Slice(outFragments, int(count))
	fragments := make([]*Fragment, len(handles))
	for i, handle := range handles {
		if handle == 0 {
			for _, adopted := range fragments[:i] {
				adopted.Close()
			}
			freeFragmentArray(b, outFragments, count)
			return nil, fmt.Errorf("ffi: lance_fragment_create_many returned a NULL fragment handle at index %d", i)
		}
		fragments[i] = wrapFragment(b, handle)
		// The array still owns every uncleared slot. Clear each adopted slot so
		// lance_fragment_array_free cannot close it a second time.
		handles[i] = 0
	}
	freeFragmentArray(b, outFragments, count)
	return fragments, nil
}

// CreateFragmentFromFile creates one detached fragment from a stored Lance
// data file. filename is relative to the dataset's data directory.
// physicalRows is nil to let native code infer the row count; a non-nil
// pointer is forwarded explicitly, including a pointer to zero.
func CreateFragmentFromFile(dataset *Dataset, filename string, fragmentID uint64, physicalRows *uint64) (*Fragment, error) {
	if dataset == nil {
		return nil, fmt.Errorf("ffi: dataset must not be nil")
	}
	datasetHandle := dataset.handle.Load()
	if datasetHandle == 0 {
		return nil, ErrClosed
	}
	if err := validateCString(filename); err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	var handle uintptr
	b := dataset.b
	rc := b.fragmentCreateFromFile(datasetHandle, filename, fragmentID, physicalRows, &handle)
	runtime.KeepAlive(physicalRows)
	if rc != 0 {
		nativeErr := b.takeError()
		runtime.UnlockOSThread()
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fragment_create_from_file failed without setting an error")
	}
	runtime.UnlockOSThread()

	if handle == 0 {
		return nil, fmt.Errorf("ffi: lance_fragment_create_from_file returned NULL without setting an error")
	}
	return wrapFragment(b, handle), nil
}

// prepareFragmentCreateOptions validates Go-side representations and builds the
// native C options. The returned string lists must be kept alive until after
// the native call. uri and reader validation deliberately leave empty values
// and empty input to native code.
func prepareFragmentCreateOptions(uri string, reader array.RecordReader, options FragmentCreateOptions) (fragmentCreateOptions, *cStringList, *cStringList, error) {
	if reader == nil {
		return fragmentCreateOptions{}, nil, nil, fmt.Errorf("ffi: record reader must not be nil")
	}
	if reader.Schema() == nil {
		return fragmentCreateOptions{}, nil, nil, fmt.Errorf("ffi: record reader schema must not be nil")
	}
	if err := validateCString(uri); err != nil {
		return fragmentCreateOptions{}, nil, nil, err
	}
	if options.Mode < WriteModeCreate || options.Mode > WriteModeOverwrite {
		return fragmentCreateOptions{}, nil, nil, fmt.Errorf("ffi: invalid write mode %d", options.Mode)
	}

	rawOptions := fragmentCreateOptions{Mode: int32(options.Mode)}
	if options.Schema != nil {
		handle := options.Schema.handle.Load()
		if handle == 0 {
			return fragmentCreateOptions{}, nil, nil, ErrClosed
		}
		rawOptions.Schema = handle
	}
	if options.Session != nil {
		handle := options.Session.nativeHandle()
		if handle == 0 {
			return fragmentCreateOptions{}, nil, nil, ErrClosed
		}
		rawOptions.Session = handle
	}

	versionList := &cStringList{}
	if options.Write != nil {
		nativeWrite, versions, err := buildWriteParameters(WriteOptions{
			Mode:            options.Mode,
			Params:          options.Write.Params,
			AutoCleanup:     options.Write.AutoCleanup,
			SkipAutoCleanup: options.Write.SkipAutoCleanup,
		})
		if err != nil {
			return fragmentCreateOptions{}, nil, nil, err
		}
		rawOptions.Write = &nativeWrite
		versionList = versions
	}

	storageList, err := storageOptionList(options.StorageOptions)
	if err != nil {
		return fragmentCreateOptions{}, nil, nil, err
	}
	rawOptions.StorageOpts = storageList.ptr()
	return rawOptions, storageList, versionList, nil
}

func freeFragmentArray(b *bindings, fragments *uintptr, count uint64) {
	runtime.LockOSThread()
	b.fragmentArrayFree(fragments, count)
	runtime.UnlockOSThread()
}
