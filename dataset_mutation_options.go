package lance

import "fmt"

// ColumnUpdate assigns Value to Column for rows matched by Update.
//
// Value is a SQL scalar expression evaluated by Lance for each row, not a Go
// value or SQL fragment to be embedded by the caller. For example, "price * 2"
// doubles the price column and "'unknown'" stores the string unknown.
type ColumnUpdate struct {
	Column string
	Value  string
}

// MergeWhenMatched selects the action for a target row that matches a source
// row during MergeInsert.
type MergeWhenMatched int32

const (
	// MergeWhenMatchedDoNothing keeps the target row unchanged. It is the
	// zero value and produces find-or-create behavior.
	MergeWhenMatchedDoNothing MergeWhenMatched = 0
	// MergeWhenMatchedUpdateAll replaces the target row with the source row.
	MergeWhenMatchedUpdateAll MergeWhenMatched = 1
	// MergeWhenMatchedUpdateIf replaces the target row only when the
	// WhenMatchedExpression filter evaluates true.
	MergeWhenMatchedUpdateIf MergeWhenMatched = 2
	// MergeWhenMatchedFail fails the operation when a target row matches.
	MergeWhenMatchedFail MergeWhenMatched = 3
	// MergeWhenMatchedDelete removes the matching target row.
	MergeWhenMatchedDelete MergeWhenMatched = 4
)

// MergeWhenNotMatched selects the action for a source row with no matching
// target row during MergeInsert.
type MergeWhenNotMatched int32

const (
	// MergeWhenNotMatchedInsertAll inserts the source row. It is the zero
	// value.
	MergeWhenNotMatchedInsertAll MergeWhenNotMatched = 0
	// MergeWhenNotMatchedDoNothing discards the source row.
	MergeWhenNotMatchedDoNothing MergeWhenNotMatched = 1
)

// MergeWhenNotMatchedBySource selects the action for a target row with no
// matching source row during MergeInsert.
type MergeWhenNotMatchedBySource int32

const (
	// MergeWhenNotMatchedBySourceKeep keeps the target row. It is the zero
	// value.
	MergeWhenNotMatchedBySourceKeep MergeWhenNotMatchedBySource = 0
	// MergeWhenNotMatchedBySourceDelete removes every unmatched target row.
	MergeWhenNotMatchedBySourceDelete MergeWhenNotMatchedBySource = 1
	// MergeWhenNotMatchedBySourceDeleteIf removes unmatched target rows that
	// satisfy WhenNotMatchedBySourceExpression.
	MergeWhenNotMatchedBySourceDeleteIf MergeWhenNotMatchedBySource = 2
)

// MergeInsertParams controls MergeInsert. Its zero value is valid and selects
// find-or-create behavior: matching rows are kept and unmatched source rows are
// inserted.
//
// The two expression fields are SQL filters. WhenMatchedExpression is required
// for MergeWhenMatchedUpdateIf and is invalid for every other when-matched
// mode. WhenNotMatchedBySourceExpression is required for
// MergeWhenNotMatchedBySourceDeleteIf and is invalid for every other
// not-matched-by-source mode.
type MergeInsertParams struct {
	WhenMatched                      MergeWhenMatched
	WhenMatchedExpression            string
	WhenNotMatched                   MergeWhenNotMatched
	WhenNotMatchedBySource           MergeWhenNotMatchedBySource
	WhenNotMatchedBySourceExpression string
}

// MergeInsertResult reports the rows affected by MergeInsert.
type MergeInsertResult struct {
	InsertedRows uint64
	UpdatedRows  uint64
	DeletedRows  uint64
}

func (p MergeInsertParams) validate() error {
	switch p.WhenMatched {
	case MergeWhenMatchedDoNothing, MergeWhenMatchedUpdateAll, MergeWhenMatchedUpdateIf,
		MergeWhenMatchedFail, MergeWhenMatchedDelete:
	default:
		return fmt.Errorf("invalid when-matched mode %d", p.WhenMatched)
	}
	switch p.WhenNotMatched {
	case MergeWhenNotMatchedInsertAll, MergeWhenNotMatchedDoNothing:
	default:
		return fmt.Errorf("invalid when-not-matched mode %d", p.WhenNotMatched)
	}
	switch p.WhenNotMatchedBySource {
	case MergeWhenNotMatchedBySourceKeep, MergeWhenNotMatchedBySourceDelete,
		MergeWhenNotMatchedBySourceDeleteIf:
	default:
		return fmt.Errorf("invalid when-not-matched-by-source mode %d", p.WhenNotMatchedBySource)
	}

	if err := validateCString(p.WhenMatchedExpression); err != nil {
		return fmt.Errorf("when-matched expression: %w", err)
	}
	if p.WhenMatched == MergeWhenMatchedUpdateIf {
		if p.WhenMatchedExpression == "" {
			return fmt.Errorf("when-matched expression is required for update-if mode")
		}
	} else if p.WhenMatchedExpression != "" {
		return fmt.Errorf("when-matched expression is only valid for update-if mode")
	}

	if err := validateCString(p.WhenNotMatchedBySourceExpression); err != nil {
		return fmt.Errorf("when-not-matched-by-source expression: %w", err)
	}
	if p.WhenNotMatchedBySource == MergeWhenNotMatchedBySourceDeleteIf {
		if p.WhenNotMatchedBySourceExpression == "" {
			return fmt.Errorf("when-not-matched-by-source expression is required for delete-if mode")
		}
	} else if p.WhenNotMatchedBySourceExpression != "" {
		return fmt.Errorf("when-not-matched-by-source expression is only valid for delete-if mode")
	}
	return nil
}

func validateMutationUpdates(updates []ColumnUpdate) error {
	if len(updates) == 0 {
		return fmt.Errorf("at least one column update is required")
	}
	for i, update := range updates {
		if update.Column == "" {
			return fmt.Errorf("update %d: column name must not be empty", i)
		}
		if err := validateCString(update.Column); err != nil {
			return fmt.Errorf("update %d column: %w", i, err)
		}
		if update.Value == "" {
			return fmt.Errorf("update %d: value for column %q must not be empty", i, update.Column)
		}
		if err := validateCString(update.Value); err != nil {
			return fmt.Errorf("update %d value: %w", i, err)
		}
	}
	return nil
}

func validateMutationKeys(columns []string, what string) error {
	if len(columns) == 0 {
		return fmt.Errorf("%s requires at least one column", what)
	}
	for i, column := range columns {
		if column == "" {
			return fmt.Errorf("%s column %d must not be empty", what, i)
		}
		if err := validateCString(column); err != nil {
			return fmt.Errorf("%s column %d: %w", what, i, err)
		}
	}
	return nil
}
