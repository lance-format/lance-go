package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	lance "github.com/lance-format/lance-go"
	"github.com/lance-format/lance-go/internal/handle"
	"github.com/lance-format/lance-go/internal/loader"
)

var errBudgetExceeded = errors.New("RSS growth budget exceeded")

// maxRetainedSamples bounds benchmark-local memory used to keep the evidence
// needed for the final summary. The command rejects a sampling schedule that
// would exceed it.
const maxRetainedSamples = 100_000

type config struct {
	Warmup                time.Duration
	Duration              time.Duration
	SampleInterval        time.Duration
	Concurrency           int
	Rows                  int
	PayloadBytes          int
	BatchSize             int
	Workloads             []string
	JSONLPath             string
	Dir                   string
	MaxGrowthBytes        int64
	DiagnosticRetainBytes int64
	Args                  []string
}

type configRun struct {
	WarmupMS              int64    `json:"warmup_ms"`
	DurationMS            int64    `json:"duration_ms"`
	SampleIntervalMS      int64    `json:"sample_interval_ms"`
	Concurrency           int      `json:"concurrency"`
	Rows                  int      `json:"rows"`
	PayloadBytes          int      `json:"payload_bytes"`
	ScanBatchSize         int      `json:"scan_batch_size"`
	Workloads             []string `json:"workloads"`
	MaxGrowthBytes        int64    `json:"max_growth_bytes,omitempty"`
	DiagnosticRetainBytes int64    `json:"diagnostic_retain_bytes,omitempty"`
	JSONLPath             string   `json:"jsonl_path"`
	FixtureDir            string   `json:"fixture_dir"`
}

type moduleInfo struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Sum     string `json:"sum,omitempty"`
	Replace string `json:"replace,omitempty"`
}

type buildRecord struct {
	Available    bool              `json:"available"`
	GoVersion    string            `json:"go_version"`
	GOOS         string            `json:"goos"`
	GOARCH       string            `json:"goarch"`
	GOMAXPROCS   int               `json:"gomaxprocs"`
	NumCPU       int               `json:"num_cpu"`
	Main         moduleInfo        `json:"main_module"`
	Dependencies []moduleInfo      `json:"dependencies,omitempty"`
	Settings     map[string]string `json:"build_settings,omitempty"`
}

type nativeRecord struct {
	Path             string      `json:"path"`
	SHA256           string      `json:"sha256,omitempty"`
	SHA256Error      string      `json:"sha256_error,omitempty"`
	DeclaredArtifact *moduleInfo `json:"declared_artifact,omitempty"`
	Provenance       string      `json:"provenance"`
}

type runState struct {
	startedAt time.Time
	cfg       config
	build     buildRecord
	native    nativeRecord
	memory    memorySource
	fixture   fixtureInfo

	warmupSets  *statsSet
	measureSets *statsSet

	baselineHandles    int
	warmupSamples      []sample
	measurementSamples []sample
	postGC             *sample
	diagnostic         *diagnosticRecord
	summary            *measurementSummary
	growth             *growthBudgetRecord
	err                error
}

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "soak:", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "soak:", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, cfg config) (err error) {
	journal, err := newJournal(cfg.JSONLPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := journal.close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close JSONL journal: %w", closeErr)
		}
	}()

	state := &runState{startedAt: time.Now(), cfg: cfg}
	err = run(ctx, state, journal)
	printHumanReport(os.Stdout, state, cfg.JSONLPath)
	return err
}

// run writes the error record and the final record even when the run fails, so
// operators can inspect partial evidence instead of an empty file.
func run(ctx context.Context, st *runState, journal *journal) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
		st.err = err
		if err != nil {
			record := errorRecord{
				recordHeader: recordHeader{Phase: "error", Version: journalVersion},
				At:           time.Now().UTC(),
				RunElapsedMS: time.Since(st.startedAt).Milliseconds(),
				Message:      err.Error(),
			}
			if writeErr := journal.write(record); writeErr != nil {
				fmt.Fprintln(os.Stderr, "soak: write error record:", writeErr)
			}
		}
		if writeErr := journal.write(newFinalRecord(st)); writeErr != nil {
			fmt.Fprintln(os.Stderr, "soak: write final record:", writeErr)
			if err == nil {
				err = writeErr
			}
		}
	}()
	return runInner(ctx, st, journal)
}

func runInner(ctx context.Context, st *runState, journal *journal) error {
	library, err := loader.Load()
	if err != nil {
		return fmt.Errorf("load lance-c (set LANCE_LIB_PATH or use the default bundled build): %w", err)
	}
	st.native, st.build = recordProvenance(library)

	sampler, err := newSampler()
	if err != nil {
		return err
	}
	initialSnapshot, err := sampler.snapshot()
	if err != nil {
		return fmt.Errorf("initial process sample: %w", err)
	}
	st.memory = memorySourceFrom(initialSnapshot.Memory)

	dir := st.cfg.Dir
	removeDir := false
	if dir == "" {
		dir, err = os.MkdirTemp("", "lance-go-soak-*")
		if err != nil {
			return fmt.Errorf("create fixture directory: %w", err)
		}
		removeDir = true
	} else if err := prepareExplicitDir(dir); err != nil {
		return err
	}
	if removeDir {
		defer os.RemoveAll(dir)
	}
	st.fixture = fixturePlan(dir, st.cfg)

	if err := journal.write(newConfigRecord(st)); err != nil {
		return err
	}
	fixture, err := newFixture(ctx, dir, st.cfg)
	if err != nil {
		return fmt.Errorf("prepare fixture: %w", err)
	}
	defer fixture.cleanup()

	workers := newPool(ctx, st.cfg, fixture, st.cfg.Workloads)
	defer workers.close()

	st.warmupSets = newStatsSet(st.cfg.Workloads)
	warmupStart := time.Now()
	warmupDeadline := warmupStart.Add(st.cfg.Warmup)
	lastSample := warmupStart
	warmupSeq := 0
	for time.Now().Before(warmupDeadline) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("interrupted during warmup: %w", err)
		}
		workers.epoch(st.warmupSets)
		if err := workers.firstErr(); err != nil {
			return fmt.Errorf("warmup: %w", err)
		}
		if time.Since(lastSample) >= st.cfg.SampleInterval {
			snapshot, err := collectSample(sampler, journal, "warmup", "", warmupSeq, st.startedAt, warmupStart)
			if err != nil {
				return err
			}
			st.warmupSamples = append(st.warmupSamples, snapshot)
			warmupSeq++
			lastSample = time.Now()
		}
	}
	baseline, err := collectSample(sampler, journal, "warmup", "baseline", warmupSeq, st.startedAt, warmupStart)
	if err != nil {
		return err
	}
	st.baselineHandles = baseline.Handles
	st.warmupSamples = append(st.warmupSamples, baseline)

	st.measureSets = newStatsSet(st.cfg.Workloads)
	measureStart := time.Now()
	measureDeadline := measureStart.Add(st.cfg.Duration)
	initial, err := collectQuiescentSample(st, sampler, journal, "measurement", "initial", 0, measureStart)
	if err != nil {
		return err
	}
	st.measurementSamples = append(st.measurementSamples, initial)
	measureSeq := 1
	lastSample = measureStart
	for time.Now().Before(measureDeadline) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("interrupted during measurement: %w", err)
		}
		workers.epoch(st.measureSets)
		if handles := handle.Len(); handles != st.baselineHandles {
			return fmt.Errorf("registered Go callback handle count %d after quiescent epoch, warmed baseline %d", handles, st.baselineHandles)
		}
		if err := workers.firstErr(); err != nil {
			return fmt.Errorf("measurement: %w", err)
		}
		if time.Since(lastSample) >= st.cfg.SampleInterval {
			snapshot, err := collectQuiescentSample(st, sampler, journal, "measurement", "", measureSeq, measureStart)
			if err != nil {
				return err
			}
			st.measurementSamples = append(st.measurementSamples, snapshot)
			measureSeq++
			lastSample = time.Now()
		}
	}
	final, err := collectQuiescentSample(st, sampler, journal, "measurement", "final", measureSeq, measureStart)
	if err != nil {
		return err
	}
	st.measurementSamples = append(st.measurementSamples, final)
	if err := workers.firstErr(); err != nil {
		return fmt.Errorf("measurement: %w", err)
	}
	if err := validateWorkloads(st.cfg.Workloads, st.measureSets); err != nil {
		return err
	}
	summary, err := summarizeSamples(st.measurementSamples, st.baselineHandles)
	if err != nil {
		return err
	}
	st.summary = &summary

	var budgetErr error
	if st.cfg.MaxGrowthBytes > 0 {
		budget := &growthBudgetRecord{
			LimitBytes:         st.cfg.MaxGrowthBytes,
			ObservedDeltaBytes: summary.LateMinusEarlyBytes,
			Exceeded:           summary.LateMinusEarlyBytes > st.cfg.MaxGrowthBytes,
		}
		st.growth = budget
		if budget.Exceeded {
			budgetErr = fmt.Errorf("%w: late-window mean RSS grew %d bytes over early-window mean, limit %d bytes", errBudgetExceeded, budget.ObservedDeltaBytes, budget.LimitBytes)
		}
	}

	// This GC is deliberately after measurement and is labelled post_gc. It
	// is not part of the primary steady-state evidence.
	runtime.GC()
	postGC, err := collectSample(sampler, journal, "post_gc", "after_runtime_gc", 0, st.startedAt, time.Now())
	if err != nil {
		return err
	}
	st.postGC = &postGC

	if st.cfg.DiagnosticRetainBytes > 0 {
		diagnostic, err := runDiagnostic(ctx, st, sampler, journal, fixture)
		if err != nil {
			return fmt.Errorf("diagnostic retention control: %w", err)
		}
		st.diagnostic = diagnostic
	}
	return budgetErr
}

func collectQuiescentSample(st *runState, sampler *sampler, journal *journal, phase, label string, seq int, phaseStart time.Time) (sample, error) {
	snapshot, err := collectSample(sampler, journal, phase, label, seq, st.startedAt, phaseStart)
	if err != nil {
		return sample{}, err
	}
	if phase == "measurement" && snapshot.Handles != st.baselineHandles {
		return sample{}, fmt.Errorf("registered Go callback handle count %d after a quiescent epoch, warmed baseline %d", snapshot.Handles, st.baselineHandles)
	}
	return snapshot, nil
}

func validateWorkloads(names []string, set *statsSet) error {
	for _, name := range names {
		stats := set.byName[name]
		if stats == nil {
			return fmt.Errorf("workload %s has no counters", name)
		}
		operations := stats.operations.Load()
		failures := stats.errors.Load()
		if failures > 0 {
			return fmt.Errorf("workload %s reported %d errors", name, failures)
		}
		if operations == 0 {
			return fmt.Errorf("workload %s completed no operations; selector may be unused", name)
		}
	}
	return nil
}

func newConfigRecord(st *runState) configRecord {
	return configRecord{
		recordHeader: recordHeader{Phase: "config", Version: journalVersion},
		Tool:         "lance-go RSS/lifecycle soak benchmark",
		StartedAt:    st.startedAt.UTC(),
		Args:         st.cfg.Args,
		Run: configRun{
			WarmupMS:              st.cfg.Warmup.Milliseconds(),
			DurationMS:            st.cfg.Duration.Milliseconds(),
			SampleIntervalMS:      st.cfg.SampleInterval.Milliseconds(),
			Concurrency:           st.cfg.Concurrency,
			Rows:                  st.cfg.Rows,
			PayloadBytes:          st.cfg.PayloadBytes,
			ScanBatchSize:         st.cfg.BatchSize,
			Workloads:             st.cfg.Workloads,
			MaxGrowthBytes:        st.cfg.MaxGrowthBytes,
			DiagnosticRetainBytes: st.cfg.DiagnosticRetainBytes,
			JSONLPath:             st.cfg.JSONLPath,
			FixtureDir:            st.fixture.Dir,
		},
		Build:   st.build,
		Native:  st.native,
		Memory:  st.memory,
		Fixture: st.fixture,
	}
}

func parseConfig(args []string) (config, error) {
	cfg := config{Args: append([]string(nil), args...)}
	fs := flag.NewFlagSet("soak", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var workloads string
	fs.DurationVar(&cfg.Warmup, "warmup", 30*time.Second, "bounded warmup duration excluded from measurement")
	fs.DurationVar(&cfg.Duration, "duration", 5*time.Minute, "measurement duration")
	fs.DurationVar(&cfg.SampleInterval, "sample-interval", 5*time.Second, "process sample interval")
	fs.IntVar(&cfg.Concurrency, "concurrency", 4, "number of concurrent readers/writers")
	fs.IntVar(&cfg.Rows, "rows", 8192, "fixture row count")
	fs.IntVar(&cfg.PayloadBytes, "payload-bytes", 256, "payload bytes per row")
	fs.IntVar(&cfg.BatchSize, "batch-size", 1024, "scan and raw-file batch size")
	fs.StringVar(&workloads, "workloads", "all", "comma-separated workload names or all")
	fs.StringVar(&cfg.JSONLPath, "jsonl", "soak.jsonl", "machine-readable JSONL output path")
	fs.StringVar(&cfg.Dir, "dir", "", "empty fixture directory to reuse and keep; temp directory is removed if omitted")
	fs.Int64Var(&cfg.MaxGrowthBytes, "max-growth-bytes", 0, "optional late-minus-early mean RSS budget in bytes; 0 disables")
	fs.Int64Var(&cfg.DiagnosticRetainBytes, "diagnostic-retain-bytes", 0, "opt-in native-retention control budget in bytes; 0 disables")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if cfg.Duration <= 0 {
		return config{}, errors.New("-duration must be positive")
	}
	if cfg.SampleInterval <= 0 {
		return config{}, errors.New("-sample-interval must be positive")
	}
	if cfg.Duration < 4*cfg.SampleInterval {
		return config{}, fmt.Errorf("-duration %s must be at least 4x -sample-interval %s", cfg.Duration, cfg.SampleInterval)
	}
	if cfg.Warmup < 0 {
		return config{}, errors.New("-warmup must not be negative")
	}
	if planned := int64(cfg.Warmup/cfg.SampleInterval) + int64(cfg.Duration/cfg.SampleInterval) + 3; planned > maxRetainedSamples {
		return config{}, fmt.Errorf("run would retain about %d samples (limit %d); increase -sample-interval or shorten -warmup/-duration", planned, maxRetainedSamples)
	}
	if cfg.Concurrency < 1 || cfg.Concurrency > 1024 {
		return config{}, errors.New("-concurrency must be between 1 and 1024")
	}
	if cfg.Rows < 1 || cfg.Rows > 1_000_000 {
		return config{}, errors.New("-rows must be between 1 and 1000000")
	}
	if cfg.PayloadBytes < 0 || cfg.PayloadBytes > 1<<20 {
		return config{}, errors.New("-payload-bytes must be between 0 and 1048576")
	}
	if cfg.BatchSize < 1 || cfg.BatchSize > 1<<20 {
		return config{}, errors.New("-batch-size must be between 1 and 1048576")
	}
	if cfg.MaxGrowthBytes < 0 {
		return config{}, errors.New("-max-growth-bytes must not be negative")
	}
	if cfg.DiagnosticRetainBytes < 0 {
		return config{}, errors.New("-diagnostic-retain-bytes must not be negative")
	}
	if cfg.JSONLPath == "" {
		return config{}, errors.New("-jsonl must not be empty")
	}
	// Fixture creation briefly holds the main payload batches while writing them.
	// Refuse configurations that would make the harness itself the memory risk.
	estimated := int64(cfg.Rows) * (int64(cfg.PayloadBytes) + 64) * 3
	if estimated > 1<<30 {
		return config{}, fmt.Errorf("fixture estimate %d bytes exceeds the 1 GiB harness safety limit; reduce -rows or -payload-bytes", estimated)
	}
	names, err := parseWorkloads(workloads)
	if err != nil {
		return config{}, err
	}
	cfg.Workloads = names
	return cfg, nil
}

func parseWorkloads(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "all" {
		return append([]string(nil), workloadNames...), nil
	}
	known := make(map[string]bool, len(workloadNames))
	for _, name := range workloadNames {
		known[name] = true
	}
	seen := make(map[string]bool, len(workloadNames))
	var names []string
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if !known[name] {
			return nil, fmt.Errorf("unknown workload %q (known: %s)", name, strings.Join(workloadNames, ", "))
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, errors.New("no workloads selected")
	}
	return names, nil
}

func prepareExplicitDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create fixture directory %q: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("inspect fixture directory %q: %w", dir, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("fixture directory %q is not empty; use a new directory or omit -dir", dir)
	}
	return nil
}

func recordProvenance(library *loader.Library) (nativeRecord, buildRecord) {
	build := readBuild()
	path := library.Path()
	native := nativeRecord{
		Path: path,
		Provenance: "sha256 identifies the library bytes loaded on this host; it does not verify an upstream lance-c commit. " +
			"The pinned commit, when present, is build-time provenance declared by the artifact module.",
	}
	if digest, err := hashFile(path); err != nil {
		native.SHA256Error = err.Error()
	} else {
		native.SHA256 = digest
	}
	native.DeclaredArtifact = declaredArtifact(build.Dependencies)
	return native, build
}

// declaredArtifact returns the artifact module that was linked into this
// binary. That module version is declared build provenance; the loaded file
// hash is separate runtime evidence.
func declaredArtifact(dependencies []moduleInfo) *moduleInfo {
	for i := range dependencies {
		if strings.HasPrefix(dependencies[i].Path, "github.com/lance-format/lance-go/artifacts/") {
			return &dependencies[i]
		}
	}
	return nil
}

func readBuild() buildRecord {
	record := buildRecord{
		GoVersion:  runtime.Version(),
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		GOMAXPROCS: runtime.GOMAXPROCS(0),
		NumCPU:     runtime.NumCPU(),
		Settings:   make(map[string]string),
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return record
	}
	record.Available = true
	record.Main = moduleFromDebug(info.Main)
	for _, dependency := range info.Deps {
		if dependency == nil {
			continue
		}
		record.Dependencies = append(record.Dependencies, moduleFromDebug(*dependency))
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision", "vcs.time", "vcs.modified", "CGO_ENABLED", "-tags", "GOAMD64", "GOARM64":
			record.Settings[setting.Key] = setting.Value
		}
	}
	return record
}

func moduleFromDebug(module debug.Module) moduleInfo {
	info := moduleInfo{Path: module.Path, Version: module.Version, Sum: module.Sum}
	if module.Replace != nil {
		info.Replace = module.Replace.Path + "@" + module.Replace.Version
	}
	return info
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

var diagnosticTouchSink uint64

func runDiagnostic(ctx context.Context, st *runState, sampler *sampler, journal *journal, fx *fixture) (*diagnosticRecord, error) {
	dataset, err := lance.Open(ctx, fx.mainURI)
	if err != nil {
		return nil, fmt.Errorf("open diagnostic dataset: %w", err)
	}
	defer func() { _ = dataset.Close() }()

	takeRows := min(64, fx.rows)
	indices := make([]uint64, takeRows)
	for i := range indices {
		indices[i] = uint64(i)
	}
	diagnosticStart := time.Now()
	before, err := collectSample(sampler, journal, "diagnostic", "before", 0, st.startedAt, diagnosticStart)
	if err != nil {
		return nil, err
	}
	record := &diagnosticRecord{
		Source:      "dataset.Take(payload) batches retained without Release, every page read, then all released",
		BytesBudget: st.cfg.DiagnosticRetainBytes,
		Before:      &before,
	}
	var held []arrow.RecordBatch
	defer func() {
		for _, batch := range held {
			batch.Release()
		}
	}()
	for record.BytesRetained < st.cfg.DiagnosticRetainBytes {
		batches, err := dataset.Take(ctx, indices, "payload")
		if err != nil {
			return nil, fmt.Errorf("take diagnostic batch: %w", err)
		}
		if len(batches) == 0 {
			return nil, errors.New("diagnostic Take returned no batches")
		}
		takenBytes := int64(0)
		for _, batch := range batches {
			held = append(held, batch)
			takenBytes += batchBufferBytes(batch)
			record.RowsRetained += batch.NumRows()
			record.TouchedPages += touchBatchPages(batch)
		}
		if takenBytes <= 0 {
			return nil, errors.New("diagnostic Take batches reported no buffer bytes")
		}
		record.BytesRetained += takenBytes
		record.BatchesRetained += len(batches)
	}
	heldSample, err := collectSample(sampler, journal, "diagnostic", "held", 1, st.startedAt, diagnosticStart)
	if err != nil {
		return nil, err
	}
	record.Held = &heldSample
	for _, batch := range held {
		batch.Release()
	}
	held = nil
	after, err := collectSample(sampler, journal, "diagnostic", "after", 2, st.startedAt, diagnosticStart)
	if err != nil {
		return nil, err
	}
	record.After = &after
	return record, nil
}

// batchBufferBytes sums Arrow buffer lengths. It bounds the diagnostic hold and
// may over-count a buffer shared by more than one array.
func batchBufferBytes(batch arrow.RecordBatch) int64 {
	var total int64
	for _, column := range batch.Columns() {
		for _, buffer := range column.Data().Buffers() {
			if buffer != nil {
				total += int64(buffer.Len())
			}
		}
	}
	return total
}

// touchBatchPages reads one byte per page into a package-level sink so the
// residency control is not optimized away.
func touchBatchPages(batch arrow.RecordBatch) int64 {
	var pages int64
	pageSize := os.Getpagesize()
	for _, column := range batch.Columns() {
		for _, buffer := range column.Data().Buffers() {
			if buffer == nil {
				continue
			}
			data := buffer.Bytes()
			for i := 0; i < len(data); i += pageSize {
				diagnosticTouchSink += uint64(data[i])
				pages++
			}
		}
	}
	return pages
}
