package main

import (
	"math"
	"testing"
	"time"
)

func TestSummarizeSamples(t *testing.T) {
	samples := make([]sample, 8)
	for i := range samples {
		samples[i] = sample{
			RSSBytes:       int64(1000 + i*100),
			PhaseElapsedMS: int64(i * 1000),
			Handles:        3,
		}
	}
	summary, err := summarizeSamples(samples, 2)
	if err != nil {
		t.Fatalf("summarizeSamples: %v", err)
	}
	if summary.Samples != 8 {
		t.Fatalf("Samples = %d, want 8", summary.Samples)
	}
	if summary.MeasuredDurationMS != 7000 {
		t.Fatalf("MeasuredDurationMS = %d, want 7000", summary.MeasuredDurationMS)
	}
	if summary.InitialRSSBytes != 1000 || summary.FinalRSSBytes != 1700 || summary.PeakRSSBytes != 1700 {
		t.Fatalf("RSS initial/final/peak = %d/%d/%d, want 1000/1700/1700", summary.InitialRSSBytes, summary.FinalRSSBytes, summary.PeakRSSBytes)
	}
	if summary.EarlyWindow.Samples != 2 || summary.LateWindow.Samples != 2 {
		t.Fatalf("window sizes = %d/%d, want 2/2", summary.EarlyWindow.Samples, summary.LateWindow.Samples)
	}
	if summary.EarlyWindow.MeanRSSBytes != 1050 || summary.LateWindow.MeanRSSBytes != 1650 {
		t.Fatalf("window means = %d/%d, want 1050/1650", summary.EarlyWindow.MeanRSSBytes, summary.LateWindow.MeanRSSBytes)
	}
	if summary.LateMinusEarlyBytes != 600 {
		t.Fatalf("LateMinusEarlyBytes = %d, want 600", summary.LateMinusEarlyBytes)
	}
	if math.Abs(summary.SlopeBytesPerSecond-100) > 1e-9 {
		t.Fatalf("SlopeBytesPerSecond = %f, want 100", summary.SlopeBytesPerSecond)
	}
	if summary.MaxHandles != 3 || summary.MaxHandleDelta != 1 {
		t.Fatalf("handles max/delta = %d/%d, want 3/1", summary.MaxHandles, summary.MaxHandleDelta)
	}
}

func TestSummarizeSamplesRejectsTooFew(t *testing.T) {
	if _, err := summarizeSamples(make([]sample, 3), 0); err == nil {
		t.Fatal("summarizeSamples accepted three samples")
	}
}

func TestSummarizeSamplesRejectsZeroDuration(t *testing.T) {
	samples := make([]sample, 4)
	for i := range samples {
		samples[i] = sample{RSSBytes: 100, PhaseElapsedMS: 1000}
	}
	if _, err := summarizeSamples(samples, 0); err == nil {
		t.Fatal("summarizeSamples accepted equal timestamps")
	}
}

func TestRunStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "ok", want: "ok"},
		{name: "budget", err: errBudgetExceeded, want: "budget_exceeded"},
		{name: "wrapped budget", err: wrappedError{errBudgetExceeded}, want: "budget_exceeded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := &runState{err: test.err}
			if got := runStatus(state); got != test.want {
				t.Fatalf("runStatus = %q, want %q", got, test.want)
			}
		})
	}
}

type wrappedError struct{ err error }

func (e wrappedError) Error() string { return "wrapped: " + e.err.Error() }
func (e wrappedError) Unwrap() error { return e.err }

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		value int64
		want  string
	}{
		{value: 0, want: "0 B"},
		{value: 1023, want: "1023 B"},
		{value: 1024, want: "1.0 KiB"},
		{value: 1 << 20, want: "1.0 MiB"},
		{value: -1 << 20, want: "-1.0 MiB"},
	}
	for _, test := range tests {
		if got := humanBytes(test.value); got != test.want {
			t.Fatalf("humanBytes(%d) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	if got := humanDuration(1500 * time.Millisecond); got != "1.5s" {
		t.Fatalf("humanDuration = %q, want 1.5s", got)
	}
}
