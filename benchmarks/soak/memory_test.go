package main

import (
	"strings"
	"testing"
)

func TestParseSmapsRollup(t *testing.T) {
	input := "Rss:                1024 kB\n" +
		"Pss:                 512 kB\n" +
		"Anonymous:           256 kB\n" +
		"Shared_Clean:        128 kB\n" +
		"Private_Dirty:        64 kB\n"
	stats, err := parseSmapsRollup(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseSmapsRollup: %v", err)
	}
	if stats.RSSBytes != 1024*1024 {
		t.Fatalf("RSSBytes = %d, want %d", stats.RSSBytes, 1024*1024)
	}
	if stats.PSSBytes == nil || *stats.PSSBytes != 512*1024 {
		t.Fatalf("PSSBytes = %v, want %d", stats.PSSBytes, 512*1024)
	}
	if stats.AnonymousBytes == nil || *stats.AnonymousBytes != 256*1024 {
		t.Fatalf("AnonymousBytes = %v, want %d", stats.AnonymousBytes, 256*1024)
	}
	if stats.PrivateDirtyBytes == nil || *stats.PrivateDirtyBytes != 64*1024 {
		t.Fatalf("PrivateDirtyBytes = %v, want %d", stats.PrivateDirtyBytes, 64*1024)
	}
	if stats.SwapBytes != nil {
		t.Fatalf("SwapBytes = %v, want nil", stats.SwapBytes)
	}
	if !containsString(stats.Unavailable, "Swap") {
		t.Fatalf("Unavailable = %v, want Swap", stats.Unavailable)
	}
	if !containsString(stats.Unavailable, "Pss_File") {
		t.Fatalf("Unavailable = %v, want Pss_File", stats.Unavailable)
	}
}

func TestParseSmapsRollupRequiresRSS(t *testing.T) {
	if _, err := parseSmapsRollup(strings.NewReader("Pss: 1 kB\n")); err == nil {
		t.Fatal("parseSmapsRollup accepted input without Rss")
	}
}

func TestParseSmapsRollupRejectsMalformedValue(t *testing.T) {
	if _, err := parseSmapsRollup(strings.NewReader("Rss: many kB\n")); err == nil {
		t.Fatal("parseSmapsRollup accepted a non-numeric Rss value")
	}
}

func TestParseThreads(t *testing.T) {
	threads, err := parseThreads(strings.NewReader("Name:\tsoak\nThreads:\t7\n"))
	if err != nil {
		t.Fatalf("parseThreads: %v", err)
	}
	if threads != 7 {
		t.Fatalf("threads = %d, want 7", threads)
	}
	if _, err := parseThreads(strings.NewReader("Name:\tsoak\n")); err == nil {
		t.Fatal("parseThreads accepted input without Threads")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
