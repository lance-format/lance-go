package lance

import "github.com/lance-format/lance-go/internal/ffi"

// UpdateOperationOptions is the supported subset of native Update options for
// publishing fragment update results. Native Update also supports removed and
// new fragments, compacted SSTables, preservation bitmaps, a caller-selected
// update mode and an inserted-rows filter; those are not exposed as options.
// UpdateColumns pins the native RewriteColumns mode and produces none of the
// other fields.
type UpdateOperationOptions struct {
	// UpdatedFragments are the complete updated fragment metadata entries.
	UpdatedFragments []*Fragment
	// FieldsModified lists the physical field IDs changed by the update.
	FieldsModified []uint32
	// UpdatedFragmentOffsets optionally restricts matched rows by fragment ID
	// and physical offset. A nil value is the native None value; a non-nil
	// value with no entries is the native Some(empty) value.
	UpdatedFragmentOffsets *UpdatedFragmentOffsets
}

// NewMergeOperation clones a complete final fragment list and its complete
// schema into a Merge operation. The caller keeps ownership of schema and
// fragments.
func NewMergeOperation(schema *NativeSchema, fragments []*Fragment, preservesNullability bool) (*Operation, error) {
	if schema == nil || !schema.res.acquire() {
		return nil, closedError("new merge operation schema", "")
	}
	defer schema.res.release()
	if len(fragments) > 0 {
		if err := acquireFragments(fragments); err != nil {
			return nil, err
		}
		defer releaseFragments(fragments)
	}
	nativeFragments := make([]*ffi.Fragment, len(fragments))
	for i := range fragments {
		nativeFragments[i] = fragments[i].native
	}
	native, err := ffi.NewMergeOperation(schema.native, nativeFragments, preservesNullability)
	if err != nil {
		return nil, wrapError("new merge operation", fragmentURI(fragments), err)
	}
	return newOperation(native, fragmentURI(fragments)), nil
}

// NewUpdateOperation clones updated fragments and metadata into an Update
// operation. It always selects native RewriteColumns mode because
// Fragment.UpdateColumns produces a horizontal column rewrite. The caller
// keeps ownership of every input.
func NewUpdateOperation(options UpdateOperationOptions) (*Operation, error) {
	var offsets *ffi.UpdatedFragmentOffsets
	if options.UpdatedFragmentOffsets != nil {
		if !options.UpdatedFragmentOffsets.res.acquire() {
			return nil, closedError("new update operation offsets", "")
		}
		defer options.UpdatedFragmentOffsets.res.release()
		offsets = options.UpdatedFragmentOffsets.native
	}
	if err := acquireFragments(options.UpdatedFragments); err != nil {
		return nil, err
	}
	defer releaseFragments(options.UpdatedFragments)
	nativeFragments := make([]*ffi.Fragment, len(options.UpdatedFragments))
	for i := range options.UpdatedFragments {
		nativeFragments[i] = options.UpdatedFragments[i].native
	}
	native, err := ffi.NewUpdateOperation(ffi.UpdateOperationOptions{
		UpdatedFragments:       nativeFragments,
		FieldsModified:         options.FieldsModified,
		UpdatedFragmentOffsets: offsets,
	})
	if err != nil {
		return nil, wrapError("new update operation", fragmentURI(options.UpdatedFragments), err)
	}
	return newOperation(native, fragmentURI(options.UpdatedFragments)), nil
}

// MergeFragmentCount returns the number of fragments in a Merge operation.
func (o *Operation) MergeFragmentCount() (uint64, error) {
	return operationValue(o, "operation merge fragment count", (*ffi.Operation).MergeFragmentCount)
}

// MergeFragment returns one detached fragment from a Merge operation.
func (o *Operation) MergeFragment(index uint64) (*Fragment, error) {
	if o == nil || !o.res.acquire() {
		return nil, closedError("operation merge fragment", o.uriOrEmpty())
	}
	defer o.res.release()
	native, err := o.native.MergeFragmentAt(index)
	if err != nil {
		return nil, wrapError("operation merge fragment", o.uri, err)
	}
	return newFragment(native, o.uri), nil
}

// MergeFragments returns every detached fragment from a Merge operation.
func (o *Operation) MergeFragments() ([]*Fragment, error) {
	count, err := o.MergeFragmentCount()
	if err != nil {
		return nil, err
	}
	fragments := make([]*Fragment, 0, count)
	for i := uint64(0); i < count; i++ {
		fragment, err := o.MergeFragment(i)
		if err != nil {
			for _, opened := range fragments {
				_ = opened.Close()
			}
			return nil, err
		}
		fragments = append(fragments, fragment)
	}
	return fragments, nil
}

// UpdateUpdatedFragmentCount returns the number of updated fragments.
func (o *Operation) UpdateUpdatedFragmentCount() (uint64, error) {
	return operationValue(o, "operation update updated fragment count", (*ffi.Operation).UpdateUpdatedFragmentCount)
}

// UpdateUpdatedFragment returns one detached updated fragment.
func (o *Operation) UpdateUpdatedFragment(index uint64) (*Fragment, error) {
	if o == nil || !o.res.acquire() {
		return nil, closedError("operation update updated fragment", o.uriOrEmpty())
	}
	defer o.res.release()
	native, err := o.native.UpdateUpdatedFragmentAt(index)
	if err != nil {
		return nil, wrapError("operation update updated fragment", o.uri, err)
	}
	return newFragment(native, o.uri), nil
}

// UpdateUpdatedFragments returns every detached updated fragment.
func (o *Operation) UpdateUpdatedFragments() ([]*Fragment, error) {
	count, err := o.UpdateUpdatedFragmentCount()
	if err != nil {
		return nil, err
	}
	fragments := make([]*Fragment, 0, count)
	for i := uint64(0); i < count; i++ {
		fragment, err := o.UpdateUpdatedFragment(i)
		if err != nil {
			for _, opened := range fragments {
				_ = opened.Close()
			}
			return nil, err
		}
		fragments = append(fragments, fragment)
	}
	return fragments, nil
}

// UpdateFieldsModified returns the native modified field IDs.
func (o *Operation) UpdateFieldsModified() ([]uint32, error) {
	return operationValue(o, "operation update fields modified", (*ffi.Operation).UpdateFieldsModified)
}

// UpdateUpdatedFragmentOffsets returns the native offsets and whether they
// were present. A present value with zero entries is distinct from absent.
func (o *Operation) UpdateUpdatedFragmentOffsets() (*UpdatedFragmentOffsets, bool, error) {
	if o == nil || !o.res.acquire() {
		return nil, false, closedError("operation update updated fragment offsets", o.uriOrEmpty())
	}
	defer o.res.release()
	native, present, err := o.native.UpdateUpdatedFragmentOffsets()
	if err != nil {
		return nil, false, wrapError("operation update updated fragment offsets", o.uri, err)
	}
	if !present {
		return nil, false, nil
	}
	return newUpdatedFragmentOffsets(native), true, nil
}
