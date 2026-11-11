package lance

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
)

// Write writes all batches from reader using the supplied options. The
// default mode is Create.
//
// reader is borrowed and advanced during the call, possibly only partially
// when a write fails. The caller retains ownership and must release reader
// after the call. Do not use or release it concurrently with Write.
//
// The native write is synchronous and cannot be interrupted once it starts.
// A canceled context is observed before the native call; if the write commits
// after cancellation, the committed Dataset is returned instead of reporting
// a failure that could be mistaken for an uncommitted write.
func Write(ctx context.Context, uri string, reader array.RecordReader, opts ...WriteOption) (*Dataset, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uri == "" {
		return nil, invalidError("write", uri, "dataset URI must not be empty")
	}
	if err := validateCString(uri); err != nil {
		return nil, invalidError("write", uri, "%v", err)
	}
	if reader == nil {
		return nil, invalidError("write", uri, "record reader must not be nil")
	}

	config := writeConfig{mode: WriteModeCreate}
	for _, option := range opts {
		if option == nil {
			return nil, invalidError("write", uri, "nil WriteOption")
		}
		if err := option(&config); err != nil {
			return nil, invalidError("write", uri, "%v", err)
		}
	}

	if config.schema != nil {
		return nil, invalidError("write", uri, "WithFragmentSchema is valid only for CreateFragment and WriteFragments")
	}
	if config.session != nil {
		return nil, invalidError("write", uri, "WithWriteSession is valid only for WriteUncommitted, CreateFragment and WriteFragments")
	}
	if config.hasTransactionProperties {
		return nil, invalidError("write", uri, "WithWriteTransactionProperties is valid only for WriteUncommitted")
	}
	nativeOptions := nativeWriteOptions(config)

	native, err := ffi.Write(uri, reader, nativeOptions, config.storageOptions)
	if err != nil {
		return nil, wrapError("write", uri, err)
	}
	dataset := &Dataset{native: native, uri: uri}
	dataset.res = newResource(native.Close)
	return dataset, nil
}

// Create writes a new dataset and fails if the URI already exists.
func Create(ctx context.Context, uri string, reader array.RecordReader, opts ...WriteOption) (*Dataset, error) {
	return Write(ctx, uri, reader, withMode(WriteModeCreate, opts)...)
}

// Append appends rows to a dataset, creating it if the URI does not exist.
func Append(ctx context.Context, uri string, reader array.RecordReader, opts ...WriteOption) (*Dataset, error) {
	return Write(ctx, uri, reader, withMode(WriteModeAppend, opts)...)
}

// Overwrite commits a new version containing only the supplied rows. It creates
// the dataset if the URI does not exist; earlier versions remain readable.
func Overwrite(ctx context.Context, uri string, reader array.RecordReader, opts ...WriteOption) (*Dataset, error) {
	return Write(ctx, uri, reader, withMode(WriteModeOverwrite, opts)...)
}

func withMode(mode WriteMode, opts []WriteOption) []WriteOption {
	all := make([]WriteOption, 0, len(opts)+1)
	all = append(all, opts...)
	// The convenience wrappers have explicit modes; they take precedence over
	// a conflicting WithWriteMode passed by the caller.
	all = append(all, WithWriteMode(mode))
	return all
}

func nativeWriteOptions(config writeConfig) ffi.WriteOptions {
	nativeOptions := ffi.WriteOptions{
		Mode: ffi.WriteMode(config.mode),
		Params: ffi.WriteParams{
			MaxRowsPerFile:     config.params.MaxRowsPerFile,
			MaxRowsPerGroup:    config.params.MaxRowsPerGroup,
			MaxBytesPerFile:    config.params.MaxBytesPerFile,
			DataStorageVersion: config.params.DataStorageVersion,
			EnableStableRowIDs: config.params.EnableStableRowIDs,
		},
		SkipAutoCleanup: config.skipAutoCleanup,
	}
	if config.autoCleanup != nil {
		seconds := int64(config.autoCleanup.OlderThan / time.Second)
		nanos := int32(config.autoCleanup.OlderThan - time.Duration(seconds)*time.Second)
		nativeOptions.AutoCleanup = &ffi.AutoCleanupParams{
			Interval:         config.autoCleanup.Interval,
			OlderThanSeconds: seconds,
			OlderThanNanos:   nanos,
		}
	}
	return nativeOptions
}
