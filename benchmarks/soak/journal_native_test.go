//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lance-format/lance-go/internal/loader"
)

// Exercise the real sampling path: testing journal.write alone cannot catch
// a runner that computes a summary but never publishes its source samples.
// The file stays Linux-only because the soak harness measures process RSS and
// open descriptors from procfs (/proc/self/smaps_rollup, /proc/self/fd).
func TestSoakJournalContainsSampleTimeline(t *testing.T) {
	if _, err := loader.Load(); errors.Is(err, loader.ErrNotFound) {
		t.Skip("lance-c is not available; set LANCE_LIB_PATH")
	} else if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "samples.jsonl")
	cfg, err := parseConfig([]string{
		"-jsonl=" + path, "-workloads=scan", "-rows=32", "-payload-bytes=16",
		"-batch-size=8", "-concurrency=1", "-warmup=200ms",
		"-duration=1s", "-sample-interval=100ms", "-diagnostic-retain-bytes=4096",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := execute(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	counts := make(map[string]int)
	labels := make(map[string]bool)
	var finalSamples int
	var previousElapsed int64
	lastPhase := ""
	for {
		var record struct {
			Phase        string `json:"phase"`
			Label        string `json:"label"`
			Seq          int    `json:"seq"`
			RunElapsedMS int64  `json:"run_elapsed_ms"`
			RSSBytes     int64  `json:"rss_bytes"`
			Status       string `json:"status"`
			Measurement  *struct {
				Samples int `json:"samples"`
			} `json:"measurement"`
		}
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		lastPhase = record.Phase
		switch record.Phase {
		case "warmup", "measurement", "post_gc", "diagnostic":
			if record.Seq != counts[record.Phase] || record.RSSBytes <= 0 || record.RunElapsedMS < previousElapsed {
				t.Fatalf("invalid timeline sample: %+v", record)
			}
			previousElapsed = record.RunElapsedMS
			labels[record.Phase+"/"+record.Label] = true
		case "final":
			if record.Status != "ok" || record.Measurement == nil {
				t.Fatalf("invalid final record: %+v", record)
			}
			finalSamples = record.Measurement.Samples
		case "config":
		default:
			t.Fatalf("unexpected journal phase %q", record.Phase)
		}
		counts[record.Phase]++
	}
	if counts["config"] != 1 || counts["final"] != 1 || lastPhase != "final" {
		t.Fatalf("incomplete journal: counts=%v last=%q", counts, lastPhase)
	}
	if counts["warmup"] < 1 || counts["measurement"] < 4 || counts["measurement"] != finalSamples || counts["post_gc"] != 1 || counts["diagnostic"] != 3 {
		t.Fatalf("missing source samples: counts=%v summary samples=%d", counts, finalSamples)
	}
	for _, label := range []string{"warmup/baseline", "measurement/initial", "measurement/final", "diagnostic/before", "diagnostic/held", "diagnostic/after"} {
		if !labels[label] {
			t.Errorf("missing sample %q", label)
		}
	}
}
