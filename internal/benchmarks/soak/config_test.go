package main

import (
	"strings"
	"testing"
)

func TestParseWorkloads(t *testing.T) {
	all, err := parseWorkloads("all")
	if err != nil {
		t.Fatalf("parseWorkloads(all): %v", err)
	}
	if len(all) != len(workloadNames) {
		t.Fatalf("parseWorkloads(all) returned %d names, want %d", len(all), len(workloadNames))
	}

	selected, err := parseWorkloads("scan, blob,scan")
	if err != nil {
		t.Fatalf("parseWorkloads(selected): %v", err)
	}
	if len(selected) != 2 || selected[0] != "scan" || selected[1] != "blob" {
		t.Fatalf("parseWorkloads(selected) = %v, want [scan blob]", selected)
	}

	if _, err := parseWorkloads("scan,does-not-exist"); err == nil {
		t.Fatal("parseWorkloads accepted an unknown workload")
	}
	if _, err := parseWorkloads(", ,"); err == nil {
		t.Fatal("parseWorkloads accepted an empty selector")
	}
}

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig([]string{
		"-duration=30s",
		"-sample-interval=1s",
		"-warmup=0s",
		"-rows=16",
		"-payload-bytes=8",
		"-workloads=scan,blob",
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Duration != 30_000_000_000 || cfg.SampleInterval != 1_000_000_000 {
		t.Fatalf("durations = %s/%s", cfg.Duration, cfg.SampleInterval)
	}
	if len(cfg.Workloads) != 2 || cfg.Workloads[0] != "scan" || cfg.Workloads[1] != "blob" {
		t.Fatalf("Workloads = %v", cfg.Workloads)
	}
}

func TestParseConfigRejectsInvalidRanges(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "too short for interval", args: []string{"-duration=3s", "-sample-interval=1s"}, want: "4x"},
		{name: "zero concurrency", args: []string{"-duration=30s", "-sample-interval=1s", "-concurrency=0"}, want: "concurrency"},
		{name: "negative warmup", args: []string{"-duration=30s", "-sample-interval=1s", "-warmup=-1s"}, want: "warmup"},
		{name: "bad payload", args: []string{"-duration=30s", "-sample-interval=1s", "-payload-bytes=-1"}, want: "payload-bytes"},
		{name: "bad row count", args: []string{"-duration=30s", "-sample-interval=1s", "-rows=0"}, want: "rows"},
		{name: "too many retained samples", args: []string{"-duration=100000s", "-sample-interval=1ms"}, want: "samples"},
		{name: "negative budget", args: []string{"-duration=30s", "-sample-interval=1s", "-max-growth-bytes=-1"}, want: "max-growth-bytes"},
		{name: "negative diagnostic", args: []string{"-duration=30s", "-sample-interval=1s", "-diagnostic-retain-bytes=-1"}, want: "diagnostic-retain-bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseConfig(test.args)
			if err == nil {
				t.Fatal("parseConfig accepted invalid arguments")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseConfig error = %q, want substring %q", err, test.want)
			}
		})
	}
}
