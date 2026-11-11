package file

import (
	"context"
	"fmt"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/lance-format/lance-go/internal/ffi"
)

// SessionOptions configures a raw file session.
type SessionOptions struct {
	// StorageOptions is nil for native defaults.
	StorageOptions *StorageOptions
	// Scheduler is nil for native defaults.
	Scheduler *SchedulerConfig
}

func (o *SessionOptions) ffiConfig() (*ffi.FileSchedulerConfig, error) {
	if o == nil || o.Scheduler == nil {
		return nil, nil
	}
	return &ffi.FileSchedulerConfig{
		IOBufferSizeBytes: o.Scheduler.IOBufferSizeBytes,
		LiteMode:          int32(o.Scheduler.LiteMode),
	}, nil
}

func sessionStorageOptions(opts *SessionOptions) *StorageOptions {
	if opts == nil {
		return nil
	}
	return opts.StorageOptions
}

// Session groups raw file operations under one resolved base path and object
// store. It is safe for concurrent use. Close waits for in-flight calls and is
// idempotent. Readers, writers, streams, and blob writers already opened
// remain usable after Close.
type Session struct {
	mu      sync.RWMutex
	closed  bool
	native  *ffi.FileSession
	baseURI string
}

// OpenSession opens a raw file session over baseURI.
func OpenSession(ctx context.Context, baseURI string, opts *SessionOptions) (*Session, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if baseURI == "" {
		return nil, invalidError("open file session", baseURI, "base URI must not be empty")
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	scheduler, err := opts.ffiConfig()
	if err != nil {
		return nil, wrapError("open file session", baseURI, err)
	}
	unlockStorage, accessor, err := lockStorage(sessionStorageOptions(opts))
	if err != nil {
		return nil, err
	}
	defer unlockStorage()

	native, err := ffi.OpenFileSession(baseURI, accessor, scheduler)
	if err != nil {
		return nil, wrapError("open file session", baseURI, err)
	}
	return &Session{native: native, baseURI: baseURI}, nil
}

func (s *Session) acquire(op string) (*ffi.FileSession, func(), error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, nil, closedError(op, s.baseURI)
	}
	return s.native, s.mu.RUnlock, nil
}

// Close releases the session allocation and waits for in-flight calls.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	native := s.native
	s.native = nil
	s.mu.Unlock()
	native.Close()
	return nil
}

// BasePath returns the resolved native base path.
func (s *Session) BasePath() (string, error) {
	native, unlock, err := s.acquire("session base path")
	if err != nil {
		return "", err
	}
	defer unlock()

	path, err := native.BasePath()
	if err != nil {
		return "", wrapError("session base path", s.baseURI, err)
	}
	return path, nil
}

// OpenReader opens relativePath as a raw Reader.
func (s *Session) OpenReader(ctx context.Context, relativePath string, opts *ReaderOptions) (*Reader, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if opts != nil && (opts.Scheduler != nil || opts.StorageOptions != nil) {
		return nil, invalidError("session open reader", relativePath, "scheduler and storage options belong to the session")
	}
	rawOptions, _, err := opts.ffiOptions()
	if err != nil {
		return nil, wrapError("session open reader", relativePath, err)
	}
	native, unlock, err := s.acquire("session open reader")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	reader, err := native.OpenReader(relativePath, rawOptions)
	if err != nil {
		return nil, wrapError("session open reader", relativePath, err)
	}
	return &Reader{native: reader, uri: relativePath}, nil
}

// OpenWriter opens relativePath as a raw Writer. A nil schema creates a lazy
// writer that adopts the first batch schema.
func (s *Session) OpenWriter(ctx context.Context, relativePath string, schema *arrow.Schema, opts *WriterOptions) (*Writer, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if opts != nil && opts.StorageOptions != nil {
		return nil, invalidError("session open writer", relativePath, "storage options belong to the session")
	}
	rawOptions, err := opts.ffiOptions()
	if err != nil {
		return nil, wrapError("session open writer", relativePath, err)
	}
	native, unlock, err := s.acquire("session open writer")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	writer, err := native.OpenWriter(relativePath, schema, rawOptions)
	if err != nil {
		return nil, wrapError("session open writer", relativePath, err)
	}
	return &Writer{native: writer, uri: relativePath}, nil
}

// Contains reports whether relativePath exists.
func (s *Session) Contains(ctx context.Context, relativePath string) (bool, error) {
	if err := checkContext(ctx); err != nil {
		return false, err
	}
	native, unlock, err := s.acquire("session contains")
	if err != nil {
		return false, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return false, err
	}

	exists, err := native.Contains(relativePath)
	if err != nil {
		return false, wrapError("session contains", relativePath, err)
	}
	return exists, nil
}

// List returns paths relative to the session base.
func (s *Session) List(ctx context.Context, relativePath string) ([]string, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	native, unlock, err := s.acquire("session list")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	paths, err := native.List(relativePath)
	if err != nil {
		return nil, wrapError("session list", relativePath, err)
	}
	return paths, nil
}

// DelimitedList is a copied native list-with-delimiter result.
type DelimitedList struct {
	CommonPrefixes []string
	Objects        []string
}

// ListWithDelimiter lists paths and common prefixes relative to the session
// base.
func (s *Session) ListWithDelimiter(ctx context.Context, relativePath string) (DelimitedList, error) {
	if err := checkContext(ctx); err != nil {
		return DelimitedList{}, err
	}
	native, unlock, err := s.acquire("session list with delimiter")
	if err != nil {
		return DelimitedList{}, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return DelimitedList{}, err
	}

	list, err := native.ListWithDelimiter(relativePath)
	if err != nil {
		return DelimitedList{}, wrapError("session list with delimiter", relativePath, err)
	}
	return DelimitedList{CommonPrefixes: list.CommonPrefixes, Objects: list.Objects}, nil
}

// ReadRange reads length bytes at offset from relativePath.
func (s *Session) ReadRange(ctx context.Context, relativePath string, offset, length uint64) ([]byte, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	native, unlock, err := s.acquire("session read range")
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	data, err := native.ReadRange(relativePath, offset, length)
	if err != nil {
		return nil, wrapError("session read range", relativePath, err)
	}
	return data, nil
}

// DeleteFile removes relativePath.
func (s *Session) DeleteFile(ctx context.Context, relativePath string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	native, unlock, err := s.acquire("session delete")
	if err != nil {
		return err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}

	if err := native.DeleteFile(relativePath); err != nil {
		return wrapError("session delete", relativePath, err)
	}
	return nil
}

// UploadFile uploads localPath to relativeRemotePath.
func (s *Session) UploadFile(ctx context.Context, localPath, relativeRemotePath string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	native, unlock, err := s.acquire("session upload")
	if err != nil {
		return err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}

	if err := native.UploadFile(localPath, relativeRemotePath); err != nil {
		return wrapError("session upload", relativeRemotePath, err)
	}
	return nil
}

// DownloadFile downloads relativeRemotePath to localPath.
func (s *Session) DownloadFile(ctx context.Context, relativeRemotePath, localPath string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	native, unlock, err := s.acquire("session download")
	if err != nil {
		return err
	}
	defer unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}

	if err := native.DownloadFile(relativeRemotePath, localPath); err != nil {
		return wrapError("session download", relativeRemotePath, err)
	}
	return nil
}

// RegistryStats is a native object-store registry counter snapshot.
type RegistryStats struct {
	Hits         uint64
	Misses       uint64
	ActiveStores uint64
}

// StoreInfo describes the resolved object store.
type StoreInfo struct {
	BlockSize            uint64
	MaxIOPSize           uint64
	IOParallelism        uint64
	IsLocal              bool
	IsCloud              bool
	PrefersLiteScheduler bool
}

// StoreIOStats is a native object-store IO counter snapshot.
type StoreIOStats struct {
	ReadIOPS     uint64
	ReadBytes    uint64
	WriteIOPS    uint64
	WrittenBytes uint64
}

// SchedulerStats returns native scheduler counters.
func (s *Session) SchedulerStats() (ScanStats, error) {
	native, unlock, err := s.acquire("session scheduler stats")
	if err != nil {
		return ScanStats{}, err
	}
	defer unlock()

	stats, err := native.SchedulerStats()
	if err != nil {
		return ScanStats{}, wrapError("session scheduler stats", s.baseURI, err)
	}
	return ScanStats{IOPS: stats.IOPS, Requests: stats.Requests, BytesRead: stats.BytesRead}, nil
}

// RegistryStats returns native object-store registry counters.
func (s *Session) RegistryStats() (RegistryStats, error) {
	native, unlock, err := s.acquire("session registry stats")
	if err != nil {
		return RegistryStats{}, err
	}
	defer unlock()

	stats, err := native.RegistryStats()
	if err != nil {
		return RegistryStats{}, wrapError("session registry stats", s.baseURI, err)
	}
	return RegistryStats{Hits: stats.Hits, Misses: stats.Misses, ActiveStores: stats.ActiveStores}, nil
}

// StoreInfo returns the resolved native object-store properties.
func (s *Session) StoreInfo() (StoreInfo, error) {
	native, unlock, err := s.acquire("session store info")
	if err != nil {
		return StoreInfo{}, err
	}
	defer unlock()

	info, err := native.StoreInfo()
	if err != nil {
		return StoreInfo{}, wrapError("session store info", s.baseURI, err)
	}
	return StoreInfo{
		BlockSize:            info.BlockSize,
		MaxIOPSize:           info.MaxIOPSize,
		IOParallelism:        info.IOParallelism,
		IsLocal:              info.IsLocal,
		IsCloud:              info.IsCloud,
		PrefersLiteScheduler: info.PrefersLiteScheduler,
	}, nil
}

// StoreIOStats returns native object-store IO counters.
func (s *Session) StoreIOStats() (StoreIOStats, error) {
	native, unlock, err := s.acquire("session store io stats")
	if err != nil {
		return StoreIOStats{}, err
	}
	defer unlock()

	stats, err := native.StoreIOStats()
	if err != nil {
		return StoreIOStats{}, wrapError("session store io stats", s.baseURI, err)
	}
	return StoreIOStats{
		ReadIOPS:     stats.ReadIOPS,
		ReadBytes:    stats.ReadBytes,
		WriteIOPS:    stats.WriteIOPS,
		WrittenBytes: stats.WrittenBytes,
	}, nil
}

func (s *Session) String() string {
	if s == nil {
		return "file.Session(nil)"
	}
	return fmt.Sprintf("file.Session(%s)", s.baseURI)
}
