package lance_test

import (
	"context"
	"reflect"
	"testing"

	lance "github.com/lance-format/lance-go"
)

func TestScanMetricKindString(t *testing.T) {
	tests := []struct {
		kind lance.ScanMetricKind
		want string
	}{
		{lance.ScanMetricKindCount, "LANCE_SCAN_METRIC_COUNT"},
		{lance.ScanMetricKindTimeNanoseconds, "LANCE_SCAN_METRIC_TIME_NANOSECONDS"},
		{lance.ScanMetricKind(-1), "ScanMetricKind(-1)"},
		{lance.ScanMetricKind(2), "ScanMetricKind(2)"},
		{lance.ScanMetricKind(42), "ScanMetricKind(42)"},
		{lance.ScanMetricKind(1 << 20), "ScanMetricKind(1048576)"},
	}
	for _, tc := range tests {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("ScanMetricKind(%d).String() = %q, want %q", int32(tc.kind), got, tc.want)
		}
	}
}

func TestScanMetricKindPinnedValuesAndConversions(t *testing.T) {
	tests := []struct {
		kind  lance.ScanMetricKind
		value int32
	}{
		{lance.ScanMetricKindCount, 0},
		{lance.ScanMetricKindTimeNanoseconds, 1},
		{42, 42},
	}
	for _, tc := range tests {
		if got := int32(tc.kind); got != tc.value {
			t.Errorf("ScanMetricKind(%d) numeric value = %d, want %d", int32(tc.kind), got, tc.value)
		}

		metric := lance.ScanMetric{Name: "metric", Kind: tc.kind, Value: 10}
		// v0.3.0 changed Kind from int32 to ScanMetricKind. These conversions
		// are the migration path for code that still stores or accepts int32.
		var numericKind int32 = int32(metric.Kind)
		metric.Kind = lance.ScanMetricKind(numericKind)
		if metric.Kind != tc.kind {
			t.Errorf("ScanMetric.Kind round trip = %v, want %v", metric.Kind, tc.kind)
		}
	}

	// Untyped literals remain assignable to the named field type.
	metric := lance.ScanMetric{Name: "count", Kind: 0, Value: 3}
	if metric.Kind != lance.ScanMetricKindCount {
		t.Errorf("ScanMetric.Kind = %v, want %v", metric.Kind, lance.ScanMetricKindCount)
	}
}

func TestScannerStatisticsCallback(t *testing.T) {
	requireNative(t)
	dataset := newSingleFragmentDataset(t, []int64{1, 2, 3})
	callbackStats := make(chan lance.ScanStatistics, 1)
	scanner, err := dataset.Scan(
		lance.WithBatchSize(2),
		lance.WithScanStatistics(func(stats lance.ScanStatistics) {
			callbackStats <- stats
		}),
	)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for batch, err := range scanner.Records(context.Background()) {
		if err != nil {
			scanner.Close()
			t.Fatalf("Records: %v", err)
		}
		_ = batch
	}
	stats := scanner.Statistics()
	if err := scanner.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var callback lance.ScanStatistics
	select {
	case callback = <-callbackStats:
	default:
		t.Fatal("scan statistics callback was not called")
	}
	if !reflect.DeepEqual(callback, stats) {
		t.Fatalf("callback statistics = %+v, Statistics() = %+v", callback, stats)
	}
}
