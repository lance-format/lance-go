package lance

import (
	"context"
	"fmt"

	"github.com/lance-format/lance-go/internal/ffi"
)

// MatchOperator combines the terms of a full-text query.
type MatchOperator int32

const (
	// MatchOperatorOr matches documents containing any term.
	MatchOperatorOr MatchOperator = 0
	// MatchOperatorAnd matches documents containing every term.
	MatchOperatorAnd MatchOperator = 1
)

// FTSDocumentGranularity selects how full-text matches map to rows.
type FTSDocumentGranularity int32

const (
	FTSDocumentUnspecified FTSDocumentGranularity = 0
	FTSDocumentRow         FTSDocumentGranularity = 1
	FTSDocumentListElement FTSDocumentGranularity = 2
)

// FTSOccur controls how a Boolean query clause participates.
type FTSOccur int32

const (
	FTSOccurShould  FTSOccur = 0
	FTSOccurMust    FTSOccur = 1
	FTSOccurMustNot FTSOccur = 2
)

// FTSCoverage selects which fragments a prepared query may serve.
type FTSCoverage int32

const (
	// FTSCoverageStrict rejects preparation if any fragment is not covered by the index.
	FTSCoverageStrict FTSCoverage = 0
	// FTSCoverageIndexOnly excludes unindexed fragments from scoring and query results.
	FTSCoverageIndexOnly FTSCoverage = 1
)

// FTSMatchOptions configures a Match query. A nil Boost or MaxExpansions keeps
// the native default. Fuzziness 0 means exact matching and -1 selects native
// automatic fuzziness.
type FTSMatchOptions struct {
	Boost         *float32
	Fuzziness     int64
	MaxExpansions *uint64
	Operator      MatchOperator
	PrefixLength  uint32
	Granularity   FTSDocumentGranularity
}

// FTSPhraseOptions configures a Phrase query.
type FTSPhraseOptions struct {
	Slop        uint32
	Granularity FTSDocumentGranularity
}

// FTSMultiMatchOptions configures a MultiMatch query. A nil or empty Boosts
// gives every column weight 1.
type FTSMultiMatchOptions struct {
	Boosts   []float32
	Operator MatchOperator
}

// FTSCombinedFieldsOptions configures a CombinedFields query. A nil or empty
// Boosts gives every field weight 1.
type FTSCombinedFieldsOptions struct {
	Boosts   []float32
	Operator MatchOperator
}

// FTSBoostOptions configures a Boost query. A nil NegativeBoost keeps the
// native default.
type FTSBoostOptions struct {
	NegativeBoost *float32
}

// FTSQuery is an immutable full-text query. Close releases the native query
// after any scanners that attached it have cloned it.
type FTSQuery struct {
	native *ffi.FTSQuery
}

// NewFTSMatchQuery builds a Match query. An empty column lets scan planning
// resolve the indexed column.
func NewFTSMatchQuery(terms, column string, options FTSMatchOptions) (*FTSQuery, error) {
	if err := validateFTSStrings(terms, column); err != nil {
		return nil, err
	}
	if options.Operator != MatchOperatorOr && options.Operator != MatchOperatorAnd {
		return nil, fmt.Errorf("lance: FTS match operator %d is invalid", options.Operator)
	}
	native, err := ffi.NewFTSMatchQuery(terms, optionalColumn(column), ffi.FTSMatchOptions{
		Boost:               options.Boost,
		Fuzziness:           options.Fuzziness,
		MaxExpansions:       options.MaxExpansions,
		MatchOperator:       int32(options.Operator),
		PrefixLength:        options.PrefixLength,
		DocumentGranularity: int32(options.Granularity),
	})
	if err != nil {
		return nil, wrapError("FTS match query", "", err)
	}
	return &FTSQuery{native: native}, nil
}

// NewFTSPhraseQuery builds a Phrase query. An empty column lets scan planning
// resolve the indexed column.
func NewFTSPhraseQuery(terms, column string, options FTSPhraseOptions) (*FTSQuery, error) {
	if err := validateFTSStrings(terms, column); err != nil {
		return nil, err
	}
	native, err := ffi.NewFTSPhraseQuery(terms, optionalColumn(column), ffi.FTSPhraseOptions{
		Slop:                options.Slop,
		DocumentGranularity: int32(options.Granularity),
	})
	if err != nil {
		return nil, wrapError("FTS phrase query", "", err)
	}
	return &FTSQuery{native: native}, nil
}

// NewFTSMultiMatchQuery builds a MultiMatch query over explicit columns.
func NewFTSMultiMatchQuery(terms string, columns []string, options FTSMultiMatchOptions) (*FTSQuery, error) {
	if err := validateFTSStrings(terms); err != nil {
		return nil, err
	}
	for _, column := range columns {
		if err := validateCString(column); err != nil {
			return nil, fmt.Errorf("lance: FTS column: %w", err)
		}
	}
	if options.Operator != MatchOperatorOr && options.Operator != MatchOperatorAnd {
		return nil, fmt.Errorf("lance: FTS multi-match operator %d is invalid", options.Operator)
	}
	native, err := ffi.NewFTSMultiMatchQuery(terms, columns, ffi.FTSMultiMatchOptions{
		Boosts:        options.Boosts,
		MatchOperator: int32(options.Operator),
	})
	if err != nil {
		return nil, wrapError("FTS multi-match query", "", err)
	}
	return &FTSQuery{native: native}, nil
}

// NewFTSCombinedFieldsQuery builds a CombinedFields query over explicit
// columns.
func NewFTSCombinedFieldsQuery(terms string, columns []string, options FTSCombinedFieldsOptions) (*FTSQuery, error) {
	if err := validateFTSStrings(terms); err != nil {
		return nil, err
	}
	for _, column := range columns {
		if err := validateCString(column); err != nil {
			return nil, fmt.Errorf("lance: FTS column: %w", err)
		}
	}
	if options.Operator != MatchOperatorOr && options.Operator != MatchOperatorAnd {
		return nil, fmt.Errorf("lance: FTS combined-fields operator %d is invalid", options.Operator)
	}
	native, err := ffi.NewFTSCombinedFieldsQuery(terms, columns, ffi.FTSCombinedFieldsOptions{
		Boosts:        options.Boosts,
		MatchOperator: int32(options.Operator),
	})
	if err != nil {
		return nil, wrapError("FTS combined-fields query", "", err)
	}
	return &FTSQuery{native: native}, nil
}

// NewFTSBooleanQuery combines queries with per-clause occurrence. Occurs and
// queries must have the same length; an empty query is valid at construction
// and rejected at planning.
func NewFTSBooleanQuery(occurs []FTSOccur, queries []*FTSQuery) (*FTSQuery, error) {
	if len(occurs) != len(queries) {
		return nil, fmt.Errorf("lance: FTS boolean query has %d occurs for %d queries", len(occurs), len(queries))
	}
	nativeOccurs := make([]int32, len(occurs))
	nativeQueries := make([]*ffi.FTSQuery, len(queries))
	for i, occur := range occurs {
		if occur != FTSOccurShould && occur != FTSOccurMust && occur != FTSOccurMustNot {
			return nil, fmt.Errorf("lance: FTS boolean occur %d is invalid", occur)
		}
		nativeOccurs[i] = int32(occur)
		if queries[i] == nil {
			return nil, fmt.Errorf("lance: FTS boolean query %d is nil", i)
		}
		nativeQueries[i] = queries[i].native
	}
	native, err := ffi.NewFTSBooleanQuery(nativeOccurs, nativeQueries)
	if err != nil {
		return nil, wrapError("FTS boolean query", "", err)
	}
	return &FTSQuery{native: native}, nil
}

// NewFTSBoostQuery builds a query that boosts or penalizes matches of positive
// and negative.
func NewFTSBoostQuery(positive, negative *FTSQuery, options FTSBoostOptions) (*FTSQuery, error) {
	if positive == nil || negative == nil {
		return nil, fmt.Errorf("lance: FTS boost query requires both positive and negative queries")
	}
	native, err := ffi.NewFTSBoostQuery(positive.native, negative.native, ffi.FTSBoostOptions{
		NegativeBoost: options.NegativeBoost,
	})
	if err != nil {
		return nil, wrapError("FTS boost query", "", err)
	}
	return &FTSQuery{native: native}, nil
}

// Close releases the native query. It is idempotent and waits for in-flight
// scanner attachment.
func (q *FTSQuery) Close() error {
	if q == nil || q.native == nil {
		return nil
	}
	if err := q.native.Close(); err != nil {
		return wrapError("close FTS query", "", err)
	}
	return nil
}

// FTSMatchPrepareOptions configures a prepared Match query.
type FTSMatchPrepareOptions struct {
	Operator         MatchOperator
	MaxFuzzyDistance uint32
	Coverage         FTSCoverage
}

// FTSPhrasePrepareOptions configures a prepared Phrase query.
type FTSPhrasePrepareOptions struct {
	Slop     int32
	Coverage FTSCoverage
}

// FTSPreparedQuery is a query planned against one process-local dataset
// snapshot. It cannot be serialized, reused after reopening the dataset, or
// reused with another snapshot, even at the same URI and version.
type FTSPreparedQuery struct {
	native *ffi.FTSQueryContext
}

// PrepareFTSMatchQuery plans a Match query against this exact snapshot. The
// prepared value remains usable after this Dataset is closed; Close waits for
// in-flight scanner attachment.
func (d *Dataset) PrepareFTSMatchQuery(ctx context.Context, column, query string, options FTSMatchPrepareOptions) (*FTSPreparedQuery, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if column == "" {
		return nil, invalidError("prepare FTS match query", d.uri, "column must not be empty")
	}
	if query == "" {
		return nil, invalidError("prepare FTS match query", d.uri, "query must not be empty")
	}
	if err := validateFTSStrings(query, column); err != nil {
		return nil, invalidError("prepare FTS match query", d.uri, "%v", err)
	}
	if options.Operator != MatchOperatorOr && options.Operator != MatchOperatorAnd {
		return nil, invalidError("prepare FTS match query", d.uri, "operator %d is invalid", options.Operator)
	}
	if err := validateFTSCoverage(options.Coverage); err != nil {
		return nil, invalidError("prepare FTS match query", d.uri, "%v", err)
	}
	if err := d.acquire("prepare FTS match query"); err != nil {
		return nil, err
	}
	defer d.res.release()
	native, err := d.native.PrepareFTSMatchQuery(column, query, int32(options.Operator), options.MaxFuzzyDistance, int32(options.Coverage))
	if err != nil {
		return nil, wrapError("prepare FTS match query", d.uri, err)
	}
	return &FTSPreparedQuery{native: native}, nil
}

// PrepareFTSPhraseQuery plans a Phrase query against this exact snapshot.
func (d *Dataset) PrepareFTSPhraseQuery(ctx context.Context, column, query string, options FTSPhrasePrepareOptions) (*FTSPreparedQuery, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if column == "" {
		return nil, invalidError("prepare FTS phrase query", d.uri, "column must not be empty")
	}
	if query == "" {
		return nil, invalidError("prepare FTS phrase query", d.uri, "query must not be empty")
	}
	if err := validateFTSStrings(query, column); err != nil {
		return nil, invalidError("prepare FTS phrase query", d.uri, "%v", err)
	}
	if options.Slop < 0 {
		return nil, invalidError("prepare FTS phrase query", d.uri, "slop must not be negative")
	}
	if err := validateFTSCoverage(options.Coverage); err != nil {
		return nil, invalidError("prepare FTS phrase query", d.uri, "%v", err)
	}
	if err := d.acquire("prepare FTS phrase query"); err != nil {
		return nil, err
	}
	defer d.res.release()
	native, err := d.native.PrepareFTSPhraseQuery(column, query, options.Slop, int32(options.Coverage))
	if err != nil {
		return nil, wrapError("prepare FTS phrase query", d.uri, err)
	}
	return &FTSPreparedQuery{native: native}, nil
}

// Close releases the native prepared context. It is idempotent and waits for
// in-flight scanner attachment.
func (q *FTSPreparedQuery) Close() error {
	if q == nil || q.native == nil {
		return nil
	}
	if err := q.native.Close(); err != nil {
		return wrapError("close FTS prepared query", "", err)
	}
	return nil
}

func optionalColumn(column string) *string {
	if column == "" {
		return nil
	}
	return &column
}

func validateFTSStrings(values ...string) error {
	for _, value := range values {
		if err := validateCString(value); err != nil {
			return fmt.Errorf("lance: FTS text: %w", err)
		}
	}
	return nil
}

func validateFTSCoverage(coverage FTSCoverage) error {
	if coverage != FTSCoverageStrict && coverage != FTSCoverageIndexOnly {
		return fmt.Errorf("coverage %d is invalid", coverage)
	}
	return nil
}
