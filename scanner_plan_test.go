package lance_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/index"
)

func TestScannerPlanDiagnosticsFilteredAndIndexed(t *testing.T) {
	requireNative(t)
	ctx := context.Background()
	dataset := newSingleFragmentDataset(t, []int64{1, 2, 3, 4, 5, 6})
	indexed, err := dataset.CreateScalarIndex(
		ctx,
		"id",
		index.ScalarBTree,
		nil,
		index.ScalarOptions{},
		lance.WithIndexName("id_btree"),
	)
	if err != nil {
		t.Fatalf("CreateScalarIndex: %v", err)
	}
	defer indexed.Close()

	filtered, err := dataset.Scan(
		lance.WithColumns("id"),
		lance.WithSQLFilter("id >= 4"),
		lance.WithUseScalarIndex(false),
	)
	if err != nil {
		t.Fatalf("Scan filtered: %v", err)
	}
	defer filtered.Close()

	plan, err := filtered.ExplainPlan(ctx, false)
	if err != nil {
		t.Fatalf("ExplainPlan filtered: %v", err)
	}
	assertPlanContains(t, plan, "full_filter=", "id")
	if strings.Contains(plan, "ScalarIndexQuery") {
		t.Fatalf("filtered non-indexed plan unexpectedly used a scalar index:\n%s", plan)
	}
	analysis, err := filtered.AnalyzePlan(ctx)
	if err != nil {
		t.Fatalf("AnalyzePlan filtered: %v", err)
	}
	assertPlanContains(t, analysis, "AnalyzeExec", "full_filter=", "metrics=")

	scanner, err := indexed.Scan(
		lance.WithColumns("id"),
		lance.WithSQLFilter("id >= 4"),
		lance.WithUseScalarIndex(true),
	)
	if err != nil {
		t.Fatalf("Scan indexed: %v", err)
	}
	defer scanner.Close()

	plan, err = scanner.ExplainPlan(ctx, false)
	if err != nil {
		t.Fatalf("ExplainPlan indexed: %v", err)
	}
	assertPlanContains(t, plan, "ScalarIndexQuery", "id_btree")

	plan, err = scanner.ExplainPlan(ctx, true)
	if err != nil {
		t.Fatalf("ExplainPlan indexed verbose: %v", err)
	}
	assertPlanContains(t, plan, "ScalarIndexQuery", "id_btree")

	analysis, err = scanner.AnalyzePlan(ctx)
	if err != nil {
		t.Fatalf("AnalyzePlan indexed: %v", err)
	}
	assertPlanContains(t, analysis, "AnalyzeExec", "ScalarIndexQuery", "id_btree")

	countPlan, err := scanner.AnalyzeCountPlan(ctx)
	if err != nil {
		t.Fatalf("AnalyzeCountPlan indexed: %v", err)
	}
	assertPlanContains(t, countPlan, "AggregateExec", "ScalarIndexQuery", "id_btree")

	count, err := scanner.CountRows(ctx)
	if err != nil {
		t.Fatalf("CountRows indexed: %v", err)
	}
	if count != 3 {
		t.Fatalf("CountRows = %d, want 3", count)
	}

	ids := scanScannerIDs(t, scanner)
	slices.Sort(ids)
	if !equalIDs(ids, 4, 5, 6) {
		t.Fatalf("scan after plan diagnostics = %v, want [4 5 6]", ids)
	}
}

func TestScannerPlanDiagnosticsLifetime(t *testing.T) {
	requireNative(t)
	dataset := newSingleFragmentDataset(t, []int64{1, 2, 3})

	scanner, err := dataset.Scan(lance.WithColumns("id"), lance.WithSQLFilter("id >= 2"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := map[string]func(context.Context) error{
		"ExplainPlan": func(ctx context.Context) error {
			_, err := scanner.ExplainPlan(ctx, false)
			return err
		},
		"AnalyzePlan": func(ctx context.Context) error {
			_, err := scanner.AnalyzePlan(ctx)
			return err
		},
		"AnalyzeCountPlan": func(ctx context.Context) error {
			_, err := scanner.AnalyzeCountPlan(ctx)
			return err
		},
		"CountRows": func(ctx context.Context) error {
			_, err := scanner.CountRows(ctx)
			return err
		},
	}
	for name, call := range calls {
		if err := call(ctx); !errors.Is(err, context.Canceled) {
			scanner.Close()
			t.Fatalf("%s with canceled context = %v, want context.Canceled", name, err)
		}
	}

	ids := scanScannerIDs(t, scanner)
	slices.Sort(ids)
	if !equalIDs(ids, 2, 3) {
		t.Fatalf("scan after canceled diagnostics = %v, want [2 3]", ids)
	}

	closed, err := dataset.Scan(lance.WithColumns("id"))
	if err != nil {
		t.Fatalf("Scan for closed checks: %v", err)
	}
	if err := closed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for name, call := range calls {
		if err := call(context.Background()); !errors.Is(err, lance.ErrClosed) {
			t.Fatalf("%s after Close = %v, want ErrClosed", name, err)
		}
	}
}

func assertPlanContains(t *testing.T, plan string, wants ...string) {
	t.Helper()
	if strings.TrimSpace(plan) == "" {
		t.Fatal("native plan is empty")
	}
	for _, want := range wants {
		if !strings.Contains(plan, want) {
			t.Fatalf("plan does not contain %q:\n%s", want, plan)
		}
	}
}
