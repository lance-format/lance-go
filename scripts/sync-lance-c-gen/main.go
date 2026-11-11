package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/lance-format/lance-go/scripts/internal/artifactgen"
)

func main() {
	var opts artifactgen.Options
	flag.StringVar(&opts.LibraryPath, "library", "", "path to the already-built liblance_c shared library")
	flag.StringVar(&opts.HeaderPath, "header", "", "path to the matching lance.h")
	flag.StringVar(&opts.Target, "target", "", "target as GOOS/GOARCH (linux, darwin, or windows on amd64 or arm64)")
	flag.StringVar(&opts.OutDir, "out", "", "new output directory to publish (must not exist)")
	flag.StringVar(&opts.ModulePath, "module-path", "", "generated artifact module path (default: github.com/lance-format/lance-go/artifacts/<goos>-<goarch>)")
	flag.StringVar(&opts.SelectorPackage, "selector-package", "bundled", "package name for the generated selector (copied into internal/bundled)")
	flag.StringVar(&opts.UpstreamRepo, "upstream-repo", "", "lance-c repository recorded as provenance")
	flag.StringVar(&opts.UpstreamCommit, "upstream-commit", "", "lance-c commit recorded as provenance")
	flag.StringVar(&opts.HeaderSHA256, "header-sha256", "", "pinned SHA-256 of the header")
	flag.Parse()

	if flag.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "sync-lance-c-gen: unexpected arguments: %s\n", strings.Join(flag.Args(), " "))
		flag.Usage()
		os.Exit(2)
	}
	if opts.Target != "" && opts.ModulePath == "" {
		opts.ModulePath = "github.com/lance-format/lance-go/artifacts/" + strings.ReplaceAll(opts.Target, "/", "-")
	}

	result, err := artifactgen.Generate(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync-lance-c-gen: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("generated artifact module for %s\n", result.Metadata.Target)
	fmt.Printf("  module: %s\n", result.ModuleDir)
	fmt.Printf("  selector: %s\n", result.SelectorPath)
	fmt.Printf("  library: %s (%d bytes uncompressed, sha256 %s)\n",
		result.Metadata.LibraryName, result.Metadata.UncompressedSize, result.Metadata.UncompressedSHA256)
	fmt.Printf("  payload: %d bytes compressed, sha256 %s\n",
		result.Metadata.CompressedSize, result.Metadata.CompressedSHA256)
	fmt.Printf("  provenance claim: lance-c commit %s, header sha256 %s\n",
		result.Metadata.UpstreamCommit, result.Metadata.HeaderSHA256)
	fmt.Println("provenance is recorded by the packaging process; lance-c has no runtime version symbol")
}
