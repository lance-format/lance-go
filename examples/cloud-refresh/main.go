package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	cfg, prefix, err := parseConfig(os.Args[1:])
	if err != nil {
		rep := newReport("", "", time.Now(), 0)
		rep.fail(newRedactor(), "config", "invalid_configuration", err)
		if writeErr := writeReport("-", rep); writeErr != nil {
			fmt.Fprintln(os.Stderr, "cloud-refresh: could not write configuration report")
		}
		os.Exit(2)
	}

	rep := execute(cfg, prefix)
	if err := writeReport(cfg.reportPath, rep); err != nil {
		fmt.Fprintln(os.Stderr, "cloud-refresh: could not write report")
		os.Exit(1)
	}
	if rep.Status != "passed" {
		os.Exit(1)
	}
}
