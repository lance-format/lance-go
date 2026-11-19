package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"
)

const journalVersion = 1

type recordHeader struct {
	Phase   string `json:"phase"`
	Version int    `json:"journal_version"`
}

type sample struct {
	Phase          string    `json:"phase"`
	Label          string    `json:"label,omitempty"`
	Seq            int       `json:"seq"`
	At             time.Time `json:"at"`
	RunElapsedMS   int64     `json:"run_elapsed_ms"`
	PhaseElapsedMS int64     `json:"phase_elapsed_ms"`

	RSSBytes          int64  `json:"rss_bytes"`
	PSSBytes          *int64 `json:"pss_bytes,omitempty"`
	AnonymousBytes    *int64 `json:"anonymous_bytes,omitempty"`
	SharedCleanBytes  *int64 `json:"shared_clean_bytes,omitempty"`
	SharedDirtyBytes  *int64 `json:"shared_dirty_bytes,omitempty"`
	PrivateCleanBytes *int64 `json:"private_clean_bytes,omitempty"`
	PrivateDirtyBytes *int64 `json:"private_dirty_bytes,omitempty"`
	PSSFileBytes      *int64 `json:"pss_file_bytes,omitempty"`
	SwapBytes         *int64 `json:"swap_bytes,omitempty"`

	GoHeapAllocBytes uint64 `json:"go_heap_alloc_bytes"`
	GoHeapSysBytes   uint64 `json:"go_heap_sys_bytes"`
	GoSysBytes       uint64 `json:"go_sys_bytes"`
	GCCount          uint32 `json:"gc_count"`
	Goroutines       int    `json:"goroutines"`
	Handles          int    `json:"handles"`
	FDs              *int64 `json:"fds,omitempty"`
	Threads          *int64 `json:"threads,omitempty"`
	FDsError         string `json:"fds_error,omitempty"`
	ThreadsError     string `json:"threads_error,omitempty"`
}

func collectSample(sp *sampler, journal *journal, phase, label string, seq int, startedAt, phaseStart time.Time) (sample, error) {
	snapshot, err := sp.snapshot()
	if err != nil {
		return sample{}, fmt.Errorf("sample process memory: %w", err)
	}
	now := time.Now()
	result := sample{
		Phase:          phase,
		Label:          label,
		Seq:            seq,
		At:             now.UTC(),
		RunElapsedMS:   now.Sub(startedAt).Milliseconds(),
		PhaseElapsedMS: now.Sub(phaseStart).Milliseconds(),

		RSSBytes:          snapshot.Memory.RSSBytes,
		PSSBytes:          snapshot.Memory.PSSBytes,
		AnonymousBytes:    snapshot.Memory.AnonymousBytes,
		SharedCleanBytes:  snapshot.Memory.SharedCleanBytes,
		SharedDirtyBytes:  snapshot.Memory.SharedDirtyBytes,
		PrivateCleanBytes: snapshot.Memory.PrivateCleanBytes,
		PrivateDirtyBytes: snapshot.Memory.PrivateDirtyBytes,
		PSSFileBytes:      snapshot.Memory.PSSFileBytes,
		SwapBytes:         snapshot.Memory.SwapBytes,

		GoHeapAllocBytes: snapshot.Go.HeapAllocBytes,
		GoHeapSysBytes:   snapshot.Go.HeapSysBytes,
		GoSysBytes:       snapshot.Go.SysBytes,
		GCCount:          snapshot.Go.NumGC,
		Goroutines:       snapshot.Go.Goroutines,
		Handles:          snapshot.Handles,
		FDs:              snapshot.FDs,
		Threads:          snapshot.Threads,
		FDsError:         snapshot.FDsError,
		ThreadsError:     snapshot.ThreadsError,
	}
	if err := journal.write(result); err != nil {
		return sample{}, err
	}
	return result, nil
}

type journal struct {
	file *os.File
	enc  *json.Encoder
}

func newJournal(path string) (*journal, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create JSONL journal: %w", err)
	}
	return &journal{file: f, enc: json.NewEncoder(f)}, nil
}

func (j *journal) write(value any) error {
	if err := j.enc.Encode(value); err != nil {
		return fmt.Errorf("write JSONL journal: %w", err)
	}
	return nil
}

func (j *journal) close() error {
	return j.file.Close()
}

type windowSummary struct {
	Samples      int   `json:"samples"`
	MeanRSSBytes int64 `json:"mean_rss_bytes"`
	MinRSSBytes  int64 `json:"min_rss_bytes"`
	MaxRSSBytes  int64 `json:"max_rss_bytes"`
}

type measurementSummary struct {
	Samples             int           `json:"samples"`
	MeasuredDurationMS  int64         `json:"measured_duration_ms"`
	InitialRSSBytes     int64         `json:"initial_rss_bytes"`
	FinalRSSBytes       int64         `json:"final_rss_bytes"`
	PeakRSSBytes        int64         `json:"peak_rss_bytes"`
	EarlyWindow         windowSummary `json:"early_window"`
	LateWindow          windowSummary `json:"late_window"`
	LateMinusEarlyBytes int64         `json:"late_minus_early_bytes"`
	SlopeBytesPerSecond float64       `json:"slope_bytes_per_second"`
	BaselineHandles     int           `json:"baseline_handles"`
	MaxHandles          int           `json:"max_handles"`
	MaxHandleDelta      int           `json:"max_handle_delta"`
}

// summarizeSamples requires at least four samples so that the early and late
// windows are distinct observations rather than the same pair re-labelled.
func summarizeSamples(samples []sample, baselineHandles int) (measurementSummary, error) {
	if len(samples) < 4 {
		return measurementSummary{}, fmt.Errorf("need at least 4 measurement samples, got %d; reduce -sample-interval or increase -duration", len(samples))
	}
	first := samples[0]
	last := samples[len(samples)-1]
	durationMS := last.PhaseElapsedMS - first.PhaseElapsedMS
	if durationMS <= 0 {
		return measurementSummary{}, fmt.Errorf("measurement duration is not positive: %d ms", durationMS)
	}

	summary := measurementSummary{
		Samples:            len(samples),
		MeasuredDurationMS: durationMS,
		InitialRSSBytes:    first.RSSBytes,
		FinalRSSBytes:      last.RSSBytes,
		PeakRSSBytes:       first.RSSBytes,
		BaselineHandles:    baselineHandles,
		MaxHandles:         first.Handles,
	}
	for _, sample := range samples {
		if sample.RSSBytes > summary.PeakRSSBytes {
			summary.PeakRSSBytes = sample.RSSBytes
		}
		if sample.Handles > summary.MaxHandles {
			summary.MaxHandles = sample.Handles
		}
	}
	summary.MaxHandleDelta = summary.MaxHandles - baselineHandles

	width := len(samples) / 4
	if width < 1 {
		width = 1
	}
	summary.EarlyWindow = summarizeWindow(samples[:width])
	summary.LateWindow = summarizeWindow(samples[len(samples)-width:])
	summary.LateMinusEarlyBytes = summary.LateWindow.MeanRSSBytes - summary.EarlyWindow.MeanRSSBytes

	slope, err := rssSlopeBytesPerSecond(samples)
	if err != nil {
		return measurementSummary{}, err
	}
	summary.SlopeBytesPerSecond = slope
	return summary, nil
}

func summarizeWindow(samples []sample) windowSummary {
	window := windowSummary{
		Samples:     len(samples),
		MinRSSBytes: samples[0].RSSBytes,
		MaxRSSBytes: samples[0].RSSBytes,
	}
	var sum int64
	for _, sample := range samples {
		sum += sample.RSSBytes
		if sample.RSSBytes < window.MinRSSBytes {
			window.MinRSSBytes = sample.RSSBytes
		}
		if sample.RSSBytes > window.MaxRSSBytes {
			window.MaxRSSBytes = sample.RSSBytes
		}
	}
	window.MeanRSSBytes = int64(math.Round(float64(sum) / float64(len(samples))))
	return window
}

func rssSlopeBytesPerSecond(samples []sample) (float64, error) {
	var sumX, sumY, sumXY, sumXX float64
	for _, sample := range samples {
		x := float64(sample.PhaseElapsedMS) / 1000
		y := float64(sample.RSSBytes)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	n := float64(len(samples))
	denominator := n*sumXX - sumX*sumX
	if denominator == 0 {
		return 0, errors.New("cannot compute RSS slope: all measurement samples share one timestamp")
	}
	return (n*sumXY - sumX*sumY) / denominator, nil
}

type workloadReport struct {
	Name            string `json:"name"`
	Operations      int64  `json:"operations"`
	Completed       int64  `json:"completed"`
	Errors          int64  `json:"errors"`
	CancelObserved  int64  `json:"cancel_observed,omitempty"`
	CancelCompleted int64  `json:"cancel_completed,omitempty"`
}

func workloadReports(names []string, set *statsSet) []workloadReport {
	if set == nil {
		return nil
	}
	reports := make([]workloadReport, 0, len(names))
	for _, name := range names {
		stats := set.byName[name]
		if stats == nil {
			continue
		}
		operations := stats.operations.Load()
		failures := stats.errors.Load()
		reports = append(reports, workloadReport{
			Name:            name,
			Operations:      operations,
			Completed:       operations - failures,
			Errors:          failures,
			CancelObserved:  stats.cancelObserved.Load(),
			CancelCompleted: stats.cancelCompleted.Load(),
		})
	}
	return reports
}

type configRecord struct {
	recordHeader
	Tool      string       `json:"tool"`
	StartedAt time.Time    `json:"started_at"`
	Args      []string     `json:"args"`
	Run       configRun    `json:"run"`
	Build     buildRecord  `json:"build"`
	Native    nativeRecord `json:"native"`
	Memory    memorySource `json:"memory"`
	Fixture   fixtureInfo  `json:"fixture"`
}

type errorRecord struct {
	recordHeader
	At           time.Time `json:"at"`
	RunElapsedMS int64     `json:"run_elapsed_ms"`
	Message      string    `json:"message"`
}

type diagnosticRecord struct {
	Source          string  `json:"source"`
	BytesBudget     int64   `json:"bytes_budget"`
	BytesRetained   int64   `json:"bytes_retained"`
	BatchesRetained int     `json:"batches_retained"`
	RowsRetained    int64   `json:"rows_retained"`
	TouchedPages    int64   `json:"touched_pages"`
	Before          *sample `json:"before"`
	Held            *sample `json:"held"`
	After           *sample `json:"after"`
}

type growthBudgetRecord struct {
	LimitBytes         int64 `json:"limit_bytes"`
	ObservedDeltaBytes int64 `json:"observed_late_minus_early_bytes"`
	Exceeded           bool  `json:"exceeded"`
}

type finalRecord struct {
	recordHeader
	At            time.Time           `json:"at"`
	Status        string              `json:"status"`
	Error         string              `json:"error,omitempty"`
	RunElapsedMS  int64               `json:"run_elapsed_ms"`
	WarmupSamples int                 `json:"warmup_samples"`
	Workloads     []workloadReport    `json:"workloads"`
	Summary       *measurementSummary `json:"measurement,omitempty"`
	PostGC        *sample             `json:"post_run_gc_sample,omitempty"`
	Diagnostic    *diagnosticRecord   `json:"diagnostic,omitempty"`
	GrowthBudget  *growthBudgetRecord `json:"growth_budget,omitempty"`
}

func newFinalRecord(st *runState) finalRecord {
	status := runStatus(st)
	record := finalRecord{
		recordHeader:  recordHeader{Phase: "final", Version: journalVersion},
		At:            time.Now().UTC(),
		Status:        status,
		RunElapsedMS:  time.Since(st.startedAt).Milliseconds(),
		WarmupSamples: len(st.warmupSamples),
		Workloads:     workloadReports(st.cfg.Workloads, st.measureSets),
		Summary:       st.summary,
		PostGC:        st.postGC,
		Diagnostic:    st.diagnostic,
		GrowthBudget:  st.growth,
	}
	if st.err != nil {
		record.Error = st.err.Error()
	}
	return record
}

func runStatus(st *runState) string {
	if st.err == nil {
		return "ok"
	}
	if errors.Is(st.err, errBudgetExceeded) {
		return "budget_exceeded"
	}
	if errors.Is(st.err, context.Canceled) || errors.Is(st.err, context.DeadlineExceeded) {
		return "interrupted"
	}
	return "error"
}

func (s sample) String() string {
	return fmt.Sprintf("rss=%s heap=%s handles=%d goroutines=%d", humanBytes(s.RSSBytes), humanBytes(int64(s.GoHeapAllocBytes)), s.Handles, s.Goroutines)
}

func printHumanReport(w io.Writer, st *runState, jsonlPath string) {
	fmt.Fprintf(w, "soak status: %s\n", runStatus(st))
	if st.err != nil {
		fmt.Fprintf(w, "error: %v\n", st.err)
	}
	fmt.Fprintf(w, "jsonl: %s\n", jsonlPath)
	fmt.Fprintf(w, "native library: %s", st.native.Path)
	if st.native.SHA256 != "" {
		fmt.Fprintf(w, " (sha256 %s)", st.native.SHA256)
	}
	fmt.Fprintln(w)
	if st.native.DeclaredArtifact != nil {
		fmt.Fprintf(w, "declared artifact module: %s@%s\n", st.native.DeclaredArtifact.Path, st.native.DeclaredArtifact.Version)
	}
	fmt.Fprintf(w, "warmup: %s (%d samples), measurement target: %s\n",
		humanDuration(st.cfg.Warmup), len(st.warmupSamples), humanDuration(st.cfg.Duration))
	if st.summary != nil {
		summary := st.summary
		fmt.Fprintf(w, "measurement: %d samples over %s\n", summary.Samples, humanDuration(time.Duration(summary.MeasuredDurationMS)*time.Millisecond))
		fmt.Fprintf(w, "rss: initial %s, final %s, peak %s\n", humanBytes(summary.InitialRSSBytes), humanBytes(summary.FinalRSSBytes), humanBytes(summary.PeakRSSBytes))
		fmt.Fprintf(w, "post-warmup windows: early mean %s [%s, %s], late mean %s [%s, %s], late-early %s, slope %.0f bytes/s\n",
			humanBytes(summary.EarlyWindow.MeanRSSBytes), humanBytes(summary.EarlyWindow.MinRSSBytes), humanBytes(summary.EarlyWindow.MaxRSSBytes),
			humanBytes(summary.LateWindow.MeanRSSBytes), humanBytes(summary.LateWindow.MinRSSBytes), humanBytes(summary.LateWindow.MaxRSSBytes),
			humanBytes(summary.LateMinusEarlyBytes), summary.SlopeBytesPerSecond)
		fmt.Fprintf(w, "registered Go callback handles: warmed baseline %d, max %d, max delta %d\n", summary.BaselineHandles, summary.MaxHandles, summary.MaxHandleDelta)
	}
	if reports := workloadReports(st.cfg.Workloads, st.measureSets); len(reports) > 0 {
		fmt.Fprintln(w, "workloads:")
		for _, report := range reports {
			line := fmt.Sprintf("  %-12s operations=%d completed=%d errors=%d", report.Name, report.Operations, report.Completed, report.Errors)
			if report.Name == "cancel" {
				line += fmt.Sprintf(" canceled=%d completed-before-cancel=%d", report.CancelObserved, report.CancelCompleted)
			}
			fmt.Fprintln(w, line)
		}
	}
	if st.growth != nil {
		fmt.Fprintf(w, "growth budget: late-early %s, limit %s, exceeded=%t\n", humanBytes(st.growth.ObservedDeltaBytes), humanBytes(st.growth.LimitBytes), st.growth.Exceeded)
	}
	if st.diagnostic != nil {
		diag := st.diagnostic
		fmt.Fprintf(w, "diagnostic retention: held %s in %d batches / %d rows; rss", humanBytes(diag.BytesRetained), diag.BatchesRetained, diag.RowsRetained)
		if diag.Before != nil && diag.Held != nil && diag.After != nil {
			fmt.Fprintf(w, " before=%s during=%s after=%s", humanBytes(diag.Before.RSSBytes), humanBytes(diag.Held.RSSBytes), humanBytes(diag.After.RSSBytes))
		}
		fmt.Fprintln(w)
	}
	if st.postGC != nil {
		fmt.Fprintf(w, "post-run forced GC sample (excluded from primary evidence): %s\n", st.postGC)
	}
	fmt.Fprintln(w, "note: rss_bytes is total Linux process residency including native allocations; it is not a native-live-byte measurement.")
}

func humanBytes(value int64) string {
	negative := value < 0
	if negative {
		value = -value
	}
	if value < 1024 {
		if negative {
			return fmt.Sprintf("-%d B", value)
		}
		return fmt.Sprintf("%d B", value)
	}
	divisor := int64(1024)
	exponent := 0
	for n := value / 1024; n >= 1024 && exponent < len("KMGTPE")-1; n /= 1024 {
		divisor *= 1024
		exponent++
	}
	formatted := fmt.Sprintf("%.1f %siB", float64(value)/float64(divisor), string("KMGTPE"[exponent]))
	if negative {
		return "-" + formatted
	}
	return formatted
}

func humanDuration(duration time.Duration) string {
	if duration < time.Second {
		return duration.String()
	}
	return duration.Round(100 * time.Millisecond).String()
}
