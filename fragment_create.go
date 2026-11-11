package lance

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
)

// FragmentCreateOption configures CreateFragment and WriteFragments.
//
// It aliases WriteOption so callers can reuse file-layout WriteParams,
// WithWriteStorageOptions, WithWriteSession and WithFragmentSchema. Creation
// does not commit: WithAutoCleanup, WithSkipAutoCleanup(true), stable row ID
// generation and transaction properties are rejected. Configure publication
// with CommitTransaction options, including WithCommitStableRowIDs and
// WithCommitSkipAutoCleanup. The native fragment API accepts static storage
// options only; WithWriteStorageOptionsAccessor and WithWriteBranch are rejected.
// Native-only WriteParams fields, including base placement, Blob policy,
// file-writer options and progress, are also rejected; use ordinary or staged
// dataset writes when those options are required.
type FragmentCreateOption = WriteOption

// CreateFragment writes one uncommitted fragment from reader and returns
// detached fragment metadata. The explicit fragmentID is used as-is; native
// code does not allocate it.
//
// Creating a fragment does not publish a version. Publish the returned
// fragment by passing it to NewAppendOperation and committing the resulting
// transaction. If the transaction is never committed, the written data file
// remains in storage until it is cleaned up separately; closing the fragment
// never deletes it. An empty stream is rejected by native code.
//
// Without WithFragmentSchema, native code infers the schema. With
// WithFragmentSchema, the native schema is cloned before the write and keeps
// field IDs that an Arrow schema cannot represent. The caller keeps ownership
// of reader: native code takes ownership of the exported stream and releases
// its retained reference on every return path. The caller must still release
// reader. A successful write exhausts the input; an error may leave it partially
// read, including batches prefetched during schema inference.
func CreateFragment(ctx context.Context, uri string, fragmentID uint64, reader array.RecordReader, opts ...FragmentCreateOption) (*Fragment, error) {
	const op = "create fragment"
	return createFragment(ctx, op, uri, fragmentID, reader, opts)
}

// WriteFragments writes zero or more uncommitted fragments and returns
// independently owned detached metadata. Native code splits the input using
// max_rows_per_file and other native write parameters. An empty input may
// produce no fragments.
//
// Native code assigns final fragment IDs during commit. The IDs visible on the
// returned fragments before commit are temporary; do not rely on them being
// unique. Duplicate explicit IDs are rejected at commit, not here.
//
// Publication, ownership and schema behavior are the same as CreateFragment.
func WriteFragments(ctx context.Context, uri string, reader array.RecordReader, opts ...FragmentCreateOption) ([]*Fragment, error) {
	const op = "write fragments"
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateCString(uri); err != nil {
		return nil, invalidError(op, uri, "%v", err)
	}
	if reader == nil {
		return nil, invalidError(op, uri, "record reader must not be nil")
	}
	config, err := parseFragmentCreateConfig(op, uri, opts)
	if err != nil {
		return nil, err
	}
	schema, releaseSchema, err := acquireFragmentCreateSchema(op, uri, config.schema)
	if err != nil {
		return nil, err
	}
	defer releaseSchema()
	session, releaseSession, err := acquireSession(op, uri, config.session)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	nativeFragments, err := ffi.WriteFragments(uri, reader, nativeFragmentCreateOptions(config, schema, session))
	if err != nil {
		return nil, wrapError(op, uri, err)
	}
	fragments := make([]*Fragment, len(nativeFragments))
	for i, native := range nativeFragments {
		fragments[i] = newFragment(native, uri)
	}
	return fragments, nil
}

// CreateFragmentFromFile creates one detached fragment from filename, a Lance
// data file stored under the dataset's data directory. filename is relative to
// that directory; it is not a standalone absolute path to an arbitrary file.
//
// The dataset supplies the snapshot, schema and storage options used to read
// the file. physicalRows is nil to let native code infer the row count. A
// non-nil pointer is forwarded explicitly, including a pointer to zero. Native
// treats this as a hint for legacy files; V2 files use their stored row count.
//
// Native code rejects files written with the Blob v2 layout because their
// stored schema cannot be projected without the descriptor columns; that
// limitation is native behavior and is not worked around here.
//
// The returned fragment is uncommitted metadata. Pass it to
// NewAppendOperation and commit the transaction to publish it.
func (d *Dataset) CreateFragmentFromFile(ctx context.Context, filename string, fragmentID uint64, physicalRows *uint64) (*Fragment, error) {
	const op = "create fragment from file"
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateCString(filename); err != nil {
		return nil, invalidError(op, d.uri, "%v", err)
	}
	if !d.res.acquire() {
		return nil, closedError(op, d.uri)
	}
	defer d.res.release()

	native, err := ffi.CreateFragmentFromFile(d.native, filename, fragmentID, physicalRows)
	if err != nil {
		return nil, wrapError(op, d.uri, err)
	}
	return newFragment(native, d.uri), nil
}

func createFragment(ctx context.Context, op, uri string, fragmentID uint64, reader array.RecordReader, opts []WriteOption) (*Fragment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateCString(uri); err != nil {
		return nil, invalidError(op, uri, "%v", err)
	}
	if reader == nil {
		return nil, invalidError(op, uri, "record reader must not be nil")
	}
	config, err := parseFragmentCreateConfig(op, uri, opts)
	if err != nil {
		return nil, err
	}
	schema, releaseSchema, err := acquireFragmentCreateSchema(op, uri, config.schema)
	if err != nil {
		return nil, err
	}
	defer releaseSchema()
	session, releaseSession, err := acquireSession(op, uri, config.session)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	native, err := ffi.CreateFragment(uri, fragmentID, reader, nativeFragmentCreateOptions(config, schema, session))
	if err != nil {
		return nil, wrapError(op, uri, err)
	}
	return newFragment(native, uri), nil
}

func parseFragmentCreateConfig(op, uri string, opts []WriteOption) (writeConfig, error) {
	config := writeConfig{mode: WriteModeCreate}
	for _, option := range opts {
		if option == nil {
			return config, invalidError(op, uri, "nil WriteOption")
		}
		if err := option(&config); err != nil {
			return config, invalidError(op, uri, "%v", err)
		}
	}
	if config.autoCleanup != nil || config.skipAutoCleanup || config.params.EnableStableRowIDs {
		return config, invalidError(op, uri, "cleanup and stable row ID options apply at commit, not fragment creation")
	}
	if field := config.params.firstNativeOptionField(); field != "" {
		return config, invalidError(op, uri, "WriteParams.%s is not supported by fragment creation", field)
	}
	if config.hasTransactionProperties {
		return config, invalidError(op, uri, "WithWriteTransactionProperties is not supported by fragment creation")
	}
	if config.storageOptionsAccessor != nil {
		return config, invalidError(op, uri, "WithWriteStorageOptionsAccessor is not supported by fragment creation; use static storage options")
	}
	if config.branch != "" {
		return config, invalidError(op, uri, "WithWriteBranch is valid only for WriteToNamespace")
	}
	return config, nil
}

func acquireFragmentCreateSchema(op, uri string, schema *NativeSchema) (*ffi.Schema, func(), error) {
	if schema == nil {
		return nil, func() {}, nil
	}
	if !schema.res.acquire() {
		return nil, nil, closedError(op, uri)
	}
	return schema.native, schema.res.release, nil
}

func nativeFragmentCreateOptions(config writeConfig, schema *ffi.Schema, session *ffi.Session) ffi.FragmentCreateOptions {
	nativeWrite := nativeWriteOptions(config)
	return ffi.FragmentCreateOptions{
		Schema: schema,
		Mode:   nativeWrite.Mode,
		Write: &ffi.FragmentWriteOptions{
			Params:          nativeWrite.Params,
			AutoCleanup:     nativeWrite.AutoCleanup,
			SkipAutoCleanup: nativeWrite.SkipAutoCleanup,
		},
		StorageOptions: config.storageOptions,
		Session:        session,
	}
}
