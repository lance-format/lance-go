package ffi

import "testing"

func TestStatsStateStorePreservesMetricKinds(t *testing.T) {
	name := []byte("rows_scanned")
	rawMetrics := []scanMetric{
		{Name: &name[0], NameLen: uint64(len(name)), Kind: 0, Value: 3},
		{Name: &name[0], NameLen: uint64(len(name)), Kind: 1, Value: 11},
		{Name: &name[0], NameLen: uint64(len(name)), Kind: 42, Value: 7},
	}
	raw := scanStatistics{
		IOPS:                  1,
		Requests:              2,
		BytesRead:             3,
		IndicesLoaded:         4,
		IndexPartitionsLoaded: 5,
		IndexComparisons:      6,
		Metrics:               &rawMetrics[0],
		MetricsLen:            uint64(len(rawMetrics)),
	}

	state := new(statsState)
	state.store(&raw)
	got := state.snapshot()

	if len(got.Metrics) != len(rawMetrics) {
		t.Fatalf("metrics len = %d, want %d", len(got.Metrics), len(rawMetrics))
	}
	for i, want := range rawMetrics {
		if got.Metrics[i].Name != string(name) {
			t.Errorf("metric %d name = %q, want %q", i, got.Metrics[i].Name, name)
		}
		if got.Metrics[i].Kind != want.Kind {
			t.Errorf("metric %d kind = %d, want %d", i, got.Metrics[i].Kind, want.Kind)
		}
		if got.Metrics[i].Value != want.Value {
			t.Errorf("metric %d value = %d, want %d", i, got.Metrics[i].Value, want.Value)
		}
	}
}
