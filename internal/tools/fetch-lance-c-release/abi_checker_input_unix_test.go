//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestABICheckerInputSelection(t *testing.T) {
	script := filepath.Join("..", "..", "..", "scripts", "abi-checker-input.sh")
	dir := t.TempDir()
	header := writeFile(t, dir, "lance.h", "/* header */\n")
	splitDir := filepath.Join(t.TempDir(), "include", "lance")
	if err := os.MkdirAll(splitDir, 0o755); err != nil {
		t.Fatalf("create split directory: %v", err)
	}
	writeFile(t, splitDir, "lance.h", "/* header */\n")

	cases := map[string]struct {
		record string
		want   string
	}{
		"split directory": {
			record: "abi_header=" + splitDir + "\nheader=" + filepath.Join(splitDir, "lance.h") + "\n",
			want:   splitDir,
		},
		"legacy file": {
			record: "abi_header=" + header + "\nheader=" + header + "\n",
			want:   header,
		},
		"historical record": {
			record: "header=" + header + "\n",
			want:   header,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			record := writeFile(t, t.TempDir(), "acquisition.txt", tc.record)
			output, err := exec.Command("bash", script, record).CombinedOutput()
			if err != nil {
				t.Fatalf("helper failed: %v\n%s", err, output)
			}
			if strings.TrimSpace(string(output)) != tc.want {
				t.Fatalf("helper printed %q, want %q", strings.TrimSpace(string(output)), tc.want)
			}
		})
	}

	missing := writeFile(t, t.TempDir(), "acquisition.txt", "abi_header="+filepath.Join(dir, "missing")+"\n")
	if output, err := exec.Command("bash", script, missing).CombinedOutput(); err == nil {
		t.Fatalf("helper accepted a missing abi_header: %s", output)
	}
}
