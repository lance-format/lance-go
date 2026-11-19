package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/lance-format/lance-go/internal/handle"
)

const (
	procSmapsRollup = "/proc/self/smaps_rollup"
	procStatus      = "/proc/self/status"
	procFDs         = "/proc/self/fd"
)

// errMemoryUnsupported distinguishes a platform that cannot provide the
// required total-process RSS measurement from a broken procfs file.
var errMemoryUnsupported = errors.New("process RSS sampling requires Linux procfs /proc/self/smaps_rollup")

// smapsFields lists the optional smaps_rollup lines this benchmark records.
// Rss is handled separately because it is mandatory: reporting zero bytes for
// an unavailable measurement would look like valid evidence.
var smapsFields = []string{
	"Rss",
	"Pss",
	"Anonymous",
	"Shared_Clean",
	"Shared_Dirty",
	"Private_Clean",
	"Private_Dirty",
	"Pss_File",
	"Swap",
}

type sampler struct {
	smapsPath  string
	statusPath string
	fdPath     string
}

func newSampler() (*sampler, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("%w (GOOS=%s)", errMemoryUnsupported, runtime.GOOS)
	}
	s := &sampler{smapsPath: procSmapsRollup, statusPath: procStatus, fdPath: procFDs}
	if _, err := s.readMemory(); err != nil {
		return nil, fmt.Errorf("process RSS sampling unavailable: %w", err)
	}
	return s, nil
}

// memoryStats holds values converted to bytes from the kB values that the
// kernel reports. Optional fields are pointers so that a missing kernel field
// is omitted from the journal instead of being encoded as zero.
type memoryStats struct {
	RSSBytes          int64
	PSSBytes          *int64
	AnonymousBytes    *int64
	SharedCleanBytes  *int64
	SharedDirtyBytes  *int64
	PrivateCleanBytes *int64
	PrivateDirtyBytes *int64
	PSSFileBytes      *int64
	SwapBytes         *int64
	Unavailable       []string
}

func (s *sampler) readMemory() (memoryStats, error) {
	f, err := os.Open(s.smapsPath)
	if err != nil {
		return memoryStats{}, err
	}
	defer f.Close()
	return parseSmapsRollup(f)
}

func parseSmapsRollup(r io.Reader) (memoryStats, error) {
	values := make(map[string]int64, len(smapsFields))
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		valueText := strings.TrimSpace(line[colon+1:])
		// Every numeric smaps_rollup field ends in kB. Ignoring other lines
		// keeps the parser stable when a kernel adds unrelated metadata.
		if !strings.HasSuffix(valueText, "kB") {
			continue
		}
		number := strings.TrimSpace(strings.TrimSuffix(valueText, "kB"))
		value, err := strconv.ParseInt(number, 10, 64)
		if err != nil {
			return memoryStats{}, fmt.Errorf("parse smaps_rollup field %q: %w", key, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return memoryStats{}, err
	}
	rssKB, ok := values["Rss"]
	if !ok {
		return memoryStats{}, fmt.Errorf("%s has no Rss field", procSmapsRollup)
	}

	stats := memoryStats{RSSBytes: rssKB * 1024}
	for _, field := range smapsFields {
		if _, ok := values[field]; !ok {
			stats.Unavailable = append(stats.Unavailable, field)
		}
	}
	stats.PSSBytes = optionalKB(values, "Pss")
	stats.AnonymousBytes = optionalKB(values, "Anonymous")
	stats.SharedCleanBytes = optionalKB(values, "Shared_Clean")
	stats.SharedDirtyBytes = optionalKB(values, "Shared_Dirty")
	stats.PrivateCleanBytes = optionalKB(values, "Private_Clean")
	stats.PrivateDirtyBytes = optionalKB(values, "Private_Dirty")
	stats.PSSFileBytes = optionalKB(values, "Pss_File")
	stats.SwapBytes = optionalKB(values, "Swap")
	return stats, nil
}

func optionalKB(values map[string]int64, key string) *int64 {
	value, ok := values[key]
	if !ok {
		return nil
	}
	bytes := value * 1024
	return &bytes
}

func parseThreads(r io.Reader) (int64, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Threads:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "Threads:"))
		threads, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse /proc/self/status Threads: %w", err)
		}
		return threads, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("/proc/self/status has no Threads field")
}

func (s *sampler) threadCount() (int64, error) {
	f, err := os.Open(s.statusPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return parseThreads(f)
}

// fdCount reads /proc/self/fd. os.ReadDir opens the directory itself, so the
// listing contains one descriptor for this count and it is subtracted. This is
// a best-effort diagnostic: another thread may open or close descriptors while
// the listing is assembled.
func (s *sampler) fdCount() (int64, error) {
	entries, err := os.ReadDir(s.fdPath)
	if err != nil {
		return 0, err
	}
	count := int64(len(entries)) - 1
	if count < 0 {
		count = 0
	}
	return count, nil
}

type goStats struct {
	HeapAllocBytes uint64
	HeapSysBytes   uint64
	SysBytes       uint64
	NumGC          uint32
	Goroutines     int
}

func readGoStats() goStats {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return goStats{
		HeapAllocBytes: stats.HeapAlloc,
		HeapSysBytes:   stats.HeapSys,
		SysBytes:       stats.Sys,
		NumGC:          stats.NumGC,
		Goroutines:     runtime.NumGoroutine(),
	}
}

type processSnapshot struct {
	Memory       memoryStats
	Handles      int
	FDs          *int64
	FDsError     string
	Threads      *int64
	ThreadsError string
	Go           goStats
}

func (s *sampler) snapshot() (processSnapshot, error) {
	memory, err := s.readMemory()
	if err != nil {
		return processSnapshot{}, err
	}
	snapshot := processSnapshot{
		Memory:  memory,
		Handles: handle.Len(),
		Go:      readGoStats(),
	}
	if fds, err := s.fdCount(); err != nil {
		snapshot.FDsError = err.Error()
	} else {
		snapshot.FDs = &fds
	}
	if threads, err := s.threadCount(); err != nil {
		snapshot.ThreadsError = err.Error()
	} else {
		snapshot.Threads = &threads
	}
	return snapshot, nil
}

type memorySource struct {
	Source            string   `json:"source"`
	Units             string   `json:"units"`
	AvailableFields   []string `json:"available_fields"`
	UnavailableFields []string `json:"unavailable_fields,omitempty"`
	Notes             []string `json:"notes"`
}

func memorySourceFrom(stats memoryStats) memorySource {
	unavailable := make(map[string]bool, len(stats.Unavailable))
	for _, field := range stats.Unavailable {
		unavailable[field] = true
	}
	source := memorySource{
		Source: procSmapsRollup,
		Units:  "bytes (kernel values are reported in kB and multiplied by 1024)",
		Notes: []string{
			"rss_bytes is the total resident set of this process, including the Go runtime, native lance-c allocations, shared mappings and file-backed pages. It is not native-live-byte accounting and rss_bytes minus go_heap_alloc_bytes is not native allocation.",
			"Optional smaps_rollup fields that this kernel does not expose are omitted and listed in unavailable_fields.",
			"threads and fds are read from procfs in separate reads and are not an atomic snapshot with rss_bytes.",
			"fds is a best-effort count from /proc/self/fd minus the directory descriptor used to enumerate it.",
		},
	}
	for _, field := range smapsFields {
		if unavailable[field] {
			source.UnavailableFields = append(source.UnavailableFields, field)
		} else {
			source.AvailableFields = append(source.AvailableFields, field)
		}
	}
	return source
}
