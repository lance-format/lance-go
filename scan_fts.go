package lance

import (
	"fmt"

	"github.com/lance-format/lance-go/index"
	"github.com/lance-format/lance-go/internal/ffi"
)

// ftsScanConfig carries full-text scan state. Exactly one of query, context,
// or a MinHash query may be set.
type ftsScanConfig struct {
	query         *FTSQuery
	context       *FTSPreparedQuery
	minHashColumn string
	minHashText   string
	minHashSet    bool
	segments      []index.UUID
	segmentsSet   bool
}

// WithFTSQuery attaches an immutable full-text query to the scan.
func WithFTSQuery(query *FTSQuery) ScanOption {
	return func(config *scanConfig) error {
		if query == nil {
			return fmt.Errorf("FTS query must not be nil")
		}
		config.fts.query = query
		return nil
	}
}

// WithFTSQueryContext attaches a prepared full-text query context. The context
// must come from this exact process-local dataset snapshot.
func WithFTSQueryContext(query *FTSPreparedQuery) ScanOption {
	return func(config *scanConfig) error {
		if query == nil {
			return fmt.Errorf("FTS prepared query must not be nil")
		}
		config.fts.context = query
		return nil
	}
}

// WithMinHashQuery attaches a MinHash LSH similarity query on column. The scan
// still needs an explicit limit and any desired filters, matching native
// scanner behavior.
func WithMinHashQuery(column, text string) ScanOption {
	return func(config *scanConfig) error {
		if column == "" {
			return fmt.Errorf("MinHash column must not be empty")
		}
		if err := validateCString(column); err != nil {
			return fmt.Errorf("MinHash column: %w", err)
		}
		if err := validateCString(text); err != nil {
			return fmt.Errorf("MinHash text: %w", err)
		}
		config.fts.minHashColumn = column
		config.fts.minHashText = text
		config.fts.minHashSet = true
		return nil
	}
}

// WithFTSIndexSegments restricts an FTS scan to the given physical index
// segments. A nonempty selection requires WithFTSQueryContext. No arguments
// clears the selection. Native planning rejects unknown UUIDs.
func WithFTSIndexSegments(uuids ...index.UUID) ScanOption {
	return func(config *scanConfig) error {
		config.fts.segments = append([]index.UUID(nil), uuids...)
		config.fts.segmentsSet = true
		return nil
	}
}

func applyFTSScan(native *ffi.Scanner, config *scanConfig) error {
	fts := &config.fts
	attached := 0
	if fts.query != nil {
		attached++
	}
	if fts.context != nil {
		attached++
	}
	if fts.minHashSet {
		attached++
	}
	if attached > 1 {
		return fmt.Errorf("FTS query, prepared context, and MinHash query are mutually exclusive")
	}
	if fts.segmentsSet && len(fts.segments) > 0 && fts.context == nil {
		return fmt.Errorf("FTS index segments require a prepared query context")
	}
	switch {
	case fts.query != nil:
		if err := native.SetFTSQuery(fts.query.native); err != nil {
			return err
		}
	case fts.context != nil:
		if err := native.SetFTSQueryContext(fts.context.native); err != nil {
			return err
		}
	case fts.minHashSet:
		if err := native.SetMinHashQuery(fts.minHashColumn, fts.minHashText); err != nil {
			return err
		}
	}
	if fts.segmentsSet && len(fts.segments) > 0 {
		raw := make([]byte, 0, len(fts.segments)*16)
		for _, uuid := range fts.segments {
			raw = append(raw, uuid[:]...)
		}
		if err := native.SetFTSIndexSegments(raw); err != nil {
			return err
		}
	}
	return nil
}
