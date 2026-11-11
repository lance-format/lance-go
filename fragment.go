package lance

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
)

// NativeSchema owns a native Lance schema, including field IDs that the lossy
// Arrow schema conversion does not represent. A NativeSchema stays usable after
// the dataset, fragment or operation that produced it is closed.
type NativeSchema struct {
	native *ffi.Schema
	res    *resource
}

func newNativeSchema(native *ffi.Schema) *NativeSchema {
	schema := &NativeSchema{native: native}
	schema.res = newResource(native.Close)
	return schema
}

// Close releases the schema. It is idempotent.
func (s *NativeSchema) Close() error {
	if s == nil {
		return nil
	}
	s.res.close()
	return nil
}

// Clone returns an independent native schema snapshot.
func (s *NativeSchema) Clone() (*NativeSchema, error) {
	if s == nil || !s.res.acquire() {
		return nil, closedError("clone schema", "")
	}
	defer s.res.release()
	native, err := s.native.Clone()
	if err != nil {
		return nil, wrapError("clone schema", "", err)
	}
	return newNativeSchema(native), nil
}

// Arrow exports a lossy Arrow view of the native schema.
func (s *NativeSchema) Arrow() (*arrow.Schema, error) {
	if s == nil || !s.res.acquire() {
		return nil, closedError("schema arrow", "")
	}
	defer s.res.release()
	schema, err := s.native.ToArrow()
	if err != nil {
		return nil, wrapError("schema arrow", "", err)
	}
	return schema, nil
}

// Project returns a native schema containing the named top-level columns.
func (s *NativeSchema) Project(columns ...string) (*NativeSchema, error) {
	if s == nil || !s.res.acquire() {
		return nil, closedError("project schema", "")
	}
	defer s.res.release()
	native, err := s.native.Project(columns)
	if err != nil {
		return nil, wrapError("project schema", "", err)
	}
	return newNativeSchema(native), nil
}

// FieldID returns the native field ID for a column path.
func (s *NativeSchema) FieldID(column string) (int32, error) {
	if s == nil || !s.res.acquire() {
		return 0, closedError("schema field id", "")
	}
	defer s.res.release()
	id, err := s.native.FieldID(column)
	if err != nil {
		return 0, wrapError("schema field id", "", err)
	}
	return id, nil
}

// Fragment is a native Lance fragment snapshot. A fragment opened from a
// dataset and one returned by Fragment.Delete own a dataset snapshot and
// support scans, take, row counts and deletes. Fragments cloned from a
// transaction operation are detached: metadata is readable, but logical schema and data access
// returns CodeNotSupported.
type Fragment struct {
	native *ffi.Fragment
	res    *resource
	uri    string
}

func newFragment(native *ffi.Fragment, uri string) *Fragment {
	fragment := &Fragment{native: native, uri: uri}
	fragment.res = newResource(native.Close)
	return fragment
}

// Close releases the fragment. It is idempotent.
func (f *Fragment) Close() error {
	if f == nil {
		return nil
	}
	f.res.close()
	return nil
}

// ID returns the native fragment ID. Zero is a valid ID.
func (f *Fragment) ID() uint64 {
	if f == nil || !f.res.acquire() {
		return 0
	}
	defer f.res.release()
	return f.native.ID()
}

// IsBound reports whether the fragment can access data files.
func (f *Fragment) IsBound() bool {
	if f == nil || !f.res.acquire() {
		return false
	}
	defer f.res.release()
	bound, err := f.native.IsBound()
	if err != nil {
		return false
	}
	return bound
}

// DataFileCount returns the number of data files in the fragment.
func (f *Fragment) DataFileCount() (uint64, error) {
	if f == nil || !f.res.acquire() {
		return 0, closedError("fragment data file count", f.uriOrEmpty())
	}
	defer f.res.release()
	count, err := f.native.DataFileCount()
	if err != nil {
		return 0, wrapError("fragment data file count", f.uri, err)
	}
	return count, nil
}

// DataFileAt returns the data file at index.
func (f *Fragment) DataFileAt(index uint64) (*DataFile, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("fragment data file", f.uriOrEmpty())
	}
	defer f.res.release()
	native, err := f.native.DataFileAt(index)
	if err != nil {
		return nil, wrapError("fragment data file", f.uri, err)
	}
	return newDataFile(native), nil
}

// DataFileForField returns the data file holding fieldID. The boolean is false
// when the fragment has no file for that field.
func (f *Fragment) DataFileForField(fieldID int32) (*DataFile, bool, error) {
	if f == nil || !f.res.acquire() {
		return nil, false, closedError("fragment data file for field", f.uriOrEmpty())
	}
	defer f.res.release()
	native, err := f.native.DataFileForField(fieldID)
	if err != nil {
		return nil, false, wrapError("fragment data file for field", f.uri, err)
	}
	if native == nil {
		return nil, false, nil
	}
	return newDataFile(native), true, nil
}

// MetadataJSON returns the native fragment metadata as JSON.
func (f *Fragment) MetadataJSON() (json.RawMessage, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("fragment metadata", f.uriOrEmpty())
	}
	defer f.res.release()
	data, err := f.native.MetadataJSON()
	if err != nil {
		return nil, wrapError("fragment metadata", f.uri, err)
	}
	return json.RawMessage(data), nil
}

// Schema exports a lossy Arrow view of the fragment's physical schema.
func (f *Fragment) Schema() (*arrow.Schema, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("fragment schema", f.uriOrEmpty())
	}
	defer f.res.release()
	schema, err := f.native.Schema()
	if err != nil {
		return nil, wrapError("fragment schema", f.uri, err)
	}
	return schema, nil
}

// NativeSchema clones the fragment's native schema, preserving field IDs and
// Lance-specific nested metadata that the Arrow view does not represent.
func (f *Fragment) NativeSchema() (*NativeSchema, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("fragment native schema", f.uriOrEmpty())
	}
	defer f.res.release()
	native, err := f.native.NativeSchema()
	if err != nil {
		return nil, wrapError("fragment native schema", f.uri, err)
	}
	return newNativeSchema(native), nil
}

// PhysicalRows returns the fragment's physical row count.
func (f *Fragment) PhysicalRows() (uint64, error) {
	return f.rowCount("fragment physical rows", f.nativePhysicalRows)
}

// DeletedRows returns the number of rows hidden by deletion files.
func (f *Fragment) DeletedRows() (uint64, error) {
	return f.rowCount("fragment deleted rows", f.nativeDeletedRows)
}

// LogicalRows returns PhysicalRows minus DeletedRows.
func (f *Fragment) LogicalRows() (uint64, error) {
	return f.rowCount("fragment logical rows", f.nativeLogicalRows)
}

func (f *Fragment) rowCount(op string, call func() (uint64, error)) (uint64, error) {
	if f == nil || !f.res.acquire() {
		return 0, closedError(op, f.uriOrEmpty())
	}
	defer f.res.release()
	count, err := call()
	if err != nil {
		return 0, wrapError(op, f.uri, err)
	}
	return count, nil
}

func (f *Fragment) nativePhysicalRows() (uint64, error) { return f.native.PhysicalRows() }
func (f *Fragment) nativeDeletedRows() (uint64, error)  { return f.native.DeletedRows() }
func (f *Fragment) nativeLogicalRows() (uint64, error)  { return f.native.LogicalRows() }

// Scan scans this fragment. The native fragment constructor does not accept
// WithColumns or WithSQLFilter; these return CodeNotSupported. The returned
// native scanner supports WithAdditionalSQLFilter, WithSubstraitFilter and
// WithSubstraitAggregate through its ordinary scanner setters. These options
// operate on this fragment snapshot and preserve native validation semantics.
func (f *Fragment) Scan(opts ...ScanOption) (*Scanner, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("scan fragment", f.uriOrEmpty())
	}
	defer f.res.release()

	config := scanConfig{}
	for _, option := range opts {
		if option == nil {
			return nil, invalidError("scan fragment", f.uri, "nil ScanOption")
		}
		if err := option(&config); err != nil {
			return nil, invalidError("scan fragment", f.uri, "%v", err)
		}
	}
	if len(config.columns) > 0 {
		return nil, &Error{Op: "scan fragment", URI: f.uri, Code: CodeNotSupported, Message: "fragment scan does not support WithColumns"}
	}
	if config.filter != nil {
		return nil, &Error{Op: "scan fragment", URI: f.uri, Code: CodeNotSupported, Message: "fragment scan does not support WithSQLFilter"}
	}
	if err := config.validateVector(); err != nil {
		return nil, invalidError("scan fragment", f.uri, "%v", err)
	}
	native, err := f.native.Scan(nativeScannerOptions(config), func(native *ffi.Scanner) error {
		return applyFTSScan(native, &config)
	})
	runtime.KeepAlive(&config)
	if err != nil {
		return nil, wrapError("scan fragment", f.uri, err)
	}
	return &Scanner{
		native:        native,
		uri:           f.uri,
		statsCallback: config.statistics,
	}, nil
}

// Take takes fragment-local logical row indices. With no columns, all columns
// are returned. An explicit empty column list projects zero columns.
func (f *Fragment) Take(ctx context.Context, indices []uint32, columns ...string) ([]arrow.RecordBatch, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("take fragment", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, wrapError("take fragment", f.uri, err)
	}
	if err := validateColumnNames(columns); err != nil {
		return nil, invalidError("take fragment", f.uri, "%v", err)
	}
	batches, err := f.native.Take(indices, columns)
	if err != nil {
		return nil, wrapError("take fragment", f.uri, err)
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			for _, batch := range batches {
				batch.Release()
			}
			return nil, wrapError("take fragment", f.uri, err)
		}
	}
	return batches, nil
}

// CountRows counts rows matching filter. A nil filter counts all logical rows.
func (f *Fragment) CountRows(filter *string) (uint64, error) {
	if f == nil || !f.res.acquire() {
		return 0, closedError("count fragment rows", f.uriOrEmpty())
	}
	defer f.res.release()
	count, err := f.native.CountRows(filter)
	if err != nil {
		return 0, wrapError("count fragment rows", f.uri, err)
	}
	return count, nil
}

// Delete marks matching rows deleted and returns an independently owned,
// scannable snapshot with the staged deletion metadata. It returns (nil, nil)
// when every row in the fragment is deleted.
func (f *Fragment) Delete(ctx context.Context, predicate string) (*Fragment, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("delete fragment rows", f.uriOrEmpty())
	}
	defer f.res.release()
	if err := checkContext(ctx); err != nil {
		return nil, wrapError("delete fragment rows", f.uri, err)
	}
	native, err := f.native.Delete(predicate)
	if err != nil {
		return nil, wrapError("delete fragment rows", f.uri, err)
	}
	if native == nil {
		return nil, nil
	}
	return newFragment(native, f.uri), nil
}

func (f *Fragment) uriOrEmpty() string {
	if f == nil {
		return ""
	}
	return f.uri
}

func (f *Fragment) String() string {
	if f == nil {
		return "Fragment{<nil>}"
	}
	if !f.res.acquire() {
		return "Fragment{closed}"
	}
	defer f.res.release()
	bound, _ := f.native.IsBound()
	return fmt.Sprintf("Fragment{id=%d bound=%t}", f.native.ID(), bound)
}

// DataFile is an owned native data file handle.
type DataFile struct {
	native *ffi.DataFile
	res    *resource
}

func newDataFile(native *ffi.DataFile) *DataFile {
	file := &DataFile{native: native}
	file.res = newResource(native.Close)
	return file
}

// Close releases the data file. It is idempotent.
func (f *DataFile) Close() error {
	if f == nil {
		return nil
	}
	f.res.close()
	return nil
}

// Clone returns an independent data file snapshot.
func (f *DataFile) Clone() (*DataFile, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError("clone data file", "")
	}
	defer f.res.release()
	native, err := f.native.Clone()
	if err != nil {
		return nil, wrapError("clone data file", "", err)
	}
	return newDataFile(native), nil
}

// Path returns the data file path.
func (f *DataFile) Path() (string, error) {
	if f == nil || !f.res.acquire() {
		return "", closedError("data file path", "")
	}
	defer f.res.release()
	path, err := f.native.Path()
	if err != nil {
		return "", wrapError("data file path", "", err)
	}
	return path, nil
}

// FieldIDs returns the native field IDs stored in the file.
func (f *DataFile) FieldIDs() ([]int32, error) {
	return f.int32List("data file field ids", func() (uint64, error) { return f.native.FieldIDCount() }, func(index uint64) (int32, error) { return f.native.FieldIDAt(index) })
}

// ColumnIndices returns the native column indices stored in the file.
func (f *DataFile) ColumnIndices() ([]int32, error) {
	return f.int32List("data file column indices", func() (uint64, error) { return f.native.ColumnIndexCount() }, func(index uint64) (int32, error) { return f.native.ColumnIndexAt(index) })
}

func (f *DataFile) int32List(op string, countFn func() (uint64, error), atFn func(uint64) (int32, error)) ([]int32, error) {
	if f == nil || !f.res.acquire() {
		return nil, closedError(op, "")
	}
	defer f.res.release()
	count, err := countFn()
	if err != nil {
		return nil, wrapError(op, "", err)
	}
	values := make([]int32, count)
	for i := range values {
		value, err := atFn(uint64(i))
		if err != nil {
			return nil, wrapError(op, "", err)
		}
		values[i] = value
	}
	return values, nil
}

// FileVersion returns the major and minor data file version.
func (f *DataFile) FileVersion() (uint32, uint32, error) {
	if f == nil || !f.res.acquire() {
		return 0, 0, closedError("data file version", "")
	}
	defer f.res.release()
	major, minor, err := f.native.FileVersion()
	if err != nil {
		return 0, 0, wrapError("data file version", "", err)
	}
	return major, minor, nil
}

// FileSize returns the data file size and whether native metadata recorded it.
func (f *DataFile) FileSize() (uint64, bool, error) {
	if f == nil || !f.res.acquire() {
		return 0, false, closedError("data file size", "")
	}
	defer f.res.release()
	size, present, err := f.native.FileSize()
	if err != nil {
		return 0, false, wrapError("data file size", "", err)
	}
	return size, present, nil
}

// BaseID returns the optional base ID selecting the base path.
func (f *DataFile) BaseID() (uint32, bool, error) {
	if f == nil || !f.res.acquire() {
		return 0, false, closedError("data file base id", "")
	}
	defer f.res.release()
	id, present, err := f.native.BaseID()
	if err != nil {
		return 0, false, wrapError("data file base id", "", err)
	}
	return id, present, nil
}

// NativeSchema clones the dataset's native schema. The schema preserves field
// IDs, nested metadata and encodings that an Arrow schema view cannot.
func (d *Dataset) NativeSchema() (*NativeSchema, error) {
	if err := d.acquire("native schema"); err != nil {
		return nil, err
	}
	defer d.res.release()
	native, err := d.native.NativeSchema()
	if err != nil {
		return nil, wrapError("native schema", d.uri, err)
	}
	return newNativeSchema(native), nil
}

// FragmentCount returns the number of fragments in the current dataset version.
func (d *Dataset) FragmentCount() (uint64, error) {
	if err := d.acquire("fragment count"); err != nil {
		return 0, err
	}
	defer d.res.release()
	count, err := d.native.FragmentCount()
	if err != nil {
		return 0, wrapError("fragment count", d.uri, err)
	}
	return count, nil
}

// FragmentIDs returns the fragment IDs in manifest order.
func (d *Dataset) FragmentIDs() ([]uint64, error) {
	if err := d.acquire("fragment ids"); err != nil {
		return nil, err
	}
	defer d.res.release()
	ids, err := d.native.FragmentIDs()
	if err != nil {
		return nil, wrapError("fragment ids", d.uri, err)
	}
	return ids, nil
}

// Fragments opens every fragment in manifest order.
func (d *Dataset) Fragments() ([]*Fragment, error) {
	ids, err := d.FragmentIDs()
	if err != nil {
		return nil, err
	}
	fragments := make([]*Fragment, 0, len(ids))
	for _, id := range ids {
		fragment, err := d.OpenFragment(id)
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

// OpenFragment opens one fragment by ID.
func (d *Dataset) OpenFragment(id uint64) (*Fragment, error) {
	if err := d.acquire("open fragment"); err != nil {
		return nil, err
	}
	defer d.res.release()
	native, err := d.native.OpenFragment(id)
	if err != nil {
		return nil, wrapError("open fragment", d.uri, err)
	}
	return newFragment(native, d.uri), nil
}

// CreateDataFile creates a native data file handle at path.
func (d *Dataset) CreateDataFile(path string, baseID *uint32) (*DataFile, error) {
	if err := d.acquire("create data file"); err != nil {
		return nil, err
	}
	defer d.res.release()
	native, err := d.native.CreateDataFile(path, baseID)
	if err != nil {
		return nil, wrapError("create data file", d.uri, err)
	}
	return newDataFile(native), nil
}
