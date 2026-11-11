package ffi

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// ftsMatchOptions mirrors LanceFtsMatchOptions.
type ftsMatchOptions struct {
	boost               float32
	fuzziness           int64
	maxExpansions       uint64
	matchOperator       int32
	prefixLength        uint32
	documentGranularity int32
}

// ftsPhraseOptions mirrors LanceFtsPhraseOptions.
type ftsPhraseOptions struct {
	slop                uint32
	documentGranularity int32
}

// ftsMultiMatchOptions mirrors LanceFtsMultiMatchOptions.
type ftsMultiMatchOptions struct {
	boosts        *float32
	boostCount    uint64
	matchOperator int32
}

// ftsCombinedFieldsOptions mirrors LanceFtsCombinedFieldsOptions.
type ftsCombinedFieldsOptions struct {
	boosts        *float32
	boostCount    uint64
	matchOperator int32
}

// ftsBoostOptions mirrors LanceFtsBoostOptions.
type ftsBoostOptions struct {
	negativeBoost float32
}

// FTSMatchOptions holds Match query options. Nil optional fields keep native
// defaults.
type FTSMatchOptions struct {
	Boost               *float32
	Fuzziness           int64
	MaxExpansions       *uint64
	MatchOperator       int32
	PrefixLength        uint32
	DocumentGranularity int32
}

// FTSPhraseOptions holds Phrase query options.
type FTSPhraseOptions struct {
	Slop                uint32
	DocumentGranularity int32
}

// FTSMultiMatchOptions holds MultiMatch query options.
type FTSMultiMatchOptions struct {
	Boosts        []float32
	MatchOperator int32
}

// FTSCombinedFieldsOptions holds CombinedFields query options.
type FTSCombinedFieldsOptions struct {
	Boosts        []float32
	MatchOperator int32
}

// FTSBoostOptions holds Boost query options. A nil NegativeBoost keeps the
// native default.
type FTSBoostOptions struct {
	NegativeBoost *float32
}

// FTSQuery is an immutable native full-text query value.
type FTSQuery struct {
	b      *bindings
	handle uintptr

	mu     sync.RWMutex
	closed bool
}

// acquire returns the native handle while holding a read lock. Callers must
// call release after the native call so Close cannot free the handle in
// between.
func (q *FTSQuery) acquire() (uintptr, bool) {
	if q == nil {
		return 0, false
	}
	q.mu.RLock()
	if q.closed || q.handle == 0 {
		q.mu.RUnlock()
		return 0, false
	}
	return q.handle, true
}

func (q *FTSQuery) release() {
	q.mu.RUnlock()
}

// Close releases the native query. It is idempotent and waits for in-flight
// native calls that borrowed the handle.
func (q *FTSQuery) Close() error {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil
	}
	handle := q.handle
	q.handle = 0
	q.closed = true
	if handle != 0 {
		q.b.ftsQueryClose(handle)
	}
	return nil
}

// FTSQueryContext is a prepared, process-local query context pinned to one
// native dataset snapshot.
type FTSQueryContext struct {
	b      *bindings
	handle uintptr

	mu     sync.RWMutex
	closed bool
}

// acquire returns the native handle while holding a read lock.
func (c *FTSQueryContext) acquire() (uintptr, bool) {
	if c == nil {
		return 0, false
	}
	c.mu.RLock()
	if c.closed || c.handle == 0 {
		c.mu.RUnlock()
		return 0, false
	}
	return c.handle, true
}

func (c *FTSQueryContext) release() {
	c.mu.RUnlock()
}

// Close releases the native prepared context. It is idempotent and waits for
// in-flight scanner attachment.
func (c *FTSQueryContext) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	handle := c.handle
	c.handle = 0
	c.closed = true
	if handle != 0 {
		c.b.ftsQueryContextClose(handle)
	}
	return nil
}

func newFTSQuery(op string, create func(*bindings) uintptr) (*FTSQuery, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	handle := create(b)
	var nativeErr *Error
	if handle == 0 {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()
	if handle == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: %s returned NULL without setting an error", op)
	}
	return &FTSQuery{b: b, handle: handle}, nil
}

func cStringListPointer(list *cStringList) uintptr {
	if list == nil || len(list.ptrs) == 0 {
		return 0
	}
	return list.ptrs[0]
}

// NewFTSMatchQuery creates an immutable Match query. A nil column lets scan
// planning resolve the indexed columns.
func NewFTSMatchQuery(terms string, column *string, opts FTSMatchOptions) (*FTSQuery, error) {
	var columnList *cStringList
	if column != nil {
		list, err := newCStringList([]string{*column})
		if err != nil {
			return nil, err
		}
		columnList = list
	}
	b, err := load()
	if err != nil {
		return nil, err
	}
	var raw ftsMatchOptions
	runtime.LockOSThread()
	b.ftsMatchOptionsInit(&raw)
	if opts.Boost != nil {
		raw.boost = *opts.Boost
	}
	raw.fuzziness = opts.Fuzziness
	if opts.MaxExpansions != nil {
		raw.maxExpansions = *opts.MaxExpansions
	}
	raw.matchOperator = opts.MatchOperator
	raw.prefixLength = opts.PrefixLength
	raw.documentGranularity = opts.DocumentGranularity
	handle := b.ftsQueryMatchNew(terms, cStringListPointer(columnList), &raw)
	var nativeErr *Error
	if handle == 0 {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()
	columnList.keepAlive()
	if handle == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fts_query_match_new returned NULL without setting an error")
	}
	return &FTSQuery{b: b, handle: handle}, nil
}

// NewFTSPhraseQuery creates an immutable Phrase query.
func NewFTSPhraseQuery(terms string, column *string, opts FTSPhraseOptions) (*FTSQuery, error) {
	var columnList *cStringList
	if column != nil {
		list, err := newCStringList([]string{*column})
		if err != nil {
			return nil, err
		}
		columnList = list
	}
	b, err := load()
	if err != nil {
		return nil, err
	}
	var raw ftsPhraseOptions
	runtime.LockOSThread()
	b.ftsPhraseOptionsInit(&raw)
	raw.slop = opts.Slop
	raw.documentGranularity = opts.DocumentGranularity
	handle := b.ftsQueryPhraseNew(terms, cStringListPointer(columnList), &raw)
	var nativeErr *Error
	if handle == 0 {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()
	columnList.keepAlive()
	if handle == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fts_query_phrase_new returned NULL without setting an error")
	}
	return &FTSQuery{b: b, handle: handle}, nil
}

func newFTSMultiMatchQuery(terms string, columns []string, opts FTSMultiMatchOptions) (*FTSQuery, error) {
	list, err := newCStringList(columns)
	if err != nil {
		return nil, err
	}
	b, err := load()
	if err != nil {
		return nil, err
	}
	var raw ftsMultiMatchOptions
	runtime.LockOSThread()
	b.ftsMultiMatchOptionsInit(&raw)
	if len(opts.Boosts) > 0 {
		raw.boosts = (*float32)(unsafe.Pointer(&opts.Boosts[0]))
		raw.boostCount = uint64(len(opts.Boosts))
	}
	raw.matchOperator = opts.MatchOperator
	handle := b.ftsQueryMultiMatchNew(terms, list.ptrs[:len(columns)], uint64(len(columns)), &raw)
	var nativeErr *Error
	if handle == 0 {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()
	list.keepAlive()
	runtime.KeepAlive(opts.Boosts)
	if handle == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fts_query_multi_match_new returned NULL without setting an error")
	}
	return &FTSQuery{b: b, handle: handle}, nil
}

// NewFTSMultiMatchQuery creates an immutable MultiMatch query.
func NewFTSMultiMatchQuery(terms string, columns []string, opts FTSMultiMatchOptions) (*FTSQuery, error) {
	return newFTSMultiMatchQuery(terms, columns, opts)
}

// NewFTSCombinedFieldsQuery creates an immutable CombinedFields query.
func NewFTSCombinedFieldsQuery(terms string, columns []string, opts FTSCombinedFieldsOptions) (*FTSQuery, error) {
	list, err := newCStringList(columns)
	if err != nil {
		return nil, err
	}
	b, err := load()
	if err != nil {
		return nil, err
	}
	var raw ftsCombinedFieldsOptions
	runtime.LockOSThread()
	b.ftsCombinedFieldsOptionsInit(&raw)
	if len(opts.Boosts) > 0 {
		raw.boosts = (*float32)(unsafe.Pointer(&opts.Boosts[0]))
		raw.boostCount = uint64(len(opts.Boosts))
	}
	raw.matchOperator = opts.MatchOperator
	handle := b.ftsQueryCombinedFieldsNew(terms, list.ptrs[:len(columns)], uint64(len(columns)), &raw)
	var nativeErr *Error
	if handle == 0 {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()
	list.keepAlive()
	runtime.KeepAlive(opts.Boosts)
	if handle == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fts_query_combined_fields_new returned NULL without setting an error")
	}
	return &FTSQuery{b: b, handle: handle}, nil
}

// NewFTSBooleanQuery creates an immutable Boolean query. Occurs and queries
// must have the same length; the native constructor accepts an empty query.
func NewFTSBooleanQuery(occurs []int32, queries []*FTSQuery) (*FTSQuery, error) {
	if len(occurs) != len(queries) {
		return nil, fmt.Errorf("ffi: boolean query occurs and queries must have the same length")
	}
	b, err := load()
	if err != nil {
		return nil, err
	}
	handles := make([]uintptr, len(queries))
	for i, query := range queries {
		handle, ok := query.acquire()
		if !ok {
			for j := 0; j < i; j++ {
				queries[j].release()
			}
			return nil, fmt.Errorf("ffi: boolean query child %d is closed or nil", i)
		}
		handles[i] = handle
	}
	runtime.LockOSThread()
	handle := b.ftsQueryBooleanNew(occurs, handles, uint64(len(queries)))
	var nativeErr *Error
	if handle == 0 {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()
	for _, query := range queries {
		query.release()
	}
	runtime.KeepAlive(occurs)
	if handle == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fts_query_boolean_new returned NULL without setting an error")
	}
	return &FTSQuery{b: b, handle: handle}, nil
}

// NewFTSBoostQuery creates an immutable Boost query.
func NewFTSBoostQuery(positive, negative *FTSQuery, opts FTSBoostOptions) (*FTSQuery, error) {
	b, err := load()
	if err != nil {
		return nil, err
	}
	positiveHandle, ok := positive.acquire()
	if !ok {
		return nil, fmt.Errorf("ffi: positive boost query is closed")
	}
	negativeHandle, ok := negative.acquire()
	if !ok {
		positive.release()
		return nil, fmt.Errorf("ffi: negative boost query is closed")
	}
	var raw ftsBoostOptions
	runtime.LockOSThread()
	b.ftsBoostOptionsInit(&raw)
	if opts.NegativeBoost != nil {
		raw.negativeBoost = *opts.NegativeBoost
	}
	handle := b.ftsQueryBoostNew(positiveHandle, negativeHandle, &raw)
	var nativeErr *Error
	if handle == 0 {
		nativeErr = b.takeError()
	}
	runtime.UnlockOSThread()
	positive.release()
	negative.release()
	if handle == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_fts_query_boost_new returned NULL without setting an error")
	}
	return &FTSQuery{b: b, handle: handle}, nil
}

// PrepareFTSMatchQuery prepares a Match query context pinned to d's snapshot.
func (d *Dataset) PrepareFTSMatchQuery(column, query string, matchOperator int32, maxFuzzyDistance uint32, coverage int32) (*FTSQueryContext, error) {
	handle, err := d.nativeHandle()
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	ctx := d.b.datasetPrepareFTSMatchQuery(handle, column, query, matchOperator, maxFuzzyDistance, coverage)
	var nativeErr *Error
	if ctx == 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	if ctx == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_prepare_fts_match_query returned NULL without setting an error")
	}
	return &FTSQueryContext{b: d.b, handle: ctx}, nil
}

// PrepareFTSPhraseQuery prepares a Phrase query context pinned to d's snapshot.
func (d *Dataset) PrepareFTSPhraseQuery(column, query string, slop int32, coverage int32) (*FTSQueryContext, error) {
	handle, err := d.nativeHandle()
	if err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	ctx := d.b.datasetPrepareFTSPhraseQuery(handle, column, query, slop, coverage)
	var nativeErr *Error
	if ctx == 0 {
		nativeErr = d.b.takeError()
	}
	runtime.UnlockOSThread()
	if ctx == 0 {
		if nativeErr != nil {
			return nil, nativeErr
		}
		return nil, fmt.Errorf("ffi: lance_dataset_prepare_fts_phrase_query returned NULL without setting an error")
	}
	return &FTSQueryContext{b: d.b, handle: ctx}, nil
}

func (s *Scanner) setFTSCall(op string, call func(uintptr) int32) error {
	handle := s.handle.Load()
	if handle == 0 {
		return ErrClosed
	}
	runtime.LockOSThread()
	rc := call(handle)
	var nativeErr *Error
	if rc != 0 {
		nativeErr = s.b.takeError()
	}
	runtime.UnlockOSThread()
	if rc != 0 {
		if nativeErr != nil {
			return nativeErr
		}
		return fmt.Errorf("ffi: %s failed without setting an error", op)
	}
	return nil
}

// SetFTSQuery attaches a typed FTS query to the scanner.
func (s *Scanner) SetFTSQuery(query *FTSQuery) error {
	handle, ok := query.acquire()
	if !ok {
		return ErrClosed
	}
	defer query.release()
	return s.setFTSCall("lance_scanner_set_fts_query", func(scanner uintptr) int32 {
		return s.b.scannerSetFTSQuery(scanner, handle)
	})
}

// SetFTSQueryContext attaches a prepared FTS query context to the scanner.
func (s *Scanner) SetFTSQueryContext(ctx *FTSQueryContext) error {
	handle, ok := ctx.acquire()
	if !ok {
		return ErrClosed
	}
	defer ctx.release()
	return s.setFTSCall("lance_scanner_set_fts_query_context", func(scanner uintptr) int32 {
		return s.b.scannerSetFTSQueryContext(scanner, handle)
	})
}

// SetMinHashQuery attaches a MinHash LSH query to the scanner.
func (s *Scanner) SetMinHashQuery(column, text string) error {
	return s.setFTSCall("lance_scanner_set_minhash_query", func(scanner uintptr) int32 {
		return s.b.scannerSetMinHashQuery(scanner, column, text)
	})
}

// SetFTSIndexSegments restricts the scan to the given raw 16-byte UUIDs. An
// empty slice clears the selection.
func (s *Scanner) SetFTSIndexSegments(uuids []byte) error {
	if len(uuids)%16 != 0 {
		return fmt.Errorf("ffi: FTS index segments must be 16-byte UUIDs")
	}
	return s.setFTSCall("lance_scanner_set_fts_index_segments", func(scanner uintptr) int32 {
		return s.b.scannerSetFTSIndexSegments(scanner, uuids, uint64(len(uuids)/16))
	})
}
