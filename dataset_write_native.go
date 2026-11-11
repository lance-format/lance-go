package lance

import (
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/lance-format/lance-go/internal/ffi"
)

func writeNativeDataset(uri string, reader array.RecordReader, config writeConfig) (*Dataset, error) {
	var storageAccessor *ffi.StorageOptionsAccessor
	var releaseStorage func()
	var err error
	if config.commitLock != nil {
		storageAccessor, releaseStorage, err = resolveLockStorageOptions("write", uri, config.storageOptions, config.storageOptionsSet, config.storageOptionsAccessor)
	} else {
		storageAccessor, releaseStorage, err = resolveStorageOptions("write", uri, config.storageOptions, config.storageOptionsSet, config.storageOptionsAccessor)
	}
	if err != nil {
		return nil, err
	}
	defer releaseStorage()
	if storageAccessor == nil && (config.storageOptionsSet || config.storageOptions != nil) {
		storageAccessor, err = ffi.NewStorageOptionsAccessorWithPresence(config.storageOptions, config.storageOptionsSet, nil)
		if err != nil {
			return nil, wrapError("write", uri, err)
		}
		defer storageAccessor.Close()
	}

	session, releaseSession, err := acquireSession("write", uri, config.session)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	spec, releases, err := nativeWriteOptionsSpec("write", uri, config, session, storageAccessor, true)
	defer releaseAll(releases)
	if err != nil {
		return nil, wrapError("write", uri, err)
	}
	if config.commitLock != nil {
		spec.CommitLock = config.commitLock.native
	}
	nativeOptions, err := ffi.NewNativeWriteOptions(spec)
	if err != nil {
		return nil, wrapError("write", uri, err)
	}
	defer nativeOptions.Close()

	native, err := ffi.WriteWithNativeOptions(uri, reader, ffi.WriteMode(config.mode), nativeOptions)
	if err != nil {
		return nil, wrapError("write", uri, err)
	}
	dataset := &Dataset{native: native, uri: uri}
	dataset.res = newResource(native.Close)
	return dataset, nil
}

func writeUncommittedNative(uri string, reader array.RecordReader, config writeConfig) (*Transaction, error) {
	var storageAccessor *ffi.StorageOptionsAccessor
	var releaseStorage func()
	var err error
	if config.commitLock != nil {
		storageAccessor, releaseStorage, err = resolveLockStorageOptions("write uncommitted", uri, config.storageOptions, config.storageOptionsSet, config.storageOptionsAccessor)
	} else {
		storageAccessor, releaseStorage, err = resolveStorageOptions("write uncommitted", uri, config.storageOptions, config.storageOptionsSet, config.storageOptionsAccessor)
	}
	if err != nil {
		return nil, err
	}
	defer releaseStorage()
	if storageAccessor == nil && (config.storageOptionsSet || config.storageOptions != nil) {
		storageAccessor, err = ffi.NewStorageOptionsAccessorWithPresence(config.storageOptions, config.storageOptionsSet, nil)
		if err != nil {
			return nil, wrapError("write uncommitted", uri, err)
		}
		defer storageAccessor.Close()
	}

	session, releaseSession, err := acquireSession("write uncommitted", uri, config.session)
	if err != nil {
		return nil, err
	}
	defer releaseSession()

	spec, releases, err := nativeWriteOptionsSpec("write uncommitted", uri, config, session, storageAccessor, false)
	defer releaseAll(releases)
	if err != nil {
		return nil, wrapError("write uncommitted", uri, err)
	}
	if config.commitLock != nil {
		spec.CommitLock = config.commitLock.native
	}
	nativeOptions, err := ffi.NewNativeWriteOptions(spec)
	if err != nil {
		return nil, wrapError("write uncommitted", uri, err)
	}
	defer nativeOptions.Close()

	native, err := ffi.WriteUncommittedWithNativeOptions(uri, reader, ffi.WriteMode(config.mode), nativeOptions)
	if err != nil {
		return nil, wrapError("write uncommitted", uri, err)
	}
	return newTransaction(native, uri), nil
}

func nativeWriteOptionsSpec(op, uri string, config writeConfig, session *ffi.Session, storageAccessor *ffi.StorageOptionsAccessor, legacyAutoCleanup bool) (ffi.NativeWriteOptionsSpec, []func(), error) {
	releases := []func(){}
	writeOptions := nativeWriteOptions(config)
	if legacyAutoCleanup && writeOptions.AutoCleanup == nil {
		writeOptions.AutoCleanup = &ffi.AutoCleanupParams{
			Interval:         20,
			OlderThanSeconds: 14 * 24 * 60 * 60,
		}
	}

	spec := ffi.NativeWriteOptionsSpec{
		WriteOptions:                  &writeOptions,
		TransactionProperties:         config.transactionProperties,
		HasTransactionProperties:      config.hasTransactionProperties,
		EnableV2ManifestPaths:         config.params.EnableV2ManifestPaths,
		TargetBases:                   config.params.TargetBases,
		TargetBaseNamesOrPaths:        config.params.TargetBaseNamesOrPaths,
		TargetAllBases:                config.params.TargetAllBases,
		AllowExternalBlobOutsideBases: config.params.AllowExternalBlobOutsideBases,
		ExternalBlobMode:              int32(config.params.ExternalBlobMode),
		BlobPackFileSizeThreshold:     config.params.BlobPackFileSizeThreshold,
		Session:                       session,
		StorageOptionsAccessor:        storageAccessor,
	}
	if config.hasTransactionProperties && spec.TransactionProperties == nil {
		spec.TransactionProperties = map[string]string{}
	}

	if config.params.InitialBases != nil {
		spec.InitialBases = make([]ffi.BasePath, len(config.params.InitialBases))
		for i, base := range config.params.InitialBases {
			spec.InitialBases[i] = ffi.BasePath{
				ID:            base.ID,
				Path:          base.Path,
				Name:          base.Name,
				IsDatasetRoot: base.IsDatasetRoot,
			}
		}
	}

	if config.params.BaseStoreParams != nil {
		spec.BaseStoreParams = make(map[string]ffi.BaseStoreParams, len(config.params.BaseStoreParams))
		for path, params := range config.params.BaseStoreParams {
			baseAccessor, release, err := acquireStorageOptionsAccessor(op, uri, params.StorageOptionsAccessor)
			if err != nil {
				return spec, releases, err
			}
			releases = append(releases, release)
			spec.BaseStoreParams[path] = ffi.BaseStoreParams{
				StorageOptionsAccessor:     baseAccessor,
				BlockSize:                  params.BlockSize,
				S3CredentialsRefreshOffset: params.S3CredentialsRefreshOffset,
				UseConstantSizeUploadParts: params.UseConstantSizeUploadParts,
				ListIsLexicallyOrdered:     params.ListIsLexicallyOrdered,
			}
		}
	}

	if config.params.FileWriterOptions != nil {
		spec.FileWriterOptions = &ffi.FileWriterOptions{
			DataCacheBytes:    config.params.FileWriterOptions.DataCacheBytes,
			MaxPageBytes:      config.params.FileWriterOptions.MaxPageBytes,
			KeepOriginalArray: config.params.FileWriterOptions.KeepOriginalArray,
		}
	}

	if config.params.Progress != nil {
		progress := &ffi.WriteProgress{}
		if begin := config.params.Progress.BeginFragment; begin != nil {
			progress.BeginFragment = func(event WriteFragmentProgress) error {
				return begin(WriteFragmentProgress{
					FragmentID:   event.FragmentID,
					PhysicalRows: event.PhysicalRows,
					FileCount:    event.FileCount,
				})
			}
		}
		if complete := config.params.Progress.CompleteFragment; complete != nil {
			progress.CompleteFragment = func(event WriteFragmentProgress) error {
				return complete(WriteFragmentProgress{
					FragmentID:   event.FragmentID,
					PhysicalRows: event.PhysicalRows,
					FileCount:    event.FileCount,
				})
			}
		}
		if stats := config.params.Progress.WriteStats; stats != nil {
			progress.WriteStats = func(event WriteStats) {
				stats(WriteStats{
					BytesWritten: event.BytesWritten,
					RowsWritten:  event.RowsWritten,
					FilesWritten: event.FilesWritten,
				})
			}
		}
		spec.Progress = progress
	}

	return spec, releases, nil
}

func releaseAll(releases []func()) {
	for _, release := range releases {
		if release != nil {
			release()
		}
	}
}
